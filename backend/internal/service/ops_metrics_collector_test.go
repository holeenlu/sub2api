package service

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestWriteOpenAIFastPolicyBlockedResponseMarksBusinessLimited(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)

	writeOpenAIFastPolicyBlockedResponse(c, &OpenAIFastBlockedError{Message: "custom fast policy block"})

	require.Equal(t, http.StatusForbidden, rec.Code)
	require.True(t, HasOpsClientBusinessLimited(c))
	reason, ok := c.Get(OpsClientBusinessLimitedReasonKey)
	require.True(t, ok)
	require.Equal(t, OpsClientBusinessLimitedReasonLocalPolicyDenied, reason)
}

func TestOpsMetricsCollectorUsesRepositoryWindowAndPropagatesFailure(t *testing.T) {
	for _, failed := range []bool{false, true} {
		t.Run(fmt.Sprint(failed), func(t *testing.T) {
			marker := &OpsInsertSystemMetricsInput{SuccessCount: 11, AccountSwitchCount: 2, DurationP50Ms: intPtr(9)}
			writes := 0
			repo := &opsRepoMock{
				GetSystemMetricsWindowFn: func(ctx context.Context, start, end time.Time) (*OpsInsertSystemMetricsInput, error) {
					require.Equal(t, time.Minute, end.Sub(start))
					require.Equal(t, end, end.Truncate(time.Minute))
					if failed {
						return nil, context.Canceled
					}
					marker.CreatedAt, marker.WindowMinutes = end, 1
					return marker, nil
				},
				InsertSystemMetricsFn: func(_ context.Context, input *OpsInsertSystemMetricsInput) error {
					writes++
					require.Same(t, marker, input)
					require.Equal(t, int64(11), input.SuccessCount)
					require.Equal(t, int64(2), input.AccountSwitchCount)
					require.Equal(t, 9, *input.DurationP50Ms)
					return nil
				},
			}
			// No raw database is needed for statistics; local node health remains separate.
			collector := &OpsMetricsCollector{opsRepo: repo}
			err := collector.collectAndPersist(t.Context())
			if failed {
				require.ErrorIs(t, err, context.Canceled)
				require.Zero(t, writes)
			} else {
				require.NoError(t, err)
				require.Equal(t, 1, writes)
			}
		})
	}
}
