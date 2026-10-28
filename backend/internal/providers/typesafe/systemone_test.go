package typesafe

import (
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/gateway/execution"
	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	protocol "github.com/Wei-Shaw/sub2api/internal/pkg/typesafe"
	httpattempt "github.com/Wei-Shaw/sub2api/internal/providers/openai"
)

type transportFunc func(*http.Request, httpattempt.TransportOptions) (*http.Response, error)

func (f transportFunc) RoundTripOnce(r *http.Request, o httpattempt.TransportOptions) (*http.Response, error) {
	return f(r, o)
}

type authority struct{ reject bool }

func (a *authority) RevalidateDispatch(context.Context, model.PreparedIdentity) (execution.DispatchValidation, error) {
	if a.reject {
		return execution.DispatchValidation{}, execution.ErrDispatchChanged
	}
	return execution.DispatchValidation{Authorization: sha256.Sum256([]byte("actual test scope")), Leases: sha256.Sum256([]byte("test acquired lease"))}, nil
}
func preparedFixture(t *testing.T, ctx context.Context, transport httpattempt.Transport) (*Attempt, *execution.Dispatcher, *authority) {
	t.Helper()
	req, err := protocol.NewSystemOneRequest(ctx, "https://typesafe.test", "test-key", []byte(`{"model":"jev-latest","extension":true}`))
	if err != nil {
		t.Fatal(err)
	}
	owner := model.ResourceOwner{AccountID: 7}
	a, err := Prepare(ctx, Spec{HTTP: httpattempt.Spec{Request: req, Identity: model.PreparedIdentity{OperationID: "op", UserID: 1, APIKeyID: 2, ResourceID: 7, Owner: owner, Mode: "native", Transport: "http", BodyKind: model.BodyReplayable, GrantDigest: sha256.Sum256([]byte("test-key-generation"))}, Transport: httpattempt.TransportOptions{Profile: httpattempt.ProfileDefault, AccountID: 7}, MaxReplayBodyBytes: 4096}, Target: model.AttemptTarget{Owner: owner, Model: protocol.JevLatestModel, Limit: 2}}, transport)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	auth := &authority{}
	d, err := execution.NewDispatcher(ctx, "op", execution.NewAttemptBudget(execution.AttemptLimits{Total: 2, PerOwner: 2, Preparation: 1, RetryWait: time.Second}), execution.NewFirstOutput(time.Now()), auth)
	if err != nil {
		t.Fatal(err)
	}
	return a, d, auth
}

func TestSystemOneAttemptPreservesWireAndConsumesOnePermit(t *testing.T) {
	ctx := context.Background()
	calls := 0
	wire := `{"answers":{"q":true},"extension":[1,2],"usage":{"input_tokens":"2","output_tokens":0}}`
	a, d, _ := preparedFixture(t, ctx, transportFunc(func(r *http.Request, o httpattempt.TransportOptions) (*http.Response, error) {
		calls++
		b, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		if string(b) != `{"model":"jev-latest","extension":true}` || r.Header.Get("Authorization") != "Bearer test-key" || o.Profile != httpattempt.ProfileDefault {
			t.Fatal("sealed wire/profile changed")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/html"}}, Body: io.NopCloser(strings.NewReader(wire))}, nil
	}))
	p, err := d.Admit(ctx, a.Identity(), a.Target())
	if err != nil {
		t.Fatal(err)
	}
	send, _, err := a.Open(ctx, p)
	if err != nil || send != model.SendSent {
		t.Fatalf("send=%v err=%v", send, err)
	}
	r, err := a.Receive(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if string(r.Body) != wire || r.ContentType != "application/json" {
		t.Fatal("raw response or safe content type lost")
	}
	f := r.Facts(nil)
	if f.Usage == nil || f.Usage.Confidence != model.UsageFinal || f.Usage.Output == nil || f.Usage.Output.Value != 0 {
		t.Fatalf("facts=%+v", f)
	}
	if _, _, err = a.Open(ctx, p); err == nil || calls != 1 {
		t.Fatalf("duplicate send: calls=%d err=%v", calls, err)
	}
}

func TestSystemOneAttemptRevocationBeforeOpenDoesNotSend(t *testing.T) {
	ctx := context.Background()
	calls := 0
	a, d, auth := preparedFixture(t, ctx, transportFunc(func(*http.Request, httpattempt.TransportOptions) (*http.Response, error) {
		calls++
		return nil, errors.New("must not send")
	}))
	p, err := d.Admit(ctx, a.Identity(), a.Target())
	if err != nil {
		t.Fatal(err)
	}
	auth.reject = true
	send, _, err := a.Open(ctx, p)
	if !errors.Is(err, execution.ErrDispatchChanged) || send != model.SendNotSent || calls != 0 {
		t.Fatalf("send=%v calls=%d err=%v", send, calls, err)
	}
}

func TestSystemOneFactsDoNotManufactureUnknownUsageOrTerminal(t *testing.T) {
	r := &Response{StatusCode: 200, Body: []byte(`{"answers":{}}`)}
	f := r.Facts(nil)
	if f.Usage != nil || f.Terminal != model.TerminalCompleted {
		t.Fatalf("unknown usage: %+v", f)
	}
	f = r.Facts(io.ErrUnexpectedEOF)
	if f.Usage != nil || f.Terminal != model.TerminalUnknown {
		t.Fatal("truncation became completed")
	}
	r.StatusCode = 429
	if f = r.Facts(nil); f.Generation != model.GenerationUnknown || f.Usage != nil {
		t.Fatal("HTTP status manufactured rejection/usage evidence")
	}
}
