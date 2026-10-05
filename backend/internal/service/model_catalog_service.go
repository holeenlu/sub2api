package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/singleflight"
)

// ModelCatalogService is the shared discovery control plane. Read APIs never
// perform upstream I/O. Refreshes use both local singleflight and a database lease.
type ModelCatalogService struct {
	lifecycleMu  sync.Mutex
	stopped      bool
	workerCtx    context.Context
	activeJobs   int
	manualSyncID string
	startOnce    sync.Once

	pricingResolver *ModelPricingResolver
	groupCatalog    *GroupModelCatalogService
	repo            ModelCatalogRepository
	accounts        AccountRepository
	syncer          *AccountTestService
	settings        *SettingService
	prices          *PricingService
	sourceFlights   singleflight.Group
	flights         singleflight.Group
	cancel          context.CancelFunc
	wg              sync.WaitGroup
	stopOnce        sync.Once
	jobs            sync.Map
}

type ModelCatalogJob struct {
	ID         string     `json:"id"`
	AccountID  int64      `json:"account_id"`
	Status     string     `json:"status"`
	Revision   string     `json:"revision,omitempty"`
	Succeeded  int        `json:"succeeded"`
	Failed     int        `json:"failed"`
	Error      string     `json:"error,omitempty"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

func NewModelCatalogService(repo ModelCatalogRepository, accounts AccountRepository, syncer *AccountTestService, settings *SettingService, prices *PricingService) *ModelCatalogService {
	ctx, cancel := context.WithCancel(context.Background())
	s := &ModelCatalogService{repo: repo, accounts: accounts, syncer: syncer, settings: settings, prices: prices, workerCtx: ctx, cancel: cancel}
	if syncer != nil {
		syncer.modelCatalog = s
	}
	if syncer != nil && syncer.openaiGatewayService != nil {
		syncer.openaiGatewayService.modelCatalog = s
	}
	return s
}

func (s *ModelCatalogService) Settings(ctx context.Context) ModelCatalogSettings {
	if cache, ok := ctx.Value(catalogReadContextKey{}).(*catalogReadCache); ok {
		cache.settingsOnce.Do(func() { cache.settings = s.settingsUncached(ctx) })
		return cache.settings
	}
	return s.settingsUncached(ctx)
}
func (s *ModelCatalogService) settingsUncached(ctx context.Context) ModelCatalogSettings {
	out := ModelCatalogSettings{Enabled: true, IntervalSeconds: 300, TimeoutSeconds: 45, Concurrency: 2, StaleSeconds: 86400, PriorityIntervalSeconds: 60, DeletionAlertPercent: 50}
	if s != nil && s.settings != nil && s.settings.settingRepo != nil {
		if raw, err := s.settings.settingRepo.GetValue(ctx, ModelCatalogSettingsKey); err == nil {
			_ = json.Unmarshal([]byte(raw), &out)
		}
	}
	if out.IntervalSeconds < 60 {
		out.IntervalSeconds = 300
	}
	if out.TimeoutSeconds < 5 || out.TimeoutSeconds > 120 {
		out.TimeoutSeconds = 45
	}
	if out.Concurrency < 1 || out.Concurrency > 8 {
		out.Concurrency = 2
	}
	if out.StaleSeconds < 300 || out.StaleSeconds > 604800 {
		out.StaleSeconds = 86400
	}
	if out.DeletionAlertPercent < 1 || out.DeletionAlertPercent > 100 {
		out.DeletionAlertPercent = 50
	}
	if out.PriorityIntervalSeconds < 60 || out.PriorityIntervalSeconds > 86400 {
		out.PriorityIntervalSeconds = 60
	}
	return out
}

func (s *ModelCatalogService) SaveSettings(ctx context.Context, cfg ModelCatalogSettings) error {
	if cfg.DeletionAlertPercent == 0 {
		cfg.DeletionAlertPercent = 50
	}
	if cfg.DeletionAlertPercent < 1 || cfg.DeletionAlertPercent > 100 {
		return fmt.Errorf("invalid deletion alert threshold")
	}
	if cfg.PriorityIntervalSeconds == 0 {
		cfg.PriorityIntervalSeconds = 60
	}
	if cfg.PriorityIntervalSeconds < 60 || cfg.PriorityIntervalSeconds > 86400 || len(cfg.PriorityAccountIDs) > 5000 {
		return fmt.Errorf("invalid priority source settings")
	}
	for _, id := range cfg.PriorityAccountIDs {
		if id <= 0 {
			return fmt.Errorf("invalid priority account ID")
		}
	}
	if cfg.IntervalSeconds < 60 || cfg.IntervalSeconds > 86400 || cfg.TimeoutSeconds < 5 || cfg.TimeoutSeconds > 120 || cfg.Concurrency < 1 || cfg.Concurrency > 8 || cfg.StaleSeconds < 300 || cfg.StaleSeconds > 604800 {
		return fmt.Errorf("invalid model catalog synchronization settings")
	}
	if s.settings == nil || s.settings.settingRepo == nil {
		return ErrModelCatalogUnavailable
	}
	body, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	return s.settings.settingRepo.Set(ctx, ModelCatalogSettingsKey, string(body))
}

func (s *ModelCatalogService) Registry(ctx context.Context) []ModelCatalogEntry {
	if cache, ok := ctx.Value(catalogReadContextKey{}).(*catalogReadCache); ok {
		cache.registryOnce.Do(func() { cache.registry = s.registryUncached(ctx) })
		return cache.registry
	}
	return s.registryUncached(ctx)
}
func (s *ModelCatalogService) registryUncached(ctx context.Context) []ModelCatalogEntry {
	entries := []ModelCatalogEntry{}
	if s == nil || s.settings == nil || s.settings.settingRepo == nil {
		return entries
	}
	if raw, err := s.settings.settingRepo.GetValue(ctx, ModelCatalogRegistryKey); err == nil {
		_ = json.Unmarshal([]byte(raw), &entries)
	}
	return entries
}

func (s *ModelCatalogService) Account(ctx context.Context, account *Account) (*ModelCatalogSnapshot, error) {
	if account != nil {
		if cache, ok := ctx.Value(catalogReadContextKey{}).(*catalogReadCache); ok {
			cache.mu.Lock()
			value := cache.snapshots[account.ID]
			cache.mu.Unlock()
			if value != nil {
				return value, nil
			}
		}
	}
	if s == nil || s.repo == nil || account == nil {
		return nil, ErrModelCatalogUnavailable
	}
	source, err := resolveCredentialAccount(ctx, s.accounts, account)
	if err != nil {
		return nil, err
	}
	scope := modelCatalogScope(account, source)
	snapshot, err := s.repo.Current(ctx, modelCatalogSourceKey(account.ID))
	if err != nil {
		return nil, err
	}
	if !account.IsActive() || !source.IsActive() || snapshot == nil || snapshot.ScopeRevision != scope {
		snapshot = &ModelCatalogSnapshot{AccountID: account.ID, Platform: account.Platform, Status: "unavailable", Models: []ModelCatalogEntry{}}
	} else if snapshot.Status != "unavailable" && time.Since(snapshot.CheckedAt) > time.Duration(s.Settings(ctx).StaleSeconds)*time.Second {
		snapshot.Status = "unavailable"
		snapshot.LastError = "catalog_expired"
	} else if snapshot.Status != "unavailable" && time.Since(snapshot.CheckedAt) > time.Duration(s.Settings(ctx).IntervalSeconds)*time.Second {
		snapshot.Status = "stale"
	}
	// Candidates supplement missing media data without pretending they were
	// returned by this account. Explicit upstream restrictions remain authoritative.
	known := map[string]bool{}
	for i := range snapshot.Models {
		e := &snapshot.Models[i]
		known[e.ID] = true
		if e.ShutdownDate != "" {
			e.Metadata.ShutdownDate = e.ShutdownDate
			e.Lifecycle = modelCatalogLifecycle(e.Metadata, time.Now())
		}
	}
	for _, candidate := range s.Registry(ctx) {
		if !catalogRegistryApplies(candidate, account, source) {
			continue
		}
		if known[candidate.ID] {
			for i := range snapshot.Models {
				e := &snapshot.Models[i]
				if e.ID != candidate.ID {
					continue
				}
				e.Metadata, _ = mergeUpstreamModelMetadata(e.Metadata, candidate.Metadata)
				// Trusted lifecycle updates may withdraw support immediately; never override native limits.
				if candidate.ShutdownDate != "" && (e.ShutdownDate == "" || candidate.ShutdownDate < e.ShutdownDate) {
					e.ShutdownDate = candidate.ShutdownDate
					e.Metadata.ShutdownDate = candidate.ShutdownDate
				}
				e.Lifecycle = modelCatalogLifecycle(e.Metadata, time.Now())
				if candidate.Lifecycle == "retired" {
					e.Lifecycle = "retired"
				}
				e.Missing = modelCatalogMissing(e.Kind, e.Metadata)
			}
			continue
		}
		candidate.Access = "candidate"
		candidate.Source = "registry"
		if candidate.Lifecycle != "retired" {
			candidate.Lifecycle = modelCatalogLifecycle(candidate.Metadata, time.Now())
		}
		snapshot.Models = append(snapshot.Models, candidate)
		known[candidate.ID] = true
	}
	for _, id := range configuredUpstreamModelsForCapabilitySync(account) {
		if known[id] {
			continue
		}
		m, _ := account.GetUpstreamModelMetadata(id)
		snapshot.Models = append(snapshot.Models, ModelCatalogEntry{ID: id, DisplayName: id, Platform: account.Platform, Kind: modelCatalogEntryKind(id, m), Lifecycle: modelCatalogLifecycle(m, time.Now()), ShutdownDate: m.ShutdownDate, Access: "configured", Source: "administrator", Metadata: m, Missing: modelCatalogMissing(modelCatalogEntryKind(id, m), m)})
	}
	if observations, err := s.repo.MediaObservations(ctx, account.ID, scope); err == nil {
		for _, o := range observations {
			matched := false
			for i := range snapshot.Models {
				e := &snapshot.Models[i]
				if e.ID != o.Model {
					continue
				}
				matched = true
				e.Observations = append(e.Observations, o)
				if e.Lifecycle != "retired" && (e.Access != "unlisted" || account.IsOpenAIOAuth() || o.ObservedAt.After(snapshot.CheckedAt)) {
					e.Access = "observed"
					if !stringSliceContains(e.Endpoints, o.Operation) {
						e.Endpoints = append(e.Endpoints, o.Operation)
					}
				}
			}
			if !matched {
				snapshot.Models = append(snapshot.Models, ModelCatalogEntry{ID: o.Model, Platform: account.Platform, Kind: "image", Access: "observed", Source: "usage", Lifecycle: "unknown", Endpoints: []string{o.Operation}, Observations: []CatalogMediaObservation{o}})
			}
		}
	}
	if s.prices != nil {
		snapshot.PriceRevision = s.prices.PriceRevision()
	}
	snapshot.Models = modelCatalogNormalizeEntries(snapshot.Models)
	if cache, ok := ctx.Value(catalogReadContextKey{}).(*catalogReadCache); ok {
		cache.mu.Lock()
		cache.snapshots[account.ID] = snapshot
		cache.mu.Unlock()
	}
	return snapshot, nil
}

func (s *ModelCatalogService) Platform(ctx context.Context, platform string) (*ModelCatalogSnapshot, error) {
	if s == nil || s.repo == nil {
		return nil, ErrModelCatalogUnavailable
	}
	rows, err := s.repo.ListPlatform(ctx, platform)
	if err != nil {
		return nil, err
	}
	out := &ModelCatalogSnapshot{Platform: platform, Status: "unavailable", Models: []ModelCatalogEntry{}}
	seen := map[string]bool{}
	for _, snapshot := range rows {
		a, err := s.accounts.GetByID(ctx, snapshot.AccountID)
		if err != nil || a == nil || !a.IsActive() {
			continue
		}
		source, err := resolveCredentialAccount(ctx, s.accounts, a)
		if err != nil || modelCatalogScope(a, source) != snapshot.ScopeRevision {
			continue
		}
		out.Status = "ready"
		if snapshot.CheckedAt.After(out.CheckedAt) {
			out.CheckedAt = snapshot.CheckedAt
			out.UpdatedAt = snapshot.UpdatedAt
		}
		for _, entry := range snapshot.Models {
			key := entry.Platform + "\x00" + entry.ID
			if seen[key] {
				continue
			}
			seen[key] = true
			// Platform browse is reference data, never a cross-account entitlement.
			entry.Access = "candidate"
			entry.Source = "platform_catalog"
			out.Models = append(out.Models, entry)
		}
	}
	for _, entry := range s.Registry(ctx) {
		if platform != "" && entry.Platform != platform {
			continue
		}
		key := entry.Platform + "\x00" + entry.ID
		if seen[key] {
			continue
		}
		seen[key] = true
		entry.Access = "candidate"
		entry.Source = "registry"
		out.Models = append(out.Models, entry)
	}
	out.Models = modelCatalogNormalizeEntries(out.Models)
	out.Revision = modelCatalogHash(out.Models)
	return out, nil
}

type catalogRefreshError struct{ code string }

func (e *catalogRefreshError) Error() string { return e.code }
func (e *catalogRefreshError) Unwrap() error { return ErrModelCatalogUnavailable }

func catalogSyncErrorCode(err error) string {
	var refresh *catalogRefreshError
	if errors.As(err, &refresh) {
		return refresh.code
	}
	if errors.Is(err, ErrModelCatalogBusy) {
		return "catalog_busy"
	}
	if errors.Is(err, ErrModelCatalogScopeChanged) {
		return "scope_changed"
	}
	var upstream *UpstreamModelSyncError
	if errors.As(err, &upstream) {
		if upstream.StatusCode == 401 || upstream.StatusCode == 403 {
			return "authentication_unavailable"
		}
		if upstream.StatusCode == 429 {
			return "upstream_rate_limited"
		}
		if upstream.StatusCode == 404 || upstream.StatusCode == 405 {
			return "discovery_not_supported"
		}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "upstream_timeout"
	}
	return "upstream_unavailable"
}

func (s *ModelCatalogService) Refresh(ctx context.Context, id int64, force bool) (*ModelCatalogSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s == nil || s.repo == nil || s.accounts == nil || s.syncer == nil {
		return nil, ErrModelCatalogUnavailable
	}
	result := s.flights.DoChan(modelCatalogSourceKey(id), func() (any, error) {
		s.lifecycleMu.Lock()
		if s.stopped {
			s.lifecycleMu.Unlock()
			return nil, ErrModelCatalogUnavailable
		}
		s.wg.Add(1)
		s.lifecycleMu.Unlock()
		defer s.wg.Done()
		cfg := s.Settings(s.workerCtx)
		for _, priorityID := range cfg.PriorityAccountIDs {
			if priorityID == id {
				cfg.IntervalSeconds = cfg.PriorityIntervalSeconds
				break
			}
		}
		work, cancel := context.WithTimeout(s.workerCtx, time.Duration(cfg.TimeoutSeconds)*time.Second)
		defer cancel()
		a, err := s.accounts.GetByID(work, id)
		if err != nil {
			return nil, err
		}
		if a == nil || !a.IsActive() || IsUnsupportedPlatform(a.Platform) {
			return nil, ErrModelCatalogUnavailable
		}
		source, err := resolveCredentialAccount(work, s.accounts, a)
		if err != nil {
			return nil, err
		}
		scope := modelCatalogScope(a, source)
		key := modelCatalogSourceKey(id)
		lease := uuid.NewString()
		claimed, err := s.repo.Claim(work, key, id, a.Platform, scope, lease, time.Now().Add(time.Duration(cfg.TimeoutSeconds+30)*time.Second), force)
		if err != nil {
			return nil, err
		}
		if !claimed {
			return nil, ErrModelCatalogBusy
		}
		published := false
		defer func() {
			if !published {
				cleanup, stop := context.WithTimeout(context.Background(), 3*time.Second)
				defer stop()
				_ = s.repo.Fail(cleanup, key, lease, "refresh_failed", time.Now().Add(time.Duration(cfg.IntervalSeconds)*time.Second))
			}
		}()
		fetched, err, _ := s.sourceFlights.Do(scope, func() (any, error) { return s.syncer.SyncUpstreamModelCatalog(work, a) })
		var catalog *UpstreamModelCatalog
		if err == nil {
			catalog = fetched.(*UpstreamModelCatalog)
			if catalog.DiscoverySource == "configured" {
				copy := *catalog
				copy.Models = configuredUpstreamModelsForCapabilitySync(a)
				catalog = &copy
			}
		}
		if err != nil {
			code := catalogSyncErrorCode(err)
			cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
			defer stop()
			if s.repo.Fail(cleanup, key, lease, code, catalogNextRetry(err, cfg.IntervalSeconds)) == nil {
				published = true
			}
			return nil, &catalogRefreshError{code: code}
		}
		latest, err := s.accounts.GetByID(work, id)
		if err != nil {
			return nil, err
		}
		latestSource, err := resolveCredentialAccount(work, s.accounts, latest)
		if err != nil {
			return nil, err
		}
		if latest == nil || !latest.IsActive() || modelCatalogScope(latest, latestSource) != scope {
			return nil, ErrModelCatalogScopeChanged
		}
		snapshot := s.snapshot(work, a, catalog, scope)
		if err = s.repo.Publish(work, key, lease, snapshot, time.Now().Add(time.Duration(cfg.IntervalSeconds)*time.Second)); err != nil {
			return nil, err
		}
		published = true

		return snapshot, nil
	})
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case v := <-result:
		if v.Err != nil {
			return nil, v.Err
		}
		return v.Val.(*ModelCatalogSnapshot), nil
	}
}

func (s *ModelCatalogService) snapshot(ctx context.Context, a *Account, catalog *UpstreamModelCatalog, scope string) *ModelCatalogSnapshot {
	now := time.Now().UTC()
	ttl := time.Duration(s.Settings(ctx).StaleSeconds) * time.Second
	out := &ModelCatalogSnapshot{AccountID: a.ID, Platform: a.Platform, ScopeRevision: scope, Status: "ready", UpdatedAt: now, CheckedAt: now, Models: []ModelCatalogEntry{}}
	registry := map[string]ModelCatalogEntry{}
	source, _ := resolveCredentialAccount(ctx, s.accounts, a)
	for _, entry := range s.Registry(ctx) {
		if catalogRegistryApplies(entry, a, source) {
			registry[entry.ID] = entry
		}
	}
	for _, id := range catalog.Models {
		m := catalog.Metadata[id]
		if fallback, ok := registry[id]; ok {
			m, _ = mergeUpstreamModelMetadata(m, fallback.Metadata)
		}
		kind := modelCatalogEntryKind(id, m)
		e := ModelCatalogEntry{ID: id, DisplayName: m.DisplayName, Platform: a.Platform, Kind: kind, Lifecycle: modelCatalogLifecycle(m, now), ShutdownDate: m.ShutdownDate, Access: "listed", Source: "upstream", Metadata: m, Missing: modelCatalogMissing(kind, m), Endpoints: m.Endpoints, CodexModel: catalog.Descriptors[id]}
		if fallback, ok := registry[id]; ok && fallback.Lifecycle == "retired" {
			e.Lifecycle = "retired"
		}
		if m.RecommendedPriority != nil {
			e.RecommendedPriority = *m.RecommendedPriority
		}
		if catalog.DiscoverySource == "configured" {
			e.Access = "configured"
			e.Source = "administrator"
		}
		e.Fields = catalogFieldEvidence(e.Metadata, now, ttl)
		out.Models = append(out.Models, e)
	}
	if previous, err := s.repo.Current(ctx, modelCatalogSourceKey(a.ID)); err == nil && previous != nil && previous.ScopeRevision == scope && catalog.DiscoverySource == "upstream" {
		seen := map[string]bool{}
		for _, entry := range out.Models {
			seen[entry.ID] = true
		}
		previousVisible, removed := 0, 0
		for _, entry := range previous.Models {
			if entry.Access == "listed" && entry.Lifecycle != "retired" {
				previousVisible++
				if !seen[entry.ID] {
					removed++
				}
			}
			if !seen[entry.ID] {
				entry.Access = "unlisted"
				out.Models = append(out.Models, entry)
			}
		}
		if previousVisible > 0 && removed > 0 && removed*100 >= previousVisible*s.Settings(ctx).DeletionAlertPercent {
			out.Warnings = append(out.Warnings, "large_visibility_drop")
		}
	}
	if s.prices != nil {
		out.PriceRevision = s.prices.PriceRevision()
	}
	out.Models = modelCatalogNormalizeEntries(out.Models)
	out.Revision = modelCatalogHash(struct {
		Scope  string
		Models []ModelCatalogEntry
	}{scope, catalogRevisionModels(out.Models)})
	return out
}

func (s *ModelCatalogService) QueueRefresh(id int64) ModelCatalogJob {
	return s.queueJob(id)
}

// QueueSync refreshes every active account and reference prices on demand,
// independent of the automatic schedule. Repeated requests on this process are
// coalesced; individual sources also use the shared database leases.
func (s *ModelCatalogService) QueueSync() ModelCatalogJob { return s.queueJob(0) }

func (s *ModelCatalogService) queueJob(id int64) ModelCatalogJob {
	s.lifecycleMu.Lock()
	if id == 0 && s.manualSyncID != "" {
		if previous, ok := s.jobs.Load(s.manualSyncID); ok && previous.(ModelCatalogJob).Status == "running" {
			s.lifecycleMu.Unlock()
			return previous.(ModelCatalogJob)
		}
	}
	if s.stopped || s.activeJobs >= 32 {
		s.lifecycleMu.Unlock()
		return ModelCatalogJob{Status: "failed", Error: "catalog_busy"}
	}
	s.activeJobs++
	s.wg.Add(1)
	now := time.Now().UTC()
	s.jobs.Range(func(key, value any) bool {
		j := value.(ModelCatalogJob)
		if now.Sub(j.StartedAt) > time.Hour {
			s.jobs.Delete(key)
		}
		return true
	})
	job := ModelCatalogJob{ID: uuid.NewString(), AccountID: id, Status: "running", StartedAt: now}
	persist, stopPersist := context.WithTimeout(context.Background(), 3*time.Second)
	err := s.repo.SaveJob(persist, job)
	stopPersist()
	if err != nil {
		s.activeJobs--
		s.lifecycleMu.Unlock()
		s.wg.Done()
		return ModelCatalogJob{Status: "failed", Error: "catalog_unavailable"}
	}
	s.jobs.Store(job.ID, job)
	if id == 0 {
		s.manualSyncID = job.ID
	}
	s.lifecycleMu.Unlock()
	initialJob := job
	go func() {
		defer s.wg.Done()
		defer func() { s.lifecycleMu.Lock(); s.activeJobs--; s.lifecycleMu.Unlock() }()
		timeout := 2 * time.Minute
		if id == 0 {
			timeout = 30 * time.Minute
		}
		ctx, cancel := context.WithTimeout(s.workerCtx, timeout)
		defer cancel()
		var snapshot *ModelCatalogSnapshot
		var err error
		if id == 0 {
			job.Succeeded, job.Failed, err = s.syncManually(ctx)
		} else {
			snapshot, err = s.Refresh(ctx, id, true)
		}
		done := time.Now().UTC()
		job.FinishedAt = &done
		if err != nil {
			job.Status = "failed"
			job.Error = catalogSyncErrorCode(err)
		} else {
			job.Status = "complete"
			if snapshot != nil {
				job.Revision = snapshot.Revision
			}
		}
		s.jobs.Store(job.ID, job)
		saved, stop := context.WithTimeout(context.Background(), 3*time.Second)
		defer stop()
		_ = s.repo.SaveJob(saved, job)
	}()
	return initialJob
}

func (s *ModelCatalogService) syncManually(ctx context.Context) (int, int, error) {
	accounts, err := s.accounts.ListActive(ctx)
	if err != nil {
		return 0, 0, err
	}
	cfg := s.Settings(ctx)
	var succeeded, failed atomic.Int64
	workers, work := errgroup.WithContext(ctx)
	workers.SetLimit(cfg.Concurrency)
	for _, account := range accounts {
		if work.Err() != nil {
			break
		}
		if IsUnsupportedPlatform(account.Platform) {
			continue
		}
		id := account.ID
		workers.Go(func() error {
			if _, err := s.Refresh(work, id, true); err != nil {
				failed.Add(1)
			} else {
				succeeded.Add(1)
			}
			return nil
		})
	}
	_ = workers.Wait()
	if ctx.Err() != nil {
		return int(succeeded.Load()), int(failed.Load()), ctx.Err()
	}
	return int(succeeded.Load()), int(failed.Load()), nil
}

func (s *ModelCatalogService) Job(ctx context.Context, id string) (ModelCatalogJob, bool) {
	if value, ok := s.jobs.Load(id); ok {
		return value.(ModelCatalogJob), true
	}
	job, err := s.repo.ReadJob(ctx, id)
	if err != nil || job == nil {
		return ModelCatalogJob{}, false
	}
	return *job, true
}

func (s *ModelCatalogService) Start() {
	s.startOnce.Do(func() {
		s.lifecycleMu.Lock()
		if s.stopped {
			s.lifecycleMu.Unlock()
			return
		}
		s.wg.Add(1)
		s.lifecycleMu.Unlock()
		go func() {
			defer s.wg.Done()
			ctx := s.workerCtx
			s.restoreReferencePrices(ctx)
			ticker := time.NewTicker(time.Minute)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					s.runSync(ctx)
				}
			}
		}()
	})
}
func (s *ModelCatalogService) Stop() {
	s.stopOnce.Do(func() {
		s.lifecycleMu.Lock()
		s.stopped = true
		if s.cancel != nil {
			s.cancel()
		}
		s.lifecycleMu.Unlock()
		s.wg.Wait()
	})
}
func (s *ModelCatalogService) runSync(ctx context.Context) {
	s.restoreReferencePrices(ctx)
	if s.prices != nil {
		if p := s.prices.ReferencePrices(); p != nil {
			raw, _ := json.Marshal(p.Models)
			_ = s.repo.SavePrices(ctx, p.Revision, raw)
		}
	}
	cfg := s.Settings(ctx)
	if !cfg.Enabled {
		return
	}
	accounts, err := s.accounts.ListActive(ctx)
	if err != nil {
		slog.Warn("model_catalog_accounts_failed")
		return
	}
	priorities := map[int64]bool{}
	for _, id := range cfg.PriorityAccountIDs {
		priorities[id] = true
	}
	sort.SliceStable(accounts, func(i, j int) bool { return priorities[accounts[i].ID] && !priorities[accounts[j].ID] })
	workers, work := errgroup.WithContext(ctx)
	workers.SetLimit(cfg.Concurrency)
	for _, a := range accounts {
		if IsUnsupportedPlatform(a.Platform) {
			continue
		}
		id := a.ID
		workers.Go(func() error {
			if _, err := s.Refresh(work, id, false); err != nil && !errors.Is(err, ErrModelCatalogBusy) {
				slog.Debug("model_catalog_sync_pending", "account_id", id)
			}
			return nil
		})
	}
	_ = workers.Wait()
}
