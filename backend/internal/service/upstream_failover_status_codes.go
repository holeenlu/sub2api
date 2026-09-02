package service

import (
	"context"
	"errors"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"golang.org/x/sync/singleflight"
)

// statusCodeRange 是一段闭区间的 HTTP 状态码。单个状态码用 Start == End 表示。
type statusCodeRange struct{ Start, End int }

// upstreamFailoverAlwaysSkipStatusCodes 是无论怎么配置都不换账号重试的状态码。
// 它们表达的是「这次请求本身有问题」，换一个账号重发只会原样再失败一次：
//   - 400/404/422：请求体或模型名不合法；间歇性 400 另有 shouldFailoverOn400 白名单。
//   - 408/499：客户端侧超时或断开，账号没有过错。
//   - 413：请求体过大，各平台另有专门的降级处理。
var upstreamFailoverAlwaysSkipStatusCodes = map[int]struct{}{
	400: {}, 404: {}, 408: {}, 413: {}, 422: {}, 499: {},
}

type cachedUpstreamFailoverStatusCodes struct {
	// ranges 为 nil 表示未配置，调用方回落到各自平台的内置默认集。
	ranges []statusCodeRange
	// resolved 标记这份结果确实反映了配置（含「settings 里没有这一项」），而不是
	// 读取失败后的兜底。与 accountSchedulingThresholds 同样的区分理由。
	resolved  bool
	expiresAt int64 // unix nano
}

var upstreamFailoverStatusCodesCache atomic.Value // *cachedUpstreamFailoverStatusCodes

// lastResolvedUpstreamFailoverStatusCodes 保留最近一次成功读到的策略，供 DB 故障时兜底。
var lastResolvedUpstreamFailoverStatusCodes atomic.Value // *cachedUpstreamFailoverStatusCodes
var upstreamFailoverStatusCodesSF singleflight.Group

const (
	upstreamFailoverStatusCodesCacheTTL  = 60 * time.Second
	upstreamFailoverStatusCodesErrorTTL  = 5 * time.Second
	upstreamFailoverStatusCodesDBTimeout = 5 * time.Second
)

// ResetUpstreamFailoverStatusCodesCacheForTest 清空进程内缓存，仅供测试使用。
func ResetUpstreamFailoverStatusCodesCacheForTest() {
	upstreamFailoverStatusCodesCache = atomic.Value{}
	lastResolvedUpstreamFailoverStatusCodes = atomic.Value{}
	upstreamFailoverStatusCodesSF = singleflight.Group{}
}

// storeUpstreamFailoverStatusCodes 在设置保存后立即刷新缓存，避免最长 60s 的旧策略窗口。
func storeUpstreamFailoverStatusCodes(raw string) {
	upstreamFailoverStatusCodesSF.Forget(SettingKeyUpstreamFailoverStatusCodes)
	entry := &cachedUpstreamFailoverStatusCodes{
		ranges:    ParseStatusCodeRanges(raw),
		resolved:  true,
		expiresAt: time.Now().Add(upstreamFailoverStatusCodesCacheTTL).UnixNano(),
	}
	upstreamFailoverStatusCodesCache.Store(entry)
	lastResolvedUpstreamFailoverStatusCodes.Store(entry)
}

// ParseStatusCodeRanges 解析 "401,403,429,500-599" 形式的配置。
// 非法片段被跳过而不是让整条配置失效——一个手滑的逗号不该让 failover 全线退回默认。
// 返回 nil 表示没有任何有效片段。
func ParseStatusCodeRanges(raw string) []statusCodeRange {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	ranges := make([]statusCodeRange, 0, 4)
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		start, end, ok := parseOneStatusCodeRange(part)
		if !ok {
			slog.Warn("upstream_failover_status_codes.invalid_segment", "segment", part)
			continue
		}
		ranges = append(ranges, statusCodeRange{Start: start, End: end})
	}
	if len(ranges) == 0 {
		return nil
	}
	return mergeStatusCodeRanges(ranges)
}

func parseOneStatusCodeRange(part string) (int, int, bool) {
	validCode := func(v int) bool { return v >= 100 && v <= 599 }
	if idx := strings.IndexByte(part, '-'); idx > 0 {
		start, err1 := strconv.Atoi(strings.TrimSpace(part[:idx]))
		end, err2 := strconv.Atoi(strings.TrimSpace(part[idx+1:]))
		if err1 != nil || err2 != nil || !validCode(start) || !validCode(end) || start > end {
			return 0, 0, false
		}
		return start, end, true
	}
	code, err := strconv.Atoi(part)
	if err != nil || !validCode(code) {
		return 0, 0, false
	}
	return code, code, true
}

// mergeStatusCodeRanges 排序并合并相邻/重叠区间，让匹配是一次线性扫描而不依赖配置书写顺序。
func mergeStatusCodeRanges(ranges []statusCodeRange) []statusCodeRange {
	sort.Slice(ranges, func(i, j int) bool {
		if ranges[i].Start != ranges[j].Start {
			return ranges[i].Start < ranges[j].Start
		}
		return ranges[i].End < ranges[j].End
	})
	merged := make([]statusCodeRange, 0, len(ranges))
	for _, r := range ranges {
		last := len(merged) - 1
		if last >= 0 && r.Start <= merged[last].End+1 {
			if r.End > merged[last].End {
				merged[last].End = r.End
			}
			continue
		}
		merged = append(merged, r)
	}
	return merged
}

func statusCodeRangesContain(ranges []statusCodeRange, code int) bool {
	for _, r := range ranges {
		if code >= r.Start && code <= r.End {
			return true
		}
	}
	return false
}

// getUpstreamFailoverStatusCodes 读取配置的重试状态码集合。
// resolved=false 表示这一刻读不到配置，调用方应当沿用内置默认集而不是当成「运维清空了配置」。
func (s *SettingService) getUpstreamFailoverStatusCodes(ctx context.Context) ([]statusCodeRange, bool) {
	if s == nil || s.settingRepo == nil {
		return nil, false
	}
	if cached, ok := upstreamFailoverStatusCodesCache.Load().(*cachedUpstreamFailoverStatusCodes); ok {
		if time.Now().UnixNano() < cached.expiresAt {
			return cached.ranges, cached.resolved
		}
	}
	result, err, _ := upstreamFailoverStatusCodesSF.Do(SettingKeyUpstreamFailoverStatusCodes, func() (any, error) {
		if cached, ok := upstreamFailoverStatusCodesCache.Load().(*cachedUpstreamFailoverStatusCodes); ok {
			if time.Now().UnixNano() < cached.expiresAt {
				return cached, nil
			}
		}
		// 独立 context：客户端断连不该把空值长期缓存进来。
		dbCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), upstreamFailoverStatusCodesDBTimeout)
		defer cancel()
		raw, err := s.settingRepo.GetValue(dbCtx, SettingKeyUpstreamFailoverStatusCodes)
		if err != nil && !errors.Is(err, ErrSettingNotFound) {
			slog.Warn("upstream_failover_status_codes.read_failed", "error", err)
			// 读不到就沿用最近一次成功读到的策略：这是一份变化极少的系统配置，
			// 让一次 DB 抖动把运维配好的重试范围换回平台默认，比暂时用旧值更糟。
			entry := &cachedUpstreamFailoverStatusCodes{
				resolved:  false,
				expiresAt: time.Now().Add(upstreamFailoverStatusCodesErrorTTL).UnixNano(),
			}
			if last, ok := lastResolvedUpstreamFailoverStatusCodes.Load().(*cachedUpstreamFailoverStatusCodes); ok && last != nil {
				entry.ranges = last.ranges
				entry.resolved = last.resolved
			}
			upstreamFailoverStatusCodesCache.Store(entry)
			return entry, nil
		}
		entry := &cachedUpstreamFailoverStatusCodes{
			ranges:    ParseStatusCodeRanges(raw),
			resolved:  true,
			expiresAt: time.Now().Add(upstreamFailoverStatusCodesCacheTTL).UnixNano(),
		}
		upstreamFailoverStatusCodesCache.Store(entry)
		lastResolvedUpstreamFailoverStatusCodes.Store(entry)
		return entry, nil
	})
	if err != nil {
		return nil, false
	}
	entry, ok := result.(*cachedUpstreamFailoverStatusCodes)
	if !ok {
		return nil, false
	}
	return entry.ranges, entry.resolved
}

// shouldFailoverStatusCode 是三个平台共用的判定：配置了就按配置，没配置或读不到就用
// 传入的平台默认判定。护栏在最外层，配置无法把「请求本身有问题」的状态码变成可重试。
//
// 不收 ctx：这条路径几乎总是命中 60s 进程内缓存，回源时读的也是一次带独立超时的点查，
// 且按本仓约定要用 context.WithoutCancel 断开取消链（客户端断连不该把空值缓存进来）。
// 为它给 shouldFailoverUpstreamError 及其 20 余个调用点加参数，收益抵不上扩散面。
func shouldFailoverStatusCode(settingService *SettingService, statusCode int, platformDefault func(int) bool) bool {
	ctx := context.Background()
	if _, skip := upstreamFailoverAlwaysSkipStatusCodes[statusCode]; skip {
		return false
	}
	if settingService != nil {
		if ranges, resolved := settingService.getUpstreamFailoverStatusCodes(ctx); resolved && len(ranges) > 0 {
			return statusCodeRangesContain(ranges, statusCode)
		}
	}
	return platformDefault(statusCode)
}

// FormatStatusCodeRanges 把解析后的区间序列化回配置形式（"401,403,429,500-599"）。
// 空结果返回空串，表示「未配置，用平台默认集」。
func FormatStatusCodeRanges(ranges []statusCodeRange) string {
	if len(ranges) == 0 {
		return ""
	}
	parts := make([]string, 0, len(ranges))
	for _, r := range ranges {
		if r.Start == r.End {
			parts = append(parts, strconv.Itoa(r.Start))
			continue
		}
		parts = append(parts, strconv.Itoa(r.Start)+"-"+strconv.Itoa(r.End))
	}
	return strings.Join(parts, ",")
}
