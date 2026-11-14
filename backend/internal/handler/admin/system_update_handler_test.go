//go:build unit

package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/authz"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type updaterHandlerFake struct {
	calls             int
	commit, operation string
	idleError         error
	startHook         func(context.Context)
}

func (f *updaterHandlerFake) Status(context.Context) (*service.OnlineUpdateStatus, error) {
	f.calls++
	return &service.OnlineUpdateStatus{Enabled: true, Repository: "holeenlu/sub2api", Branch: "main"}, nil
}
func (f *updaterHandlerFake) Check(ctx context.Context) (*service.OnlineUpdateStatus, error) {
	return f.Status(ctx)
}
func (f *updaterHandlerFake) Start(ctx context.Context, commit, operation string) (*service.OnlineUpdateStatus, error) {
	if f.startHook != nil {
		f.startHook(ctx)
	}
	f.commit, f.operation = commit, operation
	return f.Status(ctx)
}
func (f *updaterHandlerFake) EnsureIdle(context.Context) error { return f.idleError }

type updateHandlerGate struct {
	err     error
	entered bool
}

func (f *updateHandlerGate) WithIdleMaintenance(ctx context.Context, fn func(context.Context) error) error {
	f.entered = true
	if f.err != nil {
		return f.err
	}
	return fn(ctx)
}

type updaterHandlerLock struct{}

func (updaterHandlerLock) Acquire(context.Context, string) (*service.SystemOperationLock, error) {
	return &service.SystemOperationLock{}, nil
}
func (updaterHandlerLock) Release(context.Context, *service.SystemOperationLock, bool, string) error {
	return nil
}
func runUpdaterHandler(t *testing.T, method, body, role, auth string, supplier bool, h *SystemHandler, fn gin.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		ctx := authz.WithSubject(c.Request.Context(), authz.Subject{UserID: 7, Role: role})
		c.Request = c.Request.WithContext(authz.WithSupplierOrigin(ctx, supplier))
		c.Set("auth_method", auth)
		c.Next()
	})
	router.Handle(method, "/api/v1/admin/system/update", fn)
	request := httptest.NewRequest(method, "/api/v1/admin/system/update", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "fixed-ui-attempt")
	out := httptest.NewRecorder()
	router.ServeHTTP(out, request)
	return out
}
func TestSystemHandlerOnlineUpdateRejectsMachineKeysRolesAndSupplierOrigin(t *testing.T) {
	for _, tc := range []struct {
		role, auth string
		supplier   bool
	}{
		{authz.SuperAdmin, "admin_api_key", false}, {authz.Admin, "jwt", false}, {authz.User, "jwt", false}, {authz.VendorLevel1, "jwt", false}, {authz.VendorLevel2, "jwt", false}, {authz.SuperAdmin, "jwt", true}, {authz.SuperAdmin, "", false},
	} {
		for _, action := range []string{"status", "check", "start"} {
			fake := &updaterHandlerFake{}
			h := NewSystemHandler(service.NewBuildStatusService("1.1.0"), nil)
			h.SetOnlineUpdater(fake, &updateHandlerGate{})
			method, fn := http.MethodGet, h.GetUpdateStatus
			if action == "check" {
				method, fn = http.MethodPost, h.CheckUpdate
			}
			if action == "start" {
				method, fn = http.MethodPost, h.StartUpdate
			}
			out := runUpdaterHandler(t, method, `{"target_commit":"`+strings.Repeat("b", 40)+`"}`, tc.role, tc.auth, tc.supplier, h, fn)
			require.Equal(t, http.StatusForbidden, out.Code)
			require.Zero(t, fake.calls)
		}
	}
}
func TestSystemHandlerOnlineUpdateValidatesExactPayloadBeforeHostOrLocks(t *testing.T) {
	for _, body := range []string{`{}`, `{"target_commit":"main"}`, `{"target_commit":"` + strings.Repeat("B", 40) + `"}`, `{"target_commit":"` + strings.Repeat("b", 40) + `","image":"evil"}`, `{"target_commit":"` + strings.Repeat("b", 40) + `"}{}`, strings.Repeat(" ", 4097) + `{}`} {
		fake := &updaterHandlerFake{}
		h := NewSystemHandler(service.NewBuildStatusService("1.1.0"), nil)
		h.SetOnlineUpdater(fake, &updateHandlerGate{})
		out := runUpdaterHandler(t, http.MethodPost, body, authz.SuperAdmin, "jwt", false, h, h.StartUpdate)
		require.Equal(t, http.StatusBadRequest, out.Code, body)
		require.Zero(t, fake.calls)
	}
}
func TestSystemHandlerOnlineUpdateRetainsHostOperationIdentityAndBackupExclusion(t *testing.T) {
	fake := &updaterHandlerFake{}
	gate := &updateHandlerGate{}
	h := NewSystemHandler(service.NewBuildStatusService("1.1.0"), nil)
	h.lockSvc = updaterHandlerLock{}
	h.SetOnlineUpdater(fake, gate)
	body := `{"target_commit":"` + strings.Repeat("b", 40) + `"}`
	out := runUpdaterHandler(t, http.MethodPost, body, authz.SuperAdmin, "jwt", false, h, h.StartUpdate)
	require.Equal(t, http.StatusOK, out.Code, out.Body.String())
	require.True(t, gate.entered)
	require.Equal(t, strings.Repeat("b", 40), fake.commit)
	operation := fake.operation
	require.True(t, strings.HasPrefix(operation, "sysop-"))
	out = runUpdaterHandler(t, http.MethodPost, body, authz.SuperAdmin, "jwt", false, h, h.StartUpdate)
	require.Equal(t, http.StatusOK, out.Code, out.Body.String())
	require.Equal(t, operation, fake.operation)
	gate.err = service.ErrSystemOperationBusy
	before := fake.calls
	out = runUpdaterHandler(t, http.MethodPost, body, authz.SuperAdmin, "jwt", false, h, h.StartUpdate)
	require.Equal(t, http.StatusConflict, out.Code, out.Body.String())
	require.Equal(t, before, fake.calls)
	var envelope map[string]any
	require.NoError(t, json.Unmarshal(out.Body.Bytes(), &envelope))
}

func TestSystemHandlerOnlineUpdateFinishesBoundedAdmissionAfterBrowserCancellation(t *testing.T) {
	fake := &updaterHandlerFake{}
	fake.startHook = func(ctx context.Context) {
		require.NoError(t, ctx.Err())
		deadline, ok := ctx.Deadline()
		require.True(t, ok)
		require.LessOrEqual(t, time.Until(deadline), 20*time.Second)
	}
	h := NewSystemHandler(service.NewBuildStatusService("1.1.0"), nil)
	h.lockSvc = updaterHandlerLock{}
	h.SetOnlineUpdater(fake, &updateHandlerGate{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ctx = authz.WithSubject(ctx, authz.Subject{UserID: 7, Role: authz.SuperAdmin})
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set("auth_method", "jwt"); c.Next() })
	router.POST("/api/v1/admin/system/update", h.StartUpdate)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/system/update", strings.NewReader(`{"target_commit":"`+strings.Repeat("b", 40)+`"}`)).WithContext(ctx)
	req.Header.Set("Idempotency-Key", "cancelled-browser")
	out := httptest.NewRecorder()
	router.ServeHTTP(out, req)
	require.Equal(t, http.StatusOK, out.Code, out.Body.String())
	require.Equal(t, 1, fake.calls)
}
