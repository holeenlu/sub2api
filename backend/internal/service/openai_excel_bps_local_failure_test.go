package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service/basispoints"
	"github.com/stretchr/testify/require"
)

type bpsLocalReplayFailureCache struct {
	*bpsContinuationIntegrationCache
	err error
}

func (s *bpsLocalReplayFailureCache) SaveBPSState(ctx context.Context, scope, kind string, expected *uint64, data []byte) (bool, error) {
	if strings.HasPrefix(kind, "replay:") {
		return false, s.err
	}
	return s.bpsContinuationIntegrationCache.SaveBPSState(ctx, scope, kind, expected, data)
}

// Assert typed local origin, the provider-free diagnostic event, actual metering
// and zero regeneration at the BPS application boundary.
func TestExcelBPSReplayPersistenceKeepsLocalFailureAndUsage(t *testing.T) {
	for _, stateErr := range []error{basispoints.ErrStateUnavailable, basispoints.ErrStateCapacity} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stream=%t", stateErr, stream), func(t *testing.T) {
				upstream := &httpUpstreamRecorder{resp: bpsCompletionResponse(200, excelBPSRepairWire(t, "store", "Run", `{"name":"shell","arguments":{"cmd":"pwd"}}`))}
				svc := newBPSContinuationIntegrationService(upstream, &bpsContinuationIntegrationCache{})
				svc.cache = &bpsLocalReplayFailureCache{bpsContinuationIntegrationCache: &bpsContinuationIntegrationCache{}, err: stateErr}
				body := []byte(fmt.Sprintf(`{"model":"gpt-6-astra","stream":%t,"input":"test","tools":[{"type":"function","name":"shell"}]}`, stream))
				c, rec := newBPSContinuationIntegrationContext(httptest.NewRequest(http.MethodPost, "/v1/responses", nil), newBPSContinuationIntegrationKey())
				c.Request.Header.Set("session_id", "local-replay-outage")
				result, err := svc.Forward(t.Context(), c, excelAccount(), body)
				require.ErrorIs(t, err, stateErr)
				var failure *basispoints.UpstreamFailure
				require.True(t, errors.As(err, &failure))
				require.Equal(t, 503, failure.Status)
				require.True(t, failure.Local)
				require.True(t, failure.RequestScoped())
				require.True(t, c.GetBool(OpsLocalDependencyFailureKey))
				require.Len(t, upstream.requests, 1)
				require.NotNil(t, result)
				require.Equal(t, 10, result.Usage.InputTokens)
				require.Equal(t, 2, result.Usage.OutputTokens)
				require.Contains(t, rec.Body.String(), "basispoints_state_")
				require.NotContains(t, rec.Body.String(), `"type":"function_call"`)
				require.False(t, svc.ReportOpenAIAccountScheduleFailure(t.Context(), excelAccount(), "gpt-6-astra", err))
				events, _ := c.Get(OpsUpstreamErrorsKey)
				local := false
				for _, event := range events.([]*OpsUpstreamErrorEvent) {
					if event.Kind == "bridge_error" && event.Stage == "state_persist" {
						local = true
						require.Zero(t, event.UpstreamStatusCode)
						require.Empty(t, event.UpstreamURL)
					}
				}
				require.True(t, local)
			})
		}
	}
}
