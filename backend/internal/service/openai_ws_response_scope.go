package service

import (
	"context"
	"errors"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/gateway/session"
)

func openAIWSResponseScopeForStore(storeDisabled bool) session.ResponseScope {
	if storeDisabled {
		return session.ResponseScopeConnectionLocal
	}
	return session.ResponseScopePersisted
}

// Resolve the producing turn's scope; the new request's store flag is not
// evidence that its parent is portable. Account/principal authority remains
// with the existing scheduler and dispatch checks.
func (s *OpenAIGatewayService) resolveOpenAIWSParentContext(account *Account, previous string) (preferred, required string, err error) {
	previous = strings.TrimSpace(previous)
	if previous == "" {
		return "", "", nil
	}
	scope := session.ResponseScopeUnknown
	if store := s.getOpenAIWSStateStore(); store != nil {
		if connID, savedScope, found := store.GetResponseContinuation(previous); found {
			preferred, scope = connID, savedScope
		}
	}
	if scope == session.ResponseScopeConnectionLocal {
		if preferred == "" {
			return "", "", errors.Join(ErrOpenAIContinuationUnavailable, errOpenAIWSResponseContextUnavailable)
		}
		return preferred, previous, nil
	}
	if scope == session.ResponseScopeUnknown && account != nil && account.IsOpenAIOAuthLike() {
		return "", "", errors.Join(ErrOpenAIContinuationUnavailable, errOpenAIWSResponseContextUnavailable)
	}
	return preferred, "", nil
}

type openAIWSResponseRequirementKey struct{}
type openAIWSResponseRequirement struct {
	lease      *openAIWSConnLease
	responseID string
	generation uint64
}

// The exact payload subsequently passed to sendNativeWS owns this requirement.
// The existing pool writer rechecks it after acquiring its write gate. This is
// a reference to the actual receipt, not another generation or lease owner.
func (s *OpenAIGatewayService) bindOpenAIWSResponseContext(ctx context.Context, account *Account, lease *openAIWSConnLease, payload []byte) (context.Context, error) {
	previous := openAIWSPayloadStringFromRaw(payload, "previous_response_id")
	preferred, required, err := s.resolveOpenAIWSParentContext(account, previous)
	if err != nil || required == "" {
		return ctx, err
	}
	conn, connErr := lease.activeConn()
	if connErr != nil || lease.ConnID() != preferred || !lease.HasResponseContext(required) {
		return ctx, errors.Join(ErrOpenAIContinuationUnavailable, errOpenAIWSResponseContextUnavailable, connErr)
	}
	receipt := conn.latestResponse.Load()
	if receipt == nil || receipt.responseID != required || receipt.writeGeneration != conn.writeGeneration.Load() {
		return ctx, errors.Join(ErrOpenAIContinuationUnavailable, errOpenAIWSResponseContextUnavailable)
	}
	return context.WithValue(ctx, openAIWSResponseRequirementKey{}, openAIWSResponseRequirement{
		lease: lease, responseID: required, generation: receipt.writeGeneration,
	}), nil
}
