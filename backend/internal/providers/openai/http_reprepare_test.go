package openai_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/gateway/execution"
	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	native "github.com/Wei-Shaw/sub2api/internal/providers/openai"
)

func TestReprepareHTTPKeepsSealedInputAndOriginalBudget(t *testing.T) {
	spec := specFor(t, http.MethodPost, "https://example.invalid/responses", "original body")
	spec.Request.Header.Set("Authorization", "Bearer fixture")
	var sends atomic.Int32
	transport := transportFunc(func(req *http.Request, options native.TransportOptions) (*http.Response, error) {
		sends.Add(1)
		body, err := io.ReadAll(req.Body)
		if err != nil || string(body) != "original body" || req.URL.Host != "example.invalid" || req.Header.Get("Authorization") != "Bearer fixture" || options.AccountID != 17 {
			t.Errorf("sealed retry changed: body=%q err=%v target=%s", body, err, req.URL.Host)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("ok"))}, nil
	})
	first := prepare(t, context.Background(), spec, transport)
	d, budget := dispatcher(t, newAuthority(), 2)
	permit := admit(t, d, first)
	result, err := native.Open(context.Background(), first, permit)
	if err != nil {
		t.Fatal(err)
	}
	if err := result.Close(); err != nil {
		t.Fatal(err)
	}
	// Caller-owned inputs can no longer alter a replay of the sealed wire.
	spec.Request.URL.Host = "changed.invalid"
	spec.Request.Header.Set("Authorization", "Bearer changed")
	spec.Request.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(strings.NewReader("changed body")), nil }
	second, err := native.ReprepareHTTP(context.Background(), first)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = second.Close() })
	if second.Identity() != first.Identity() || sends.Load() != 1 {
		t.Fatal("repreparation changed identity or sent a request")
	}
	if _, err := native.Open(context.Background(), second, permit); !errors.Is(err, execution.ErrSendPermitConsumed) {
		t.Fatalf("spent permit unexpectedly authorized replay: %v", err)
	}
	third, err := native.ReprepareHTTP(context.Background(), first)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = third.Close() })
	result, err = native.Open(context.Background(), third, admit(t, d, third))
	if err != nil {
		t.Fatal(err)
	}
	_ = result.Close()
	if sends.Load() != 2 || budget.Snapshot().Attempts != 2 {
		t.Fatalf("retry escaped original budget: sends=%d budget=%+v", sends.Load(), budget.Snapshot())
	}
	if _, err := d.Admit(context.Background(), third.Identity(), model.AttemptTarget{Owner: third.Identity().Owner, Limit: 5}); !errors.Is(err, execution.ErrAttemptBudgetExhausted) {
		t.Fatalf("original allowance reset: %v", err)
	}
}

func TestReprepareHTTPRejectsExclusiveAndCanceledInput(t *testing.T) {
	transport := transportFunc(func(*http.Request, native.TransportOptions) (*http.Response, error) {
		t.Fatal("unexpected send")
		return nil, nil
	})
	spec := specFor(t, http.MethodPost, "https://example.invalid/responses", "opaque stream")
	spec.Identity.BodyKind = model.BodyExclusiveStream
	stream := prepare(t, context.Background(), spec, transport)
	if _, err := native.ReprepareHTTP(context.Background(), stream); !errors.Is(err, native.ErrInvalidHTTPPreparation) {
		t.Fatalf("exclusive body became replayable: %v", err)
	}
	replayable := prepare(t, context.Background(), specFor(t, http.MethodPost, "https://example.invalid/responses", "body"), transport)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := native.ReprepareHTTP(ctx, replayable); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled recovery was prepared: %v", err)
	}
	if _, err := native.ReprepareHTTP(context.Background(), nil); !errors.Is(err, native.ErrInvalidHTTPPreparation) {
		t.Fatalf("nil preparation accepted: %v", err)
	}
}

func TestHTTPControlRequestUsesOnlySealedReplayAndRejectsURLCredentials(t *testing.T) {
	transport := transportFunc(func(*http.Request, native.TransportOptions) (*http.Response, error) {
		t.Fatal("unexpected network send")
		return nil, nil
	})
	spec := specFor(t, http.MethodPost, "https://example.invalid/responses", "actual bytes")
	spec.Request.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(strings.NewReader("different bytes")), nil }
	p := prepare(t, context.Background(), spec, transport)
	request, err := native.HTTPControlRequest(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(request.Body)
	_ = request.Body.Close()
	if err != nil || string(body) != "actual bytes" {
		t.Fatalf("wrong control body %q: %v", body, err)
	}
	bad := specFor(t, http.MethodPost, "https://fixture:secret@example.invalid/responses", "body")
	if _, err := native.PrepareHTTP(context.Background(), bad, transport); !errors.Is(err, native.ErrInvalidHTTPPreparation) {
		t.Fatalf("URL could inject unsealed authorization: %v", err)
	}
}
