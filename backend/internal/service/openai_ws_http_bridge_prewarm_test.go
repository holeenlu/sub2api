package service

import (
	"context"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
)

// A client gone mid-prewarm ends the connection on its next read, as during a
// bridged turn; any other write failure fails the turn.
func TestAnswerOpenAIWSHTTPBridgePrewarmWriteFailure(t *testing.T) {
	for _, tc := range []struct {
		name    string
		failAt  int
		err     error
		wantErr bool
	}{
		{name: "disconnect", failAt: 1, err: io.EOF},
		{name: "write timeout", failAt: 2, err: context.DeadlineExceeded, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			writes := 0
			result, err := answerOpenAIWSHTTPBridgePrewarm(1, "gpt-5.1", 1, func([]byte) error {
				writes++
				if writes == tc.failAt {
					return tc.err
				}
				return nil
			})
			require.Equal(t, tc.failAt, writes)
			if !tc.wantErr {
				require.NoError(t, err)
				require.True(t, result.LocalPrewarm)
				return
			}
			require.Nil(t, result)
			require.ErrorIs(t, err, context.DeadlineExceeded)
			var turnErr *openAIWSIngressTurnError
			require.ErrorAs(t, err, &turnErr)
			require.Equal(t, "write_client", turnErr.stage)
			require.True(t, turnErr.wroteDownstream, "response.created already reached the client")
		})
	}
}
