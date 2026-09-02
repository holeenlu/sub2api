//go:build unit

package service

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseStatusCodeRanges(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string // 经 FormatStatusCodeRanges 归一化后的形式
	}{
		{"empty", "", ""},
		{"single", "429", "429"},
		{"list", "401,403,429", "401,403,429"},
		{"range", "500-599", "500-599"},
		{"mixed", "401,500-503,429", "401,429,500-503"},
		// 相邻与重叠区间合并，让匹配不依赖书写顺序
		{"merges adjacent", "500-502,503-504", "500-504"},
		{"merges overlapping", "500-550,520-599", "500-599"},
		{"merges single into range", "500-599,502", "500-599"},
		// 非法片段被跳过，不让一个手滑的逗号使整条配置失效
		{"skips invalid", "401,abc,429", "401,429"},
		{"skips out of range", "401,99,600,429", "401,429"},
		{"skips reversed", "500-400,429", "429"},
		{"all invalid yields empty", "abc,,999", ""},
		{"tolerates spaces", " 401 , 500 - 503 ", "401,500-503"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, FormatStatusCodeRanges(ParseStatusCodeRanges(tt.in)))
		})
	}
}

// 护栏：无论配置怎么写，「请求本身有问题」的状态码都不换账号重试——换一个账号
// 重发只会原样再失败一次。
func TestShouldFailoverStatusCodeGuardrails(t *testing.T) {
	t.Parallel()

	alwaysRetry := func(int) bool { return true }
	for _, code := range []int{
		http.StatusBadRequest, http.StatusNotFound, http.StatusRequestTimeout,
		http.StatusRequestEntityTooLarge, http.StatusUnprocessableEntity, 499,
	} {
		require.False(t, shouldFailoverStatusCode(nil, code, alwaysRetry),
			"状态码 %d 属于护栏集，平台默认判定说重试也不能重试", code)
	}
}

// settingService 为 nil（未接线）或配置为空时回落到平台默认判定。
func TestShouldFailoverStatusCodeFallsBackToPlatformDefault(t *testing.T) {
	t.Parallel()

	anthropicDefault := func(statusCode int) bool {
		switch statusCode {
		case 401, 403, 429, 529:
			return true
		default:
			return statusCode >= 500
		}
	}

	require.True(t, shouldFailoverStatusCode(nil, http.StatusUnauthorized, anthropicDefault))
	require.True(t, shouldFailoverStatusCode(nil, 529, anthropicDefault))
	require.True(t, shouldFailoverStatusCode(nil, http.StatusBadGateway, anthropicDefault))
	require.False(t, shouldFailoverStatusCode(nil, http.StatusPaymentRequired, anthropicDefault))
}

func TestStatusCodeRangesContain(t *testing.T) {
	t.Parallel()

	ranges := ParseStatusCodeRanges("401,429,500-503")
	require.True(t, statusCodeRangesContain(ranges, 401))
	require.True(t, statusCodeRangesContain(ranges, 429))
	require.True(t, statusCodeRangesContain(ranges, 500))
	require.True(t, statusCodeRangesContain(ranges, 503))
	require.False(t, statusCodeRangesContain(ranges, 402))
	require.False(t, statusCodeRangesContain(ranges, 504))
	require.False(t, statusCodeRangesContain(nil, 429))
}

// DB 读不到时沿用最近一次成功读到的策略：这是一份变化极少的系统配置，让一次抖动
// 把运维配好的范围换回平台默认，比暂时用旧值更糟。
func TestUpstreamFailoverStatusCodesKeepsLastResolvedOnReadFailure(t *testing.T) {
	ResetUpstreamFailoverStatusCodesCacheForTest()
	defer ResetUpstreamFailoverStatusCodesCacheForTest()

	storeUpstreamFailoverStatusCodes("401,429")
	ranges, resolved := (&SettingService{}).getUpstreamFailoverStatusCodes(nil)
	// settingRepo 为 nil 时直接返回未解析，不 panic
	require.Nil(t, ranges)
	require.False(t, resolved)

	// 保存后缓存立即生效，不必等 60s TTL
	cached, ok := upstreamFailoverStatusCodesCache.Load().(*cachedUpstreamFailoverStatusCodes)
	require.True(t, ok)
	require.True(t, cached.resolved)
	require.Equal(t, "401,429", FormatStatusCodeRanges(cached.ranges))
}

// nil 接收者与 nil settingRepo 都不能 panic：测试与错误初始化路径都会走到。
func TestGetUpstreamFailoverStatusCodesNilSafety(t *testing.T) {
	ResetUpstreamFailoverStatusCodesCacheForTest()
	defer ResetUpstreamFailoverStatusCodesCacheForTest()

	require.NotPanics(t, func() {
		var svc *SettingService
		ranges, resolved := svc.getUpstreamFailoverStatusCodes(nil)
		require.Nil(t, ranges)
		require.False(t, resolved)
	})
}
