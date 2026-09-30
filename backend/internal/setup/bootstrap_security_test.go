package setup

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestSetupMutationsRequireOperatorToken(t *testing.T) {
	t.Setenv("DATA_DIR", t.TempDir())
	t.Setenv("SKIP_SETUP", "false")
	t.Setenv("SETUP_BOOTSTRAP_TOKEN", "")
	gin.SetMode(gin.TestMode)
	r := gin.New()
	token, err := RegisterRoutes(r)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(token), 32)
	for _, endpoint := range []string{"/setup/test-db", "/setup/test-redis", "/setup/install"} {
		for _, supplied := range []string{"", "incorrect", token} {
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, endpoint, strings.NewReader(`{}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Setup-Token", supplied)
			r.ServeHTTP(w, req)
			want := http.StatusUnauthorized
			if supplied == token {
				want = http.StatusBadRequest // reaches validation, never DB/network work
			}
			require.Equal(t, want, w.Code, endpoint)
		}
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/setup/status", nil))
	require.Equal(t, http.StatusOK, w.Code)
	require.NotContains(t, w.Body.String(), token)
	// A URL parameter is not authorization and must not invite token leakage.
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/setup/test-db?token="+token, strings.NewReader(`{}`)))
	require.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestSetupTokenCannotReopenInstalledSystem(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("DATA_DIR", dataDir)
	t.Setenv("SKIP_SETUP", "false")
	t.Setenv("SETUP_BOOTSTRAP_TOKEN", "")
	r := gin.New()
	token, err := RegisterRoutes(r)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dataDir, InstallLockFile), []byte("installed"), 0600))
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/setup/install", strings.NewReader(`{}`))
	req.Header.Set("X-Setup-Token", token)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusForbidden, w.Code)
}

func TestSetupBootstrapTokenConfiguration(t *testing.T) {
	t.Setenv("SETUP_BOOTSTRAP_TOKEN", "weak")
	_, err := RegisterRoutes(gin.New())
	require.ErrorContains(t, err, "at least 32")
	configured := strings.Repeat("configured", 5)
	t.Setenv("SETUP_BOOTSTRAP_TOKEN", configured)
	token, err := RegisterRoutes(gin.New())
	require.NoError(t, err)
	require.Equal(t, configured, token)
}
