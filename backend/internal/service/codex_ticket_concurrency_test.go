package service

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"os"
	"testing"
	"testing/synctest"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type waitingTicketAccountRepo struct {
	AccountRepository
	entered chan struct{}
	release chan struct{}
}

func (r *waitingTicketAccountRepo) GetByID(ctx context.Context, _ int64) (*Account, error) {
	r.entered <- struct{}{}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-r.release:
		return nil, ErrAccountNotFound
	}
}

func TestCodexTicketAttemptsShareBoundedDatabaseCapacity(t *testing.T) {
	for _, tc := range []struct{ pool, limit int }{{100, 4}, {4, 2}, {2, 1}} {
		t.Run(fmt.Sprintf("pool_%d", tc.pool), func(t *testing.T) {
			svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true}, nil)
			svc.cfg.Database.MaxOpenConns = tc.pool
			repo := &waitingTicketAccountRepo{entered: make(chan struct{}, tc.limit), release: make(chan struct{})}
			svc.accountRepo = repo
			done := make(chan error, tc.limit)
			for i := 0; i < tc.limit; i++ {
				go func(id int64) {
					_, err := svc.runCodexTicketAttempt(context.Background(), ticketTestAccount(id), "gpt-6-astra", "automatic")
					done <- err
				}(int64(i + 1))
			}
			for i := 0; i < tc.limit; i++ {
				select {
				case <-repo.entered:
				case <-time.After(5 * time.Second):
					t.Fatal("attempt failed to enter repository")
				}
			}
			_, err := svc.runCodexTicketAttempt(context.Background(), ticketTestAccount(99), "gpt-6-astra", "manual")
			require.ErrorIs(t, err, ErrCodexTicketBusy)
			close(repo.release)
			for i := 0; i < tc.limit; i++ {
				require.ErrorIs(t, <-done, ErrAccountNotFound)
			}
			require.Zero(t, svc.openaiCodexTicketActive.Load())
			_, err = svc.runCodexTicketAttempt(context.Background(), ticketTestAccount(99), "gpt-6-astra", "manual")
			require.ErrorIs(t, err, ErrAccountNotFound)
			require.Zero(t, svc.openaiCodexTicketActive.Load())
		})
	}
}

func TestCodexTicketDeadlineIncludesAccountLookup(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true, HarvestAttemptTimeoutSeconds: 1}, nil)
		svc.accountRepo = &waitingTicketAccountRepo{entered: make(chan struct{}, 1), release: make(chan struct{})}
		start := time.Now()
		_, err := svc.runCodexTicketAttempt(context.Background(), ticketTestAccount(1), "gpt-6-astra", "manual")
		require.ErrorIs(t, err, context.DeadlineExceeded)
		require.Equal(t, time.Second, time.Since(start))
		require.Zero(t, svc.openaiCodexTicketActive.Load())
	})
}

func TestCodexTicketRejectsSingleConnectionPool(t *testing.T) {
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true}, nil)
	svc.cfg.Database.MaxOpenConns = 1
	svc.accountRepo = &waitingTicketAccountRepo{}
	_, err := svc.runCodexTicketAttempt(context.Background(), ticketTestAccount(1), "gpt-6-astra", "manual")
	require.ErrorIs(t, err, ErrCodexTicketUnavailable)
	require.Contains(t, err.Error(), "at least 2")
	require.Zero(t, svc.openaiCodexTicketActive.Load())
}

func TestCancelledCodexTicketDoesNotReserveCapacity(t *testing.T) {
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true}, nil)
	svc.accountRepo = &waitingTicketAccountRepo{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := svc.runCodexTicketAttempt(ctx, ticketTestAccount(1), "gpt-6-astra", "manual")
	require.ErrorIs(t, err, context.Canceled)
	require.Zero(t, svc.openaiCodexTicketActive.Load())
}

type copiedTicketAccountRepo struct{ *challengeHarvestAccountRepo }

func (r *copiedTicketAccountRepo) GetByID(context.Context, int64) (*Account, error) {
	account := *r.account
	account.Extra = maps.Clone(account.Extra)
	account.Credentials = maps.Clone(account.Credentials)
	return &account, nil
}
func TestManualCodexTicketReturnsFreshPersistedStatus(t *testing.T) {
	raw, err := os.ReadFile("testdata/modeltrace_gpt_reference.json")
	require.NoError(t, err)
	var samples []struct{ Model, Text string }
	require.NoError(t, json.Unmarshal(raw, &samples))
	var output string
	for _, sample := range samples {
		if sample.Model == "gpt-6-astra" {
			output = sample.Text
		}
	}
	require.NotEmpty(t, output)
	upstream := &codexTicketFuncUpstream{do: func(*http.Request) (*http.Response, error) { return challengeHarvestResponse(t, output), nil }}
	svc, account, _ := challengeHarvestService(t, upstream)
	repo, ok := svc.accountRepo.(*challengeHarvestAccountRepo)
	require.True(t, ok)
	svc.accountRepo = &copiedTicketAccountRepo{repo}
	result, err := svc.ManualCodexTicketHarvest(context.Background(), account.ID, "gpt-6-astra")
	require.NoError(t, err)
	require.Equal(t, "success", result.Outcome)
	require.True(t, result.TicketStatus.Ready)
	require.NotNil(t, result.TicketGenerationID)
}

type rejectingTicketStore struct{ *challengeHarvestAccountRepo }

func (r *rejectingTicketStore) StoreCodexTicketIfUnchanged(context.Context, *Account, string, json.RawMessage) (bool, error) {
	return false, nil
}
func TestCodexTicketStaleProbeDoesNotEnterLocalCache(t *testing.T) {
	svc, account, _ := challengeHarvestService(t, nil)
	repo, ok := svc.accountRepo.(*challengeHarvestAccountRepo)
	require.True(t, ok)
	svc.accountRepo = &rejectingTicketStore{repo}
	ticket := &openAICodexTicket{Model: "gpt-6-astra", GenerationID: "new-generation", State: "new-state"}
	require.ErrorIs(t, svc.storeOpenAICodexTicket(context.Background(), account, ticket), ErrCodexTicketStale)
	_, cached := svc.openaiCodexTickets.Load(openAICodexTicketKey(account.ID, ticket.Model))
	require.False(t, cached)
	require.Empty(t, account.Extra[openAICodexTicketExtraKey(ticket.Model)])
}
