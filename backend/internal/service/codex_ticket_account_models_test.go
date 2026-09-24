package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestCodexTicketModelsFollowAccountRestriction(t *testing.T) {
	bank, err := ModelTraceTicketModels()
	require.NoError(t, err)
	for _, tc := range []struct {
		name    string
		mapping map[string]any
		want    []string
	}{
		{name: "unrestricted", want: bank},
		{name: "empty whitelist", mapping: map[string]any{}, want: bank},
		{name: "whitelist", mapping: map[string]any{"gpt-6-astra": "gpt-6-astra"}, want: []string{"gpt-6-astra"}},
		{name: "aliases deduplicated", mapping: map[string]any{"custom": "gpt-6-astra", "gpt-6-sol": "gpt-6-astra"}, want: []string{"gpt-6-astra"}},
		{name: "wildcard", mapping: map[string]any{"gpt-6-*": "gpt-6-*"}, want: []string{"gpt-6-astra", "gpt-6-sol", "gpt-6-luna"}},
		{name: "wildcard mapped target", mapping: map[string]any{"gpt-*": "gpt-6-astra"}, want: []string{"gpt-6-astra"}},
		{name: "exact override", mapping: map[string]any{"gpt-6-*": "gpt-6-*", "gpt-6-sol": "gpt-6-astra"}, want: []string{"gpt-6-astra", "gpt-6-luna"}},
		{name: "unsupported fingerprint models", mapping: map[string]any{"gpt-image-2.5-flare": "gpt-image-2.5-flare", "custom": "custom"}, want: []string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			account := ticketTestAccount(71)
			account.Credentials["model_mapping"] = tc.mapping
			statuses := OpenAICodexTicketStatuses(account, config.OpenAICodexTicketConfig{Enabled: true}, time.Now())
			got := make([]string, 0, len(statuses))
			for _, status := range statuses {
				got = append(got, status.Model)
				require.True(t, status.HarvestEnabled)
			}
			require.ElementsMatch(t, tc.want, got)
			for _, model := range bank {
				require.Equal(t, containsTicketModel(tc.want, model), CodexTicketHarvestEnabled(account, model), model)
			}
		})
	}
}

func containsTicketModel(models []string, model string) bool {
	for _, item := range models {
		if item == model {
			return true
		}
	}
	return false
}

func TestCodexTicketRestrictionRefreshPreservesStoredTicketAndChoices(t *testing.T) {
	account := ticketTestAccount(71)
	ticket := verifiedTicket(account, "gpt-6-astra", "saved-ticket", "cookie")
	account.Extra = map[string]any{
		openAICodexTicketExtraKey(ticket.Model): ticket,
		codexTicketModelsEnabledKey:             map[string]any{"gpt-6-astra": false},
	}
	mapping := map[string]any{"gpt-6-astra": "gpt-6-astra"}
	account.Credentials["model_mapping"] = mapping
	cfg := config.OpenAICodexTicketConfig{Enabled: true}
	require.True(t, OpenAICodexTicketStatuses(account, cfg, time.Now())[0].Ready)
	delete(mapping, "gpt-6-astra")
	mapping["gpt-6-sol"] = "gpt-6-sol"
	statuses := OpenAICodexTicketStatuses(account, cfg, time.Now())
	require.Len(t, statuses, 1)
	require.Equal(t, "gpt-6-sol", statuses[0].Model)
	require.False(t, CodexTicketHarvestEnabled(account, "gpt-6-astra"))
	require.Same(t, ticket, account.Extra[openAICodexTicketExtraKey(ticket.Model)])
	delete(mapping, "gpt-6-sol")
	mapping["gpt-6-astra"] = "gpt-6-astra"
	statuses = OpenAICodexTicketStatuses(account, cfg, time.Now())
	require.True(t, statuses[0].Ready)
	require.False(t, statuses[0].HarvestEnabled)
}

func TestCodexTicketManualHarvestRejectsRestrictedModel(t *testing.T) {
	account := ticketTestAccount(71)
	account.Credentials["model_mapping"] = map[string]any{"gpt-6-sol": "gpt-6-sol"}
	upstream := &httpUpstreamRecorder{}
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true}, upstream)
	svc.accountRepo = &codexParticipationRepo{account: account}
	_, err := svc.ManualCodexTicketHarvest(context.Background(), account.ID, "gpt-6-astra")
	require.ErrorIs(t, err, ErrCodexTicketUnavailable)
	require.Empty(t, upstream.requests)
}
