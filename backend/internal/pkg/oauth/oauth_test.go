package oauth

import (
	"context"
	"strings"
	"sync"
	"testing"
)

func TestAuthorizeURLMatchesClaudeCodeCLI(t *testing.T) {
	const want = "https://claude.com/cai/oauth/authorize"
	if AuthorizeURL != want {
		t.Fatalf("AuthorizeURL = %q, want %q", AuthorizeURL, want)
	}

	authURL := BuildAuthorizationURL("state-value", "challenge-value", ScopeOAuth)
	if !strings.HasPrefix(authURL, want+"?") {
		t.Fatalf("BuildAuthorizationURL() = %q, want prefix %q", authURL, want+"?")
	}
	for _, part := range []string{
		"code=true",
		"client_id=" + ClientID,
		"response_type=code",
		"code_challenge=challenge-value",
		"code_challenge_method=S256",
		"state=state-value",
	} {
		if !strings.Contains(authURL, part) {
			t.Fatalf("BuildAuthorizationURL() missing %q\nURL: %s", part, authURL)
		}
	}
}

func TestSessionStore_Stop_Idempotent(t *testing.T) {
	store := NewSessionStore()
	if err := store.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	store.Stop(context.Background())
	store.Stop(context.Background())

	if _, _, err := store.Begin(context.Background()); err == nil {
		t.Fatal("stopped owner accepted new work")
	}
}

func TestSessionStore_Stop_Concurrent(t *testing.T) {
	store := NewSessionStore()
	if err := store.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			store.Stop(context.Background())
		}()
	}

	wg.Wait()

	if _, _, err := store.Begin(context.Background()); err == nil {
		t.Fatal("stopped owner accepted new work")
	}
}
