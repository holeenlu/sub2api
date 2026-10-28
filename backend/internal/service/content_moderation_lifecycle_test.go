//go:build unit

package service

import (
	"context"
	"errors"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func lifecycleModerationService(t *testing.T) (*ContentModerationService, *contentModerationRuntimeSettingRepo) {
	t.Helper()
	settings := &contentModerationRuntimeSettingRepo{values: map[string]string{SettingKeyContentModerationConfig: runtimeCacheTestConfig(t)}}
	s := NewContentModerationService(settings, &contentModerationTestRepo{}, nil, nil, nil, nil, nil, nil)
	s.workerCount = 1
	return s, settings
}
func TestModerationLifecycleDrainPreservesAcceptedRecords(t *testing.T) {
	s, _ := lifecycleModerationService(t)
	require.False(t, s.lifecycleStarted)
	require.Nil(t, s.lifecycleDone)
	for i := 0; i < 3; i++ {
		s.enqueueRecord(ContentModerationCheckInput{}, nil, &ContentModerationLog{RequestID: "accepted"}, "", false, false)
	}
	require.NoError(t, s.Start(context.Background()))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, s.Drain(ctx))
	require.EqualValues(t, 3, s.asyncProcessed.Load())
	s.enqueueRecord(ContentModerationCheckInput{}, nil, &ContentModerationLog{}, "", false, false)
	require.Empty(t, s.asyncQueue)
	require.EqualValues(t, 1, s.asyncDropped.Load())
	require.NoError(t, s.Stop(ctx))
	require.Error(t, s.Start(context.Background()))
}
func TestModerationLifecycleFailedConfigRetainsQueueAndBoundedJoin(t *testing.T) {
	s, settings := lifecycleModerationService(t)
	settings.failMultiple(errors.New("unavailable"))
	s.enqueueRecord(ContentModerationCheckInput{}, nil, &ContentModerationLog{}, "", false, false)
	require.NoError(t, s.Start(context.Background()))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, s.Drain(ctx), context.DeadlineExceeded)
	require.Len(t, s.asyncQueue, 1)
	bounded, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	require.ErrorContains(t, s.Stop(bounded), "unfinished")
	select {
	case <-s.lifecycleDone:
	default:
		t.Fatal("workers/cleanup not joined")
	}
	require.Zero(t, s.asyncProcessed.Load())
}
