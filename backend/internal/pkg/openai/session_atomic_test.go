package openai

import (
	"context"
	"github.com/stretchr/testify/require"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSessionConsumeHasOneWinner(t *testing.T) {
	s := NewSessionStore()
	defer s.Stop(context.Background())
	value := &OAuthSession{CreatedAt: time.Now()}
	s.Set("id", value)
	var n atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if s.Consume("id", value) {
				n.Add(1)
			}
		}()
	}
	wg.Wait()
	require.Equal(t, int32(1), n.Load())
}
