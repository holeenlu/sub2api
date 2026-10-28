package openai

import (
	"context"
	"net/url"
	"sync"
	"testing"
)

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

func TestBuildAuthorizationURLForPlatform_OpenAI(t *testing.T) {
	authURL := BuildAuthorizationURLForPlatform("state-1", "challenge-1", DefaultRedirectURI, OAuthPlatformOpenAI)
	parsed, err := url.Parse(authURL)
	if err != nil {
		t.Fatalf("Parse URL failed: %v", err)
	}
	q := parsed.Query()
	if got := q.Get("client_id"); got != ClientID {
		t.Fatalf("client_id mismatch: got=%q want=%q", got, ClientID)
	}
	if got := q.Get("codex_cli_simplified_flow"); got != "true" {
		t.Fatalf("codex flow mismatch: got=%q want=true", got)
	}
	if got := q.Get("id_token_add_organizations"); got != "true" {
		t.Fatalf("id_token_add_organizations mismatch: got=%q want=true", got)
	}
}
