//go:build unit

package handler

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/Wei-Shaw/sub2api/internal/config"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

type openAIImagesNoAccountRepo struct {
	service.AccountRepository
	accounts []service.Account
}

func (r openAIImagesNoAccountRepo) ListSchedulableByGroupIDAndPlatform(_ context.Context, _ int64, platform string) ([]service.Account, error) {
	return r.forPlatform(platform), nil
}

func (r openAIImagesNoAccountRepo) ListSchedulableByPlatform(_ context.Context, platform string) ([]service.Account, error) {
	return r.forPlatform(platform), nil
}

func (r openAIImagesNoAccountRepo) ListSchedulableUngroupedByPlatform(_ context.Context, platform string) ([]service.Account, error) {
	return r.forPlatform(platform), nil
}

// ListModelAvailabilityCandidates ignores transient state on purpose — that is
// what lets the diagnosis see a fully cooling pool.
func (r openAIImagesNoAccountRepo) ListModelAvailabilityCandidates(_ context.Context, _ *int64, platforms []string, _ bool) ([]service.Account, error) {
	allowed := make(map[string]struct{}, len(platforms))
	for _, platform := range platforms {
		allowed[platform] = struct{}{}
	}
	out := make([]service.Account, 0, len(r.accounts))
	for _, account := range r.accounts {
		if _, ok := allowed[account.Platform]; ok {
			out = append(out, account)
		}
	}
	return out, nil
}

func (r openAIImagesNoAccountRepo) forPlatform(platform string) []service.Account {
	out := make([]service.Account, 0, len(r.accounts))
	for _, account := range r.accounts {
		if account.Platform == platform && account.IsSchedulable() {
			out = append(out, account)
		}
	}
	return out
}

// /v1/images/generations 曾对所有非 404 分类把响应文案覆盖成
// "No available compatible accounts"：全池限流时状态码是 429、头里带着
// Retry-After，文案却在暗示分组配置有问题。其余入口都用
// messageWithSelectionDetail 保留分类自己的文案。
func TestOpenAIGatewayHandlerImages_RateLimitedPoolKeeps429Message(t *testing.T) {
	gin.SetMode(gin.TestMode)
	groupID := int64(3131)
	reset := time.Now().Add(90 * time.Second)
	accountRepo := openAIImagesNoAccountRepo{accounts: []service.Account{
		{
			ID:               1,
			Name:             "image-account-cooling",
			Platform:         service.PlatformOpenAI,
			Type:             service.AccountTypeOAuth,
			Status:           service.StatusActive,
			Schedulable:      true,
			RateLimitResetAt: &reset,
			Credentials:      map[string]any{"access_token": "token-1"},
		},
	}}
	require.False(t, accountRepo.accounts[0].IsSchedulable(),
		"the cooling account must be invisible to the scheduler so selection fails")

	cfg := &config.Config{RunMode: config.RunModeSimple}
	gatewayService := service.NewOpenAIGatewayService(
		accountRepo, nil, nil, nil, nil, nil, nil, cfg, nil, nil, nil, nil, nil,
		nil, nil, nil, nil, nil, nil, nil, nil, nil,
	)
	billingService := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(billingService.Stop)
	handler := NewOpenAIGatewayHandler(
		gatewayService,
		service.NewConcurrencyService(nil),
		billingService,
		service.NewAPIKeyService(nil, nil, nil, nil, nil, nil, cfg),
		nil, nil, nil, nil, cfg,
	)

	body := []byte(`{"model":"gpt-image-2","prompt":"draw a cat"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req
	c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{
		ID:      99,
		GroupID: &groupID,
		Group: &service.Group{
			ID:                   groupID,
			AllowImageGeneration: true,
		},
		User: &service.User{ID: 100},
	})
	c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 100, Concurrency: 0})

	handler.Images(c)

	require.Equal(t, http.StatusTooManyRequests, rec.Code)
	require.Equal(t, "rate_limit_error", gjson.GetBytes(rec.Body.Bytes(), "error.type").String())
	require.Equal(t, noAccountRateLimitedMessage, gjson.GetBytes(rec.Body.Bytes(), "error.message").String())
	require.NotEmpty(t, rec.Header().Get("Retry-After"))
}
