//go:build unit

package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type settingHandlerPublicRepoStub struct {
	values map[string]string
	err    error
}

func (s *settingHandlerPublicRepoStub) Get(ctx context.Context, key string) (*service.Setting, error) {
	panic("unexpected Get call")
}

func (s *settingHandlerPublicRepoStub) GetValue(ctx context.Context, key string) (string, error) {
	panic("unexpected GetValue call")
}

func (s *settingHandlerPublicRepoStub) Set(ctx context.Context, key, value string) error {
	panic("unexpected Set call")
}

func (s *settingHandlerPublicRepoStub) GetMultiple(ctx context.Context, keys []string) (map[string]string, error) {
	if s.err != nil {
		return nil, s.err
	}
	out := make(map[string]string, len(keys))
	for _, key := range keys {
		if value, ok := s.values[key]; ok {
			out[key] = value
		}
	}
	return out, nil
}

func (s *settingHandlerPublicRepoStub) SetMultiple(ctx context.Context, settings map[string]string) error {
	panic("unexpected SetMultiple call")
}

func (s *settingHandlerPublicRepoStub) GetAll(ctx context.Context) (map[string]string, error) {
	panic("unexpected GetAll call")
}

func (s *settingHandlerPublicRepoStub) Delete(ctx context.Context, key string) error {
	panic("unexpected Delete call")
}

func TestSettingHandler_GetPublicSettings_ExposesForceEmailOnThirdPartySignup(t *testing.T) {
	gin.SetMode(gin.TestMode)

	repo := &settingHandlerPublicRepoStub{
		values: map[string]string{
			service.SettingKeyForceEmailOnThirdPartySignup: "true",
		},
	}
	h := NewSettingHandler(service.NewSettingService(repo, &config.Config{}), "test-version")

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/settings/public", nil)

	h.GetPublicSettings(c)

	require.Equal(t, http.StatusOK, recorder.Code)

	var resp struct {
		Code int `json:"code"`
		Data struct {
			ForceEmailOnThirdPartySignup bool `json:"force_email_on_third_party_signup"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &resp))
	require.Equal(t, 0, resp.Code)
	require.True(t, resp.Data.ForceEmailOnThirdPartySignup)
}

func TestSettingHandler_GetPublicSettings_ExposesTencentCaptchaConfiguration(t *testing.T) {
	gin.SetMode(gin.TestMode)

	repo := &settingHandlerPublicRepoStub{
		values: map[string]string{
			service.SettingKeyTencentCaptchaEnabled: "true",
			service.SettingKeyTencentCaptchaAppID:   "123456789",
			service.SettingKeyTencentCaptchaRegion:  service.TencentCaptchaRegionINTL,
		},
	}
	h := NewSettingHandler(service.NewSettingService(repo, &config.Config{}), "test-version")

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/settings/public", nil)

	h.GetPublicSettings(c)

	require.Equal(t, http.StatusOK, recorder.Code)

	var resp struct {
		Code int `json:"code"`
		Data struct {
			TencentCaptchaEnabled bool   `json:"tencent_captcha_enabled"`
			TencentCaptchaAppID   string `json:"tencent_captcha_app_id"`
			TencentCaptchaRegion  string `json:"tencent_captcha_region"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &resp))
	require.Equal(t, 0, resp.Code)
	require.True(t, resp.Data.TencentCaptchaEnabled)
	require.Equal(t, "123456789", resp.Data.TencentCaptchaAppID)
	require.Equal(t, service.TencentCaptchaRegionINTL, resp.Data.TencentCaptchaRegion)
}

func TestSettingHandler_GetPublicSettings_ExposesWeChatOAuthModeCapabilities(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewSettingHandler(service.NewSettingService(&settingHandlerPublicRepoStub{
		values: map[string]string{
			service.SettingKeyWeChatConnectEnabled:             "true",
			service.SettingKeyWeChatConnectAppID:               "wx-mp-app",
			service.SettingKeyWeChatConnectAppSecret:           "wx-mp-secret",
			service.SettingKeyWeChatConnectMode:                "mp",
			service.SettingKeyWeChatConnectScopes:              "snsapi_base",
			service.SettingKeyWeChatConnectOpenEnabled:         "true",
			service.SettingKeyWeChatConnectMPEnabled:           "true",
			service.SettingKeyWeChatConnectRedirectURL:         "https://api.example.com/api/v1/auth/oauth/wechat/callback",
			service.SettingKeyWeChatConnectFrontendRedirectURL: "/auth/wechat/callback",
		},
	}, &config.Config{}), "test-version")

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/settings/public", nil)

	h.GetPublicSettings(c)

	require.Equal(t, http.StatusOK, recorder.Code)

	var resp struct {
		Code int `json:"code"`
		Data struct {
			WeChatOAuthEnabled     bool `json:"wechat_oauth_enabled"`
			WeChatOAuthOpenEnabled bool `json:"wechat_oauth_open_enabled"`
			WeChatOAuthMPEnabled   bool `json:"wechat_oauth_mp_enabled"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &resp))
	require.Equal(t, 0, resp.Code)
	require.True(t, resp.Data.WeChatOAuthEnabled)
	require.True(t, resp.Data.WeChatOAuthOpenEnabled)
	require.True(t, resp.Data.WeChatOAuthMPEnabled)
}

func TestSettingHandler_GetFavicon(t *testing.T) {
	for _, tc := range []struct {
		name, logo                  string
		status                      int
		location, body, contentType string
	}{
		{name: "png URL", logo: "https://example.com/logo.png?a=1&b=2", status: 307, location: "https://example.com/logo.png?a=1&b=2"},
		{name: "relative SVG", logo: "/logo-custom.svg", status: 307, location: "/logo-custom.svg"},
		{name: "default", status: 307, location: "/logo.svg"},
		{name: "base64 PNG", logo: "data:image/png;base64,cG5n", status: 200, body: "png", contentType: "image/png"},
		{name: "encoded SVG", logo: "data:image/svg+xml,%3Csvg%3E%3C/svg%3E", status: 200, body: "<svg></svg>", contentType: "image/svg+xml"},
		{name: "malformed data", logo: "data:image/png;base64,invalid", status: 204},
		{name: "missing comma", logo: "data:image/png;base64", status: 204},
		{name: "unsafe scheme", logo: "javascript:alert(1)", status: 204},
		{name: "protocol relative", logo: "//example.com/logo.png", status: 204},
		{name: "URL credentials", logo: "https://user:secret@example.com/logo.png", status: 204},
		{name: "header injection", logo: "/logo.svg\r\nX-Test: injected", status: 204},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &settingHandlerPublicRepoStub{values: map[string]string{service.SettingKeySiteLogo: tc.logo}}
			h := NewSettingHandler(service.NewSettingService(repo, &config.Config{}), "test")
			recorder := httptest.NewRecorder()
			router := gin.New()
			router.GET("/api/v1/settings/favicon", h.GetFavicon)
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/settings/favicon", nil))
			require.Equal(t, tc.status, recorder.Code)
			require.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))
			require.Equal(t, tc.location, recorder.Header().Get("Location"))
			if tc.body != "" {
				require.Equal(t, tc.body, recorder.Body.String())
				require.Equal(t, tc.contentType, recorder.Header().Get("Content-Type"))
				require.Equal(t, "default-src 'none'; sandbox", recorder.Header().Get("Content-Security-Policy"))
			}
		})
	}
	repo := &settingHandlerPublicRepoStub{err: errors.New("unavailable")}
	h := NewSettingHandler(service.NewSettingService(repo, &config.Config{}), "test")
	recorder := httptest.NewRecorder()
	router := gin.New()
	router.GET("/api/v1/settings/favicon", h.GetFavicon)
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/settings/favicon", nil))
	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	require.Empty(t, recorder.Body.String(), "failure must not flash an unrelated brand")
}
