//go:build unit

package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type faviconSettingsRepo struct {
	service.SettingRepository
	logo string
	err  error
}

func (r *faviconSettingsRepo) GetMultiple(ctx context.Context, keys []string) (map[string]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r.err != nil {
		return nil, r.err
	}
	values := make(map[string]string)
	for _, key := range keys {
		if key == service.SettingKeySiteLogo {
			values[key] = r.logo
		}
	}
	return values, nil
}

func faviconHandlerRouter(repo *faviconSettingsRepo) *gin.Engine {
	h := NewSettingHandler(service.NewSettingService(repo, &config.Config{}), "test")
	router := gin.New()
	router.GET("/api/v1/settings/favicon", h.GetFavicon)
	return router
}

func TestSettingHandler_GetFavicon(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svg := `<svg xmlns="http://www.w3.org/2000/svg"><text>A+B</text></svg>`
	activeSVG := `<svg xmlns="http://www.w3.org/2000/svg" onload="alert(1)"><script>alert(1)</script></svg>`
	for _, tc := range []struct {
		name, logo                  string
		status                      int
		location, body, contentType string
	}{
		{name: "png URL", logo: "https://example.com/logo.png?a=1&b=2", status: 307, location: "https://example.com/logo.png?a=1&b=2"},
		{name: "HTTP URL", logo: "http://example.com/logo.ico", status: 307, location: "http://example.com/logo.ico"},
		{name: "relative SVG", logo: "/logo-custom.svg", status: 307, location: "/logo-custom.svg"},
		{name: "trim URL", logo: "  /saved-logo.svg  ", status: 307, location: "/saved-logo.svg"},
		{name: "unconfigured neutral asset", status: 307, location: "/logo.svg"},
		{name: "blank neutral asset", logo: "  ", status: 307, location: "/logo.svg"},
		{name: "base64 PNG", logo: "data:image/png;base64,cG5n", status: 200, body: "png", contentType: "image/png"},
		{name: "case insensitive data metadata", logo: "DATA:image/png;BASE64,cG5n", status: 200, body: "png", contentType: "image/png"},
		{name: "encoded SVG", logo: "data:image/svg+xml," + url.PathEscape(svg), status: 200, body: svg, contentType: "image/svg+xml"},
		{name: "SVG charset and literal plus", logo: "data:image/svg+xml;charset=utf-8,%3Csvg%3EA+B%3C/svg%3E", status: 200, body: "<svg>A+B</svg>", contentType: "image/svg+xml"},
		{name: "SVG remains sandboxed", logo: "data:image/svg+xml," + url.PathEscape(activeSVG), status: 200, body: activeSVG, contentType: "image/svg+xml"},
		{name: "malformed base64", logo: "data:image/png;base64,invalid", status: 204},
		{name: "missing comma", logo: "data:image/png;base64", status: 204},
		{name: "empty payload", logo: "data:image/png;base64,", status: 204},
		{name: "malformed escaping", logo: "data:image/svg+xml,%GG", status: 204},
		{name: "HTML data rejected", logo: "data:text/html,%3Cscript%3Ealert(1)%3C/script%3E", status: 204},
		{name: "unsafe scheme", logo: "javascript:alert(1)", status: 204},
		{name: "local file rejected", logo: "file:///etc/passwd", status: 204},
		{name: "protocol relative", logo: "//example.com/logo.png", status: 204},
		{name: "URL credentials", logo: "https://user:secret@example.com/logo.png", status: 204},
		{name: "empty URL credentials", logo: "https://@example.com/logo.png", status: 204},
		{name: "port without hostname", logo: "https://:443/logo.png", status: 204},
		{name: "missing host", logo: "https:///logo.png", status: 204},
		{name: "bare path", logo: "logo.png", status: 204},
		{name: "header injection", logo: "/logo.svg\r\nX-Test: injected", status: 204},
		{name: "backslash", logo: "/\\example.com/logo.svg", status: 204},
		{name: "embedded tab", logo: "https://exa\tmple.com/logo.svg", status: 204},
	} {
		t.Run(tc.name, func(t *testing.T) {
			router := faviconHandlerRouter(&faviconSettingsRepo{logo: tc.logo})
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/settings/favicon", nil))
			require.Equal(t, tc.status, recorder.Code)
			require.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))
			require.Equal(t, tc.location, recorder.Header().Get("Location"))
			if tc.status == http.StatusOK {
				require.Equal(t, tc.body, recorder.Body.String())
				require.Equal(t, tc.contentType, recorder.Header().Get("Content-Type"))
				require.Equal(t, "nosniff", recorder.Header().Get("X-Content-Type-Options"))
				require.Equal(t, "default-src 'none'; sandbox", recorder.Header().Get("Content-Security-Policy"))
			}
			if tc.status == http.StatusNoContent {
				require.Empty(t, recorder.Body.String(), "invalid configured logos must not fall back to another brand")
			}
		})
	}
}

func TestSettingHandler_GetFaviconReadsLiveBrandAndNeverFetchesURL(t *testing.T) {
	var fetched atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fetched.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(target.Close)
	repo := &faviconSettingsRepo{logo: target.URL + "/logo.svg"}
	router := faviconHandlerRouter(repo)
	for _, logo := range []string{repo.logo, "/updated-logo.svg"} {
		repo.logo = logo
		recorder := httptest.NewRecorder()
		// Client query parameters cannot replace the persisted branding source.
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/settings/favicon?url=http://169.254.169.254/latest/meta-data/&site_logo=/other.svg", nil))
		require.Equal(t, http.StatusTemporaryRedirect, recorder.Code)
		require.Equal(t, logo, recorder.Header().Get("Location"))
		require.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))
	}
	require.Zero(t, fetched.Load(), "the handler must redirect without downloading the target")
}

func TestSettingHandler_GetFaviconFailureDoesNotFallback(t *testing.T) {
	for _, canceled := range []bool{false, true} {
		repo := &faviconSettingsRepo{err: errors.New("private storage failure")}
		req := httptest.NewRequest(http.MethodGet, "/api/v1/settings/favicon", nil)
		if canceled {
			ctx, cancel := context.WithCancel(req.Context())
			cancel()
			req = req.WithContext(ctx)
		}
		recorder := httptest.NewRecorder()
		faviconHandlerRouter(repo).ServeHTTP(recorder, req)
		require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
		require.Empty(t, recorder.Header().Get("Location"))
		require.Empty(t, recorder.Body.String(), "storage errors and fallback branding must not leak")
		require.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))
	}
}
