package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/stretchr/testify/require"
)

type securityAudioUpstream struct {
	initial   []byte
	writes    int
	sent      bool
	readError error
}

func (u *securityAudioUpstream) ReadMessage(ctx context.Context) ([]byte, error) {
	if !u.sent {
		u.sent = true
		if u.initial != nil {
			return u.initial, nil
		}
		return []byte(`{"type":"response.audio.delta","delta":"YWJj"}`), nil
	}
	<-ctx.Done()
	return nil, ctx.Err()
}
func (u *securityAudioUpstream) WriteJSON(context.Context, any) error { u.writes++; return u.readError }
func (u *securityAudioUpstream) Ping(context.Context) error           { return nil }
func (u *securityAudioUpstream) Close() error                         { return nil }

func TestSecurityRealtimeAudioAccruedOnEveryExit(t *testing.T) {
	for _, scenario := range []string{"malformed", "normal", "upstream-write-error", "foreign-voice", "model-change"} {
		t.Run(scenario, func(t *testing.T) {
			svc := &OpenAIGatewayService{mediaRepo: newSecurityMediaRepo()}
			up := &securityAudioUpstream{}
			if scenario == "upstream-write-error" {
				up.readError = errors.New("upstream transport failed")
			}
			type result struct {
				audio bool
				err   error
			}
			finished := make(chan result, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				conn, err := coderws.Accept(w, r, nil)
				if err != nil {
					finished <- result{err: err}
					return
				}
				defer func() { _ = conn.CloseNow() }()
				c := securityVoiceContext(1, 11)
				c.Request = r
				key, _ := mediaPrincipal(c)
				key.Group = &Group{ModelAllowlist: GroupModelAllowlist{Enabled: true, Models: []string{"grok-voice-latest"}}}
				observed, err := svc.ProxyGrokRealtimeConn(r.Context(), c, conn, &GrokRealtimeUpstream{conn: up, accountID: 77})
				finished <- result{observed, err}
			}))
			defer server.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			client, _, err := coderws.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
			require.NoError(t, err)
			defer func() { _ = client.CloseNow() }()
			_, audio, err := client.Read(ctx)
			require.NoError(t, err)
			require.Contains(t, string(audio), "response.audio.delta")
			switch scenario {
			case "malformed":
				require.NoError(t, client.Write(ctx, coderws.MessageText, []byte(`{`)))
			case "normal":
				_ = client.Close(coderws.StatusNormalClosure, "")
			case "upstream-write-error":
				require.NoError(t, client.Write(ctx, coderws.MessageText, []byte(`{"type":"input_audio_buffer.append","audio":"YWJj"}`)))
			case "foreign-voice":
				require.NoError(t, client.Write(ctx, coderws.MessageText, []byte(`{"type":"session.update","session":{"voice":"private"}}`)))
			case "model-change":
				require.NoError(t, client.Write(ctx, coderws.MessageText, []byte(`{"type":"session.update","session":{"model":"not-allowed"}}`)))
			}
			select {
			case got := <-finished:
				require.True(t, got.audio, "error exits must retain accrued audio")
				require.Error(t, got.err)
			case <-ctx.Done():
				t.Fatal("relay did not join both pumps")
			}
		})
	}
}

func TestSecurityRealtimeFirstCustomVoiceCanRebindWithoutDisconnect(t *testing.T) {
	repo := newSecurityMediaRepo()
	require.NoError(t, repo.PutVoice(context.Background(), &GatewayMediaVoice{ID: "mine", UserID: 1, GroupID: 10, AccountID: 88, Metadata: json.RawMessage(`{"voice_id":"mine"}`)}))
	svc := &OpenAIGatewayService{mediaRepo: repo}
	first := &securityAudioUpstream{initial: []byte(`{"type":"session.created"}`)}
	second := &securityAudioUpstream{}
	done := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := coderws.Accept(w, r, nil)
		if err != nil {
			done <- err
			return
		}
		defer func() { _ = conn.CloseNow() }()
		c := securityVoiceContext(1, 99)
		c.Request = r
		observed, err := svc.ProxyGrokRealtimeConn(r.Context(), c, conn, &GrokRealtimeUpstream{conn: first, accountID: 77})
		var change *GrokRealtimeVoiceAccountChange
		if observed || !errors.As(err, &change) || change.AccountID != 88 {
			done <- errors.New("missing safe voice account transition")
			return
		}
		replacement := &GrokRealtimeUpstream{conn: second, accountID: 88}
		if err := svc.ReplayGrokRealtimeVoiceSetup(r.Context(), c, replacement, change); err != nil {
			done <- err
			return
		}
		observed, err = svc.ProxyGrokRealtimeConn(r.Context(), c, conn, replacement)
		if !observed || err == nil {
			done <- errors.New("replacement audio was not accrued")
			return
		}
		done <- nil
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	client, _, err := coderws.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	require.NoError(t, err)
	defer func() { _ = client.CloseNow() }()
	_, _, err = client.Read(ctx)
	require.NoError(t, err)
	require.NoError(t, client.Write(ctx, coderws.MessageText, []byte(`{"type":"session.update","session":{"voice":"mine"}}`)))
	_, audio, err := client.Read(ctx)
	require.NoError(t, err)
	require.Contains(t, string(audio), "response.audio.delta")
	require.NoError(t, client.Write(ctx, coderws.MessageText, []byte(`{`)))
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-ctx.Done():
		t.Fatal("rebind did not complete")
	}
	require.Zero(t, first.writes, "voice setup must never reach another account")
	require.Equal(t, 1, second.writes, "owned voice setup is replayed once")
}
