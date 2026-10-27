package service

import (
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
)

// 上游 OpenAI/ChatGPT 的 503 "Our servers are currently overloaded" 在分类上是
// 请求级容量降载（RequestScopedTransient）：单次请求先在同账号退避一次，再换号，
// 且不冷却账号。生产观测却显示该错误在短时间内高度集中于少数账号——粘性会话把
// 客户端的重试一再带回同一个被降载的账号。这里用进程内、按账号的滑动窗口做一道
// 兜底：窗口内达到阈值即视为账号级降载，对该账号施加短暂的调度冷却。
//
// 冷却只存在于进程内存：不写数据库、不改账号状态、不影响其他平台；到期自动恢复。
// 数值为常量（不提供配置项），与 openAIModelTransient* 的量级保持一致。
const (
	// openAICapacityShedWindow 滑动窗口长度：只统计最近这段时间内的降载次数。
	openAICapacityShedWindow = 60 * time.Second
	// openAICapacityShedThreshold 窗口内达到该次数即判定为账号级降载。
	openAICapacityShedThreshold = 3
	// openAICapacityShedCooldown 达到阈值后该账号在原生 OpenAI 调度中被过滤的时长。
	openAICapacityShedCooldown = 90 * time.Second
	// openAICapacityShedMaxEntries 进程内条目上限，超过后先惰性清理过期条目，再淘汰最久未触达者。
	openAICapacityShedMaxEntries = 4096
)

type openAICapacityShedEntry struct {
	// hits 为窗口内的降载时间戳，升序；长度不超过阈值（更早的在 prune 时丢弃）。
	hits          []time.Time
	cooldownUntil time.Time
	lastTouched   time.Time
}

func (e *openAICapacityShedEntry) prune(now time.Time) {
	cutoff := now.Add(-openAICapacityShedWindow)
	kept := e.hits[:0]
	for _, hit := range e.hits {
		// 时钟回拨时丢弃“未来”的命中，避免窗口永远不滑动。
		if hit.After(cutoff) && !hit.After(now) {
			kept = append(kept, hit)
		}
	}
	for i := len(kept); i < len(e.hits); i++ {
		e.hits[i] = time.Time{}
	}
	e.hits = kept
}

func (e *openAICapacityShedEntry) cooling(now time.Time) bool {
	return !e.cooldownUntil.IsZero() && now.Before(e.cooldownUntil)
}

// expired 表示条目既无窗口内命中也不在冷却中，可以被惰性回收。
func (e *openAICapacityShedEntry) expired(now time.Time) bool {
	return len(e.hits) == 0 && !e.cooling(now)
}

type openAICapacityShedDecision struct {
	// Hits 为本次记录后窗口内的降载次数。
	Hits int
	// Tripped 表示本次记录使账号从未冷却进入冷却（用于只记一次日志）。
	Tripped bool
	// Cooling 表示记录后账号处于冷却中（包含 Tripped 与此前已在冷却中的情况）。
	Cooling bool
	// CooldownUntil 为冷却截止时间；未冷却时为零值。
	CooldownUntil time.Time
}

type openAICapacityShedState struct {
	mu         sync.Mutex
	entries    map[int64]*openAICapacityShedEntry
	maxEntries int
}

func newOpenAICapacityShedState(maxEntries int) *openAICapacityShedState {
	if maxEntries <= 0 {
		maxEntries = openAICapacityShedMaxEntries
	}
	return &openAICapacityShedState{
		entries:    make(map[int64]*openAICapacityShedEntry),
		maxEntries: maxEntries,
	}
}

// record 记录一次容量降载并返回记录后的状态。冷却中再次命中不会延长冷却：
// 冷却期间该账号已不在调度候选中，此时的命中只来自冷却前已发出的请求。
func (s *openAICapacityShedState) record(accountID int64, now time.Time) openAICapacityShedDecision {
	if s == nil || accountID <= 0 {
		return openAICapacityShedDecision{}
	}
	if now.IsZero() {
		now = time.Now()
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.entries == nil {
		s.entries = make(map[int64]*openAICapacityShedEntry)
	}
	if s.maxEntries <= 0 {
		s.maxEntries = openAICapacityShedMaxEntries
	}

	entry, exists := s.entries[accountID]
	if !exists {
		s.makeRoomLocked(now)
		entry = &openAICapacityShedEntry{}
		s.entries[accountID] = entry
	}
	entry.prune(now)
	entry.hits = append(entry.hits, now)
	if len(entry.hits) > openAICapacityShedThreshold {
		entry.hits = append(entry.hits[:0], entry.hits[len(entry.hits)-openAICapacityShedThreshold:]...)
	}
	entry.lastTouched = now

	wasCooling := entry.cooling(now)
	if !wasCooling && len(entry.hits) >= openAICapacityShedThreshold {
		entry.cooldownUntil = now.Add(openAICapacityShedCooldown)
	}
	decision := openAICapacityShedDecision{
		Hits:    len(entry.hits),
		Cooling: entry.cooling(now),
	}
	decision.Tripped = decision.Cooling && !wasCooling
	if decision.Cooling {
		decision.CooldownUntil = entry.cooldownUntil
	}
	return decision
}

// isCooling 报告账号是否处于容量降载冷却中；到期的条目顺手回收。
func (s *openAICapacityShedState) isCooling(accountID int64, now time.Time) bool {
	if s == nil || accountID <= 0 {
		return false
	}
	if now.IsZero() {
		now = time.Now()
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	entry, exists := s.entries[accountID]
	if !exists {
		return false
	}
	if entry.cooling(now) {
		return true
	}
	entry.prune(now)
	if entry.expired(now) {
		delete(s.entries, accountID)
	}
	return false
}

// resetHits 在账号成功响应后清空窗口计数。已经进入的冷却不被提前解除：
// 冷却期间完成的成功流来自冷却前就已发出的请求，不足以证明降载已结束。
func (s *openAICapacityShedState) resetHits(accountID int64, now time.Time) {
	if s == nil || accountID <= 0 {
		return
	}
	if now.IsZero() {
		now = time.Now()
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	entry, exists := s.entries[accountID]
	if !exists {
		return
	}
	entry.hits = entry.hits[:0]
	if entry.expired(now) {
		delete(s.entries, accountID)
		return
	}
	entry.lastTouched = now
}

func (s *openAICapacityShedState) size() int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.entries)
}

// makeRoomLocked 在插入新条目前保证容量：先惰性清理已过期条目，仍满则淘汰最久未触达者。
func (s *openAICapacityShedState) makeRoomLocked(now time.Time) {
	if len(s.entries) < s.maxEntries {
		return
	}
	for id, entry := range s.entries {
		entry.prune(now)
		if entry.expired(now) {
			delete(s.entries, id)
		}
	}
	if len(s.entries) < s.maxEntries {
		return
	}
	var oldestID int64
	var oldestTime time.Time
	found := false
	for id, entry := range s.entries {
		if !found || entry.lastTouched.Before(oldestTime) {
			oldestID = id
			oldestTime = entry.lastTouched
			found = true
		}
	}
	if found {
		delete(s.entries, oldestID)
	}
}

func (s *OpenAIGatewayService) getOpenAICapacityShedState() *openAICapacityShedState {
	if s == nil {
		return nil
	}
	s.openaiCapacityShedOnce.Do(func() {
		if s.openaiCapacityShed == nil {
			s.openaiCapacityShed = newOpenAICapacityShedState(openAICapacityShedMaxEntries)
		}
	})
	return s.openaiCapacityShed
}

// isOpenAICapacityShedAccount 限定只对原生 OpenAI 平台账号计数与冷却。
func isOpenAICapacityShedAccount(account *Account) bool {
	return account != nil && account.ID > 0 && account.Platform == PlatformOpenAI
}

// recordOpenAICapacityShed 记录一次已被分类函数识别的容量降载（HTTP 响应与
// WS/SSE 流内 error 帧共用），并在首次达到阈值时记一条冷却日志。
func (s *OpenAIGatewayService) recordOpenAICapacityShed(account *Account, now time.Time) openAICapacityShedDecision {
	if s == nil || !isOpenAICapacityShedAccount(account) {
		return openAICapacityShedDecision{}
	}
	state := s.getOpenAICapacityShedState()
	if state == nil {
		return openAICapacityShedDecision{}
	}
	decision := state.record(account.ID, now)
	if decision.Tripped {
		logger.LegacyPrintf("service.openai_gateway",
			"openai_capacity_shed_cooldown: account_id=%d account_name=%q hits=%d window_s=%d cooldown_s=%d until=%s block_scope=account_runtime",
			account.ID, account.Name, decision.Hits,
			int(openAICapacityShedWindow/time.Second), int(openAICapacityShedCooldown/time.Second),
			decision.CooldownUntil.UTC().Format(time.RFC3339))
	}
	return decision
}

func (s *OpenAIGatewayService) isOpenAICapacityShedCooling(account *Account) bool {
	if s == nil || !isOpenAICapacityShedAccount(account) {
		return false
	}
	return s.getOpenAICapacityShedState().isCooling(account.ID, time.Now())
}

func (s *OpenAIGatewayService) clearOpenAICapacityShedHits(accountID int64) {
	if s == nil || accountID <= 0 {
		return
	}
	s.getOpenAICapacityShedState().resetHits(accountID, time.Now())
}
