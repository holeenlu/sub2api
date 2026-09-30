//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestSecurityMediaDurableOwnerAndCapacity(t *testing.T) {
	ctx := context.Background()
	repo := NewGatewayMediaRepository(integrationDB)
	user := time.Now().UnixNano()
	group := int64(10)
	t.Cleanup(func() {
		_, _ = integrationDB.Exec(`DELETE FROM gateway_media_jobs WHERE user_id=$1`, user)
		_, _ = integrationDB.Exec(`DELETE FROM gateway_media_voices WHERE user_id=$1`, user)
	})
	v := &service.GatewayMediaVoice{ID: fmt.Sprintf("voice-%d", user), UserID: user, GroupID: group, AccountID: 77, Metadata: json.RawMessage(`{"voice_id":"owned"}`)}
	require.NoError(t, repo.PutVoice(ctx, v))
	restarted := NewGatewayMediaRepository(integrationDB)
	got, err := restarted.GetVoice(ctx, group, user, v.ID)
	require.NoError(t, err)
	require.EqualValues(t, 77, got.AccountID)
	_, err = restarted.GetVoice(ctx, group, user+1, v.ID)
	require.ErrorIs(t, err, service.ErrMediaNotOwned)
	stolen := *v
	stolen.UserID++
	require.ErrorIs(t, repo.PutVoice(ctx, &stolen), service.ErrMediaNotOwned)
	list, err := repo.ListVoices(ctx, group, user+1)
	require.NoError(t, err)
	require.Empty(t, list)
	var wins atomic.Int32
	var wg sync.WaitGroup
	winner := make(chan *service.GatewayMediaJob, 1)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			j := &service.GatewayMediaJob{ID: fmt.Sprintf("media-%d-%d", user, i), UserID: user, GroupID: int64(i), AccountID: 77, State: "reserving", Key: &service.APIKey{ID: int64(i + 1)}}
			err := repo.CreateJob(ctx, j)
			if err == nil {
				wins.Add(1)
				winner <- j
			} else {
				require.ErrorIs(t, err, service.ErrMediaPendingCapacity)
			}
		}(i)
	}
	wg.Wait()
	require.EqualValues(t, 1, wins.Load(), "capacity is atomic across keys and groups")
	j := <-winner
	j.State = "pending"
	j.TaskID = fmt.Sprintf("task-%d", user)
	require.NoError(t, repo.SaveJob(ctx, j))
	stale := *j
	_, err = integrationDB.Exec(`UPDATE gateway_media_jobs SET next_attempt_at=now()-interval '1 minute' WHERE id=$1`, j.ID)
	require.NoError(t, err)
	jobs, err := restarted.ClaimJobs(ctx, 1)
	require.NoError(t, err)
	require.Len(t, jobs, 1)
	require.Equal(t, j.ID, jobs[0].ID)
	next, err := repo.ClaimJobs(ctx, 1)
	require.NoError(t, err)
	require.Empty(t, next, "restart/parallel worker cannot share a lease")
	stale.State = "billing"
	require.Error(t, repo.SaveJob(ctx, &stale), "old worker must be fenced")
	claimed := jobs[0]
	claimed.State = "settled"
	require.NoError(t, repo.SaveJob(ctx, &claimed))
	followup := &service.GatewayMediaJob{ID: fmt.Sprintf("next-%d", user), UserID: user, State: "reserving", Key: &service.APIKey{ID: 99}}
	require.NoError(t, repo.CreateJob(ctx, followup), "capacity is released only after terminal settlement")
}
