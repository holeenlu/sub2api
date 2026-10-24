package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/authz"
	"github.com/stretchr/testify/require"
)

func TestCloudReconciliationUsageExplicitFinalFacts(t *testing.T) {
	valid := `{"input_tokens":10,"output_tokens":2,"cached_input_tokens":1,"cache_write_input_tokens":0,"total_tokens":12,"web_search_calls":0,"image_count":0,"image_sizes":{},"image_output_sizes":[]}`
	observed := &OpenAICloudUsage{InputTokens: 10, OutputTokens: 1, TotalTokens: 11}
	got, err := cloudReconciliationUsage([]byte(valid), observed)
	require.NoError(t, err)
	require.Equal(t, 12, got.TotalTokens)
	for _, field := range []string{"input_tokens", "output_tokens", "cached_input_tokens", "cache_write_input_tokens", "total_tokens", "web_search_calls", "image_count", "image_sizes", "image_output_sizes"} {
		t.Run(field, func(t *testing.T) {
			var fields map[string]any
			require.NoError(t, json.Unmarshal([]byte(valid), &fields))
			delete(fields, field)
			raw, _ := json.Marshal(fields)
			_, err := cloudReconciliationUsage(raw, observed)
			require.Error(t, err)
			fields[field] = nil
			raw, _ = json.Marshal(fields)
			_, err = cloudReconciliationUsage(raw, observed)
			require.Error(t, err)
		})
	}
	for _, raw := range []string{`null`, `{}`, valid[:len(valid)-1] + `,"unexpected":1}`, valid[:len(valid)-1] + `,"input_tokens":10}`} {
		_, err := cloudReconciliationUsage([]byte(raw), observed)
		require.Error(t, err)
	}
	_, err = cloudReconciliationUsage([]byte(valid), &OpenAICloudUsage{InputTokens: 11, TotalTokens: 11})
	require.Error(t, err)
}

func TestCloudReconciliationRequiresManagementSuperAdmin(t *testing.T) {
	svc := &OpenAICloudService{}
	for _, actor := range []authz.Subject{{}, {Role: authz.SuperAdmin, UserID: 1}, {Role: authz.Admin, UserID: 1, ManagementRequest: true}} {
		ctx := authz.WithSubject(context.Background(), actor)
		_, err := svc.Unsettled(ctx, 1)
		require.ErrorIs(t, err, authz.ErrPermissionDenied)
		err = svc.Reconcile(ctx, 1, OpenAICloudReconciliationInput{Confirmed: true})
		require.ErrorIs(t, err, authz.ErrPermissionDenied)
	}
}

type cloudReconciliationRepoFixture struct {
	*cloudAuthorizationRepoFixture
	audits   int
	final    *OpenAICloudSettlement
	complete bool
}

func (r *cloudReconciliationRepoFixture) RecordCloudReconciliation(_ context.Context, _ int64, bill *OpenAICloudSettlement, _, _ string) error {
	r.audits++
	r.final = bill
	return nil
}
func (r *cloudReconciliationRepoFixture) RecordCloudRuntimeBlock(context.Context, string, int64) error {
	return nil
}
func (r *cloudReconciliationRepoFixture) PendingCloudReconciliationAcks(context.Context, int64) ([]string, error) {
	return []string{"root"}, nil
}
func (r *cloudReconciliationRepoFixture) MarkCloudReconciliationSettled(context.Context, string) error {
	r.complete = true
	return nil
}
func (r *cloudReconciliationRepoFixture) CloudRuntimeReconciled(context.Context, int64) (bool, error) {
	return r.complete, nil
}

type cloudReconciliationStoreFixture struct {
	*cloudBillingStoreStub
	old     *OpenAICloudSettlement
	applied int
}

func (s *cloudReconciliationStoreFixture) GetCloudSettlement(context.Context, string) (*OpenAICloudSettlement, error) {
	return s.old, nil
}
func (s *cloudReconciliationStoreFixture) ApplyCloudReconciliation(_ context.Context, _, next *OpenAICloudSettlement) error {
	s.applied++
	s.old = next
	return nil
}
func TestCloudReconciliationAuditAndLedgerProof(t *testing.T) {
	a := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"chatgpt_account_id": "workspace", "chatgpt_user_id": "user"}}
	repo := &cloudReconciliationRepoFixture{cloudAuthorizationRepoFixture: &cloudAuthorizationRepoFixture{account: a, binding: &OpenAICloudAuthorizationBinding{Generation: "one", RuntimeBlocked: true}}}
	old := &OpenAICloudSettlement{OpenAICloudBinding: OpenAICloudBinding{OpenAICloudOwner: OpenAICloudOwner{AccountID: 1}, RootRequestID: "root"}, Sequence: 3, Unknown: true, BillingSnapshot: json.RawMessage(`{"price":"frozen"}`)}
	store := &cloudReconciliationStoreFixture{cloudBillingStoreStub: &cloudBillingStoreStub{}, old: old}
	svc := NewOpenAICloudService(repo, nil, nil)
	svc.gateway = &OpenAIGatewayService{cloudSessions: &OpenAICloudSessions{store: store}}
	ctx := authz.WithSubject(t.Context(), authz.Subject{UserID: 7, Role: authz.SuperAdmin, ManagementRequest: true})
	in := OpenAICloudReconciliationInput{RootID: "root", Sequence: 3, Status: "failed", Evidence: "verified source", Reason: "confirmed facts", Confirmed: true, Usage: json.RawMessage(`{"input_tokens":10,"output_tokens":2,"cached_input_tokens":0,"cache_write_input_tokens":0,"total_tokens":12,"web_search_calls":0,"image_count":0,"image_sizes":{},"image_output_sizes":[]}`)}
	require.NoError(t, svc.Reconcile(ctx, 1, in))
	require.Equal(t, 1, repo.audits)
	require.Equal(t, 1, store.applied)
	require.Equal(t, old.BillingSnapshot, repo.final.BillingSnapshot)
	require.Equal(t, uint64(4), repo.final.Sequence)
	require.NoError(t, svc.clearSettledRuntime(ctx, 1))
	require.True(t, repo.binding.RuntimeBlocked, "audit alone cannot unlock")
	store.acked = map[string]bool{"root": true}
	require.NoError(t, svc.clearSettledRuntime(ctx, 1))
	require.False(t, repo.binding.RuntimeBlocked)
	require.False(t, repo.account.IsOpenAICloudEnabled(), "reconciliation never automatically enables Cloud")
}
