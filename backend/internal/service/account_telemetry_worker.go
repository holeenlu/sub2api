package service

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/tidwall/gjson"
)

// One cancellable account worker per gateway, with Redis per-account exclusion.
// No public telemetry input participates in either generation or activation.
type accountTelemetryWorker struct {
	epoch        string
	cancel       context.CancelFunc
	done         chan struct{}
	jobs         sync.WaitGroup
	running      sync.Map
	initializers chan struct{}
	senders      chan struct{}
}

func (s *GatewayService) StartAccountTelemetry(settings *OpsService) {
	if s == nil || s.accountTelemetryWorker != nil {
		return
	}
	if _, ok := s.accountRepo.(AccountTelemetryRepository); !ok {
		return
	}
	if _, ok := s.cache.(AccountTelemetryRuntimeCache); !ok {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.telemetrySettings = settings
	worker := &accountTelemetryWorker{epoch: uuid.NewString(), cancel: cancel, done: make(chan struct{}), initializers: make(chan struct{}, 2), senders: make(chan struct{}, 16)}
	s.accountTelemetryWorker = worker
	s.accountTelemetryWake = make(chan struct{}, 1)
	go func() {
		defer close(worker.done)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		startup := true
		owner := worker.epoch
		leader := false
		cache := s.cache.(AccountTelemetryRuntimeCache)
		// Initialization always runs. Only event/eval jobs use this cancellable
		// context, so disabling sending cannot interrupt an official CLI hello.
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
		}()
		defer func() {
			finish, done := context.WithTimeout(context.Background(), time.Second)
			defer done()
			_ = cache.ReleaseAccountTelemetryLeader(finish, owner)
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
				s.scanAccountTelemetry(ctx, worker, startup)
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
			case <-s.accountTelemetryWake:
				lastScan = time.Time{}
			case <-ticker.C:
			}
		}
	}()
}

func (s *GatewayService) StopAccountTelemetry() {
	if s == nil || s.accountTelemetryWorker == nil {
		return
	}
	s.accountTelemetryWorker.cancel()
	<-s.accountTelemetryWorker.done
}

func (s *GatewayService) scanAccountTelemetry(ctx context.Context, worker *accountTelemetryWorker, startup bool) {
	repo := s.accountRepo.(AccountTelemetryRepository)
	var after int64
	for ctx.Err() == nil {
		accounts, err := repo.ListAccountTelemetry(ctx, after, 100, !startup)
		if err != nil || len(accounts) == 0 {
			return
		}
		for _, account := range accounts {
			after = account.ID
			if !accountTelemetryEligible(account) {
				continue
			}
			state := ReadAccountTelemetry(account)
			if state.Status == "ready" && state.Owner == accountTelemetryOwner(account) {
				if startup {
					s.activateAccountTelemetry(ctx, account, nil)
				}
				continue
			}
			if state.RetryAt != nil && time.Now().Before(*state.RetryAt) {
				continue
			}
			if _, loaded := worker.running.LoadOrStore(account.ID, true); loaded {
				continue
			}
			select {
			case worker.initializers <- struct{}{}:
				worker.jobs.Add(1)
				go func() {
					defer worker.jobs.Done()
					defer worker.running.Delete(account.ID)
					defer func() { <-worker.initializers }()
					s.generateAccountTelemetryProfile(ctx, account)
				}()
			default:
				worker.running.Delete(account.ID)
			}
		}
		if len(accounts) < 100 {
			return
		}
	}
}

func (s *GatewayService) generateAccountTelemetryProfile(ctx context.Context, account *Account) {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Minute)
	defer cancel()
	cache := s.cache.(AccountTelemetryRuntimeCache)
	lease, run, err := cache.BeginAccountTelemetryRun(ctx, account.ID, 5*time.Minute)
	if err != nil || lease == "" {
		return
	}
	defer func() {
		finish, done := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer done()
		_ = cache.EndAccountTelemetryRun(finish, account.ID, lease, run)
	}()
	state := ReadAccountTelemetry(account)
	if state.Revision == run.BootstrapRevision && time.Now().Before(run.BootstrapAfter) {
		return
	}
	run.BootstrapRevision = state.Revision
	run.BootstrapAfter = time.Now().Add(5 * time.Minute)
	profile, err := s.initializeClaudeTelemetryProfile(ctx, account)
	state.Owner = accountTelemetryOwner(account)
	if err != nil {
		state.Status = "error"
		state.ErrorCode = "telemetry_initialization_failed"
		if failure, ok := err.(*accountTelemetryForwardError); ok {
			state.ErrorCode = failure.Code
		}
		retry := time.Now().Add(5 * time.Minute)
		state.RetryAt = &retry
		logClaudeTelemetrySkipped(account.ID, err)
	} else {
		now := time.Now().UTC()
		state.Status = "ready"
		state.ErrorCode = ""
		state.RetryAt = nil
		state.GeneratedAt = &now
		state.Profile = &profile
		run = AccountTelemetryRunState{}
	}
	credentials, _ := json.Marshal(account.Credentials)
	saveCtx, saveCancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
	defer saveCancel()
	saved, saveErr := s.accountRepo.(AccountTelemetryRepository).SaveAccountTelemetry(saveCtx, account.ID, string(credentials), state.Revision, state)
	if saveErr != nil {
		logClaudeTelemetrySkipped(account.ID, telemetryForwardFailure("telemetry_profile_save_failed"))
		return
	}
	if saved && state.Status == "ready" {
		account.Extra[AccountTelemetryExtraKey] = state
		s.activateAccountTelemetry(ctx, account, nil)
	}
}

func (s *GatewayService) activateAccountTelemetry(ctx context.Context, account *Account, wireBody []byte) {
	if !accountTelemetryEligible(account) {
		return
	}
	state := ReadAccountTelemetry(account)
	if state.Revision != "" && state.Owner != accountTelemetryOwner(account) {
		// An identity replacement can happen after the previous activity window
		// expired. Do not wait for a stale active entry to notice that change.
		queueCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 100*time.Millisecond)
		defer cancel()
		_, _ = s.QueueAccountTelemetry(queueCtx, account)
		return
	}
	if state.Status != "ready" || state.Profile == nil || state.Owner != accountTelemetryOwner(account) {
		return
	}
	if !s.accountTelemetrySendingEnabled() {
		return
	}
	cache, ok := s.cache.(AccountTelemetryRuntimeCache)
	if !ok {
		return
	}
	activity := AccountTelemetryActivity{}
	// Only server-generated matching metadata can refine the current session.
	// Passthrough/client metadata never changes the account's installation identity.
	if id := ParseMetadataUserID(gjson.GetBytes(wireBody, "metadata.user_id").String()); id != nil {
		if device := gjson.Get(state.Profile.PayloadJSON, "event_data.device_id").String(); id.DeviceID == device {
			activity.DeviceID = id.DeviceID
			activity.SessionID = id.SessionID
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
	state := ReadAccountTelemetry(account)
	if state.Status != "ready" {
		return
	}
	if state.Owner != accountTelemetryOwner(account) {
		_, _ = s.QueueAccountTelemetry(ctx, account)
		return
	}
	profile, exists, err := accountTelemetryProfile(account)
	if err != nil || !exists {
		return
	}
	identity, headers, err := claudeTelemetryIdentityForActivity(profile, account, activity)
	if err != nil {
		logClaudeTelemetrySkipped(id, err)
		return
	}
	profile.Headers = headers
	if err := s.generateClaudeTelemetry(ctx, &claudeTelemetryDispatch{account: account, profile: profile, identity: identity}); err != nil && s.requireAccountTelemetrySendingEnabled(ctx) == nil {
		logClaudeTelemetrySkipped(id, err)
	}
}
