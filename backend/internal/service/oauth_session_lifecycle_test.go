package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/oauth"
	"github.com/Wei-Shaw/sub2api/internal/pkg/sessionstore"
)

type sessionOwnerTest interface {
	Start(context.Context) error
	Drain(context.Context) error
	Stop(context.Context) error
}

func startSessionOwnerTest(t *testing.T, owner sessionOwnerTest) {
	t.Helper()
	if err := owner.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stopSessionOwnerTest(t, owner) })
}

// Each phase gets its own bounded join. A Drain timeout fails the test and does
// not skip Stop, which must still cancel and join the admitted work.
func stopSessionOwnerTest(t *testing.T, owner sessionOwnerTest) {
	t.Helper()
	drainCtx, cancelDrain := context.WithTimeout(context.Background(), time.Second)
	err := owner.Drain(drainCtx)
	cancelDrain()
	if err != nil {
		t.Errorf("session owner Drain: %v", err)
	}
	stopCtx, cancelStop := context.WithTimeout(context.Background(), time.Second)
	defer cancelStop()
	if err := owner.Stop(stopCtx); err != nil {
		t.Errorf("session owner Stop: %v", err)
	}
}

func TestOAuthSessionOwnersExplicitLifecycle(t *testing.T) {
	claude := NewOAuthService(nil, nil)
	openai := NewOpenAIOAuthService(nil, nil)
	grok := NewGrokOAuthService(nil, nil)
	gemini := NewGeminiOAuthService(nil, nil, nil, nil, &config.Config{})
	antigravity := NewAntigravityOAuthService(nil)
	for _, tc := range []struct {
		name     string
		owner    sessionOwnerTest
		generate func(context.Context) error
	}{
		{"claude", claude, func(ctx context.Context) error { _, err := claude.GenerateAuthURL(ctx, nil); return err }},
		{"openai", openai, func(ctx context.Context) error { _, err := openai.GenerateAuthURL(ctx, nil, "", "openai"); return err }},
		{"grok", grok, func(ctx context.Context) error { _, err := grok.GenerateAuthURL(ctx, nil, ""); return err }},
		{"gemini", gemini, func(ctx context.Context) error {
			_, err := gemini.GenerateAuthURL(ctx, nil, "", "", "code_assist", "")
			return err
		}},
		{"antigravity", antigravity, func(ctx context.Context) error { _, err := antigravity.GenerateAuthURL(ctx, nil); return err }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.generate(context.Background()); !errors.Is(err, sessionstore.ErrClosed) {
				t.Fatal("accepted before Start", err)
			}
			startSessionOwnerTest(t, tc.owner)
			if err := tc.generate(context.Background()); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if err := tc.owner.Drain(ctx); err != nil {
				t.Fatal(err)
			}
			if err := tc.generate(context.Background()); !errors.Is(err, sessionstore.ErrClosed) {
				t.Fatal("accepted after Drain", err)
			}
			if err := tc.owner.Stop(ctx); err != nil {
				t.Fatal(err)
			}
			if err := tc.owner.Start(ctx); !errors.Is(err, sessionstore.ErrClosed) {
				t.Fatal("restarted closed owner", err)
			}
		})
	}
}

type lifecycleClaudeClient struct {
	ClaudeOAuthClient
	entered, canceled, release chan struct{}
}

func (c *lifecycleClaudeClient) RefreshToken(ctx context.Context, _, _ string) (*oauth.TokenResponse, error) {
	close(c.entered)
	<-ctx.Done()
	close(c.canceled)
	<-c.release
	return nil, ctx.Err()
}

func TestOAuthSessionOwnerStopJoinsRealClientAndPreservesSession(t *testing.T) {
	client := &lifecycleClaudeClient{entered: make(chan struct{}), canceled: make(chan struct{}), release: make(chan struct{})}
	s := NewOAuthService(nil, client)
	startSessionOwnerTest(t, s)
	session := &oauth.OAuthSession{State: "preserved", CreatedAt: time.Now()}
	s.sessionStore.Set("pending", session)
	result := make(chan error, 1)
	go func() { _, err := s.RefreshToken(context.Background(), "fixture-token", ""); result <- err }()
	<-client.entered
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := s.Stop(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	<-client.canceled
	select {
	case <-result:
		t.Fatal("client has not returned")
	default:
	}
	close(client.release)
	ctx2, cancel2 := context.WithTimeout(context.Background(), time.Second)
	defer cancel2()
	if err := s.Stop(ctx2); err != nil {
		t.Fatal(err)
	}
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if got, ok := s.sessionStore.Get("pending"); !ok || got != session {
		t.Fatal("shutdown changed valid OAuth session")
	}
}
