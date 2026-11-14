package service

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// SetOnlineUpdateGuard is installed before the application lifetime starts.
// An unconfigured updater's guard returns immediately and preserves the existing
// backup behavior. A configured but unreachable updater fails closed for new work.
func (s *BackupService) SetOnlineUpdateGuard(check func(context.Context) error) {
	s.recordsMu.Lock()
	defer s.recordsMu.Unlock()
	s.onlineUpdateGuard = check
}

// WithIdleMaintenance serializes host-job admission with the publication of
// backup/restore running records across instances. The long-running owners are
// the durable records and host job, not the short HTTP/system-operation lease.
func (s *BackupService) WithIdleMaintenance(ctx context.Context, execute func(context.Context) error) error {
	if s == nil {
		return ErrOnlineUpdaterUnavailable
	}
	ctx, release, err := s.lockBackupRecordUpdates(ctx)
	if err != nil {
		return ErrOnlineUpdaterUnavailable
	}
	defer release()
	restoreRelease, err := s.acquireRestoreUpdateLease(ctx)
	if err != nil {
		return err
	}
	defer restoreRelease()
	records, err := s.loadRecordsLocked(ctx)
	if err != nil {
		return ErrOnlineUpdaterUnavailable
	}
	for _, record := range records {
		if record.Status == "running" || record.RestoreStatus == "running" {
			return ErrSystemOperationBusy
		}
	}
	return execute(ctx)
}

func (s *BackupService) checkOnlineUpdateBeforeBackupLocked(ctx context.Context, record *BackupRecord, records []BackupRecord) error {
	if s.onlineUpdateGuard == nil || record.Status != "running" {
		return nil
	}
	for _, existing := range records {
		if existing.ID == record.ID && existing.Status == "running" {
			return nil
		}
	}
	return s.onlineUpdateGuard(ctx)
}

const backupRestoreUpdateLeaseKey = "backup:restore:update-boundary"

// SQL restore can overwrite its own settings record. This lease lives in Redis
// (or a PostgreSQL session advisory lock), outside the restored table contents.
// It spans actual restore IO and its final state persistence. Redis errors never
// switch lock backends while a peer may still hold the Redis lease.
func (s *BackupService) acquireRestoreUpdateLease(ctx context.Context) (func(), error) {
	if s.onlineUpdateGuard == nil {
		return func() {}, nil
	}
	owner := s.instanceID + "/" + uuid.NewString()
	if s.lockCache != nil {
		ok, err := s.lockCache.TryAcquireLeaderLock(ctx, backupRestoreUpdateLeaseKey, owner, backupScheduledLeaderLockTTL)
		if err != nil {
			return nil, ErrOnlineUpdaterUnavailable
		}
		if !ok {
			return nil, ErrSystemOperationBusy
		}
		return func() {
			releaseCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_ = s.lockCache.ReleaseLeaderLock(releaseCtx, backupRestoreUpdateLeaseKey, owner)
		}, nil
	}
	if s.db != nil {
		// A restore lease and the record-writer lease each hold a connection, while
		// the settings repository requires another. Do not deadlock small pools.
		max := s.db.Stats().MaxOpenConnections
		if max > 0 && max < 3 {
			return nil, ErrOnlineUpdaterUnavailable
		}
		release, ok, err := tryAcquireDBAdvisoryLockWithError(ctx, s.db, hashAdvisoryLockID(backupRestoreUpdateLeaseKey))
		if err != nil {
			return nil, ErrOnlineUpdaterUnavailable
		}
		if !ok {
			return nil, ErrSystemOperationBusy
		}
		return release, nil
	}
	return nil, ErrOnlineUpdaterUnavailable
}
