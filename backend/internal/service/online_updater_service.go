package service

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const OnlineUpdateReleaseURL = "https://github.com/holeenlu/sub2api/releases/tag/NewAPI/latest"
const onlineUpdateMaxResponse = 128 << 10

var onlineUpdateCommit = regexp.MustCompile(`^[a-f0-9]{40}$`)
var onlineUpdateVersion = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(?:\.[0-9]+)?$`)
var ErrOnlineUpdaterUnavailable = infraerrors.ServiceUnavailable("ONLINE_UPDATER_UNAVAILABLE", "Online update service is unavailable")
var ErrOnlineUpdaterDisabled = infraerrors.ServiceUnavailable("ONLINE_UPDATER_DISABLED", "Online update is not configured on this server")
var ErrOnlineUpdateBusy = infraerrors.Conflict("ONLINE_UPDATE_BUSY", "An update or recovery operation is in progress")

// OnlineUpdateStatus is the limited control-plane projection of the host job.
// It never contains images, commands, filesystem paths, environment or credentials.
type OnlineUpdateStatus struct {
	Enabled    bool                `json:"enabled"`
	Repository string              `json:"repository"`
	Branch     string              `json:"branch"`
	Current    OnlineUpdateBuild   `json:"current"`
	Available  bool                `json:"available"`
	Target     *OnlineUpdateTarget `json:"target,omitempty"`
	Job        *OnlineUpdateJob    `json:"job,omitempty"`
	Message    string              `json:"message,omitempty"`
}
type OnlineUpdateBuild struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
}
type OnlineUpdateTarget struct {
	Version                 string `json:"version"`
	Commit                  string `json:"commit"`
	ReleaseURL              string `json:"release_url"`
	RequiresManualMigration bool   `json:"requires_manual_migration"`
}
type OnlineUpdateJob struct {
	ID           string `json:"id"`
	State        string `json:"state"`
	Phase        string `json:"phase"`
	Message      string `json:"message"`
	TargetCommit string `json:"target_commit"`
	StartedAt    string `json:"started_at"`
	FinishedAt   string `json:"finished_at,omitempty"`
}

type OnlineUpdaterService struct {
	enabled bool
	client  *http.Client
	current OnlineUpdateBuild
}

// NewOnlineUpdaterService can only connect to the explicitly configured local
// socket. HTTP proxies, DNS and HTTP redirects are never used for host control.
func NewOnlineUpdaterService(socket string, build BuildStatus) *OnlineUpdaterService {
	s := &OnlineUpdaterService{current: OnlineUpdateBuild{Version: build.CurrentVersion, Commit: build.BuildCommit}}
	socket = strings.TrimSpace(socket)
	if socket == "" {
		return s
	}
	s.enabled = true
	if !filepath.IsAbs(socket) || strings.ContainsAny(socket, "\x00\r\n") {
		return s
	}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, "unix", socket)
		},
		MaxIdleConns: 2, MaxIdleConnsPerHost: 2, IdleConnTimeout: 30 * time.Second,
		ResponseHeaderTimeout: 120 * time.Second,
	}
	s.client = &http.Client{Transport: transport, Timeout: 125 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return s
}
func (s *OnlineUpdaterService) Status(ctx context.Context) (*OnlineUpdateStatus, error) {
	if s == nil || !s.enabled {
		status := &OnlineUpdateStatus{Repository: "holeenlu/sub2api", Branch: "main"}
		if s != nil {
			status.Current = s.current
		}
		return status, nil
	}
	statusCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	return s.request(statusCtx, http.MethodGet, "/status", nil)
}
func (s *OnlineUpdaterService) Check(ctx context.Context) (*OnlineUpdateStatus, error) {
	return s.request(ctx, http.MethodPost, "/check", struct{}{})
}
func (s *OnlineUpdaterService) Start(ctx context.Context, commit, operationID string) (*OnlineUpdateStatus, error) {
	if !ValidOnlineUpdateCommit(commit) || operationID == "" || len(operationID) > 128 {
		return nil, infraerrors.BadRequest("INVALID_UPDATE_REQUEST", "Invalid update request")
	}
	return s.request(ctx, http.MethodPost, "/update", struct {
		TargetCommit string `json:"target_commit"`
		OperationID  string `json:"operation_id"`
	}{commit, operationID})
}
func ValidOnlineUpdateCommit(commit string) bool { return onlineUpdateCommit.MatchString(commit) }
func (s *OnlineUpdaterService) EnsureIdle(ctx context.Context) error {
	status, err := s.Status(ctx)
	if err != nil {
		return err
	}
	if status.Job != nil && (status.Job.State == "running" || status.Job.State == "manual_intervention_required") {
		return ErrOnlineUpdateBusy
	}
	return nil
}
func (s *OnlineUpdaterService) request(ctx context.Context, method, path string, payload any) (*OnlineUpdateStatus, error) {
	if s == nil || !s.enabled {
		return nil, ErrOnlineUpdaterDisabled
	}
	if s.client == nil {
		return nil, ErrOnlineUpdaterUnavailable
	}
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return nil, ErrOnlineUpdaterUnavailable
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://localhost"+path, body)
	if err != nil {
		return nil, ErrOnlineUpdaterUnavailable
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, ErrOnlineUpdaterUnavailable
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, onlineUpdateMaxResponse+1))
	if err != nil || len(data) > onlineUpdateMaxResponse {
		return nil, ErrOnlineUpdaterUnavailable
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Only known safe errors cross the privilege boundary; host output is never
		// returned or attached as an error cause, including malformed JSON responses.
		var problem struct {
			Code string `json:"code"`
		}
		_ = json.Unmarshal(data, &problem)
		switch problem.Code {
		case "UPDATE_BUSY", "ONLINE_UPDATE_BUSY", "RECOVERY_REQUIRED":
			return nil, ErrOnlineUpdateBusy
		case "MIGRATION_REQUIRED", "MANUAL_MIGRATION_REQUIRED":
			return nil, infraerrors.Conflict("UPDATE_MIGRATION_REQUIRED", "This release requires a manual database upgrade")
		case "TARGET_CHANGED", "STALE_TARGET", "RELEASE_CHANGED":
			return nil, infraerrors.Conflict("UPDATE_TARGET_CHANGED", "The published release changed; check for updates again")
		}
		if resp.StatusCode == http.StatusConflict {
			return nil, infraerrors.Conflict("ONLINE_UPDATE_CONFLICT", "The update cannot start; refresh its status")
		}
		if resp.StatusCode == http.StatusBadRequest {
			return nil, infraerrors.BadRequest("INVALID_UPDATE_REQUEST", "The update request was rejected")
		}
		return nil, ErrOnlineUpdaterUnavailable
	}
	var status OnlineUpdateStatus
	if json.Unmarshal(data, &status) != nil || !validOnlineUpdateStatus(&status) {
		return nil, ErrOnlineUpdaterUnavailable
	}
	return &status, nil
}
func validOnlineUpdateStatus(s *OnlineUpdateStatus) bool {
	if s.Repository != "holeenlu/sub2api" || s.Branch != "main" || !s.Enabled || !onlineUpdateVersion.MatchString(s.Current.Version) || !ValidOnlineUpdateCommit(s.Current.Commit) {
		return false
	}
	if s.Target != nil && (!onlineUpdateVersion.MatchString(s.Target.Version) || !ValidOnlineUpdateCommit(s.Target.Commit) || s.Target.ReleaseURL != OnlineUpdateReleaseURL) {
		return false
	}
	if s.Available && s.Target == nil {
		return false
	}
	if len(s.Message) > 512 {
		return false
	}
	if s.Job != nil {
		j := s.Job
		if j.ID == "" || len(j.ID) > 128 || len(j.Phase) > 64 || len(j.Message) > 512 || !ValidOnlineUpdateCommit(j.TargetCommit) {
			return false
		}
		switch j.State {
		case "running", "succeeded", "failed", "manual_intervention_required":
		default:
			return false
		}
	}
	return true
}
