//go:build unit

package service

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type accountRepoStubForCompositeModelsList struct {
	accountRepoStub
	accounts []Account
}

func (s *accountRepoStubForCompositeModelsList) ListSchedulableByGroupID(_ context.Context, _ int64) ([]Account, error) {
	return s.accounts, nil
}

func TestAdminService_CreateCompositeGroupCopiesAccountsFromConcreteGroups(t *testing.T) {
	var copiedFrom []int64
	var boundGroupID int64
	var boundAccountIDs []int64
	groupRepo := &groupRepoStubForAdmin{
		createID: 99,
		getByIDByID: map[int64]*Group{
			10: {ID: 10, Platform: PlatformOpenAI},
			20: {ID: 20, Platform: PlatformGemini},
		},
		getAccountIDsByGroupIDsFn: func(groupIDs []int64) ([]int64, error) {
			copiedFrom = append([]int64{}, groupIDs...)
			return []int64{101, 202}, nil
		},
		bindAccountsToGroupFn: func(groupID int64, accountIDs []int64) error {
			boundGroupID = groupID
			boundAccountIDs = append([]int64{}, accountIDs...)
			return nil
		},
	}
	svc := &adminServiceImpl{groupRepo: groupRepo}

	group, err := svc.CreateGroup(context.Background(), &CreateGroupInput{
		Name:                        "Composite",
		Platform:                    PlatformComposite,
		RateMultiplier:              1,
		MaxReasoningEffort:          "medium",
		MaxReasoningEffortOverLimit: ReasoningEffortOverLimitDeny,
		ReasoningEffortMappings: []ReasoningEffortMapping{
			{From: "max", To: "xhigh"},
		},
		CopyAccountsFromGroupIDs: []int64{10, 20, 10},
	})

	require.NoError(t, err)
	require.Equal(t, PlatformComposite, groupRepo.created.Platform)
	require.Equal(t, "medium", groupRepo.created.MaxReasoningEffort)
	require.Equal(t, ReasoningEffortOverLimitDeny, groupRepo.created.MaxReasoningEffortOverLimit)
	require.Equal(t, []ReasoningEffortMapping{{From: "max", To: "xhigh"}}, groupRepo.created.ReasoningEffortMappings)
	require.Equal(t, int64(99), group.ID)
	require.Equal(t, int64(2), group.AccountCount)
	require.ElementsMatch(t, []int64{10, 20}, copiedFrom)
	require.Equal(t, int64(99), boundGroupID)
	require.ElementsMatch(t, []int64{101, 202}, boundAccountIDs)
}

func TestAdminService_UpdateCompositeGroupCopiesAccountsFromConcreteGroups(t *testing.T) {
	var clearedGroupID int64
	var copiedFrom []int64
	var boundGroupID int64
	var boundAccountIDs []int64
	groupRepo := &groupRepoStubForAdmin{
		getByIDByID: map[int64]*Group{
			10: {ID: 10, Platform: PlatformOpenAI},
			20: {ID: 20, Platform: PlatformGrok},
			99: {ID: 99, Platform: PlatformComposite, RateMultiplier: 1, SubscriptionType: SubscriptionTypeStandard},
		},
		deleteAccountGroupsByGroupIDFn: func(groupID int64) (int64, error) {
			clearedGroupID = groupID
			return 2, nil
		},
		getAccountIDsByGroupIDsFn: func(groupIDs []int64) ([]int64, error) {
			copiedFrom = append([]int64{}, groupIDs...)
			return []int64{301, 302}, nil
		},
		bindAccountsToGroupFn: func(groupID int64, accountIDs []int64) error {
			boundGroupID = groupID
			boundAccountIDs = append([]int64{}, accountIDs...)
			return nil
		},
	}
	svc := &adminServiceImpl{groupRepo: groupRepo}
	maxReasoningEffort := "low"
	maxReasoningEffortOverLimit := ReasoningEffortOverLimitDeny
	reasoningEffortMappings := []ReasoningEffortMapping{{From: "max", To: "high"}}

	group, err := svc.UpdateGroup(context.Background(), 99, &UpdateGroupInput{
		MaxReasoningEffort:          &maxReasoningEffort,
		MaxReasoningEffortOverLimit: &maxReasoningEffortOverLimit,
		ReasoningEffortMappings:     &reasoningEffortMappings,
		CopyAccountsFromGroupIDs:    []int64{10, 20},
	})

	require.NoError(t, err)
	require.Equal(t, PlatformComposite, group.Platform)
	require.Equal(t, "low", group.MaxReasoningEffort)
	require.Equal(t, ReasoningEffortOverLimitDeny, group.MaxReasoningEffortOverLimit)
	require.Equal(t, reasoningEffortMappings, group.ReasoningEffortMappings)
	require.Equal(t, int64(99), clearedGroupID)
	require.ElementsMatch(t, []int64{10, 20}, copiedFrom)
	require.Equal(t, int64(99), boundGroupID)
	require.ElementsMatch(t, []int64{301, 302}, boundAccountIDs)
}

func TestAdminService_CreateAccountAllowsCompositeGroupAssignment(t *testing.T) {
	accountRepo := &accountRepoStubForBulkUpdate{createID: 7}
	groupRepo := &groupRepoStubForAdmin{
		getByIDByID: map[int64]*Group{
			99: {ID: 99, Platform: PlatformComposite},
		},
	}
	svc := &adminServiceImpl{accountRepo: accountRepo, groupRepo: groupRepo}

	account, err := svc.CreateAccount(context.Background(), &CreateAccountInput{
		Name:                  "OpenAI account",
		Platform:              PlatformOpenAI,
		Type:                  AccountTypeAPIKey,
		Concurrency:           1,
		GroupIDs:              []int64{99},
		SkipDefaultGroupBind:  true,
		SkipMixedChannelCheck: true,
	})

	require.NoError(t, err)
	require.Equal(t, int64(7), account.ID)
	require.Equal(t, PlatformOpenAI, accountRepo.createAccount.Platform)
	require.ElementsMatch(t, []int64{99}, accountRepo.bindGroupsByAccount[7])
}

func TestAdminService_UpdateAccountAllowsCompositeGroupAssignment(t *testing.T) {
	accountRepo := &accountRepoStubForBulkUpdate{
		getByIDAccounts: map[int64]*Account{
			7: {ID: 7, Platform: PlatformGemini, Type: AccountTypeAPIKey, Status: StatusActive, Extra: map[string]any{}},
		},
	}
	groupRepo := &groupRepoStubForAdmin{
		getByIDByID: map[int64]*Group{
			99: {ID: 99, Platform: PlatformComposite},
		},
	}
	svc := &adminServiceImpl{accountRepo: accountRepo, groupRepo: groupRepo}
	groupIDs := []int64{99}

	account, err := svc.UpdateAccount(context.Background(), 7, &UpdateAccountInput{
		GroupIDs:              &groupIDs,
		SkipMixedChannelCheck: true,
	})

	require.NoError(t, err)
	require.Equal(t, int64(7), account.ID)
	require.Len(t, accountRepo.updatedAccounts, 1)
	require.ElementsMatch(t, []int64{99}, accountRepo.bindGroupsByAccount[7])
}

func TestAdminService_CompositeModelsListCandidatesIncludeConcreteAccountMappings(t *testing.T) {
	accountRepo := &accountRepoStubForCompositeModelsList{
		accounts: []Account{
			{
				ID:       1,
				Platform: PlatformOpenAI,
				Credentials: map[string]any{
					"model_mapping": map[string]any{"gpt-custom": "gpt-5"},
				},
			},
			{
				ID:       2,
				Platform: PlatformGemini,
				Credentials: map[string]any{
					"model_mapping": map[string]any{"gemini-custom": "gemini-2.5-flash"},
				},
			},
			{
				ID:       3,
				Platform: PlatformKimi,
				Credentials: map[string]any{
					"model_mapping": map[string]any{"kimi-custom": "kimi-k2"},
				},
			},
		},
	}
	groupRepo := &groupRepoStubForAdmin{
		getByIDByID: map[int64]*Group{
			99: {ID: 99, Platform: PlatformComposite},
		},
	}
	svc := &adminServiceImpl{accountRepo: accountRepo, groupRepo: groupRepo}

	candidates, err := svc.GetGroupModelsListCandidates(context.Background(), 99, PlatformComposite)

	require.NoError(t, err)
	require.Contains(t, candidates, "gpt-custom")
	require.Contains(t, candidates, "gemini-custom")
	require.Contains(t, candidates, "kimi-custom")
	require.Contains(t, candidates, "gpt-6.1-sol")
	require.Contains(t, candidates, "gemini-3.8-flash")
	require.Contains(t, candidates, "kimi-k3")
	require.Contains(t, candidates, "glm-5.3")
	require.Contains(t, candidates, "deepseek-flash")
	require.Contains(t, candidates, "MiniMax-M3")
	require.NotContains(t, candidates, "gemini-2.0-flash")
	require.NotContains(t, candidates, "jev-latest")
}

func TestAdminService_GroupModelsListCandidatesAreCurrentAndProviderSpecific(t *testing.T) {
	cases := []struct {
		platform string
		current  string
		omitted  string
	}{
		{PlatformAnthropic, "claude-haiku-5-5", "claude-sonnet-4-5-20250929"},
		{PlatformOpenAI, "gpt-6.1-sol", "gpt-image-1"},
		{PlatformGemini, "gemini-3.8-flash", "gemini-3-pro-preview"},
		{PlatformAntigravity, "gemini-3.8-flash-high", "gemini-2.5-flash-image-preview"},
		{PlatformGrok, "grok-4.7", "grok-4.20-0309-reasoning"},
		{PlatformKimi, "kimi-k3", "claude-haiku-5-5"},
		{PlatformZhipu, "glm-5.3", "claude-haiku-5-5"},
		{PlatformDeepseek, "deepseek-flash", "claude-haiku-5-5"},
		{PlatformMiniMax, "MiniMax-M3", "claude-haiku-5-5"},
		{PlatformOpenCodeGo, "deepseek-v4.1-flash", "omen-alpha"},
	}
	svc := &adminServiceImpl{}
	for _, tc := range cases {
		t.Run(tc.platform, func(t *testing.T) {
			ids, err := svc.GetGroupModelsListCandidates(context.Background(), 0, tc.platform)
			require.NoError(t, err)
			require.Contains(t, ids, tc.current)
			require.NotContains(t, ids, tc.omitted)
			seen := make(map[string]bool)
			for _, id := range ids {
				require.NotEmpty(t, id)
				require.Equal(t, strings.TrimSpace(id), id)
				require.False(t, seen[id], "duplicate candidate %s", id)
				seen[id] = true
			}
		})
	}
	// Coding-plan IDs differ from the same vendor's pay-as-you-go IDs.
	ids, err := svc.GetGroupModelsListCandidates(context.Background(), 0, PlatformKimi)
	require.NoError(t, err)
	require.Contains(t, ids, "kimi-for-coding")
	require.Contains(t, ids, "k3-256k")
	for _, platform := range []string{PlatformCommandCode, PlatformCline} {
		ids, err := svc.GetGroupModelsListCandidates(context.Background(), 0, platform)
		require.NoError(t, err)
		require.Empty(t, ids, "aggregators without curated candidates must not inherit Claude models")
	}
}

func TestAdminService_GroupModelsListCandidatesPreserveExplicitMappingsOnly(t *testing.T) {
	for _, platform := range []string{PlatformAntigravity, PlatformGrok} {
		t.Run(platform, func(t *testing.T) {
			mapping := map[string]any{
				" custom-model ":   "upstream-model",
				"gemini-2.0-flash": "upstream-model",
				"my-model-*":       "upstream-model",
				"invalid-model":    42,
			}
			accounts := &accountRepoStubForCompositeModelsList{accounts: []Account{
				{ID: 1, Platform: platform}, // Runtime defaults must not repopulate stale candidates.
				{ID: 2, Platform: platform, Credentials: map[string]any{"model_mapping": mapping}},
				{ID: 3, Platform: PlatformKimi, Credentials: map[string]any{
					"model_mapping": map[string]any{"wrong-platform": "kimi-k3"},
				}},
			}}
			svc := &adminServiceImpl{accountRepo: accounts, groupRepo: &groupRepoStubForAdmin{
				getByIDByID: map[int64]*Group{99: {ID: 99, Platform: platform}},
			}}
			ids, err := svc.GetGroupModelsListCandidates(context.Background(), 99, platform)
			require.NoError(t, err)
			require.Len(t, ids, len(defaultModelsListCandidateIDs(platform))+3)
			require.Contains(t, ids, "custom-model")
			require.Contains(t, ids, "gemini-2.0-flash")
			require.Contains(t, ids, "my-model-*")
			require.NotContains(t, ids, "wrong-platform")
			require.NotContains(t, ids, "invalid-model")
			require.NotContains(t, ids, "gpt-*")
			require.Len(t, mapping, 4)
		})
	}
}
