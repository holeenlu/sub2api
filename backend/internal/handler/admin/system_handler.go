package admin

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/authz"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/pkg/sysutil"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// SystemHandler handles system-related operations
type SystemHandler struct {
	buildStatus systemBuildStatusService
	lockSvc     systemOperationLocker
	updater     systemOnlineUpdater
	backupGate  systemUpdateBackupGate
}

// systemBuildStatusService exposes version metadata and manual deployment status only.
type systemBuildStatusService interface {
	Status() service.BuildStatus
}

type systemOperationLocker interface {
	Acquire(context.Context, string) (*service.SystemOperationLock, error)
	Release(context.Context, *service.SystemOperationLock, bool, string) error
}

type systemOnlineUpdater interface {
	Status(context.Context) (*service.OnlineUpdateStatus, error)
	Check(context.Context) (*service.OnlineUpdateStatus, error)
	Start(context.Context, string, string) (*service.OnlineUpdateStatus, error)
	EnsureIdle(context.Context) error
}
type systemUpdateBackupGate interface {
	WithIdleMaintenance(context.Context, func(context.Context) error) error
}

// SetOnlineUpdater is called during dependency construction, before serving.
func (h *SystemHandler) SetOnlineUpdater(updater systemOnlineUpdater, backup systemUpdateBackupGate) {
	h.updater, h.backupGate = updater, backup
}

// NewSystemHandler creates a new SystemHandler
func NewSystemHandler(buildStatus systemBuildStatusService, lockSvc *service.SystemOperationLockService) *SystemHandler {
	return &SystemHandler{
		buildStatus: buildStatus,
		lockSvc:     lockSvc,
	}
}

// GetVersion returns the current version
// GET /api/v1/admin/system/version
func (h *SystemHandler) GetVersion(c *gin.Context) {
	info := h.buildStatus.Status()
	// Portal and admin clients receive the running backend version.
	// This endpoint retains the existing live super-administrator authorization.
	response.Success(c, gin.H{
		"version":          info.CurrentVersion,
		"upstream_version": info.UpstreamVersion,
		"build_commit":     info.BuildCommit,
	})
}

// Update endpoints accept only a live human super-administrator session. This
// remains required when the site's optional step-up switch is disabled.
func requireOnlineUpdateSession(c *gin.Context) bool {
	actor, ok := authz.FromContext(c.Request.Context())
	supplier, _ := authz.SupplierOrigin(c.Request.Context())
	if !ok || actor.UserID <= 0 || actor.Role != authz.SuperAdmin || c.GetString("auth_method") != "jwt" || supplier {
		response.Forbidden(c, "A super-administrator session on the main site is required")
		return false
	}
	return true
}
func (h *SystemHandler) GetUpdateStatus(c *gin.Context) {
	if !requireOnlineUpdateSession(c) {
		return
	}
	if h.updater == nil {
		response.ErrorFrom(c, service.ErrOnlineUpdaterUnavailable)
		return
	}
	status, err := h.updater.Status(c.Request.Context())
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, status)
}
func (h *SystemHandler) CheckUpdate(c *gin.Context) {
	if !requireOnlineUpdateSession(c) {
		return
	}
	if h.updater == nil {
		response.ErrorFrom(c, service.ErrOnlineUpdaterUnavailable)
		return
	}
	var req struct{}
	if !decodeOnlineUpdateRequest(c, &req) {
		return
	}
	status, err := h.updater.Check(c.Request.Context())
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, status)
}
func (h *SystemHandler) StartUpdate(c *gin.Context) {
	if !requireOnlineUpdateSession(c) {
		return
	}
	var req struct {
		TargetCommit string `json:"target_commit"`
	}
	if !decodeOnlineUpdateRequest(c, &req) {
		return
	}
	if !service.ValidOnlineUpdateCommit(req.TargetCommit) {
		response.BadRequest(c, "Invalid target commit")
		return
	}
	if h.updater == nil || h.backupGate == nil {
		response.ErrorFrom(c, service.ErrOnlineUpdaterUnavailable)
		return
	}
	operationID := buildSystemOperationID(c, "update")
	executeAdminIdempotentJSON(c, "admin.system.update", req, service.DefaultSystemOperationIdempotencyTTL(), func(ctx context.Context) (any, error) {
		// Browser cancellation cannot release the backup admission boundary while
		// the host is still durably accepting a command. The ACK contains no remote
		// GitHub IO; the independently bounded handoff survives a disconnected UI.
		admissionCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 20*time.Second)
		defer cancel()
		ctx = admissionCtx
		_, release, err := h.acquireSystemLock(ctx, operationID)
		if err != nil {
			return nil, err
		}
		succeeded := false
		defer func() { release("", succeeded) }()
		var status *service.OnlineUpdateStatus
		// Backup/restore records and host job publication share the durable writer
		// boundary. The host job, rather than this short HTTP lock, owns the update.
		err = h.backupGate.WithIdleMaintenance(ctx, func(ctx context.Context) error {
			var err error
			status, err = h.updater.Start(ctx, req.TargetCommit, operationID)
			return err
		})
		if err != nil {
			return nil, err
		}
		succeeded = true
		return status, nil
	})
}
func decodeOnlineUpdateRequest(c *gin.Context, value any) bool {
	if c.Request.Body == nil {
		response.BadRequest(c, "Invalid update request")
		return false
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(value) != nil {
		response.BadRequest(c, "Invalid update request")
		return false
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		response.BadRequest(c, "Invalid update request")
		return false
	}
	return true
}

// RestartService restarts the systemd service
// POST /api/v1/admin/system/restart
func (h *SystemHandler) RestartService(c *gin.Context) {
	operationID := buildSystemOperationID(c, "restart")
	payload := gin.H{"operation_id": operationID}
	executeAdminIdempotentJSON(c, "admin.system.restart", payload, service.DefaultSystemOperationIdempotencyTTL(), func(ctx context.Context) (any, error) {
		lock, release, err := h.acquireSystemLock(ctx, operationID)
		if err != nil {
			return nil, err
		}
		if h.updater != nil {
			if err := h.updater.EnsureIdle(ctx); err != nil {
				release("ONLINE_UPDATE_BUSY", false)
				return nil, err
			}
		}

		// Schedule service restart in background after sending response
		// This ensures the client receives the success response before the service restarts
		go func() {
			// Wait a moment to ensure the response is sent
			time.Sleep(500 * time.Millisecond)
			// Keep the shared lease until the exit occurs, rather than releasing it
			// before the delayed restart and allowing a new host update in that gap.
			sysutil.RestartServiceAsync()
			time.Sleep(250 * time.Millisecond)
			release("", true) // Non-Linux fallback; Linux has already exited.
		}()
		return gin.H{
			"message":      "Service restart initiated",
			"operation_id": lock.OperationID(),
		}, nil
	})
}

func (h *SystemHandler) acquireSystemLock(
	ctx context.Context,
	operationID string,
) (*service.SystemOperationLock, func(string, bool), error) {
	if h.lockSvc == nil {
		return nil, nil, service.ErrIdempotencyStoreUnavail
	}
	lock, err := h.lockSvc.Acquire(ctx, operationID)
	if err != nil {
		return nil, nil, err
	}
	release := func(reason string, succeeded bool) {
		releaseCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = h.lockSvc.Release(releaseCtx, lock, succeeded, reason)
	}
	return lock, release, nil
}

func buildSystemOperationID(c *gin.Context, operation string) string {
	key := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if key == "" {
		return "sysop-" + operation + "-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	actorScope := "admin:0"
	if subject, ok := middleware2.GetAuthSubjectFromContext(c); ok {
		actorScope = "admin:" + strconv.FormatInt(subject.UserID, 10)
	}
	seed := operation + "|" + actorScope + "|" + c.FullPath() + "|" + key
	hash := service.HashIdempotencyKey(seed)
	if len(hash) > 24 {
		hash = hash[:24]
	}
	return "sysop-" + hash
}
