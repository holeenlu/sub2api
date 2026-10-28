package service

import (
	"fmt"

	"github.com/Wei-Shaw/sub2api/internal/gateway/session"
	"github.com/gin-gonic/gin"
)

const (
	openAIWSThreadIDHeader = "thread-id"
	openAIWSWindowIDHeader = "x-codex-window-id"
)

// openAIWSExecutionIdentity extracts only the real ingress header/context
// signals. Stream identity is owned by the multiplexer, not the wire body.
func openAIWSExecutionIdentity(c *gin.Context) session.ExecutionIdentity {
	if c == nil || c.Request == nil {
		return session.ExecutionIdentity{}
	}
	identity := session.ExecutionIdentity{
		ThreadIDHeader:     c.GetHeader(openAIWSThreadIDHeader),
		TurnMetadataHeader: c.GetHeader(openAIWSTurnMetadataHeader),
		WindowIDHeader:     c.GetHeader(openAIWSWindowIDHeader),
		SubagentHeader:     c.GetHeader(openAISubagentHeader),
	}
	if stream, ok := c.Request.Context().Value(openAIWSStreamContextKey{}).(openAIWSStreamIdentity); ok {
		identity.ConnectionID = stream.connection
		identity.StreamID = stream.stream
	}
	return identity
}

func resolveOpenAIWSExecutionLane(c *gin.Context, body []byte) string {
	return session.ExecutionLane(openAIWSExecutionIdentity(c), body)
}

// resolveOpenAIWSExecutionScope retains the shared explicit-session resolver
// and hash format at the service boundary; session owns lane/identity rules.
func resolveOpenAIWSExecutionScope(c *gin.Context, body []byte, apiKeyID int64) (scope, threadID string) {
	identity := openAIWSExecutionIdentity(c)
	seed, threadID := session.ExecutionScopeSeed(identity, body, apiKeyID)
	// Preserve the original lazy fallback: a body-declared thread can resolve
	// without a request; otherwise the shared helper owns explicit-session
	// semantics, including nil context and response-envelope prompt_cache_key.
	if threadID == "" {
		identity.SessionID = explicitOpenAIRequestSessionID(c, body)
		seed, _ = session.ExecutionScopeSeed(identity, body, apiKeyID)
	}
	scope, _ = deriveOpenAISessionHashes(seed)
	return scope, threadID
}

func openAIWSTenantScope(c *gin.Context) string {
	if c != nil {
		if raw, ok := c.Get(openAIHTTPResponseOwnerContextKey); ok {
			if owner, ok := raw.(openAIHTTPResponseOwner); ok && owner.userID > 0 {
				return fmt.Sprintf("%d/user/%d", getOpenAIGroupIDFromContext(c), owner.userID)
			}
		}
	}
	return fmt.Sprintf("%d/key/%d", getOpenAIGroupIDFromContext(c), getAPIKeyIDFromContext(c))
}
