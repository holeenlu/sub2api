package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type codexTicketLookupStub struct {
	CodexTicketLifecycleRepository
	raw   json.RawMessage
	err   error
	calls int
}

func (stub *codexTicketLookupStub) CurrentTicket(context.Context, int64, string) (json.RawMessage, error) {
	stub.calls++
	return stub.raw, stub.err
}

func TestCodexDiagnosticAndNormalRequestsUseSameTicketPolicy(t *testing.T) {
	for _, test := range []struct {
		name                string
		enabled, failClosed bool
		global              string
		accountPolicy       any
		harvestDisabled     bool
		modelDisabled       bool
		ticket              bool
		model               string
		allowed             bool
	}{
		{name: "default off and allow", allowed: true},
		{name: "global off bypasses all missing-ticket restrictions", failClosed: true, global: "false", accountPolicy: false, allowed: true},
		{name: "account excluded bypasses configured deny", enabled: true, failClosed: true, harvestDisabled: true, allowed: true},
		{name: "account excluded bypasses global deny", enabled: true, global: "false", harvestDisabled: true, allowed: true},
		{name: "account excluded bypasses explicit account deny", enabled: true, global: "false", accountPolicy: false, harvestDisabled: true, allowed: true},
		{name: "account excluded still injects saved ticket", enabled: true, failClosed: true, harvestDisabled: true, ticket: true, allowed: true},
		{name: "model excluded bypasses config deny", enabled: true, failClosed: true, modelDisabled: true, allowed: true},
		{name: "model excluded bypasses global deny", enabled: true, global: "false", modelDisabled: true, allowed: true},
		{name: "model excluded bypasses account deny", enabled: true, accountPolicy: false, modelDisabled: true, allowed: true},
		{name: "model excluded still injects saved ticket", enabled: true, failClosed: true, modelDisabled: true, ticket: true, allowed: true},
		{name: "enabled defaults allow", enabled: true, allowed: true},
		{name: "configured deny", enabled: true, failClosed: true},
		{name: "saved global allow", enabled: true, failClosed: true, global: "true", allowed: true},
		{name: "saved global deny", enabled: true, global: "false"},
		{name: "account allows over global deny", enabled: true, global: "false", accountPolicy: true, allowed: true},
		{name: "account denies over global allow", enabled: true, global: "true", accountPolicy: false},
		{name: "saved ticket still injected", enabled: true, failClosed: true, ticket: true, allowed: true},
		{name: "old model exempt", enabled: true, failClosed: true, model: "gpt-5.4", allowed: true},
	} {
		for _, advanced := range []string{"false", "true"} {
			t.Run(test.name+"/advanced="+advanced, func(t *testing.T) {
				resetOpenAIAdvancedSchedulerSettingCacheForTest()
				model := test.model
				if model == "" {
					model = "gpt-6-astra"
				}
				account := ticketTestAccount(41)
				account.Concurrency = 1
				account.Schedulable = true
				account.Extra = map[string]any{}
				if test.modelDisabled {
					account.Extra[codexTicketModelsEnabledKey] = map[string]bool{model: false}
				}
				if test.harvestDisabled {
					account.Extra[codexTicketAccountEnabledKey] = false
				}
				if test.accountPolicy != nil {
					account.Extra["codex_allow_without_ticket"] = test.accountPolicy
				}
				if test.ticket {
					account.Extra[openAICodexTicketExtraKey(model)] = verifiedTicket(account, model, "saved-state", "__oailb=saved")
				}
				cfg := &config.Config{}
				cfg.Gateway.OpenAICodexTicket = config.OpenAICodexTicketConfig{Enabled: test.enabled, FailClosed: test.failClosed}
				values := map[string]string{openAIAdvancedSchedulerSettingKey: advanced}
				if test.global != "" {
					values[SettingKeyOpenAICodexTicketAllowWithoutTicket] = test.global
				}
				svc := &OpenAIGatewayService{cfg: cfg, accountRepo: schedulerTestOpenAIAccountRepo{accounts: []Account{*account}}, cache: &schedulerTestGatewayCache{}, concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
					settingService: NewSettingService(&codexTicketSettingRepo{codexPolicyMigrationRepoStub: &codexPolicyMigrationRepoStub{values: values}}, cfg)}
				// Exercise the production lifecycle lookup, including SQL-normalized JSON null.
				raw, err := json.Marshal(account.Extra[openAICodexTicketExtraKey(model)])
				require.NoError(t, err)
				lookup := &codexTicketLookupStub{raw: raw}
				svc.openaiCodexTicketLifecycle = lookup
				for _, ctx := range []context.Context{context.Background(), WithCodexTicketDiagnostic(context.Background(), account.ID)} {
					selection, _, err := svc.SelectAccountWithScheduler(ctx, nil, "", "", model, nil, OpenAIUpstreamTransportAny, false)
					if !test.allowed {
						require.Error(t, err)
						require.Nil(t, selection)
					} else {
						require.NoError(t, err)
						require.Equal(t, account.ID, selection.Account.ID)
						if selection.ReleaseFunc != nil {
							selection.ReleaseFunc()
						}
					}
					headers := http.Header{}
					err = svc.applyOpenAICodexTicket(ctx, account, model, headers)
					if test.allowed {
						require.NoError(t, err)
					} else {
						require.ErrorIs(t, err, ErrOpenAICodexTicketUnavailable)
					}
					if test.ticket {
						require.Equal(t, "saved-state", headers.Get(openAICodexTurnStateHeader))
						require.Equal(t, "__oailb=saved", headers.Get("Cookie"))
					}
				}
				if test.enabled && svc.openAICodexTicketGatedModel(model) {
					require.Equal(t, 2, lookup.calls)
				} else {
					require.Zero(t, lookup.calls)
				}
			})
		}
	}
}

func TestCodexDiagnosticCannotFailOverToAnotherAccount(t *testing.T) {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()
	target, other := ticketTestAccount(41), ticketTestAccount(42)
	for _, account := range []*Account{target, other} {
		account.Schedulable = true
		account.Concurrency = 1
	}
	target.Extra = map[string]any{"codex_allow_without_ticket": false}
	other.Extra = map[string]any{"codex_allow_without_ticket": true}
	svc := newOpenAICompactionSchedulerTestService([]Account{*target, *other}, false)
	svc.cfg.Gateway.OpenAICodexTicket = config.OpenAICodexTicketConfig{Enabled: true, FailClosed: true}
	selection, _, err := svc.SelectAccountWithScheduler(WithCodexTicketDiagnostic(context.Background(), target.ID), nil, "", "", "gpt-6-astra", nil, OpenAIUpstreamTransportAny, false)
	require.Error(t, err)
	require.Nil(t, selection)
	selection, _, err = svc.SelectAccountWithScheduler(context.Background(), nil, "", "", "gpt-6-astra", nil, OpenAIUpstreamTransportAny, false)
	require.NoError(t, err)
	require.Equal(t, other.ID, selection.Account.ID)
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}
}

func TestCodexTicketLookupErrorDoesNotBecomeNoTicket(t *testing.T) {
	account := ticketTestAccount(41)
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true, FailClosed: false}, nil)
	lookup := &codexTicketLookupStub{err: errors.New("database unavailable")}
	svc.openaiCodexTicketLifecycle = lookup
	for _, ctx := range []context.Context{context.Background(), WithCodexTicketDiagnostic(context.Background(), account.ID)} {
		err := svc.applyOpenAICodexTicket(ctx, account, "gpt-6-astra", http.Header{})
		require.ErrorIs(t, err, lookup.err)
		require.NotErrorIs(t, err, ErrOpenAICodexTicketUnavailable)
	}
	require.Equal(t, 2, lookup.calls)
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
