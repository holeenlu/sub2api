package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCodexDiagnosticCannotFailOverToAnotherAccount(t *testing.T) {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()
	target, other := diagnosticTestAccount(41), diagnosticTestAccount(42)
	target.Status, target.Schedulable = StatusDisabled, false
	other.Schedulable, other.Concurrency = true, 1
	svc := newOpenAICompactionSchedulerTestService([]Account{*target, *other}, false)
	selection, _, err := svc.SelectAccountWithScheduler(withCodexDiagnosticAccount(context.Background(), target.ID), nil, "", "", "gpt-6-astra", nil, OpenAIUpstreamTransportAny, false)
	require.Error(t, err)
	require.Nil(t, selection)
	selection, _, err = svc.SelectAccountWithScheduler(context.Background(), nil, "", "", "gpt-6-astra", nil, OpenAIUpstreamTransportAny, false)
	require.NoError(t, err)
	require.Equal(t, other.ID, selection.Account.ID)
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}
}

type diagnosticMemoryRepo struct {
	ScheduledTestPlanRepository
	plan  *CodexDiagnosticPlan
	run   *CodexDiagnosticRun
	saved []*CodexDiagnosticRun
}

func (r *diagnosticMemoryRepo) GetPlan(context.Context, int64) (*CodexDiagnosticPlan, error) {
	return r.plan, nil
}
func (r *diagnosticMemoryRepo) SavePlan(_ context.Context, p *CodexDiagnosticPlan) error {
	r.plan = p
	return nil
}
func (r *diagnosticMemoryRepo) GetRun(context.Context, int64, int64) (*CodexDiagnosticRun, error) {
	return r.run, nil
}
func (r *diagnosticMemoryRepo) SaveProgress(_ context.Context, run *CodexDiagnosticRun) error {
	copy := *run
	copy.Items = append([]CodexDiagnosticItem(nil), run.Items...)
	r.saved = append(r.saved, &copy)
	return nil
}

type diagnosticUsers struct {
	UserRepository
	user *User
}

func (r diagnosticUsers) GetByID(context.Context, int64) (*User, error) { return r.user, nil }

type diagnosticAccounts struct {
	AccountRepository
	account *Account
}

func (r diagnosticAccounts) GetByID(context.Context, int64) (*Account, error) { return r.account, nil }

type diagnosticKeys struct {
	APIKeyRepository
	key *APIKey
}

func (r diagnosticKeys) GetByID(context.Context, int64) (*APIKey, error) { return r.key, nil }

func diagnosticFixture() (*ScheduledTestService, *diagnosticMemoryRepo, *APIKey, *User) {
	user := &User{ID: 7, Role: RoleAdmin, Status: StatusActive}
	groupID := int64(3)
	key := &APIKey{ID: 9, UserID: 7, Key: "private-key", Status: StatusActive, GroupID: &groupID, Group: &Group{ID: 3, ModelAllowlist: GroupModelAllowlist{Enabled: true, Models: []string{"gpt-5.4", "gpt-5.5"}}}}
	acc := &Account{ID: 41, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, GroupIDs: []int64{3}}
	repo := &diagnosticMemoryRepo{plan: &CodexDiagnosticPlan{AccountID: 41, OwnerID: 7, APIKeyID: 9, Models: []string{"gpt-5.4", "gpt-5.5"}, Enabled: true, Revision: 1}}
	repo.run = &CodexDiagnosticRun{ID: 1, AccountID: 41, OwnerID: 7, APIKeyID: 9, Models: append([]string{}, repo.plan.Models...), PlanRevision: 1, Status: "running", Source: "scheduled", WorkerToken: "worker"}
	svc := &ScheduledTestService{planRepo: repo, accounts: diagnosticAccounts{account: acc}, keys: NewAPIKeyService(diagnosticKeys{key: key}, nil, nil, nil, nil, nil, nil), users: diagnosticUsers{user: user}}
	return svc, repo, key, user
}
func TestCodexMonitorRevalidatesBillingOwnerAndKey(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*APIKey, *User)
	}{
		{"foreign key", func(k *APIKey, _ *User) { k.UserID = 8 }},
		{"revoked key", func(k *APIKey, _ *User) { k.Status = StatusDisabled }},
		{"demoted owner", func(_ *APIKey, u *User) { u.Role = RoleUser }},
		{"disabled owner", func(_ *APIKey, u *User) { u.Status = StatusDisabled }},
	} {
		t.Run(test.name, func(t *testing.T) {
			s, r, k, u := diagnosticFixture()
			test.change(k, u)
			calls := 0
			s.diagnosticProbe = func(context.Context, *APIKey, int64, string) CodexDiagnosticItem {
				calls++
				return CodexDiagnosticItem{}
			}
			s.executeDiagnostic(context.Background(), r.run)
			require.Zero(t, calls)
			require.Equal(t, "failed", r.run.Status)
			require.Len(t, r.run.Items, 2)
			for _, item := range r.run.Items {
				require.Equal(t, "configuration_unavailable", item.Reason)
			}
		})
	}
}
func TestCodexMonitorLowConfidenceIsNotDegradation(t *testing.T) {
	s, r, _, _ := diagnosticFixture()
	s.diagnosticProbe = func(_ context.Context, _ *APIKey, _ int64, m string) CodexDiagnosticItem {
		status, reason := diagnosticConclusion(m, "other", .3)
		return CodexDiagnosticItem{Model: m, Status: status, Reason: reason, PredictedModel: "other", Probability: .3}
	}
	s.executeDiagnostic(context.Background(), r.run)
	require.Equal(t, "uncertain", r.run.Status)
	require.Equal(t, "low_confidence", r.run.Items[0].Reason)
	require.NotNil(t, r.run.FinishedAt)
	require.Len(t, r.saved, 3)
}
func TestCodexMonitorCancelAndSettingsChangePreventNextRequest(t *testing.T) {
	for _, mode := range []string{"cancel", "revision", "disabled", "owner-revoked"} {
		t.Run(mode, func(t *testing.T) {
			s, r, k, _ := diagnosticFixture()
			calls := 0
			s.diagnosticProbe = func(_ context.Context, _ *APIKey, _ int64, m string) CodexDiagnosticItem {
				calls++
				switch mode {
				case "cancel":
					r.run.CancelRequested = true
				case "revision":
					r.plan.Revision++
				case "disabled":
					r.plan.Enabled = false
				case "owner-revoked":
					k.Status = StatusDisabled
				}
				return CodexDiagnosticItem{Model: m, Status: "normal", Probability: .99}
			}
			s.executeDiagnostic(context.Background(), r.run)
			require.Equal(t, 1, calls)
			if mode == "owner-revoked" {
				require.Equal(t, "uncertain", r.run.Status)
			} else {
				require.Equal(t, "canceled", r.run.Status)
			}
		})
	}
}
func TestCodexMonitorExpiredWorkerDoesNotReplay(t *testing.T) {
	s, r, _, _ := diagnosticFixture()
	working := *r.run
	r.run.Status = "failed"
	s.diagnosticProbe = func(context.Context, *APIKey, int64, string) CodexDiagnosticItem {
		t.Fatal("expired worker sent request")
		return CodexDiagnosticItem{}
	}
	s.executeDiagnostic(context.Background(), &working)
	require.Equal(t, "worker_interrupted", working.Reason)
}
func TestCodexMonitorCanDisableAfterKeyDeleted(t *testing.T) {
	s, r, k, _ := diagnosticFixture()
	k.Status = StatusDisabled
	disabled := *r.plan
	disabled.Enabled = false
	disabled.OwnerID = 999
	require.NoError(t, s.SaveDiagnosticPlan(context.Background(), &disabled))
	require.False(t, r.plan.Enabled)
	require.EqualValues(t, 7, r.plan.OwnerID)
}
func TestCodexMonitorAggregation(t *testing.T) {
	for _, tc := range []struct {
		items    []string
		expected string
	}{
		{[]string{"normal", "normal"}, "normal"}, {[]string{"normal", "failed"}, "uncertain"},
		{[]string{"degraded", "failed"}, "degraded"}, {[]string{"failed", "failed"}, "failed"},
		{[]string{"normal", "uncertain"}, "uncertain"},
	} {
		var items []CodexDiagnosticItem
		for _, status := range tc.items {
			items = append(items, CodexDiagnosticItem{Status: status})
		}
		require.Equal(t, tc.expected, DiagnosticRunStatus(items, 2))
	}
	require.Equal(t, "uncertain", DiagnosticRunStatus([]CodexDiagnosticItem{{Status: "normal"}}, 2))
}
func TestCodexMonitorModelAndAccountScope(t *testing.T) {
	s, r, k, _ := diagnosticFixture()
	_, err := s.validateDiagnostic(context.Background(), r.plan)
	require.NoError(t, err)
	gid := int64(99)
	k.GroupID = &gid
	_, err = s.validateDiagnostic(context.Background(), r.plan)
	require.ErrorIs(t, err, ErrDiagnosticInvalid)
	k.GroupID = nil
	r.plan.Models = []string{"not-in-bank"}
	_, err = s.validateDiagnostic(context.Background(), r.plan)
	require.Error(t, err)
}
func TestCodexMonitorInterruptedContextDoesNotProbe(t *testing.T) {
	s, r, _, _ := diagnosticFixture()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s.diagnosticProbe = func(context.Context, *APIKey, int64, string) CodexDiagnosticItem {
		t.Fatal("canceled probe")
		return CodexDiagnosticItem{}
	}
	s.executeDiagnostic(ctx, r.run)
	require.Equal(t, "failed", r.run.Status)
	require.Equal(t, "interrupted", r.run.Reason)
	require.WithinDuration(t, time.Now(), *r.run.FinishedAt, time.Second)
}

func TestCodexMonitorValidatesCustomInterval(t *testing.T) {
	for _, minutes := range []int{30, 61, -60, 10081} {
		s, r, _, _ := diagnosticFixture()
		p := *r.plan
		p.IntervalMinutes = minutes
		require.ErrorIs(t, s.SaveDiagnosticPlan(context.Background(), &p), ErrDiagnosticInvalid)
	}
	for _, minutes := range []int{60, 120, 300, 10080} {
		s, r, _, _ := diagnosticFixture()
		p := *r.plan
		p.IntervalMinutes = minutes
		require.NoError(t, s.SaveDiagnosticPlan(context.Background(), &p))
		require.Equal(t, minutes, r.plan.IntervalMinutes)
	}
}

func TestCodexMonitorCandidatesComeFromGroupWhitelist(t *testing.T) {
	s, _, key, _ := diagnosticFixture()
	key.Group.Name = "selected group"
	key.Group.ModelAllowlist = GroupModelAllowlist{Enabled: true, Models: []string{"gpt-5.5", "gpt-6.1-sol", "gpt-image-2.5-flare", "gpt-5.4"}}
	choices, err := s.DiagnosticModels(context.Background(), 41, 7, 9)
	require.NoError(t, err)
	require.Equal(t, []string{"gpt-5.5", "gpt-5.4"}, choices.Models)
	require.Equal(t, "selected group", choices.GroupName)
	require.Equal(t, "gpt-6.1-sol", choices.Items[1].ID)
	require.False(t, choices.Items[1].Eligible)
	require.Equal(t, "fingerprint_unavailable", choices.Items[1].Reason)
	require.Len(t, choices.Items, 4)
	key.Group.ModelAllowlist.Enabled = false
	choices, err = s.DiagnosticModels(context.Background(), 41, 7, 9)
	require.NoError(t, err)
	require.Empty(t, choices.Items)
	require.False(t, choices.WhitelistEnabled)
}
func TestCodexMonitorWhitelistPatternsDoNotLeakOtherBankModels(t *testing.T) {
	s, _, key, _ := diagnosticFixture()
	key.Group.ModelAllowlist = GroupModelAllowlist{Enabled: true, Models: []string{"gpt-5.5*", "unrecognized-*"}}
	choices, err := s.DiagnosticModels(context.Background(), 41, 7, 9)
	require.NoError(t, err)
	require.Equal(t, []string{"gpt-5.5"}, choices.Models)
	require.Len(t, choices.Items, 2)
	require.Equal(t, "pattern_no_fingerprint", choices.Items[1].Reason)
}
