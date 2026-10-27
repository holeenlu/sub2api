package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Extends the actor fixture with fenced durable records. No RPC or reconnect
// hook is provided: recovery can only publish state through the store contract.
type cloudRecoveryStoreFixture struct {
	cloudActorTestStore
	expired                                        []*OpenAICloudSessionRecord
	records                                        map[string]*OpenAICloudSessionRecord
	bills                                          map[string]*OpenAICloudSettlement
	acked                                          map[string]bool
	live                                           map[string]bool
	released                                       []string
	saved                                          []*OpenAICloudSettlement
	claimTokens                                    []string
	listErr, claimErr, saveErr, ackErr, getBillErr error
}

func cloudRecoveryClone(r *OpenAICloudSessionRecord) *OpenAICloudSessionRecord {
	if r == nil {
		return nil
	}
	raw, _ := json.Marshal(r)
	var copy OpenAICloudSessionRecord
	_ = json.Unmarshal(raw, &copy)
	return &copy
}

func newCloudRecoveryFixture(r *OpenAICloudSessionRecord) *cloudRecoveryStoreFixture {
	return &cloudRecoveryStoreFixture{expired: []*OpenAICloudSessionRecord{cloudRecoveryClone(r)}, records: map[string]*OpenAICloudSessionRecord{r.SessionID: cloudRecoveryClone(r)}, bills: map[string]*OpenAICloudSettlement{}, acked: map[string]bool{}, live: map[string]bool{}}
}

func (s *cloudRecoveryStoreFixture) ListExpiredCloudSessions(context.Context, int) ([]*OpenAICloudSessionRecord, error) {
	return s.expired, s.listErr
}
func (s *cloudRecoveryStoreFixture) ListCloudSettlements(context.Context, int) ([]*OpenAICloudSettlement, error) {
	out := make([]*OpenAICloudSettlement, 0, len(s.bills))
	for _, b := range s.bills {
		out = append(out, b)
	}
	return out, nil
}

func (s *cloudRecoveryStoreFixture) GetCloudSettlement(_ context.Context, id string) (*OpenAICloudSettlement, error) {
	return s.bills[id], s.getBillErr
}
func (s *cloudRecoveryStoreFixture) ClaimCloudSession(_ context.Context, id, token string, ttl time.Duration) (*OpenAICloudSessionRecord, bool, error) {
	if s.claimErr != nil {
		return nil, false, s.claimErr
	}
	r := s.records[id]
	if r == nil || s.live[id] || r.Status == "closed" {
		return nil, false, nil
	}
	r = cloudRecoveryClone(r)
	r.OwnerToken = token
	r.OwnerInstance = token
	r.LeaseUntil = time.Now().Add(ttl)
	s.records[id] = r
	s.live[id] = true
	s.claimTokens = append(s.claimTokens, token)
	return cloudRecoveryClone(r), true, nil
}
func (s *cloudRecoveryStoreFixture) SaveCloudSession(_ context.Context, r *OpenAICloudSessionRecord, _ []string, b *OpenAICloudSettlement) error {
	if s.saveErr != nil {
		return s.saveErr
	}
	old := s.records[r.SessionID]
	if old == nil || !s.live[r.SessionID] || old.OwnerToken != r.OwnerToken || old.Sequence >= r.Sequence {
		return errors.New("owner or sequence changed")
	}
	s.records[r.SessionID] = cloudRecoveryClone(r)
	if b != nil {
		raw, _ := json.Marshal(b)
		var copy OpenAICloudSettlement
		_ = json.Unmarshal(raw, &copy)
		s.bills[b.RootRequestID] = &copy
		s.saved = append(s.saved, &copy)
	}
	if r.Status == "unknown" || r.Status == "failed" || r.Status == "interrupted" {
		s.live[r.SessionID] = false
	}
	return nil
}
func (s *cloudRecoveryStoreFixture) CloudSettlementArchived(context.Context, string) (bool, error) {
	return false, nil
}
func (s *cloudRecoveryStoreFixture) CloudSettlementAcknowledged(_ context.Context, id string) (bool, error) {
	return s.acked[id], s.ackErr
}
func (s *cloudRecoveryStoreFixture) ReleaseCloudSession(_ context.Context, id, owner string) error {
	r := s.records[id]
	if r == nil || r.Status != "completed" || !s.live[id] || r.OwnerToken != owner {
		return errors.New("release owner changed")
	}
	r.Status = "closed"
	s.live[id] = false
	s.released = append(s.released, id)
	return nil
}

func cloudRecoveryRecord(status string) *OpenAICloudSessionRecord {
	return &OpenAICloudSessionRecord{OpenAICloudBinding: OpenAICloudBinding{OpenAICloudOwner: OpenAICloudOwner{UserID: 1, APIKeyID: 2, GroupID: 3, AccountID: 4, GrantGeneration: "grant", Model: "model", SettingsDigest: "settings"}, SessionID: "session", RootRequestID: "root", ThreadID: "thread", TurnID: "turn", OwnerInstance: "dead-process", ExpiresAt: time.Now().Add(time.Minute), Status: status}, OwnerToken: "dead-owner", LeaseUntil: time.Now().Add(-time.Minute), Sequence: 7, BillingSnapshot: json.RawMessage(`{"owner":1,"rate":2}`)}
}

func TestCloudRecoveryExpiredTurnPersistsUnknownAndFencesDeadOwner(t *testing.T) {
	for _, status := range []string{"created", "running", "awaiting_tools"} {
		t.Run(status, func(t *testing.T) {
			r := cloudRecoveryRecord(status)
			r.Usage = &OpenAICloudUsage{InputTokens: 10, OutputTokens: 2, TotalTokens: 12}
			r.SamplingUsage = []OpenAICloudUsage{*r.Usage}
			store := newCloudRecoveryFixture(r)
			gateway := &OpenAIGatewayService{}
			require.NoError(t, gateway.recoverCloudSessions(t.Context(), store, "worker"))
			require.Len(t, store.saved, 1)
			bill := store.saved[0]
			require.True(t, bill.Unknown)
			require.Equal(t, "unknown", bill.Status)
			require.Equal(t, r.RootRequestID, bill.RootRequestID)
			require.Equal(t, r.OpenAICloudOwner, bill.OpenAICloudOwner)
			require.JSONEq(t, string(r.BillingSnapshot), string(bill.BillingSnapshot))
			require.Equal(t, *r.Usage, *bill.Usage, "a measured watermark is preserved without claiming it is final")
			require.Equal(t, r.SamplingUsage, bill.SamplingUsage)
			r.Sequence += 10
			require.Error(t, store.SaveCloudSession(t.Context(), r, nil, nil), "the expired actor cannot publish after the recovery claim")
			require.Zero(t, store.enqueueCalls, "recovery never dispatches a turn or tool command")
		})
	}
}

func TestCloudRecoveryDoesNotRecreateAnArchivedBillAfterRedisMarkerExpires(t *testing.T) {
	r := cloudRecoveryRecord("completed")
	store := newCloudRecoveryFixture(r)
	svc, _, ledger := cloudBillingFixture(t)
	archive := svc.usageBillingRepo.(*cloudBillingArchiveRepoStub)
	archive.archives = map[string][]byte{r.RootRequestID: []byte("retained facts")}
	require.NoError(t, svc.recoverCloudSessions(t.Context(), store, "worker"))
	require.Empty(t, store.saved)
	require.Equal(t, []string{r.SessionID}, store.released)
	require.Zero(t, ledger.calls)
	require.False(t, store.acked[r.RootRequestID])
}

func TestCloudRecoveryUsesClaimedCurrentRootAndPreservesUnknownUsage(t *testing.T) {
	r := cloudRecoveryRecord("running")
	store := newCloudRecoveryFixture(r)
	store.records[r.SessionID].RootRequestID = "next-root"
	store.records[r.SessionID].BillingSnapshot = json.RawMessage(`{"owner":1,"root":"next-root"}`)
	require.NoError(t, (&OpenAIGatewayService{}).recoverCloudSessions(t.Context(), store, "worker"))
	require.Len(t, store.saved, 1)
	require.Equal(t, "next-root", store.saved[0].RootRequestID)
	require.Nil(t, store.saved[0].Usage, "unknown consumption must never become measured zero")
}

func TestCloudRecoveryPreservesCompletedBillsAndReceipts(t *testing.T) {
	for _, scenario := range []string{"acknowledged", "pending bill", "rebuild proof", "unproven completion"} {
		t.Run(scenario, func(t *testing.T) {
			r := cloudRecoveryRecord("completed")
			r.Usage = &OpenAICloudUsage{InputTokens: 10, OutputTokens: 2, TotalTokens: 12}
			r.SamplingUsage = []OpenAICloudUsage{*r.Usage}
			finished := time.Now().Add(-time.Minute).Truncate(time.Second)
			response, _ := json.Marshal(map[string]any{"status": "completed", "created_at": finished.Unix(), "usage": cloudResponseUsage(r.Usage)})
			r.Result = &OpenAICloudResult{SessionID: r.SessionID, RootRequestID: r.RootRequestID, ThreadID: r.ThreadID, TurnID: r.TurnID, Terminal: true, UsageCredible: true, Usage: r.Usage, Response: response}
			store := newCloudRecoveryFixture(r)
			var original *OpenAICloudSettlement
			switch scenario {
			case "acknowledged":
				store.acked[r.RootRequestID] = true
			case "pending bill":
				original = &OpenAICloudSettlement{OpenAICloudBinding: r.OpenAICloudBinding, BillingSnapshot: json.RawMessage(`{"rate":2,"owner":1}`), Status: "completed", Usage: r.Usage, SamplingUsage: r.SamplingUsage, Sequence: r.Sequence, ObservedAt: finished}
				store.bills[r.RootRequestID] = original
			case "unproven completion":
				r.Result.UsageCredible = false
				store.records[r.SessionID] = cloudRecoveryClone(r)
			}
			require.NoError(t, (&OpenAIGatewayService{}).recoverCloudSessions(t.Context(), store, "worker"))
			switch scenario {
			case "acknowledged":
				require.Empty(t, store.saved)
				require.Equal(t, []string{r.SessionID}, store.released)
			case "pending bill":
				require.Empty(t, store.saved)
				require.Same(t, original, store.bills[r.RootRequestID])
				require.False(t, original.Unknown)
				require.Equal(t, []string{r.SessionID}, store.released)
			case "rebuild proof":
				require.Len(t, store.saved, 1)
				require.False(t, store.saved[0].Unknown)
				require.Equal(t, "completed", store.saved[0].Status)
				require.True(t, finished.Equal(store.saved[0].ObservedAt))
				require.Equal(t, []string{r.SessionID}, store.released)
			case "unproven completion":
				require.Len(t, store.saved, 1)
				require.True(t, store.saved[0].Unknown, "single-root lookup confirmed there is no prior completed bill to preserve")
				require.Empty(t, store.released)
			}
		})
	}
}

func TestCloudRecoveryDoesNotMutateAccountsOrPublishFailedClaims(t *testing.T) {
	for _, scenario := range []string{"probe", "list failure", "claim failure", "live owner", "save failure"} {
		t.Run(scenario, func(t *testing.T) {
			r := cloudRecoveryRecord("running")
			r.Probe = scenario == "probe"
			if r.Probe {
				r.APIKeyID = 0
				r.BillingSnapshot = nil
			}
			store := newCloudRecoveryFixture(r)
			repo := &cloudAuthorizationRepoFixture{account: &Account{ID: 4, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"chatgpt_account_id": "workspace", "chatgpt_user_id": "user"}}, binding: &OpenAICloudAuthorizationBinding{Generation: "grant", AccountID: "workspace", UserID: "user"}}
			gateway := &OpenAIGatewayService{cloudService: NewOpenAICloudService(repo, nil, nil)}
			switch scenario {
			case "list failure":
				store.listErr = errors.New("unavailable")
			case "claim failure":
				store.claimErr = errors.New("claim outcome unknown")
			case "live owner":
				store.live[r.SessionID] = true
			case "save failure":
				store.saveErr = errors.New("save outcome unknown")
			}
			err := gateway.recoverCloudSessions(t.Context(), store, "worker")
			if scenario == "probe" || scenario == "live owner" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
			require.Zero(t, repo.writes, "recovery never changes account settings")
			if scenario == "probe" {
				require.Len(t, store.saved, 1)
				require.True(t, store.saved[0].Probe)
				require.True(t, store.saved[0].Unknown)
			} else {
				require.Empty(t, store.saved)
			}
		})
	}
}

func TestCloudRecoveryRetainsUnknownUsageWithoutChangingAuthorization(t *testing.T) {
	for _, generation := range []string{"grant", "new-login"} {
		t.Run(generation, func(t *testing.T) {
			r := cloudRecoveryRecord("running")
			store := newCloudRecoveryFixture(r)
			repo := &cloudAuthorizationRepoFixture{account: &Account{ID: 4, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"chatgpt_account_id": "workspace", "chatgpt_user_id": "user"}}, binding: &OpenAICloudAuthorizationBinding{Generation: generation, AccountID: "workspace", UserID: "user"}}
			gateway := &OpenAIGatewayService{cloudService: NewOpenAICloudService(repo, nil, nil)}
			require.NoError(t, gateway.recoverCloudSessions(t.Context(), store, "worker"))
			require.Len(t, store.saved, 1)
			require.True(t, store.saved[0].Unknown)
			require.Zero(t, repo.writes)
			require.Equal(t, generation, repo.binding.Generation, "unknown consumption preserves the authorization identity")
		})
	}
}

func TestCloudRecoverySettlementLookupFailureDoesNotMeanMissingBill(t *testing.T) {
	r := cloudRecoveryRecord("completed")
	store := newCloudRecoveryFixture(r)
	store.getBillErr = errors.New("settlement read unavailable")
	require.ErrorIs(t, (&OpenAIGatewayService{}).recoverCloudSessions(t.Context(), store, "worker"), store.getBillErr)
	require.Empty(t, store.saved)
	require.Empty(t, store.released)
	require.Equal(t, "completed", store.records[r.SessionID].Status)
}

func TestCloudRecoveryCompletedProofChecksHostedFactsAndPublicTokenUsage(t *testing.T) {
	r := cloudRecoveryRecord("completed")
	r.Usage = &OpenAICloudUsage{InputTokens: 10, OutputTokens: 2, CachedInputTokens: 3, TotalTokens: 12,
		WebSearchCalls: 2, ImageCount: 1, ImageSizes: map[string]int{"1K": 1}, ImageOutputSizes: []string{"512x512"}}
	response, err := json.Marshal(map[string]any{"status": "completed", "created_at": time.Now().Add(-time.Minute).Unix(), "usage": cloudResponseUsage(r.Usage), "output": []any{}})
	require.NoError(t, err)
	r.Result = &OpenAICloudResult{SessionID: r.SessionID, RootRequestID: r.RootRequestID, ThreadID: r.ThreadID, TurnID: r.TurnID,
		Terminal: true, UsageCredible: true, Usage: r.Usage, Response: response}
	_, valid := cloudRecoveryCompletedProof(r)
	require.True(t, valid, "whole-turn hosted usage need not appear in the final HTTP response output")
	lastStep := cloudRecoveryClone(r)
	lastStep.Result.Response, err = json.Marshal(map[string]any{"status": "completed", "created_at": time.Now().Add(-time.Minute).Unix(), "usage": cloudResponseUsage(&OpenAICloudUsage{InputTokens: 4, OutputTokens: 1, CachedInputTokens: 3, TotalTokens: 5}), "output": []any{}})
	require.NoError(t, err)
	_, valid = cloudRecoveryCompletedProof(lastStep)
	require.True(t, valid, "the final response publishes the turn's last sampling step, within the settled usage")
	for name, alter := range map[string]func(*OpenAICloudSessionRecord){
		"search_count": func(r *OpenAICloudSessionRecord) { r.Result.Usage.WebSearchCalls++ },
		"image_count":  func(r *OpenAICloudSessionRecord) { r.Result.Usage.ImageCount++ },
		"image_tier": func(r *OpenAICloudSessionRecord) {
			r.Result.Usage.ImageSizes = map[string]int{"2K": 1}
		},
		"image_dimensions": func(r *OpenAICloudSessionRecord) { r.Result.Usage.ImageOutputSizes[0] = "1024x1024" },
		"public_usage_missing": func(r *OpenAICloudSessionRecord) {
			r.Result.Response, _ = json.Marshal(map[string]any{"status": "completed", "created_at": time.Now().Unix()})
		},
		"public_cached_exceeds": func(r *OpenAICloudSessionRecord) {
			other := *r.Usage
			other.InputTokens, other.CachedInputTokens, other.OutputTokens, other.TotalTokens = 4, 4, 1, 5
			r.Result.Response, _ = json.Marshal(map[string]any{"status": "completed", "created_at": time.Now().Unix(), "usage": cloudResponseUsage(&other)})
		},
		"public_usage_exceeds": func(r *OpenAICloudSessionRecord) {
			other := *r.Usage
			other.InputTokens++
			other.TotalTokens++
			r.Result.Response, _ = json.Marshal(map[string]any{"status": "completed", "created_at": time.Now().Unix(), "usage": cloudResponseUsage(&other)})
		},
	} {
		t.Run(name, func(t *testing.T) {
			copy := cloudRecoveryClone(r)
			alter(copy)
			_, valid := cloudRecoveryCompletedProof(copy)
			require.False(t, valid)
		})
	}
}
