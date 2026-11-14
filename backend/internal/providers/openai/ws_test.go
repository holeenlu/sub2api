package openai_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/gateway/execution"
	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	native "github.com/Wei-Shaw/sub2api/internal/providers/openai"
	"github.com/stretchr/testify/require"
)

type wsWriterFunc func(context.Context, *native.WSWrite) (model.SendState, error)

func (f wsWriterFunc) WriteWS(ctx context.Context, w *native.WSWrite) (model.SendState, error) {
	return f(ctx, w)
}
func wsSpec(payload string) native.WSSpec {
	id := identity()
	id.Transport = "ws"
	return native.WSSpec{MessageType: 1, Identity: id, Target: native.WSTarget{URL: "wss://localhost/v1/responses", HandshakeDigest: sha256.Sum256([]byte("verified handshake"))}, Payload: []byte(payload)}
}
func TestNativeWSImmutableSingleSend(t *testing.T) {
	authority := newAuthority()
	dispatch, budget := dispatcher(t, authority, 2)
	var frames atomic.Int32
	spec := wsSpec("{\"type\":\"response.create\",\"input\":[9007199254740993],\"unknown\":\"<中文>\"}")
	original := string(spec.Payload)
	writer := wsWriterFunc(func(ctx context.Context, w *native.WSWrite) (model.SendState, error) {
		copyWrite := *w
		body, err := w.Begin(ctx)
		require.NoError(t, err)
		require.Equal(t, original, string(body))
		frames.Add(1)
		_, err = copyWrite.Begin(ctx)
		require.ErrorIs(t, err, native.ErrWSWriteStarted)
		return model.SendSent, nil
	})
	p, err := native.PrepareWS(context.Background(), spec, writer)
	require.NoError(t, err)
	spec.Payload[0] = '!'
	permit, err := dispatch.Admit(context.Background(), p.Identity(), model.AttemptTarget{Owner: p.Identity().Owner, Limit: 2})
	require.NoError(t, err)
	result, err := native.OpenWS(context.Background(), p, permit)
	require.NoError(t, err)
	require.Equal(t, model.SendSent, result.SendState)
	copy := *p
	_, err = native.OpenWS(context.Background(), &copy, permit)
	require.ErrorIs(t, err, native.ErrWSAttemptOpened)
	require.EqualValues(t, 1, frames.Load())
	require.Equal(t, 1, budget.Snapshot().Attempts)
}
func TestNativeWSRejectsUnsealedFrames(t *testing.T) {
	for _, body := range []string{
		"{}", "[]", "{\"type\":\"response.cancel\"}",
		"{\"type\":\"response.create\",\"stream_id\":\"lane\"}",
		"{\"type\":\"response.create\",\"type\":\"response.create\"}",
		"{\"type\":\"response.create\",\"generate\":false,\"generate\":true}",
	} {
		t.Run(body, func(t *testing.T) {
			_, err := native.PrepareWS(context.Background(), wsSpec(body), wsWriterFunc(func(context.Context, *native.WSWrite) (model.SendState, error) {
				t.Fatal("unexpected write")
				return model.SendUnknown, nil
			}))
			require.ErrorIs(t, err, native.ErrInvalidWSPreparation)
		})
	}
}
func TestNativeWSPreparationMustSealLiteralGenerateFalse(t *testing.T) {
	for _, value := range []string{"true", "null", "0", "\"false\"", "false"} {
		t.Run(value, func(t *testing.T) {
			spec := wsSpec("{\"type\":\"response.create\",\"generate\":" + value + "}")
			spec.InternalPreparation = true
			spec.Identity.Purpose = model.DispatchInternalPreparation
			_, err := native.PrepareWS(context.Background(), spec, wsWriterFunc(func(context.Context, *native.WSWrite) (model.SendState, error) { return model.SendUnknown, nil }))
			if value == "false" {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, native.ErrInvalidWSPreparation)
			}
		})
	}
}
func TestNativeWSPermitMismatchAndCopyCannotSend(t *testing.T) {
	authority := newAuthority()
	dispatch, _ := dispatcher(t, authority, 3)
	var frames int
	writer := wsWriterFunc(func(ctx context.Context, w *native.WSWrite) (model.SendState, error) {
		_, err := w.Begin(ctx)
		if err != nil {
			return model.SendNotSent, err
		}
		frames++
		return model.SendSent, nil
	})
	p, err := native.PrepareWS(context.Background(), wsSpec("{\"type\":\"response.create\"}"), writer)
	require.NoError(t, err)
	q, err := native.PrepareWS(context.Background(), wsSpec("{\"type\":\"response.create\",\"input\":\"different\"}"), writer)
	require.NoError(t, err)
	permit, err := dispatch.Admit(context.Background(), p.Identity(), model.AttemptTarget{Owner: p.Identity().Owner, Limit: 3})
	require.NoError(t, err)
	copied := *permit
	result, err := native.OpenWS(context.Background(), q, &copied)
	require.ErrorIs(t, err, execution.ErrInvalidPreparedIdentity)
	require.Equal(t, model.SendNotSent, result.SendState)
	result, err = native.OpenWS(context.Background(), p, permit)
	require.ErrorIs(t, err, execution.ErrSendPermitConsumed)
	require.Equal(t, model.SendNotSent, result.SendState)
	require.Zero(t, frames)
}
func TestNativeWSRevalidatesAfterWriterWait(t *testing.T) {
	for _, cause := range []string{"revoke", "cancel_original", "deadline"} {
		t.Run(cause, func(t *testing.T) {
			original, cancel := context.WithCancel(context.Background())
			defer cancel()
			authority := newAuthority()
			dispatch, _ := dispatcher(t, authority, 1)
			if cause == "deadline" {
				authority.validation.ValidUntil = time.Now().Add(30 * time.Millisecond)
			}
			var frames, bytes int
			writer := wsWriterFunc(func(ctx context.Context, w *native.WSWrite) (model.SendState, error) {
				switch cause {
				case "revoke":
					authority.mu.Lock()
					authority.denied = execution.ErrDispatchChanged
					authority.mu.Unlock()
				case "cancel_original":
					cancel()
				case "deadline":
					<-ctx.Done()
				}
				body, err := w.Begin(ctx)
				if err != nil {
					return model.SendNotSent, err
				}
				frames++
				bytes += len(body)
				return model.SendSent, nil
			})
			p, err := native.PrepareWS(original, wsSpec("{\"type\":\"response.create\"}"), writer)
			require.NoError(t, err)
			permit, err := dispatch.Admit(context.Background(), p.Identity(), model.AttemptTarget{Owner: p.Identity().Owner, Limit: 1})
			require.NoError(t, err)
			result, err := native.OpenWS(context.Background(), p, permit)
			require.Error(t, err)
			require.Equal(t, model.SendNotSent, result.SendState)
			require.Zero(t, frames)
			require.Zero(t, bytes)
		})
	}
}
func TestNativeWSSendEvidenceIsNotInvented(t *testing.T) {
	for _, state := range []model.SendState{model.SendUnknown, model.SendNotSent, model.SendAmbiguous} {
		t.Run(string(rune('0'+state)), func(t *testing.T) {
			authority := newAuthority()
			dispatch, _ := dispatcher(t, authority, 1)
			p, err := native.PrepareWS(context.Background(), wsSpec("{\"type\":\"response.create\"}"), wsWriterFunc(func(ctx context.Context, w *native.WSWrite) (model.SendState, error) {
				if state == model.SendAmbiguous {
					_, err := w.Begin(ctx)
					require.NoError(t, err)
				}
				return state, errors.New("transport failed")
			}))
			require.NoError(t, err)
			permit, err := dispatch.Admit(context.Background(), p.Identity(), model.AttemptTarget{Owner: p.Identity().Owner, Limit: 1})
			require.NoError(t, err)
			result, err := native.OpenWS(context.Background(), p, permit)
			require.Error(t, err)
			require.Equal(t, state, result.SendState)
		})
	}
}
