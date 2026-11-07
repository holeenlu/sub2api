//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/Wei-Shaw/sub2api/internal/authz"
	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	"strings"
	"sync"
	"testing"
	"time"
)

func vendorProxyFixture(t *testing.T, limit int) (*service.VendorService, context.Context, int64) {
	return vendorProxyFixtureWithAdmin(t, limit, nil)
}

func vendorProxyFixtureWithAdmin(t *testing.T, limit int, admin service.AdminService) (*service.VendorService, context.Context, int64) {
	t.Helper()
	ctx := context.Background()
	var userID, adminID, vID int64
	for i, target := range []*int64{&userID, &adminID} {
		role := "user"
		if i == 1 {
			role = "super_admin"
		}
		require.NoError(t, integrationDB.QueryRowContext(ctx, "INSERT INTO users(email,password_hash,role,status,created_at,updated_at) VALUES($1,'test-only',$2,'active',NOW(),NOW()) RETURNING id", fmt.Sprintf("proxy-vendor-%d-%d@invalid", time.Now().UnixNano(), i), role).Scan(target))
	}
	require.NoError(t, integrationDB.QueryRowContext(ctx, "INSERT INTO vendors(user_id,name,proxy_limit) VALUES($1,'proxy-test',$2) RETURNING id", userID, limit).Scan(&vID))
	t.Cleanup(func() {
		integrationDB.ExecContext(ctx, "DELETE FROM scheduler_outbox WHERE account_id IN (SELECT id FROM accounts WHERE vendor_id=$1)", vID)
		integrationDB.ExecContext(ctx, "DELETE FROM accounts WHERE vendor_id=$1", vID)
		integrationDB.ExecContext(ctx, "UPDATE proxies SET backup_proxy_id=NULL WHERE vendor_id=$1", vID)
		integrationDB.ExecContext(ctx, "DELETE FROM proxies WHERE vendor_id=$1", vID)
		integrationDB.ExecContext(ctx, "DELETE FROM vendors WHERE id=$1", vID)
		integrationDB.ExecContext(ctx, "DELETE FROM users WHERE id=$1 OR id=$2", userID, adminID)
	})
	actor := authz.Subject{UserID: adminID, Role: authz.SuperAdmin}
	ctx = authz.WithSubject(ctx, actor)
	ctx = authz.WithVendorScope(ctx, authz.VendorScope{VendorID: vID, PolicyVersion: 1, ActorUserID: adminID, Admin: true})
	repo := NewProxyRepository(integrationEntClient, integrationDB)
	s := service.NewVendorService(integrationDB, integrationEntClient, nil, repo, nil, nil, admin, nil, nil, nil, integrationRedis, nil, nil, &config.Config{Timezone: "UTC"})
	return s, ctx, vID
}

func TestVendorProxyListExportImportAndPartialDelete(t *testing.T) {
	s, ctx, vID := vendorProxyFixture(t, 30)
	ids := make([]int64, 0, 12)
	for i := 0; i < 12; i++ {
		in := vendorProxyInput(fmt.Sprintf("inventory-%02d", i))
		in.Port += i
		in.Username, in.Password = "SECRET_USER", "SECRET_PASSWORD"
		in.RegionLabelCountryCode = "US"
		p, err := s.CreateVendorProxy(ctx, in)
		require.NoError(t, err)
		ids = append(ids, p.ID)
	}
	page, err := s.ListVendorProxies(ctx, service.VendorProxyFilter{Page: 1, PageSize: 10, Search: "inventory", SortBy: "name", SortOrder: "asc"})
	require.NoError(t, err)
	require.Len(t, page.Items, 10)
	require.Equal(t, 12, page.Total)
	require.Equal(t, 12, page.Summary.Total)
	filtered, err := s.ListVendorProxies(ctx, service.VendorProxyFilter{Search: "inventory-00"})
	require.NoError(t, err)
	require.Equal(t, 1, filtered.Total)
	require.Equal(t, 12, filtered.Summary.Total)
	_, err = s.ListVendorProxies(ctx, service.VendorProxyFilter{StartDate: "2026-04-31"})
	require.Error(t, err)
	jsonBytes, _, err := s.ExportVendorProxies(ctx, service.VendorProxyExportRequest{Format: "json", Filter: service.VendorProxyFilter{Search: "inventory", Page: 1, PageSize: 10}})
	require.NoError(t, err)
	require.NotContains(t, string(jsonBytes), "SECRET")
	require.NotContains(t, string(jsonBytes), "vendor_id")
	var bundle struct {
		Proxies []json.RawMessage `json:"proxies"`
	}
	require.NoError(t, json.Unmarshal(jsonBytes, &bundle))
	require.Len(t, bundle.Proxies, 12)
	csvBytes, _, err := s.ExportVendorProxies(ctx, service.VendorProxyExportRequest{Format: "csv", IDs: []int64{ids[0]}})
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(string(csvBytes), "\xef\xbb\xbf"))
	require.NotContains(t, string(csvBytes), "SECRET")
	_, _, err = s.ExportVendorProxies(ctx, service.VendorProxyExportRequest{Format: "json", IDs: []int64{ids[0], -1}})
	require.ErrorIs(t, err, service.ErrProxyNotFound)
	request := service.VendorProxyImportRequest{Format: "quick", Content: "http://a.invalid:9000\nhttp://a.invalid:9000\nhttp://127.0.0.1:80"}
	preview, err := s.ImportVendorProxies(ctx, request, true)
	require.NoError(t, err)
	require.Equal(t, 1, preview.ValidCount)
	require.Equal(t, 1, preview.DuplicateCount)
	require.Equal(t, 1, preview.InvalidCount)
	request.InputDigest = preview.InputDigest
	imported, err := s.ImportVendorProxies(ctx, request, false)
	require.NoError(t, err)
	require.Equal(t, 1, imported.ValidCount)
	require.Positive(t, imported.Rows[0].ProxyID)
	replayed, err := s.ImportVendorProxies(ctx, request, false)
	require.NoError(t, err)
	require.Equal(t, 0, replayed.ValidCount)
	require.Equal(t, 2, replayed.DuplicateCount)
	require.NoError(t, s.RegisterVendorProxyReference(ctx, vID, ids[0], "oauth", "pending", time.Now().Add(time.Minute)))
	batch, err := s.BatchDeleteVendorProxies(ctx, []int64{ids[0], ids[1], -1})
	require.NoError(t, err)
	require.Equal(t, 1, batch.SuccessCount)
	require.Equal(t, 2, batch.FailedCount)
	require.Equal(t, "PROXY_IN_USE", batch.Items[0].Code)
	require.NoError(t, s.ReleaseVendorProxyReference(ctx, vID, ids[0], "oauth", "pending"))
}

type vendorProxyProbeAdmin struct {
	service.AdminService
	started chan int64
	release chan struct{}
}

func (a *vendorProxyProbeAdmin) TestProxy(ctx context.Context, id int64) (*service.ProxyTestResult, error) {
	a.started <- id
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-a.release:
		return &service.ProxyTestResult{Success: true, Message: "sanitized", LatencyMs: 1}, nil
	}
}
func TestVendorProxyProbeConcurrencyStalenessAndCancellation(t *testing.T) {
	a := &vendorProxyProbeAdmin{started: make(chan int64, 3), release: make(chan struct{})}
	s, ctx, _ := vendorProxyFixtureWithAdmin(t, 10, a)
	proxies := make([]*service.VendorProxyDTO, 3)
	for i := range proxies {
		in := vendorProxyInput(fmt.Sprint(i))
		in.Port += i
		p, err := s.CreateVendorProxy(ctx, in)
		require.NoError(t, err)
		proxies[i] = p
	}
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func(p *service.VendorProxyDTO) {
			_, err := s.ProbeVendorProxy(ctx, p.ID, p.ConfigVersion, false)
			results <- err
		}(proxies[i])
	}
	for i := 0; i < 2; i++ {
		select {
		case <-a.started:
		case <-time.After(5 * time.Second):
			t.Fatal("probe did not start")
		}
	}
	_, err := s.ProbeVendorProxy(ctx, proxies[2].ID, 1, false)
	require.Equal(t, "PROXY_PROBE_LIMIT", infraerrors.Reason(err))
	changed := "changed.invalid"
	_, err = s.UpdateVendorProxy(ctx, proxies[0].ID, service.VendorProxyPatch{ConfigVersion: 1, Host: &changed})
	require.NoError(t, err)
	close(a.release)
	errs := []error{<-results, <-results}
	stale := 0
	success := 0
	for _, err := range errs {
		if err == nil {
			success++
		} else {
			require.ErrorIs(t, err, service.ErrVendorProxyVersion)
			stale++
		}
	}
	require.Equal(t, 1, stale)
	require.Equal(t, 1, success)
	// A cancelled native probe releases its reference and concurrency lease.
	a.release = make(chan struct{})
	cancelCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { _, err := s.ProbeVendorProxy(cancelCtx, proxies[2].ID, 1, false); done <- err }()
	select {
	case <-a.started:
	case <-time.After(5 * time.Second):
		t.Fatal("cancel probe did not start")
	}
	cancel()
	require.Error(t, <-done)
	require.NoError(t, s.DeleteVendorProxy(ctx, proxies[2].ID))
}
func vendorProxyInput(name string) service.VendorProxyInput {
	return service.VendorProxyInput{Name: name, Protocol: "http", Host: "example.invalid", Port: 8080}
}

func TestVendorProxyConcurrentLimitAndSafeNativeScope(t *testing.T) {
	s, ctx, vID := vendorProxyFixture(t, 1)
	var wg sync.WaitGroup
	ids := make(chan int64, 6)
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			in := vendorProxyInput(fmt.Sprint(n))
			in.Port += n
			p, err := s.CreateVendorProxy(ctx, in)
			if err == nil {
				ids <- p.ID
			}
		}(i)
	}
	wg.Wait()
	close(ids)
	var created []int64
	for id := range ids {
		created = append(created, id)
	}
	require.Len(t, created, 1)
	repo := newProxyRepositoryWithSQL(integrationEntClient, integrationDB)
	ordinary := authz.WithSubject(context.Background(), authz.Subject{Role: authz.Admin})
	_, err := repo.GetByID(ordinary, created[0])
	require.ErrorIs(t, err, service.ErrProxyNotFound)
	list, _, err := repo.ListWithFilters(ordinary, pagination.PaginationParams{Page: 1, PageSize: 100}, "", "", "")
	require.NoError(t, err)
	for _, p := range list {
		require.Nil(t, p.VendorID)
	}
	_, err = repo.CountAccountsByProxyID(ordinary, created[0])
	require.ErrorIs(t, err, service.ErrProxyNotFound)
	err = repo.Update(context.Background(), &service.Proxy{ID: created[0], Name: "forged", Protocol: "http", Host: "forged.invalid", Port: 8080, Status: service.StatusActive})
	require.ErrorIs(t, err, service.ErrProxyNotFound)
	wrong := authz.WithVendorScope(context.Background(), authz.VendorScope{VendorID: vID + 9999})
	_, err = repo.GetByID(wrong, created[0])
	require.ErrorIs(t, err, service.ErrProxyNotFound)
}

func TestVendorProxyReferencesEditAndDeleteLifecycle(t *testing.T) {
	s, ctx, vID := vendorProxyFixture(t, 10)
	p, err := s.CreateVendorProxy(ctx, vendorProxyInput("first"))
	require.NoError(t, err)
	require.NoError(t, s.RegisterVendorProxyReference(ctx, vID, p.ID, "oauth", "session", time.Now().Add(time.Minute)))
	require.ErrorIs(t, s.DeleteVendorProxy(ctx, p.ID), service.ErrProxyInUse)
	require.NoError(t, s.ReleaseVendorProxyReference(ctx, vID, p.ID, "oauth", "session"))
	var accountID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `INSERT INTO accounts(name,platform,type,credentials,extra,status,proxy_id,vendor_id,vendor_paused_at,created_at,updated_at) VALUES('bound','openai','oauth','{"access_token":"KEEP"}','{}','disabled',$1,$2,NOW(),NOW(),NOW()) RETURNING id`, p.ID, vID).Scan(&accountID))
	_, err = integrationDB.ExecContext(ctx, `INSERT INTO accounts(name,platform,type,credentials,extra,status,proxy_id,vendor_id,created_at,updated_at) VALUES('snapshot','openai','apikey','{}','{"upstream_billing_probe":{"status":"ok"},"independent":"KEEP"}','active',$1,$2,NOW(),NOW())`, p.ID, vID)
	require.NoError(t, err)
	newHost := "changed.invalid"
	updated, err := s.UpdateVendorProxy(ctx, p.ID, service.VendorProxyPatch{ConfigVersion: p.ConfigVersion, Host: &newHost})
	require.NoError(t, err)
	require.Equal(t, p.ConfigVersion+1, updated.ConfigVersion)
	var token, status string
	var paused bool
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT credentials->>'access_token',status,vendor_paused_at IS NOT NULL FROM accounts WHERE id=$1", accountID).Scan(&token, &status, &paused))
	require.Equal(t, "KEEP", token)
	require.Equal(t, "disabled", status)
	require.True(t, paused)
	var snapshotPresent bool
	var independent string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT extra ? 'upstream_billing_probe',extra->>'independent' FROM accounts WHERE vendor_id=$1 AND name='snapshot'`, vID).Scan(&snapshotPresent, &independent))
	require.False(t, snapshotPresent)
	require.Equal(t, "KEEP", independent)
	_, err = s.UpdateVendorProxy(ctx, p.ID, service.VendorProxyPatch{ConfigVersion: p.ConfigVersion, Host: &newHost})
	require.ErrorIs(t, err, service.ErrVendorProxyVersion)
	require.ErrorIs(t, s.DeleteVendorProxy(ctx, p.ID), service.ErrProxyInUse)
	_, err = integrationDB.ExecContext(ctx, "UPDATE accounts SET deleted_at=NOW() WHERE vendor_id=$1", vID)
	require.NoError(t, err)
	require.NoError(t, s.DeleteVendorProxy(ctx, p.ID))
	require.NoError(t, s.DeleteVendorProxy(ctx, p.ID))
	_, err = s.GetVendorProxy(ctx, p.ID)
	require.ErrorIs(t, err, service.ErrProxyNotFound)
}

func TestVendorProxyDeleteWaitsForNewBinding(t *testing.T) {
	s, ctx, vID := vendorProxyFixture(t, 10)
	p, err := s.CreateVendorProxy(ctx, vendorProxyInput("binding"))
	require.NoError(t, err)
	tx, err := integrationDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer tx.Rollback()
	require.NoError(t, service.AcquireVendorProxyLock(ctx, tx, vID))
	var id int64
	require.NoError(t, tx.QueryRowContext(ctx, "SELECT id FROM vendors WHERE id=$1 FOR UPDATE", vID).Scan(&id))
	_, err = service.ValidateVendorProxyBinding(ctx, tx, vID, p.ID)
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() { done <- s.DeleteVendorProxy(ctx, p.ID) }()
	select {
	case e := <-done:
		t.Fatalf("delete passed active binding transaction: %v", e)
	case <-time.After(80 * time.Millisecond):
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO accounts(name,platform,type,credentials,extra,status,proxy_id,vendor_id,created_at,updated_at) VALUES('race','openai','oauth','{}','{}','active',$1,$2,NOW(),NOW())", p.ID, vID)
	require.NoError(t, err)
	require.NoError(t, tx.Commit())
	select {
	case err = <-done:
		require.ErrorIs(t, err, service.ErrProxyInUse)
	case <-time.After(5 * time.Second):
		t.Fatal("delete remained blocked")
	}
}

func TestVendorProxyNativeFallbackAndRevertRequireLiveSameOwner(t *testing.T) {
	s, ctx, vID := vendorProxyFixture(t, 10)
	origin, err := s.CreateVendorProxy(ctx, vendorProxyInput("origin"))
	require.NoError(t, err)
	in := vendorProxyInput("backup")
	in.Port++
	backup, err := s.CreateVendorProxy(ctx, in)
	require.NoError(t, err)
	_, otherCtx, _ := vendorProxyFixture(t, 10)
	foreign, err := s.CreateVendorProxy(otherCtx, vendorProxyInput("foreign"))
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, "UPDATE proxies SET fallback_mode='proxy',backup_proxy_id=$2 WHERE id=$1", origin.ID, foreign.ID)
	require.Error(t, err, "database ownership guard must reject cross-vendor backup")
	var accountID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `INSERT INTO accounts(name,platform,type,credentials,extra,status,proxy_id,vendor_id,vendor_paused_at,created_at,updated_at) VALUES('fallback','openai','oauth','{"access_token":"KEEP"}','{}','disabled',$1,$2,NOW(),NOW(),NOW()) RETURNING id`, origin.ID, vID).Scan(&accountID))
	_, err = integrationDB.ExecContext(ctx, "UPDATE proxies SET expires_at=NOW()-INTERVAL '1 minute',fallback_mode='proxy',backup_proxy_id=$2 WHERE id=$1", origin.ID, backup.ID)
	require.NoError(t, err)
	repo := NewProxyRepository(integrationEntClient, integrationDB)
	n, err := repo.SweepExpiredProxies(ctx, time.Now())
	require.NoError(t, err)
	require.Equal(t, int64(1), n)
	var bound, initial int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT proxy_id,proxy_fallback_origin_id FROM accounts WHERE id=$1", accountID).Scan(&bound, &initial))
	require.Equal(t, backup.ID, bound)
	require.Equal(t, origin.ID, initial)
	accounts := NewAccountRepository(integrationEntClient, integrationDB, nil)
	require.ErrorIs(t, accounts.RevertProxyFallback(ctx, accountID), service.ErrVendorProxyUnavailable)
	require.ErrorIs(t, s.DeleteVendorProxy(ctx, origin.ID), service.ErrProxyInUse)
	expires, _ := json.Marshal(time.Now().Add(time.Hour))
	_, err = s.UpdateVendorProxy(ctx, origin.ID, service.VendorProxyPatch{ConfigVersion: 1, ExpiresAt: expires})
	require.NoError(t, err)
	require.NoError(t, accounts.RevertProxyFallback(ctx, accountID))
	var token, status string
	var paused, cleared bool
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT proxy_id,credentials->>'access_token',status,vendor_paused_at IS NOT NULL,proxy_fallback_origin_id IS NULL FROM accounts WHERE id=$1", accountID).Scan(&bound, &token, &status, &paused, &cleared))
	require.Equal(t, origin.ID, bound)
	require.Equal(t, "KEEP", token)
	require.Equal(t, "disabled", status)
	require.True(t, paused)
	require.True(t, cleared)
}
