//go:build unit

package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/sessionstore"
)

type lifecycleCachedContentRepo struct {
	GeminiCachedContentRepository
	record                    *GeminiCachedContent
	claims, released, deleted atomic.Int64
}

func (r *lifecycleCachedContentRepo) ListStaleMutations(context.Context, int) ([]int64, error) {
	return []int64{r.record.ID}, nil
}
func (r *lifecycleCachedContentRepo) TakeOverStaleMutation(context.Context, int64, string, time.Duration) (*GeminiCachedContent, error) {
	r.claims.Add(1)
	return r.record, nil
}
func (r *lifecycleCachedContentRepo) ReleaseMutation(context.Context, int64, string) error {
	r.released.Add(1)
	return nil
}
func (r *lifecycleCachedContentRepo) SoftDelete(context.Context, int64) error {
	r.deleted.Add(1)
	return nil
}

type lifecycleCachedAccountRepo struct {
	AccountRepository
	account *Account
}

func (r *lifecycleCachedAccountRepo) GetByID(context.Context, int64) (*Account, error) {
	return r.account, nil
}

type lifecycleCachedUpstream struct {
	HTTPUpstream
	entered, canceled, release chan struct{}
}

func (u *lifecycleCachedUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	close(u.entered)
	<-req.Context().Done()
	close(u.canceled)
	<-u.release
	return nil, req.Context().Err()
}

func TestGeminiCachedContentRecoveryStopJoinsAndRetainsIntent(t *testing.T) {
	repo := &lifecycleCachedContentRepo{record: &GeminiCachedContent{ID: 1, AccountID: 7, UpstreamName: "cachedContents/fixture", ExpireTime: time.Now().Add(time.Hour)}}
	account := &Account{ID: 7, Type: AccountTypeAPIKey, Platform: PlatformGemini, Credentials: map[string]any{"api_key": "fixture"}}
	up := &lifecycleCachedUpstream{entered: make(chan struct{}), canceled: make(chan struct{}), release: make(chan struct{})}
	s := NewGeminiCachedContentService(repo, &GeminiMessagesCompatService{
		httpUpstream: up, accountRepo: &lifecycleCachedAccountRepo{account: account}, cfg: &config.Config{},
	}, &config.Config{})
	if _, err := s.RecoverStaleMutations(context.Background()); !errors.Is(err, sessionstore.ErrClosed) {
		t.Fatal("recovery admitted before Start", err)
	}
	if repo.claims.Load() != 0 {
		t.Fatal("constructor recovered a mutation")
	}
	// Exercise the real periodic consumer with a short test-only interval.
	s.recovery = sessionstore.NewLifecycle(time.Millisecond, s.runMutationRecovery)
	startSessionOwnerTest(t, s)
	select {
	case <-up.entered:
	case <-time.After(time.Second):
		t.Fatal("recovery did not reach upstream")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := s.Stop(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("unreturned dependency reported joined", err)
	}
	select {
	case <-up.canceled:
	case <-time.After(time.Second):
		t.Fatal("recovery cancellation detached by upstream helper")
	}
	if _, err := s.RecoverStaleMutations(context.Background()); !errors.Is(err, sessionstore.ErrClosed) {
		t.Fatal(err)
	}
	close(up.release)
	joined, done := context.WithTimeout(context.Background(), time.Second)
	defer done()
	if err := s.Stop(joined); err != nil {
		t.Fatal(err)
	}
	if repo.claims.Load() != 1 || repo.released.Load() != 0 || repo.deleted.Load() != 0 {
		t.Fatalf("canceled recovery changed durable intent: claims=%d released=%d deleted=%d", repo.claims.Load(), repo.released.Load(), repo.deleted.Load())
	}
}

func TestGeminiCachedContentStopCancelsDetachedClientWrite(t *testing.T) {
	up := &lifecycleCachedUpstream{entered: make(chan struct{}), canceled: make(chan struct{}), release: make(chan struct{})}
	s := NewGeminiCachedContentService(&lifecycleCachedContentRepo{}, &GeminiMessagesCompatService{httpUpstream: up, cfg: &config.Config{}}, &config.Config{})
	startSessionOwnerTest(t, s)
	request, err := ParseGeminiCachedContentCreateRequest([]byte(`{"model":"gemini-3.8-flash"}`), time.Now(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	client, disconnect := context.WithCancel(context.Background())
	disconnect()
	result := make(chan error, 1)
	go func() {
		_, err := s.CreateUpstream(client, &Account{ID: 7, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "fixture"}}, request, "gemini-3.8-flash", "fixture", time.Minute)
		result <- err
	}()
	<-up.entered
	select {
	case <-up.canceled:
		t.Fatal("client cancellation aborted write")
	default:
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := s.Drain(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	select {
	case <-up.canceled:
		t.Fatal("drain canceled accepted write")
	default:
	}
	stopCtx, cancelStop := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancelStop()
	if err := s.Stop(stopCtx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	<-up.canceled
	close(up.release)
	joined, done := context.WithTimeout(context.Background(), time.Second)
	defer done()
	if err := s.Stop(joined); err != nil {
		t.Fatal(err)
	}
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

type lifecycleCloseBody struct {
	io.Reader
	entered, release chan struct{}
	err              error
}

func (b *lifecycleCloseBody) Close() error {
	close(b.entered)
	<-b.release
	return b.err
}

type lifecycleCloseUpstream struct {
	HTTPUpstream
	body io.ReadCloser
}

func (u *lifecycleCloseUpstream) Do(*http.Request, string, int64, int) (*http.Response, error) {
	return &http.Response{StatusCode: http.StatusOK, Body: u.body}, nil
}

func TestGeminiCachedContentStopReturnsCloseFailureAfterRealJoin(t *testing.T) {
	closeErr := errors.New("fixture response close failed")
	body := &lifecycleCloseBody{
		Reader:  strings.NewReader(`{"name":"cachedContents/fixture","usageMetadata":{"totalTokenCount":1}}`),
		entered: make(chan struct{}), release: make(chan struct{}), err: closeErr,
	}
	s := NewGeminiCachedContentService(&lifecycleCachedContentRepo{}, &GeminiMessagesCompatService{
		httpUpstream: &lifecycleCloseUpstream{body: body}, cfg: &config.Config{},
	}, &config.Config{})
	if err := s.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	request, err := ParseGeminiCachedContentCreateRequest([]byte(`{"model":"gemini-3.8-flash"}`), time.Now(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		_, err := s.CreateUpstream(context.Background(), &Account{ID: 7, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "fixture"}}, request, "gemini-3.8-flash", "fixture", time.Minute)
		result <- err
	}()
	<-body.entered
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := s.Stop(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	close(body.release)
	joined, done := context.WithTimeout(context.Background(), time.Second)
	defer done()
	if err := s.Stop(joined); !errors.Is(err, closeErr) {
		t.Fatal("lost close error", err)
	}
	if err := <-result; err != nil {
		t.Fatal("close failure changed successful upstream business result", err)
	}
	if err := s.Stop(joined); !errors.Is(err, closeErr) {
		t.Fatal("retry lost recorded close error", err)
	}
}
