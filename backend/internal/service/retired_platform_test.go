package service

import (
	"context"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestRetiredPlatformCannotScheduleOrForward(t *testing.T) {
	account := &Account{Platform: "openai_bps", Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true}
	require.False(t, account.IsSchedulable())
	require.False(t, account.IsOpenAICompatible())
	// Empty service/context deliberately proves this exits before any upstream or DB call.
	result, err := (&OpenAIGatewayService{}).Forward(context.Background(), nil, account, nil)
	require.ErrorIs(t, err, ErrPlatformRetired)
	require.Nil(t, result)
}

func TestRetiredPlatformCannotCreate(t *testing.T) {
	_, groupErr := (&adminServiceImpl{}).CreateGroup(context.Background(), &CreateGroupInput{Platform: "openai_bps"})
	require.ErrorIs(t, groupErr, ErrPlatformRetired)
	_, err := buildAccountForCreate(&CreateAccountInput{Platform: "openai_bps", Type: AccountTypeOAuth}, nil)
	require.ErrorIs(t, err, ErrPlatformRetired)
	_, err = (&AccountService{}).Create(context.Background(), CreateAccountRequest{Platform: "openai_bps"})
	require.ErrorIs(t, err, ErrPlatformRetired)
}

func TestRetirementPreservesOpenAIOAuthExcelBPS(t *testing.T) {
	account, err := buildAccountForCreate(&CreateAccountInput{Platform: PlatformOpenAI, Type: AccountTypeOAuth}, map[string]any{"openai_excel_bps": true})
	require.NoError(t, err)
	require.True(t, account.IsOpenAICompatible())
	require.True(t, account.IsSchedulable())
	require.True(t, account.IsExcelBPSEnabledForModel("gpt-5"))
}

func TestRetiredCompositeRouteDoesNotFallBackToNativeOpenAI(t *testing.T) {
	for _, platform := range []string{"openai_bps", PlatformOpenAI} {
		t.Run(platform, func(t *testing.T) {
			resolver := NewCompositeRouteResolver(compositeRouteRepoStub{routes: []CompositeModelRoute{{GroupID: 7, PublicModel: "gpt-5", MatchType: CompositeRouteMatchExact, Endpoint: CompositeRouteEndpointAny, TargetPlatform: platform, Enabled: true}}})
			decision, err := resolver.Resolve(context.Background(), 7, "gpt-5", CompositeRouteEndpointResponses)
			if IsRetiredPlatform(platform) {
				require.ErrorIs(t, err, ErrPlatformRetired)
				require.False(t, decision.Matched)
			} else {
				require.NoError(t, err)
				require.True(t, decision.Matched)
				require.Equal(t, PlatformOpenAI, decision.TargetPlatform)
			}
		})
	}
}

type retiredGroupRepoStub struct {
	GroupRepository
	platform string
}

func (r retiredGroupRepoStub) GetByID(context.Context, int64) (*Group, error) {
	return &Group{Platform: r.platform}, nil
}
func TestRetiredGroupCannotReactivateOrConvert(t *testing.T) {
	for _, tc := range []struct{ current, target string }{{"openai_bps", ""}, {"openai_bps", PlatformOpenAI}, {PlatformOpenAI, "openai_bps"}} {
		svc := &adminServiceImpl{groupRepo: retiredGroupRepoStub{platform: tc.current}}
		_, err := svc.UpdateGroup(context.Background(), 1, &UpdateGroupInput{Platform: tc.target, Status: StatusActive})
		require.ErrorIs(t, err, ErrPlatformRetired)
	}
}
