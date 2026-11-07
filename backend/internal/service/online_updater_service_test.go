//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func updaterUnixFixture(t *testing.T, handler http.HandlerFunc) string {
	t.Helper()
	directory, err := os.MkdirTemp("/tmp", "updater-")
	require.NoError(t, err)
	socket := filepath.Join(directory, "control.sock")
	listener, err := net.Listen("unix", socket)
	require.NoError(t, err)
	server := &http.Server{Handler: handler, ReadHeaderTimeout: time.Second}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close(); _ = os.RemoveAll(directory) })
	return socket
}
func updaterStatusFixture() OnlineUpdateStatus {
	return OnlineUpdateStatus{Enabled: true, Repository: "holeenlu/sub2api", Branch: "main", Current: OnlineUpdateBuild{Version: "1.1.0", Commit: strings.Repeat("a", 40)}, Available: true, Target: &OnlineUpdateTarget{Version: "1.1.1", Commit: strings.Repeat("b", 40), ReleaseURL: OnlineUpdateReleaseURL}}
}
func TestOnlineUpdaterUnconfiguredPreservesBuildAndDoesNotPerformIO(t *testing.T) {
	svc := NewOnlineUpdaterService("", BuildStatus{CurrentVersion: "1.1.0", BuildCommit: "abcdef01"})
	state, err := svc.Status(context.Background())
	require.NoError(t, err)
	require.False(t, state.Enabled)
	require.Equal(t, "abcdef01", state.Current.Commit)
	require.NoError(t, svc.EnsureIdle(context.Background()))
	_, err = svc.Check(context.Background())
	require.ErrorIs(t, err, ErrOnlineUpdaterDisabled)
}
func TestOnlineUpdaterUsesFixedUnixMethodsAndHostJobIdentity(t *testing.T) {
	paths := make(chan string, 3)
	socket := updaterUnixFixture(t, func(w http.ResponseWriter, r *http.Request) {
		paths <- r.Method + " " + r.URL.Path
		require.Equal(t, "localhost", r.Host)
		require.Empty(t, r.Header.Get("Authorization"))
		if r.URL.Path == "/update" {
			var payload map[string]string
			require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
			require.Equal(t, map[string]string{"target_commit": strings.Repeat("b", 40), "operation_id": "sysop-test"}, payload)
		}
		_ = json.NewEncoder(w).Encode(updaterStatusFixture())
	})
	svc := NewOnlineUpdaterService(socket, BuildStatus{})
	_, err := svc.Status(context.Background())
	require.NoError(t, err)
	_, err = svc.Check(context.Background())
	require.NoError(t, err)
	_, err = svc.Start(context.Background(), strings.Repeat("b", 40), "sysop-test")
	require.NoError(t, err)
	require.Equal(t, "GET /status", <-paths)
	require.Equal(t, "POST /check", <-paths)
	require.Equal(t, "POST /update", <-paths)
}
func TestOnlineUpdaterRejectsMalformedMetadataAndOversizedResponses(t *testing.T) {
	for _, name := range []string{"oversized", "wrong_repository", "wrong_branch", "unsafe_link", "no_target", "unknown_job"} {
		t.Run(name, func(t *testing.T) {
			socket := updaterUnixFixture(t, func(w http.ResponseWriter, r *http.Request) {
				status := updaterStatusFixture()
				switch name {
				case "oversized":
					_, _ = w.Write([]byte(strings.Repeat("x", onlineUpdateMaxResponse+1)))
					return
				case "wrong_repository":
					status.Repository = "attacker/repo"
				case "wrong_branch":
					status.Branch = "NewAPI"
				case "unsafe_link":
					status.Target.ReleaseURL = "javascript:alert(1)"
				case "no_target":
					status.Target = nil
				case "unknown_job":
					status.Job = &OnlineUpdateJob{ID: "job", State: "mystery", TargetCommit: strings.Repeat("b", 40)}
				}
				_ = json.NewEncoder(w).Encode(status)
			})
			_, err := NewOnlineUpdaterService(socket, BuildStatus{}).Status(context.Background())
			require.ErrorIs(t, err, ErrOnlineUpdaterUnavailable)
		})
	}
}
func TestOnlineUpdaterErrorsNeverLeakHostOutputAndCancellationStopsIO(t *testing.T) {
	t.Run("errors", func(t *testing.T) {
		socket := updaterUnixFixture(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(503)
			_, _ = w.Write([]byte(`{"code":"DOCKER_ERROR","message":"password=secret /private/root/socket"}`))
		})
		_, err := NewOnlineUpdaterService(socket, BuildStatus{}).Check(context.Background())
		require.ErrorIs(t, err, ErrOnlineUpdaterUnavailable)
		require.NotContains(t, err.Error(), "secret")
		require.NotContains(t, err.Error(), "socket")
	})
	t.Run("cancel", func(t *testing.T) {
		started := make(chan struct{})
		socket := updaterUnixFixture(t, func(w http.ResponseWriter, r *http.Request) { close(started); <-r.Context().Done() })
		ctx, cancel := context.WithCancel(context.Background())
		result := make(chan error, 1)
		go func() { _, err := NewOnlineUpdaterService(socket, BuildStatus{}).Check(ctx); result <- err }()
		<-started
		cancel()
		require.ErrorIs(t, <-result, context.Canceled)
	})
}
func TestOnlineUpdaterBlocksMaintenanceDuringRunningOrUncertainHostJobs(t *testing.T) {
	state := atomic.Value{}
	state.Store("running")
	socket := updaterUnixFixture(t, func(w http.ResponseWriter, r *http.Request) {
		status := updaterStatusFixture()
		status.Job = &OnlineUpdateJob{ID: "job", State: state.Load().(string), TargetCommit: strings.Repeat("b", 40)}
		_ = json.NewEncoder(w).Encode(status)
	})
	svc := NewOnlineUpdaterService(socket, BuildStatus{})
	for _, value := range []string{"running", "manual_intervention_required"} {
		state.Store(value)
		require.ErrorIs(t, svc.EnsureIdle(context.Background()), ErrOnlineUpdateBusy)
	}
	state.Store("succeeded")
	require.NoError(t, svc.EnsureIdle(context.Background()))
	for _, commit := range []string{"main", strings.Repeat("b", 39), strings.Repeat("B", 40), strings.Repeat("b", 40) + ";echo bad"} {
		_, err := svc.Start(context.Background(), commit, "sysop-test")
		require.Error(t, err)
	}
}
func TestOnlineUpdaterBackupAdmissionRetainsLongRunningOwnership(t *testing.T) {
	repo := newMockSettingRepo()
	svc := newTestBackupService(t, repo, &mockDumper{}, newMockObjectStore())
	ctx := context.Background()
	hostBusy := false
	svc.SetLeaderLock(&fakeLeaderLockCache{}, nil)
	svc.SetOnlineUpdateGuard(func(context.Context) error {
		if hostBusy {
			return ErrOnlineUpdateBusy
		}
		return nil
	})
	require.NoError(t, svc.saveRecord(ctx, &BackupRecord{ID: "backup", Status: "running"}))
	invoked := false
	err := svc.WithIdleMaintenance(ctx, func(context.Context) error { invoked = true; return nil })
	require.ErrorIs(t, err, ErrSystemOperationBusy)
	require.False(t, invoked)
	require.NoError(t, svc.saveRecord(ctx, &BackupRecord{ID: "backup", Status: "completed"}))
	require.NoError(t, svc.WithIdleMaintenance(ctx, func(context.Context) error { hostBusy = true; return nil }))
	require.ErrorIs(t, svc.saveRecord(ctx, &BackupRecord{ID: "new-backup", Status: "running"}), ErrOnlineUpdateBusy)
	_, err = svc.beginBackupRestore(ctx, "backup")
	require.ErrorIs(t, err, ErrOnlineUpdateBusy)
	hostBusy = false
	_, err = svc.beginBackupRestore(ctx, "backup")
	require.NoError(t, err)
	err = svc.WithIdleMaintenance(ctx, func(context.Context) error { return errors.New("must not run") })
	require.ErrorIs(t, err, ErrSystemOperationBusy)
}
func TestOnlineUpdaterBackupRecordWriterSerializesPeerAdmission(t *testing.T) {
	repo := newMockSettingRepo()
	writer := newTestBackupService(t, repo, &mockDumper{}, newMockObjectStore())
	peer := newTestBackupService(t, repo, &mockDumper{}, newMockObjectStore())
	cache := &fakeLeaderLockCache{}
	writer.SetLeaderLock(cache, nil)
	peer.SetLeaderLock(cache, nil)
	var hostBusy atomic.Bool
	peer.SetOnlineUpdateGuard(func(context.Context) error {
		if hostBusy.Load() {
			return ErrOnlineUpdateBusy
		}
		return nil
	})
	entered, proceed := make(chan struct{}), make(chan struct{})
	updateDone := make(chan error, 1)
	go func() {
		updateDone <- writer.WithIdleMaintenance(context.Background(), func(context.Context) error { close(entered); <-proceed; hostBusy.Store(true); return nil })
	}()
	<-entered
	backupDone := make(chan error, 1)
	go func() {
		backupDone <- peer.saveRecord(context.Background(), &BackupRecord{ID: "peer", Status: "running"})
	}()
	select {
	case err := <-backupDone:
		t.Fatalf("peer escaped writer lock: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	close(proceed)
	require.NoError(t, <-updateDone)
	require.ErrorIs(t, <-backupDone, ErrOnlineUpdateBusy)
}

func TestOnlineUpdaterUnavailableHostBlocksNewBackupWithoutLosingRecords(t *testing.T) {
	repo := newMockSettingRepo()
	svc := newTestBackupService(t, repo, &mockDumper{}, newMockObjectStore())
	require.NoError(t, svc.saveRecord(context.Background(), &BackupRecord{ID: "completed", Status: "completed"}))
	svc.SetOnlineUpdateGuard(NewOnlineUpdaterService("/tmp/nonexistent-newapi-updater.sock", BuildStatus{}).EnsureIdle)
	require.ErrorIs(t, svc.saveRecord(context.Background(), &BackupRecord{ID: "blocked", Status: "running"}), ErrOnlineUpdaterUnavailable)
	_, err := svc.beginBackupRestore(context.Background(), "completed")
	require.ErrorIs(t, err, ErrOnlineUpdaterUnavailable)
	records, err := svc.ListBackups(context.Background())
	require.NoError(t, err)
	require.Len(t, records, 1)
	require.Equal(t, "completed", records[0].Status)
	require.Empty(t, records[0].RestoreStatus)
}

type updaterRestoreDumper struct {
	*mockDumper
	entered chan struct{}
	resume  chan struct{}
}

func (d *updaterRestoreDumper) Restore(ctx context.Context, reader io.Reader) error {
	if _, err := io.ReadAll(reader); err != nil {
		return err
	}
	close(d.entered)
	select {
	case <-d.resume:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func TestOnlineUpdaterRestoreLeaseSurvivesRestoredSettingsOnAnotherInstance(t *testing.T) {
	repo := newMockSettingRepo()
	seedS3Config(t, repo)
	store := newMockObjectStore()
	dumper := &updaterRestoreDumper{mockDumper: &mockDumper{dumpData: []byte("SELECT 1;\n")}, entered: make(chan struct{}), resume: make(chan struct{})}
	restoring := newTestBackupService(t, repo, dumper, store)
	peer := newTestBackupService(t, repo, &mockDumper{}, store)
	cache := &fakeLeaderLockCache{}
	restoring.SetLeaderLock(cache, nil)
	peer.SetLeaderLock(cache, nil)
	idle := func(context.Context) error { return nil }
	restoring.SetOnlineUpdateGuard(idle)
	peer.SetOnlineUpdateGuard(idle)
	record, err := restoring.CreateBackup(context.Background(), "manual", 14)
	require.NoError(t, err)
	prior, err := repo.GetValue(context.Background(), settingKeyBackupRecords)
	require.NoError(t, err)
	_, err = restoring.StartRestore(context.Background(), record.ID)
	require.NoError(t, err)
	<-dumper.entered
	require.NotEmpty(t, cache.heldBy(backupRestoreUpdateLeaseKey))
	// A real full restore replaces this settings row with the pre-restore value.
	// Its durable status no longer says running, but the external lease remains.
	require.NoError(t, repo.Set(context.Background(), settingKeyBackupRecords, prior))
	invoked := false
	err = peer.WithIdleMaintenance(context.Background(), func(context.Context) error { invoked = true; return nil })
	require.ErrorIs(t, err, ErrSystemOperationBusy)
	require.False(t, invoked)
	close(dumper.resume)
	waitBackupTasks(t, restoring)
	require.Eventually(t, func() bool { return cache.heldBy(backupRestoreUpdateLeaseKey) == "" }, time.Second, time.Millisecond)
	require.NoError(t, peer.WithIdleMaintenance(context.Background(), func(context.Context) error { return nil }))
}
func TestOnlineUpdaterRestoreLeaseFailsClosedWithoutCoordinationBackend(t *testing.T) {
	repo := newMockSettingRepo()
	svc := newTestBackupService(t, repo, &mockDumper{}, newMockObjectStore())
	svc.SetOnlineUpdateGuard(func(context.Context) error { return nil })
	_, err := svc.acquireRestoreUpdateLease(context.Background())
	require.ErrorIs(t, err, ErrOnlineUpdaterUnavailable)
	require.ErrorIs(t, svc.WithIdleMaintenance(context.Background(), func(context.Context) error { t.Fatal("unsafe update admitted"); return nil }), ErrOnlineUpdaterUnavailable)
}
