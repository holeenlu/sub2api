package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/tidwall/gjson"
)

// One cancellable account worker per gateway, with Redis per-account exclusion.
// No public telemetry input participates in either generation or activation.
type accountTelemetryWorker struct {
	epoch   string
	cancel  context.CancelFunc
	done    chan struct{}
	jobs    sync.WaitGroup
	running sync.Map
	senders chan struct{}
	err     error // published by closing done
}

func (s *GatewayService) StartAccountTelemetry(parent context.Context) error {
	if s == nil {
		return nil
	}
	if parent == nil {
		return errors.New("account telemetry start context is nil")
	}
	if err := parent.Err(); err != nil {
		return err
	}
	s.runtime.mu.Lock()
	defer s.runtime.mu.Unlock()
	if s.runtime.stopping {
		return errGatewayRuntimeStopped
	}
	if s.accountTelemetryWorker != nil {
		return nil
	}
	if _, ok := s.accountRepo.(AccountTelemetryRepository); !ok {
		return nil
	}
	if _, ok := s.cache.(AccountTelemetryRuntimeCache); !ok {
		return nil
	}
	ctx, cancel := context.WithCancel(parent)
	worker := &accountTelemetryWorker{epoch: uuid.NewString(), cancel: cancel, done: make(chan struct{}), senders: make(chan struct{}, 16)}
	s.accountTelemetryWorker = worker
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		startup := true
		owner := worker.epoch
		leader := false
		cache := s.cache.(AccountTelemetryRuntimeCache)
		// Device binding is local; only event/eval jobs use this cancellable context.
		var sendCtx context.Context
		var cancelSending context.CancelFunc
		stopSending := func() {
			if cancelSending != nil {
				cancelSending()
				cancelSending = nil
			}
		}
		defer func() {
			stopSending()
			worker.jobs.Wait()
			finish, done := context.WithTimeout(context.Background(), time.Second)
			defer done()
			if err := cache.ReleaseAccountTelemetryLeader(finish, owner); err != nil {
				worker.err = fmt.Errorf("release account telemetry leader: %w", err)
			}
			close(worker.done)
		}()
		var lastScan time.Time
		offset := 0
		for {
			if ctx.Err() != nil {
				return
			}
			if !s.accountTelemetrySendingEnabled() {
				stopSending()
			}
			leaseCtx, leaseCancel := context.WithTimeout(ctx, time.Second)
			acquired, acquireErr := cache.AcquireAccountTelemetryLeader(leaseCtx, owner)
			leaseCancel()
			if acquireErr != nil || !acquired {
				stopSending()
				leader = false
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					continue
				}
			}
			if !leader {
				startup = true
				leader = true
			}
			if s.accountTelemetrySendingEnabled() && cancelSending == nil {
				sendCtx, cancelSending = context.WithCancel(ctx)
				startup = true // Enabling activates ready accounts for ten minutes.
			}
			if startup || time.Since(lastScan) >= 10*time.Second {
				s.scanAccountTelemetry(ctx, startup)
				startup = false
				lastScan = time.Now()
			}
			var ids []int64
			var err error
			if cancelSending != nil {
				ids, err = cache.ActiveAccountTelemetry(ctx)
			}
			if err == nil && len(ids) > 0 {
				for i := 0; i < len(ids); i++ {
					id := ids[(offset+i)%len(ids)]
					if _, loaded := worker.running.LoadOrStore(id, true); loaded {
						continue
					}
					select {
					case worker.senders <- struct{}{}:
						worker.jobs.Add(1)
						go func(jobCtx context.Context) {
							defer worker.jobs.Done()
							defer worker.running.Delete(id)
							defer func() { <-worker.senders }()
							s.runAccountTelemetryTick(jobCtx, id)
						}(sendCtx)
					default:
						worker.running.Delete(id)
						offset = (offset + i) % len(ids)
						i = len(ids)
					}
				}
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	return nil
}

func (s *GatewayService) StopAccountTelemetry(ctx context.Context) error {
	if s == nil {
		return nil
	}
	if ctx == nil {
		return errors.New("account telemetry stop context is nil")
	}
	s.runtime.mu.Lock()
	s.runtime.stopping = true
	worker := s.accountTelemetryWorker
	s.runtime.mu.Unlock()
	if worker == nil {
		return nil
	}
	worker.cancel()
	select {
	case <-worker.done:
		return worker.err
	default:
	}
	select {
	case <-worker.done:
		return worker.err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *GatewayService) scanAccountTelemetry(ctx context.Context, startup bool) {
	repo := s.accountRepo.(AccountTelemetryRepository)
	var after int64
	for ctx.Err() == nil {
		accounts, err := repo.ListAccountTelemetry(ctx, after, 100)
		if err != nil || len(accounts) == 0 {
			return
		}
		for _, account := range accounts {
			after = account.ID
			if !accountTelemetryEligible(account) || ReadAccountTelemetry(account).DeviceInstallation == nil {
				continue
			}
			if _, err := s.bindAccountTelemetry(ctx, account); err != nil {
				continue
			}
			if startup {
				s.activateAccountTelemetry(ctx, account, nil)
			}
		}
		if len(accounts) < 100 {
			return
		}
	}
}

func (s *GatewayService) activateAccountTelemetry(ctx context.Context, account *Account, wireBody []byte) {
	if !accountTelemetryEligible(account) {
		return
	}
	state := ReadAccountTelemetry(account)
	if state.DeviceInstallation == nil {
		return
	}
	if state.Owner != accountTelemetryOwner(account) || state.SessionID == "" {
		bindCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 100*time.Millisecond)
		defer cancel()
		_, _ = s.bindAccountTelemetry(bindCtx, account)
		return
	}
	if !s.accountTelemetrySendingEnabled() {
		return
	}
	cache, ok := s.cache.(AccountTelemetryRuntimeCache)
	if !ok {
		return
	}
	activity := AccountTelemetryActivity{Revision: state.Revision}
	if id := ParseMetadataUserID(gjson.GetBytes(wireBody, "metadata.user_id").String()); id != nil {
		if id.DeviceID == officialTelemetryDevice(state.DeviceInstallation.DeviceID) {
			activity.DeviceID, activity.SessionID = id.DeviceID, id.SessionID
		}
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 100*time.Millisecond)
	defer cancel()
	if err := cache.TouchAccountTelemetry(ctx, account.ID, activity); err != nil {
		logClaudeTelemetrySkipped(account.ID, telemetryForwardFailure("telemetry_activation_failed"))
	}
}

func (s *GatewayService) runAccountTelemetryTick(ctx context.Context, id int64) {
	if s.requireAccountTelemetrySendingEnabled(ctx) != nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cache := s.cache.(AccountTelemetryRuntimeCache)
	activity, err := cache.GetAccountTelemetryActivity(ctx, id)
	if err != nil {
		return
	}
	account, err := s.accountRepo.GetByID(ctx, id)
	if err != nil || !accountTelemetryEligible(account) {
		return
	}
	before := ReadAccountTelemetry(account)
	state, err := s.bindAccountTelemetry(ctx, account)
	if err != nil {
		return
	}
	if state.Revision != before.Revision {
		activity = AccountTelemetryActivity{}
	}
	identity, headers, err := claudeTelemetryIdentityForActivity(account, activity)
	if err != nil {
		logClaudeTelemetrySkipped(id, err)
		return
	}
	if err := s.generateClaudeTelemetry(ctx, &claudeTelemetryDispatch{account: account, headers: headers, identity: identity}); err != nil && s.requireAccountTelemetrySendingEnabled(ctx) == nil {
		logClaudeTelemetrySkipped(id, err)
	}
}
