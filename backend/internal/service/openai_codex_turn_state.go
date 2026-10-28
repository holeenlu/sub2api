package service

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// openAICodexTurnStateHeader 是 Codex 的回合状态头。上游在响应头中铸造该
// 不透明 blob，客户端在同一回合的后续请求中原样回带（codex-rs 侧从
// /responses SSE、/responses/compact JSON 与 WS 握手三种响应中捕获，见
// codex-api/src/sse/responses.rs 与 endpoint/compact.rs）。
const openAICodexTurnStateHeader = "x-codex-turn-state"

// turn-state blob 是上游在"出站身份"（含 #5553 指纹收敛改写后的
// installation/session/thread 标识）下铸造的，同账号回放自洽；跨账号回放
// （failover 换号后客户端仍回带旧账号的 blob）是代理链独有、真实 Codex
// 不会主动构造的矛盾信号。溯源按租户、会话和具体 blob 摘要记录凭证，
// 经现有 Redis 状态层共享，出站守卫据此剥离已知异账号的回带值。

// openAICodexTurnStateSeed 返回溯源表键：API Key + 客户端原始会话标识。
// 客户端会话标识取自请求头（与指纹收敛的 thread 派生同源，见
// extractClientSessionID），确保同一下游会话的记录/守卫两侧使用同一键。
// 无会话头时仍可按已认证 API Key 加具体 blob 隔离；不把路由键当会话证明。
func openAICodexTurnStateSeed(c *gin.Context) string {
	if c == nil || c.Request == nil {
		return ""
	}
	sessionID := extractClientSessionID(c.Request.Header)
	if sessionID == "" && getAPIKeyIDFromContext(c) <= 0 {
		return ""
	}
	return strconv.FormatInt(getAPIKeyIDFromContext(c), 10) + "\x00" + sessionID
}

// relayOpenAICodexTurnState 将上游响应中的 turn-state 显式写入下游响应头，
// 并记录铸造账号。必须在响应头提交点调用（WriteHeader 之前、且确认本次
// 上游响应就是将要写回客户端的响应之后）。上游无该头时主动清除 writer 上
// 可能残留的上一 failover attempt 的值——否则换号后旧账号的 blob 会粘到
// 新账号的响应上，这正是本文件要防止的跨账号矛盾。
func (s *OpenAIGatewayService) relayOpenAICodexTurnState(c *gin.Context, account *Account, upstream http.Header) {
	if c == nil || c.Writer == nil {
		return
	}
	canonical := http.CanonicalHeaderKey(openAICodexTurnStateHeader)
	state := extractOpenAICodexTurnState(upstream)
	if state == "" {
		c.Writer.Header().Del(canonical)
		return
	}
	c.Writer.Header().Set(canonical, state)
	s.noteOpenAICodexTurnStateProvenance(c, account, state)
}

// stageOpenAICodexTurnState 将上游 turn-state 暂存到延迟提交的响应头集合
// （首输出守卫路径先缓存头、见到首个输出事件才提交）。此处**不**记录铸造
// 账号：该 attempt 仍可能在首输出超时后 failover，暂存头会被整体丢弃，
// 客户端从未收到该 blob。溯源必须在真正提交时记录，见
// noteStagedOpenAICodexTurnStateCommitted。
func stageOpenAICodexTurnState(dst *http.Header, upstream http.Header) {
	if dst == nil {
		return
	}
	canonical := http.CanonicalHeaderKey(openAICodexTurnStateHeader)
	state := extractOpenAICodexTurnState(upstream)
	if state == "" {
		if *dst != nil {
			dst.Del(canonical)
		}
		return
	}
	if *dst == nil {
		*dst = http.Header{}
	}
	dst.Set(canonical, state)
}

// noteStagedOpenAICodexTurnStateCommitted 在暂存响应头真正写入下游时记录
// 铸造账号——只有此刻客户端才确定收到了该 blob，溯源表才与客户端持有的
// 值一致（否则被 failover 丢弃的 attempt 会污染溯源，导致后续误剥离）。
func (s *OpenAIGatewayService) noteStagedOpenAICodexTurnStateCommitted(c *gin.Context, account *Account, staged http.Header) {
	if staged == nil || strings.TrimSpace(staged.Get(openAICodexTurnStateHeader)) == "" {
		return
	}
	s.noteOpenAICodexTurnStateProvenance(c, account, extractOpenAICodexTurnState(staged))
}

func extractOpenAICodexTurnState(upstream http.Header) string {
	if upstream == nil {
		return ""
	}
	return strings.TrimSpace(upstream.Get(openAICodexTurnStateHeader))
}

// A blob has its own immutable origin. A session's most recent account cannot
// establish ownership of an older blob arriving concurrently or after failover.
// Reuse the bounded WS state store and its Redis backing; no new cache interface.
func openAICodexTurnStateKey(c *gin.Context, state string) string {
	seed := openAICodexTurnStateSeed(c)
	if seed == "" || state == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(seed + "|state:" + state))
	return fmt.Sprintf("codex-turn-state:%x", sum[:])
}

func openAICodexTurnStateOwner(account *Account) int64 {
	if account == nil {
		return 0
	}
	if namespace := codexAccountIdentityNamespace(account); namespace != "" {
		sum := sha256.Sum256([]byte(namespace))
		return int64(binary.BigEndian.Uint64(sum[:8]) & ((1 << 63) - 1))
	}
	return account.ID
}

func (s *OpenAIGatewayService) noteOpenAICodexTurnStateProvenance(c *gin.Context, account *Account, state string) {
	if s == nil || account == nil || c == nil || c.Request == nil {
		return
	}
	key, owner := openAICodexTurnStateKey(c, state), openAICodexTurnStateOwner(codexAccountIdentitySource(c, account))
	if key == "" || owner <= 0 {
		return
	}
	ctx := context.WithoutCancel(c.Request.Context())
	if err := s.getOpenAIWSStateStore().BindResponseAccount(ctx, getOpenAIGroupIDFromContext(c), key, owner, s.openAIWSSessionStickyTTL()); err != nil {
		logger.FromContext(ctx).Warn("gateway.turn_state_provenance_write_failed", zap.Error(err))
	}
}

// Unknown/expired provenance preserves the existing compatibility contract. A
// cache failure is observable, never silently interpreted as verified ownership.
func (s *OpenAIGatewayService) guardOpenAICodexTurnStateEcho(c *gin.Context, account *Account, h http.Header) {
	if s == nil || account == nil || h == nil || c == nil || c.Request == nil {
		return
	}
	key := openAICodexTurnStateKey(c, strings.TrimSpace(h.Get(openAICodexTurnStateHeader)))
	if key == "" {
		return
	}
	owner, err := s.getOpenAIWSStateStore().GetResponseAccount(c.Request.Context(), getOpenAIGroupIDFromContext(c), key)
	if err != nil {
		logger.FromContext(c.Request.Context()).Warn("gateway.turn_state_provenance_read_failed", zap.Error(err))
		return
	}
	if owner > 0 && owner != openAICodexTurnStateOwner(codexAccountIdentitySource(c, account)) {
		h.Del(openAICodexTurnStateHeader)
	}
}
