package service

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service/basispoints"
	"github.com/stretchr/testify/require"
)

type excelBPSImagesAdmissionRepo struct {
	AccountRepository
	account *Account
	reads   int
	change  func(*Account)
}

func (r *excelBPSImagesAdmissionRepo) GetOpenAITurnAdmission(context.Context, int64) (*Account, *Account, error) {
	r.reads++
	latest := *r.account
	latest.Extra = maps.Clone(r.account.Extra)
	if r.reads >= 2 {
		r.change(&latest)
	}
	return &latest, nil, nil
}

func TestExcelBPSImagesRechecksBeforeActualSend(t *testing.T) {
	for _, reason := range []string{"paused", "bps_disabled", "rpm_lowered"} {
		t.Run(reason, func(t *testing.T) {
			account := excelBPSImagesAccount()
			account.Extra["base_rpm"] = 2
			cache := &openAIRPMTestCache{counts: map[int64]int{account.ID: 1}}
			repo := &excelBPSImagesAdmissionRepo{account: account, change: func(a *Account) {
				switch reason {
				case "paused":
					a.Schedulable = false
				case "bps_disabled":
					a.Extra["openai_excel_bps"] = false
				case "rpm_lowered":
					a.Extra["base_rpm"] = 1
				}
			}}
			upstream := &httpUpstreamRecorder{resp: openAIImagesJSONResponse()}
			svc := newOpenAIImagesTestService(upstream)
			svc.accountRepo, svc.rpmCache = repo, cache
			body := []byte(`{"model":"gpt-image-2","prompt":"draw"}`)
			c, _ := newOpenAIImagesTestContext(t, body)
			parsed, err := svc.ParseOpenAIImagesRequest(c, body)
			require.NoError(t, err)
			result, err := svc.ForwardImages(context.Background(), c, account, body, parsed, "")
			require.Nil(t, result)
			if reason == "rpm_lowered" {
				require.True(t, IsOpenAIRPMError(err))
			} else {
				require.True(t, IsOpenAITurnAdmissionError(err))
			}
			require.GreaterOrEqual(t, repo.reads, 2)
			require.Empty(t, upstream.requests)
			require.Equal(t, 1, cache.counts[account.ID], "local refusal must not consume RPM")
			require.False(t, c.Writer.Written())
			_, upstreamError := c.Get(OpsUpstreamErrorsKey)
			require.False(t, upstreamError, "local admission must not be reported as upstream failure")
		})
	}
}

func TestExcelBPSImagesFallbackSharesActualSendRPM(t *testing.T) {
	for _, limit := range []int{1, 2} {
		t.Run(fmt.Sprint(limit), func(t *testing.T) {
			account := excelBPSImagesAccount()
			account.Extra["base_rpm"] = limit
			cache := &openAIRPMTestCache{counts: map[int64]int{}}
			var urls []string
			svc := newOpenAIImagesTestService(excelBPSImagesUpstream(http.StatusUnprocessableEntity, &urls))
			svc.rpmCache = cache
			body := []byte(`{"model":"gpt-image-2","prompt":"draw"}`)
			c, _ := newOpenAIImagesTestContext(t, body)
			parsed, err := svc.ParseOpenAIImagesRequest(c, body)
			require.NoError(t, err)
			_, err = svc.ForwardImages(context.Background(), c, account, body, parsed, "")
			if limit == 1 {
				require.True(t, IsOpenAIRPMError(err))
				require.Equal(t, []string{basispoints.ImagesGenerationsURL}, urls)
			} else {
				require.NoError(t, err)
				require.Equal(t, []string{basispoints.ImagesGenerationsURL, codexDirectImagesURL}, urls)
			}
			require.Equal(t, limit, cache.counts[account.ID])
		})
	}
}

func TestExcelBPSImagesFreeAndCommittedOutputSafeguards(t *testing.T) {
	free := excelBPSImagesAccount()
	free.Credentials["plan_type"] = "free"
	require.False(t, free.IsExcelBPSImagesEnabledForModel("gpt-image-2"))
	var urls []string
	svc := newOpenAIImagesTestService(excelBPSImagesUpstream(http.StatusUnprocessableEntity, &urls))
	body := []byte(`{"model":"gpt-image-2","prompt":"draw"}`)
	c, _ := newOpenAIImagesTestContext(t, body)
	parsed, err := svc.ParseOpenAIImagesRequest(c, body)
	require.NoError(t, err)
	_, err = c.Writer.WriteString("already delivered")
	require.NoError(t, err)
	_, fallback, err := svc.forwardExcelBPSImages(context.Background(), c, excelBPSImagesAccount(), parsed, parsed.Model, parsed.Model, time.Now())
	require.Error(t, err)
	require.False(t, fallback)
	require.Equal(t, []string{basispoints.ImagesGenerationsURL}, urls)
}
