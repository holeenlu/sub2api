package service

import (
	"context"
	"crypto/sha256"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	"github.com/Wei-Shaw/sub2api/internal/gateway/routing"
	"github.com/stretchr/testify/require"
)

func nativeWSHeldFixture(t *testing.T) (*nativeAuthorityFixture, nativeWSHandshake, model.PreparedIdentity) {
	t.Helper()
	f := newNativeAuthorityFixture(t)
	headers := http.Header{"Authorization": []string{"Bearer " + f.resource.GetOpenAIProtocolAPIKey()}}
	require.NoError(t, validateNativeWSHandshakeAuthentication(f.resource, headers, time.Now()))
	h, err := nativeWSHandshakeSnapshot(f.resource, f.resource, "wss://api.openai.com/v1/responses", "", headers)
	require.NoError(t, err)
	h.succeeded = true
	r := f.gate.request
	id := model.PreparedIdentity{OperationID: r.OperationID, ResourceID: f.resource.ID, Owner: h.owner, UserID: r.UserID, APIKeyID: r.APIKeyID, GroupID: r.GroupID, GroupBound: r.GroupBound, Transport: "ws", Mode: VendorOpenAIModeNative, GrantDigest: h.grant, TargetDigest: sha256.Sum256([]byte("fixture-target")), WireDigest: sha256.Sum256([]byte("fixture-wire")), BodyKind: model.BodyReplayable, BodyDigest: sha256.Sum256([]byte("fixture-body"))}
	return f, h, id
}
func TestNativeWSHeldAuthorityRechecksCurrentScopeAndSocket(t *testing.T) {
	for _, kind := range []string{"key_revoked", "credential_replaced", "model_mapping_changed", "resource_scope_changed", "lease_revoked", "socket_closed", "handshake_not_successful"} {
		t.Run(kind, func(t *testing.T) {
			f, h, id := nativeWSHeldFixture(t)
			live := true
			_, err := f.gate.revalidateWS(t.Context(), id, h, func() bool { return live }, nil)
			require.NoError(t, err)
			switch kind {
			case "key_revoked":
				f.key.Status = StatusDisabled
			case "credential_replaced":
				f.resource.Credentials["api_key"] = "new generation"
			case "model_mapping_changed":
				f.resource.Credentials["model_mapping"] = map[string]any{"client-model": "new-target"}
			case "resource_scope_changed":
				f.resource.GroupIDs = []int64{10}
			case "lease_revoked":
				f.leases.proof.Fingerprint = [32]byte{}
			case "socket_closed":
				live = false
			case "handshake_not_successful":
				h.succeeded = false
			}
			_, err = f.gate.revalidateWS(t.Context(), id, h, func() bool { return live }, nil)
			require.Error(t, err)
		})
	}
}
func TestNativeWSHeldSocketDoesNotInventHTTPExpiryRevocation(t *testing.T) {
	f, _, id := nativeWSHeldFixture(t)
	now := time.Now()
	expiry := now.Add(time.Minute)
	f.resource.Type = AccountTypeOAuth
	f.resource.Credentials = map[string]any{"access_token": "held-token", "expires_at": expiry.Format(time.RFC3339)}
	headers := http.Header{"Authorization": []string{"Bearer held-token"}}
	require.NoError(t, validateNativeWSHandshakeAuthentication(f.resource, headers, now))
	h, err := nativeWSHandshakeSnapshot(f.resource, f.resource, "wss://chatgpt.com/backend-api/codex/responses", "", headers)
	require.NoError(t, err)
	h.succeeded = true
	id.GrantDigest = h.grant
	f.gate.now = func() time.Time { return now.Add(2 * time.Minute) }
	_, err = f.gate.revalidateWS(t.Context(), id, h, func() bool { return true }, nil)
	require.NoError(t, err, "same live socket and unchanged durable credentials retain the held grant")
	require.ErrorIs(t, validateNativeWSHandshakeAuthentication(f.resource, headers, f.gate.now()), ErrNativeDispatchGrantUnproven, "a NEW dial cannot use that expired bearer")
}
func TestNativeWSAgentIdentityUsesActualSignedHandshake(t *testing.T) {
	key, encoded := newTestAgentIdentityKey(t)
	account := &Account{ID: 71, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{openAIAuthModeCredentialKey: OpenAIAuthModeAgentIdentity, "agent_private_key": encoded, "agent_runtime_id": key.runtimeID, "task_id": key.taskID}}
	signed, err := buildAgentAssertion(key, time.Now())
	require.NoError(t, err)
	headers := http.Header{"Authorization": []string{signed}}
	require.NoError(t, validateNativeWSHandshakeAuthentication(account, headers, time.Now()))
	headers.Set("Authorization", "Bearer "+signed)
	require.ErrorIs(t, validateNativeWSHandshakeAuthentication(account, headers, time.Now()), ErrNativeDispatchGrantUnproven)
	headers.Set("Authorization", signed)
	account.Credentials["task_id"] = "replaced-task"
	require.ErrorIs(t, validateNativeWSHandshakeAuthentication(account, headers, time.Now()), ErrNativeDispatchGrantUnproven)
	account.Credentials["task_id"] = key.taskID
	headers["authorization"] = []string{signed}
	require.ErrorIs(t, validateNativeWSHandshakeAuthentication(account, headers, time.Now()), ErrNativeDispatchGrantUnproven)
}
func TestNativeWSTurnPublicationSharesContextWithoutPreviousOwnerAncestry(t *testing.T) {
	f := newNativeOperationFixture(t, true)
	ds := &OpenAIWSDownstream{}
	ds.budget.Store(f.op.budget)
	require.NoError(t, ds.PublishNativeWSTurn(t.Context(), f.ctx))
	require.Same(t, f.ctx, ds.TurnBudgetContext(context.Background(), f.service.cfg, nil))
	require.Error(t, ds.PublishNativeWSTurn(f.ctx, f.ctx), "the previous operation cannot become the next turn parent")
	next := NewOpenAITurnBudget(f.service.cfg)
	ds.budget.Store(next)
	previous, cancel := context.WithCancel(f.ctx)
	cancel()
	nextCtx := ds.TurnBudgetContext(previous, f.service.cfg, nil)
	require.NoError(t, nextCtx.Err())
	require.Nil(t, nativeDispatchFromContext(nextCtx))
	require.Nil(t, routing.CapacityScopeFrom(nextCtx))
	require.Same(t, next, nextCtx.Value(openAITurnBudgetKey{}))
}

func TestNativeWSHandshakeBindsCurrentWorkspaceAndFedRAMP(t *testing.T) {
	owner := &Account{ID: 71, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{
		"access_token": "unchanged-bearer", "expires_at": time.Now().Add(time.Hour).Format(time.RFC3339),
		"chatgpt_account_id": "current-workspace", "chatgpt_account_is_fedramp": true,
	}}
	headers := http.Header{"Authorization": []string{"Bearer unchanged-bearer"}}
	setOpenAIChatGPTAccountHeaders(headers, owner)
	require.NoError(t, validateNativeWSHandshakeAuthentication(owner, headers, time.Now()))
	for _, name := range []string{"chatgpt-account-id", "x-openai-fedramp"} {
		stale := headers.Clone()
		stale.Set(name, "stale")
		require.ErrorIs(t, validateNativeWSHandshakeAuthentication(owner, stale, time.Now()), ErrNativeDispatchGrantUnproven)
	}
	duplicate := headers.Clone()
	duplicate["chatgpt-account-id"] = []string{"current-workspace"}
	require.ErrorIs(t, validateNativeWSHandshakeAuthentication(owner, duplicate, time.Now()), ErrNativeDispatchGrantUnproven)
}

type nativeWSObservedHandshakeFixture struct {
	*openAIWSFakeConn
	target  string
	headers http.Header
}

func (c *nativeWSObservedHandshakeFixture) takeNativeHandshakeRequest() (string, http.Header) {
	return c.target, c.headers
}
func TestNativeWSHandshakeRejectsChangedTransportAuthentication(t *testing.T) {
	_, fact, _ := nativeWSHeldFixture(t)
	sent := http.Header{"Authorization": []string{"Bearer synthetic-upstream-key"}}
	for _, actual := range []http.Header{nil, {"Authorization": []string{"Bearer replacement"}}, {"Authorization": []string{"Bearer synthetic-upstream-key"}, "Chatgpt-Account-Id": []string{"other-tenant"}}} {
		conn := &nativeWSObservedHandshakeFixture{openAIWSFakeConn: &openAIWSFakeConn{}, target: "wss://allowed.invalid/redirect-target", headers: actual}
		_, err := completeNativeWSHandshake(fact, conn, sent)
		require.ErrorIs(t, err, ErrNativeDispatchGrantUnproven)
		require.Empty(t, conn.payload)
	}
}
