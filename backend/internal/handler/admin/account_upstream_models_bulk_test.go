//go:build unit

package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func postUpstreamModelsBulk(router *gin.Engine, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/models/sync-upstream-bulk", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)
	return rec
}

func TestSyncUpstreamModelsBulkOpenAIOAuthAndAPIKeyFreshIntersection(t *testing.T) {
	apiKey := anthropicAPIKeyAccount(11, "openai.example")
	apiKey.Platform = service.PlatformOpenAI
	oauth := &service.Account{ID: 12, Name: "codex", Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, Credentials: map[string]any{"access_token": "test-token"}}
	upstream := &anthropicModelsBulkUpstream{bodies: map[string]string{
		"openai.example": `{"data":[{"id":"gpt-common"},{"id":"api-only"},{"id":"gpt-common"}]}`,
		"chatgpt.com":    `{"models":[{"slug":"gpt-common"},{"slug":"oauth-only"}]}`,
	}}
	svc := &anthropicModelsBulkAdminService{stubAdminService: newStubAdminService(), accounts: []*service.Account{apiKey, oauth}}
	router := setupAnthropicModelsBulkRouter(svc, upstream)
	for _, expected := range []string{"gpt-common", "gpt-latest"} {
		rec := postUpstreamModelsBulk(router, `{"account_ids":[11,12,11]}`)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var result struct {
			Data struct {
				Models []string `json:"models"`
				Count  int      `json:"account_count"`
				Error  string   `json:"error"`
			} `json:"data"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &result))
		require.Equal(t, []string{expected}, result.Data.Models)
		require.Equal(t, 2, result.Data.Count)
		require.Empty(t, result.Data.Error)
		upstream.bodies["openai.example"] = `{"data":[{"id":"gpt-latest"}]}`
		upstream.bodies["chatgpt.com"] = `{"models":[{"slug":"gpt-latest"}]}`
	}
	require.NotContains(t, apiKey.Credentials, "model_mapping")
	require.NotContains(t, oauth.Credentials, "model_mapping")
}

func TestSyncUpstreamModelsBulkRejectsIncompleteAndEmptyIntersections(t *testing.T) {
	for _, scenario := range []string{"upstream failure", "deleted target", "no common models", "unsupported platform"} {
		t.Run(scenario, func(t *testing.T) {
			a := anthropicAPIKeyAccount(21, "first.example")
			b := anthropicAPIKeyAccount(22, "second.example")
			a.Platform, b.Platform = service.PlatformOpenAI, service.PlatformOpenAI
			accounts := []*service.Account{a, b}
			upstream := &anthropicModelsBulkUpstream{bodies: map[string]string{
				"first.example":  `{"data":[{"id":"gpt-common"}]}`,
				"second.example": `{"data":[{"id":"gpt-common"}]}`,
			}, statuses: map[string]int{}}
			switch scenario {
			case "upstream failure":
				upstream.statuses["second.example"] = http.StatusUnauthorized
				upstream.bodies["second.example"] = `{"error":"secret-token-do-not-return"}`
			case "deleted target":
				accounts = accounts[:1]
			case "no common models":
				upstream.bodies["second.example"] = `{"data":[{"id":"different"}]}`
			case "unsupported platform":
				b.Platform = "unknown"
			}
			svc := &anthropicModelsBulkAdminService{stubAdminService: newStubAdminService(), accounts: accounts}
			rec := postUpstreamModelsBulk(setupAnthropicModelsBulkRouter(svc, upstream), `{"account_ids":[21,22]}`)
			require.Equal(t, http.StatusOK, rec.Code)
			var result struct {
				Data struct {
					Models   []string                    `json:"models"`
					Failures []upstreamModelsBulkFailure `json:"failures"`
					Error    string                      `json:"error"`
				} `json:"data"`
			}
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &result))
			require.Empty(t, result.Data.Models)
			require.NotEmpty(t, result.Data.Error)
			if scenario != "no common models" {
				require.Len(t, result.Data.Failures, 1)
				require.EqualValues(t, 22, result.Data.Failures[0].AccountID)
			}
			require.NotContains(t, rec.Body.String(), "secret-token-do-not-return")
		})
	}
}

func TestSyncUpstreamModelsBulkFilteredSelection(t *testing.T) {
	stub := newStubAdminService()
	stub.bulkUpdateTargetIDs = []int64{31}
	a := anthropicAPIKeyAccount(31, "filtered.example")
	a.Platform = service.PlatformOpenAI
	svc := &anthropicModelsBulkAdminService{stubAdminService: stub, accounts: []*service.Account{a}}
	upstream := &anthropicModelsBulkUpstream{bodies: map[string]string{"filtered.example": `{"data":[{"id":"gpt-new"}]}`}}
	rec := postUpstreamModelsBulk(setupAnthropicModelsBulkRouter(svc, upstream), `{"filters":{"platform":"openai","type":"apikey","group":"ungrouped","search":"prod","status":"active","privacy_mode":"enabled"}}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), "gpt-new")
	require.NotNil(t, stub.lastBulkUpdateTargetFilters)
	require.Equal(t, "openai", stub.lastBulkUpdateTargetFilters.Platform)
	require.Equal(t, "ungrouped", stub.lastBulkUpdateTargetFilters.Group)
	require.Equal(t, "prod", stub.lastBulkUpdateTargetFilters.Search)
	require.Equal(t, "enabled", stub.lastBulkUpdateTargetFilters.PrivacyMode)
}

func TestBulkUpstreamModelsBoundsConcurrencyAndCancelsQueuedRequests(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ids := make([]int64, 12)
	accounts := make([]*service.Account, len(ids))
	for i := range ids {
		ids[i] = int64(i + 1)
		accounts[i] = &service.Account{ID: ids[i]}
	}
	var calls atomic.Int32
	started := make(chan struct{})
	done := make(chan []upstreamModelsBulkFailure, 1)
	go func() {
		_, failures := fetchBulkUpstreamModels(ctx, ids, accounts, func(ctx context.Context, _ *service.Account) ([]string, error) {
			if calls.Add(1) == 4 {
				close(started)
			}
			<-ctx.Done()
			return nil, ctx.Err()
		})
		done <- failures
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("workers did not start")
	}
	require.EqualValues(t, 4, calls.Load())
	cancel()
	select {
	case failures := <-done:
		require.Len(t, failures, len(ids))
		require.EqualValues(t, 4, calls.Load())
	case <-time.After(time.Second):
		t.Fatal("batch did not stop after cancellation")
	}
}
