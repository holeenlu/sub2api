package service

import (
	"context"
	"errors"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/gateway/session"

	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tidwall/gjson"
)

var errOpenAIWSSessionPreempted = session.ErrPreempted

const (
	openAIWSSessionPreemptCloseGrace    = session.OwnerCloseGrace
	openAIWSSessionPreemptedCloseReason = "session preempted by a newer connection"
)

// Both real ingress implementations expose a termination hook and bounded
// close handshake. The owner cancels execution at the grace deadline even
// when the websocket library is still joining its already-started Close.
type openAIWSPreemptClientCloser interface {
	Close(code coderws.StatusCode, reason string) error
	CloseNow() error
}

// These aliases keep existing composition/capability assertions compiling;
// state and lifecycle are owned by session, not a second service registry.
// MAIN removes the assembly alias when OpenAIGatewayService directly names
// session.OwnerRegistry; Redis remains the same atomic window implementation.
type openAIWSSessionPreemptRegistry = session.OwnerRegistry
type OpenAIWSSessionPreemptionCache = session.OwnerCache

func NewOpenAIWSSessionPreemptedError() error {
	return errOpenAIWSSessionPreempted
}

type openAIWSSessionPreemptContextKey struct{}

// BeginOpenAIWSIngressSessionPreemption keeps a persistent inbound WS session
// registered across upstream retry attempts. Nested forwarding calls reuse the
// registration so returning from one attempt cannot create a preemption gap.
func (s *OpenAIGatewayService) BeginOpenAIWSIngressSessionPreemption(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	firstClientMessage []byte,
) (context.Context, func(), bool) {
	return s.BeginOpenAIWSIngressSessionPreemptionWithClient(ctx, c, account, firstClientMessage, nil)
}

// BeginOpenAIWSIngressSessionPreemptionWithClient 与 BeginOpenAIWSIngressSessionPreemption 相同，
// 另外登记客户端连接：本连接被更新的连接取代时，先给它发带原因的关闭帧，再取消上下文，
// 客户端因此能立即重试，而不是在裸断开后静默等到空闲超时。
func (s *OpenAIGatewayService) BeginOpenAIWSIngressSessionPreemptionWithClient(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	firstClientMessage []byte,
	clientConn openAIWSPreemptClientCloser,
	requestModels ...string,
) (context.Context, func(), bool) {
	if ctx == nil {
		ctx = context.Background()
	}
	if owner := OpenAIWSSessionOwner(ctx); owner != nil {
		return ctx, func() {}, true
	}
	var notifyPreempted func(context.Context)
	if clientConn != nil {
		notifyPreempted = func(ctx context.Context) {
			forced := make(chan struct{})
			stopForce := context.AfterFunc(ctx, func() {
				defer close(forced)
				_ = clientConn.CloseNow()
			})
			_ = clientConn.Close(coderws.StatusTryAgainLater, openAIWSSessionPreemptedCloseReason)
			if !stopForce() {
				<-forced
			}
		}
	}
	requestModel := gjson.GetBytes(firstClientMessage, "model").String()
	if len(requestModels) > 0 {
		requestModel = requestModels[0] // handler supplies the channel-mapped model
	}
	if account != nil && (account.IsExcelBPSEnabledForModel(requestModel) && s.excelBPSGloballyEnabled(ctx)) {
		// BPS owns its upstream and replay history per connection, just
		// like explicit HTTP bridge mode. Shared client identity is not shared
		// upstream state and must not preempt an independent generation.
		return ctx, func() {}, false
	}
	if s != nil && s.cfg != nil && s.cfg.Gateway.OpenAIWS.ModeRouterV2Enabled && account != nil {
		switch account.ResolveOpenAIResponsesWebSocketV2Mode(s.cfg.Gateway.OpenAIWS.IngressModeDefault) {
		case OpenAIWSIngressModePassthrough, OpenAIWSIngressModeHTTPBridge:
			// These modes own their upstream transport and replay state per
			// inbound connection. Sharing a sticky/cache identity does not mean
			// a new connection should cancel an independent in-flight request.
			return ctx, func() {}, false
		}
	}

	preemptScope := ""
	preemptThreadID := ""
	preemptGroupID := getOpenAIGroupIDFromContext(c)
	preemptAPIKeyID := getAPIKeyIDFromContext(c)
	if account != nil && account.Platform == PlatformOpenAI && account.Type == AccountTypeOAuth {
		// Codex multi-agent sessions share one session-id across the parent thread
		// and every sub-agent. Key preemption by the declared execution identity
		// (thread first, explicit session otherwise) so siblings never cancel each
		// other while a reconnect of the same thread still replaces it. Requests
		// without any declared identity are not registered at all.
		preemptScope, preemptThreadID = resolveOpenAIWSExecutionScope(c, firstClientMessage, preemptAPIKeyID)
	}
	preemptCtx, cleanup, armed, preemptedPrevious := s.beginOpenAIWSSessionPreemptContext(
		ctx,
		account,
		preemptGroupID,
		preemptAPIKeyID,
		preemptScope,
		false,
		notifyPreempted,
	)
	if !armed {
		return ctx, func() {}, false
	}
	if preemptedPrevious {
		lane := resolveOpenAIWSExecutionLane(c, firstClientMessage)
		if lane == "" {
			lane = "main"
		}
		logOpenAIWSModeInfo(
			"ingress_ws_session_preempted account_id=%d group_id=%d api_key_id=%d scope=%s thread_id=%s lane=%s",
			account.ID,
			preemptGroupID,
			preemptAPIKeyID,
			truncateOpenAIWSLogValue(preemptScope, 12),
			truncateOpenAIWSLogValue(preemptThreadID, openAIWSIDValueMaxLen),
			truncateOpenAIWSLogValue(lane, openAIWSIDValueMaxLen),
		)
	}
	return preemptCtx, cleanup, true
}

func (s *OpenAIGatewayService) beginOpenAIWSSessionPreemptContext(
	ctx context.Context,
	account *Account,
	groupID, apiKeyID int64,
	sessionHash string,
	httpIngressWSOneShot bool,
	notifyPreempted func(context.Context),
) (context.Context, func(), bool, bool) {
	if ctx == nil {
		ctx = context.Background()
	}
	if s == nil || account == nil || account.Platform != PlatformOpenAI || account.Type != AccountTypeOAuth || httpIngressWSOneShot {
		return ctx, func() {}, false, false
	}

	scope := session.OwnerScope{GroupID: groupID, APIKeyID: apiKeyID, SessionHash: sessionHash}
	var cache session.OwnerCache
	if s.cache != nil {
		cache, _ = s.cache.(session.OwnerCache)
	}
	owner, previous := s.openaiWSSessionPreemptions.Acquire(ctx, scope, uuid.NewString(), cache, func() {
		if store := s.getOpenAIWSStateStore(); store != nil {
			store.DeleteSessionTurnState(groupID, sessionHash)
			store.DeleteSessionConn(groupID, sessionHash)
		}
	}, notifyPreempted)
	if owner == nil {
		return ctx, func() {}, false, false
	}
	ownerCtx := context.WithValue(owner.Context(), openAIWSSessionPreemptContextKey{}, owner)
	return ownerCtx, owner.Close, true, previous
}

func isOpenAIWSSessionPreempted(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	if owner := OpenAIWSSessionOwner(ctx); owner != nil && owner.Preempted() {
		return true
	}
	return errors.Is(context.Cause(ctx), errOpenAIWSSessionPreempted)
}

// OpenAIWSSessionOwner exposes the ingress-owned lifetime to its forwarding
// consumer. Publish fences local conn/turn-state updates; Current alone is not
// a send permit or authorization. MAIN owns forwarding call-site integration.
func OpenAIWSSessionOwner(ctx context.Context) *session.Owner {
	if ctx == nil {
		return nil
	}
	owner, _ := ctx.Value(openAIWSSessionPreemptContextKey{}).(*session.Owner)
	return owner
}

func IsOpenAIWSSessionPreemptedError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, errOpenAIWSSessionPreempted) {
		return true
	}
	var fallbackErr *openAIWSFallbackError
	return errors.As(err, &fallbackErr) && fallbackErr != nil && strings.TrimPrefix(strings.TrimSpace(fallbackErr.Reason), "prewarm_") == "session_preempted"
}
