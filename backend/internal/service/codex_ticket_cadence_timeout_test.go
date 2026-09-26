package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type codexCadenceBlockingSettings struct {
	SettingRepository
	deadline time.Time
	calls    int
}

func (repo *codexCadenceBlockingSettings) GetValue(ctx context.Context, _ string) (string, error) {
	repo.calls++
	repo.deadline, _ = ctx.Deadline()
	<-ctx.Done()
	return "", ctx.Err()
}

func TestCodexTicketCadenceBackgroundReadIsBounded(t *testing.T) {
	repo := &codexCadenceBlockingSettings{}
	svc := &OpenAIGatewayService{
		cfg: &config.Config{Gateway: config.GatewayConfig{OpenAICodexTicket: config.OpenAICodexTicketConfig{
			HarvestRetryMinSeconds: 11,
			HarvestRetryMaxSeconds: 23,
			HarvestRefreshSeconds:  1700,
		}}},
		settingService: &SettingService{settingRepo: repo},
	}
	start := time.Now()
	value := svc.codexTicketCadence(context.Background())
	require.Equal(t, CodexTicketCadence{RetryMinSeconds: 11, RetryMaxSeconds: 23, RefreshSeconds: 1700}, value)
	require.Equal(t, 1, repo.calls)
	require.False(t, repo.deadline.IsZero(), "background setting reads must carry a deadline")
	require.WithinDuration(t, start.Add(2*time.Second), repo.deadline, 250*time.Millisecond)
	require.Less(t, time.Since(start), 3*time.Second, "retry scheduling must return its configured fallback")
}

func TestCodexTicketCadenceReadHonorsEarlierCancellation(t *testing.T) {
	repo := &codexCadenceBlockingSettings{}
	settings := &SettingService{settingRepo: repo}
	fallback := CodexTicketCadence{RetryMinSeconds: 10, RetryMaxSeconds: 30}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	expectedDeadline, _ := ctx.Deadline()
	value, err := settings.GetCodexTicketCadence(ctx, fallback)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Equal(t, fallback, value)
	require.Equal(t, expectedDeadline, repo.deadline)
}

func TestCodexTicketCadenceCacheAvoidsDatabaseWait(t *testing.T) {
	repo := &codexCadenceBlockingSettings{}
	settings := &SettingService{settingRepo: repo}
	cached := CodexTicketCadence{RetryMinSeconds: 4, RetryMaxSeconds: 9, RefreshSeconds: 300}
	settings.codexTicketCadenceCache.Store(&cachedCodexTicketCadence{value: cached, expiresAt: time.Now().Add(time.Minute)})
	value, err := settings.GetCodexTicketCadence(context.Background(), CodexTicketCadence{})
	require.NoError(t, err)
	require.Equal(t, cached, value)
	require.Zero(t, repo.calls)
}
