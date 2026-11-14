package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type bpsReviewScopeReadErrorRepository struct {
	AccountRepository
	scope string
	cause error
}

func (r *bpsReviewScopeReadErrorRepository) GetExcelBPSCooldown(_ context.Context, _ int64, scopes []string) (time.Time, error) {
	for _, scope := range scopes {
		if scope == r.scope {
			return time.Time{}, r.cause
		}
	}
	return time.Time{}, nil
}
func (*bpsReviewScopeReadErrorRepository) ExtendExcelBPSCooldown(context.Context, int64, string, time.Time) (time.Time, error) {
	return time.Time{}, errors.New("unexpected cooldown write on a read failure")
}

func TestExcelBPSImagesCooldownDependencyIs503WithoutAnySend(t *testing.T) {
	for _, kind := range []string{"rate_limit", "model_permission"} {
		t.Run(kind, func(t *testing.T) {
			var urls []string
			svc := excelBPSImagesTestService(excelBPSImagesUpstream(http.StatusOK, &urls))
			account := excelBPSImagesAccount()
			useExcelBPSMixedGrant(t, svc, account)
			cause := errors.New("synthetic cooldown dependency unavailable")
			scope := excelBPSAccountCooldownScope
			if kind == "model_permission" {
				scope = excelBPSModelPermissionScope("gpt-image-2")
			}
			svc.accountRepo = &bpsReviewScopeReadErrorRepository{AccountRepository: svc.accountRepo, scope: scope, cause: cause}
			body, err := json.Marshal(map[string]string{"model": "gpt-image-2", "prompt": "draw"})
			require.NoError(t, err)
			c, _ := newOpenAIImagesTestContext(t, body)
			parsed, err := svc.ParseOpenAIImagesRequest(c, body)
			require.NoError(t, err)
			result, err := svc.ForwardImages(t.Context(), c, account, body, parsed, "")
			require.Nil(t, result)
			require.ErrorIs(t, err, cause)
			var failure *UpstreamFailoverError
			require.ErrorAs(t, err, &failure)
			require.Equal(t, http.StatusServiceUnavailable, failure.StatusCode)
			require.Equal(t, http.StatusServiceUnavailable, failure.ClientStatusCode)
			require.True(t, failure.RequestNotSent)
			require.False(t, failure.ShouldRetryNextAccount())
			require.Empty(t, urls, "neither BPS nor Native may send on an unproven state read")
			require.True(t, account.IsExcelBPSEnabled())
			require.Nil(t, account.RateLimitResetAt)
			require.NotContains(t, account.Extra, ExcelBPS403DisabledAtKey)
		})
	}
}
