package service

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/authz"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

// Extend the shared disposable Postgres fixture; never use a configured app DB.
func vendorProxyFieldsPostgres(t *testing.T) (*VendorService, context.Context, context.Context) {
	t.Helper()
	s, _, own := vendorSettlementPostgres(t)
	_, err := s.db.Exec(`
 CREATE SEQUENCE proxy_fields_id_seq;
 ALTER TABLE proxies ALTER COLUMN id SET DEFAULT nextval('proxy_fields_id_seq');
 ALTER TABLE proxies ADD COLUMN name TEXT NOT NULL DEFAULT '', ADD COLUMN protocol TEXT NOT NULL DEFAULT 'http', ADD COLUMN host TEXT NOT NULL DEFAULT 'public.invalid', ADD COLUMN port INT NOT NULL DEFAULT 8080, ADD COLUMN username TEXT, ADD COLUMN password TEXT, ADD COLUMN status TEXT NOT NULL DEFAULT 'active', ADD COLUMN created_at TIMESTAMPTZ DEFAULT NOW(), ADD COLUMN updated_at TIMESTAMPTZ DEFAULT NOW(), ADD COLUMN expires_at TIMESTAMPTZ, ADD COLUMN fallback_mode TEXT NOT NULL DEFAULT 'none', ADD COLUMN backup_proxy_id BIGINT REFERENCES proxies(id), ADD COLUMN expiry_warn_days INT NOT NULL DEFAULT 7, ADD COLUMN created_by BIGINT, ADD COLUMN vendor_notes TEXT NOT NULL DEFAULT '', ADD COLUMN region_label_country_code TEXT NOT NULL DEFAULT '', ADD COLUMN region_label_city TEXT NOT NULL DEFAULT '', ADD COLUMN config_version BIGINT NOT NULL DEFAULT 1;
 ALTER TABLE accounts ADD COLUMN proxy_id BIGINT, ADD COLUMN proxy_fallback_origin_id BIGINT;
 CREATE TABLE scheduler_outbox(id BIGSERIAL PRIMARY KEY,event_type TEXT,account_id BIGINT);
 UPDATE vendors SET proxy_limit=100;
 INSERT INTO users(id,role,status,session_generation,email,username) VALUES(5,'vendor_level2','active',1,'child@invalid','child');
 INSERT INTO vendors(id,user_id,name,status,policy_version,platforms,account_limit,proxy_limit,allow_direct,default_concurrency,default_priority,default_rate_multiplier,contact_name,settlement_email,binance_id,default_coefficient,group_coefficients,contract_version,tier,parent_vendor_id)
 SELECT 3,5,'child',status,policy_version,platforms,account_limit,proxy_limit,allow_direct,default_concurrency,default_priority,default_rate_multiplier,contact_name,settlement_email,binance_id,default_coefficient,group_coefficients,contract_version,2,1 FROM vendors WHERE id=1;
 INSERT INTO vendor_groups VALUES(3,1,TRUE);
 `)
	require.NoError(t, err)
	r := miniredis.RunT(t)
	s.rdb = redis.NewClient(&redis.Options{Addr: r.Addr()})
	t.Cleanup(func() { s.rdb.Close() })
	child := authz.WithSubject(context.Background(), authz.Subject{UserID: 5, Role: authz.VendorLevel2, SessionGeneration: 1})
	child = authz.WithVendorScope(child, authz.VendorScope{VendorID: 3, PolicyVersion: 1, ActorUserID: 5})
	return s, own, child
}
func proxyFieldsInput(name string) VendorProxyInput {
	in := vendorProxyTestInput()
	in.Name = name
	in.Host = name + ".invalid"
	return in
}
func assertProxyFields(t *testing.T, p *VendorProxyDTO, backup int64, warn int) {
	t.Helper()
	require.Equal(t, FallbackModeProxy, p.FallbackMode)
	require.Equal(t, backup, *p.BackupProxyID)
	require.Equal(t, warn, p.ExpiryWarnDays)
	b, e := json.Marshal(p)
	require.NoError(t, e)
	require.NotContains(t, string(b), "SECRET")
	var fields map[string]any
	require.NoError(t, json.Unmarshal(b, &fields))
	require.NotContains(t, fields, "password")
	require.NotContains(t, fields, "username")
}

func TestVendorProxyFieldsStoragePreviewImportExport(t *testing.T) {
	s, ctx, _ := vendorProxyFieldsPostgres(t)
	backup, err := s.CreateVendorProxy(ctx, proxyFieldsInput("backup"))
	require.NoError(t, err)
	in := proxyFieldsInput("origin")
	in.Username = " SECRET_USER "
	in.Password = " SECRET:p@ss "
	in.FallbackMode = FallbackModeProxy
	in.BackupProxyID = &backup.ID
	warn := 0
	in.ExpiryWarnDays = &warn
	expiry := time.Now().UTC().Add(24 * time.Hour).Truncate(time.Second)
	in.ExpiresAt = &expiry
	origin, err := s.CreateVendorProxy(ctx, in)
	require.NoError(t, err)
	assertProxyFields(t, origin, backup.ID, 0)
	got, err := s.GetVendorProxy(ctx, origin.ID)
	require.NoError(t, err)
	assertProxyFields(t, got, backup.ID, 0)
	list, err := s.ListVendorProxies(ctx, VendorProxyFilter{Search: "origin", SortBy: "created_at", SortOrder: "asc"})
	require.NoError(t, err)
	require.Len(t, list.Items, 1)
	assertProxyFields(t, &list.Items[0], backup.ID, 0)
	name := "renamed"
	got, err = s.UpdateVendorProxy(ctx, origin.ID, VendorProxyPatch{ConfigVersion: 1, Name: &name})
	require.NoError(t, err)
	assertProxyFields(t, got, backup.ID, 0)
	var username, password string
	require.NoError(t, s.db.QueryRow("SELECT username,password FROM proxies WHERE id=$1", origin.ID).Scan(&username, &password))
	require.Equal(t, in.Username, username)
	require.Equal(t, in.Password, password)
	for _, format := range []string{"json", "csv"} {
		data, _, e := s.ExportVendorProxies(ctx, VendorProxyExportRequest{Format: format, IDs: []int64{origin.ID}})
		require.NoError(t, e)
		require.NotContains(t, string(data), "SECRET")
		if format == "json" {
			inputs, errs, e := parseVendorProxyImport(VendorProxyImportRequest{Format: "json", Content: string(data)})
			require.NoError(t, e)
			require.NoError(t, errs[0])
			require.Len(t, inputs, 1)
			require.Equal(t, FallbackModeProxy, inputs[0].FallbackMode)
			require.Equal(t, backup.ID, *inputs[0].BackupProxyID)
			require.Zero(t, *inputs[0].ExpiryWarnDays)
			require.True(t, expiry.Equal(*inputs[0].ExpiresAt))
			require.Empty(t, inputs[0].Username)
			require.Empty(t, inputs[0].Password)
		} else {
			records, e := csv.NewReader(strings.NewReader(strings.TrimPrefix(string(data), "\ufeff"))).ReadAll()
			require.NoError(t, e)
			require.Len(t, records, 2)
			fields := map[string]string{}
			for i, key := range records[0] {
				fields[key] = records[1][i]
			}
			require.Equal(t, "proxy", fields["fallback_mode"])
			require.Equal(t, fmt.Sprint(backup.ID), fields["backup_proxy_id"])
			require.Equal(t, "0", fields["expiry_warn_days"])
		}
	}
	in.Host = "imported.invalid"
	raw, err := json.Marshal(map[string]any{"type": "sub2api-data", "version": 1, "proxies": []VendorProxyInput{in}})
	require.NoError(t, err)
	req := VendorProxyImportRequest{Format: "json", Content: string(raw)}
	preview, err := s.ImportVendorProxies(ctx, req, true)
	require.NoError(t, err)
	require.Equal(t, 1, preview.ValidCount)
	assertProxyFields(t, preview.Rows[0].Proxy, backup.ID, 0)
	req.InputDigest = preview.InputDigest
	imported, err := s.ImportVendorProxies(ctx, req, false)
	require.NoError(t, err)
	require.Equal(t, 1, imported.ValidCount)
	assertProxyFields(t, imported.Rows[0].Proxy, backup.ID, 0)
	duplicate, err := s.ImportVendorProxies(ctx, req, false)
	require.NoError(t, err)
	require.Equal(t, 1, duplicate.DuplicateCount)
	require.NoError(t, s.db.QueryRow("SELECT username,password FROM proxies WHERE id=$1", imported.Rows[0].ProxyID).Scan(&username, &password))
	require.Equal(t, in.Username, username)
	require.Equal(t, in.Password, password)
	none := FallbackModeNone
	got, err = s.UpdateVendorProxy(ctx, origin.ID, VendorProxyPatch{ConfigVersion: 2, FallbackMode: &none, BackupProxyID: json.RawMessage("null")})
	require.NoError(t, err)
	require.Nil(t, got.BackupProxyID)
	require.Equal(t, none, got.FallbackMode)
	require.Zero(t, got.ExpiryWarnDays)
	_, err = s.UpdateVendorProxy(ctx, origin.ID, VendorProxyPatch{ConfigVersion: 2, Name: &name})
	require.ErrorIs(t, err, ErrVendorProxyVersion)
}

func TestVendorProxyFallbackDirectTenantBoundary(t *testing.T) {
	s, own, child := vendorProxyFieldsPostgres(t)
	parent, err := s.CreateVendorProxy(own, proxyFieldsInput("parent"))
	require.NoError(t, err)
	childProxy, err := s.CreateVendorProxy(child, proxyFieldsInput("child"))
	require.NoError(t, err)
	var foreign, platform, archived int64
	for _, seed := range []struct {
		owner   any
		target  *int64
		deleted bool
	}{{int64(2), &foreign, false}, {nil, &platform, false}, {int64(1), &archived, true}} {
		require.NoError(t, s.db.QueryRow("INSERT INTO proxies(name,vendor_id,username,password,deleted_at) VALUES('private',$1,'SECRET_USER','SECRET_PASSWORD',CASE WHEN $2 THEN NOW() END) RETURNING id", seed.owner, seed.deleted).Scan(seed.target))
	}
	for _, tc := range []struct {
		name   string
		ctx    context.Context
		backup int64
	}{{"foreign", own, foreign}, {"platform", own, platform}, {"child", own, childProxy.ID}, {"parent", child, parent.ID}, {"archived", own, archived}, {"missing", own, 9999}} {
		t.Run(tc.name, func(t *testing.T) {
			in := proxyFieldsInput("blocked-" + tc.name)
			in.FallbackMode = FallbackModeProxy
			in.BackupProxyID = &tc.backup
			_, e := s.CreateVendorProxy(tc.ctx, in)
			require.ErrorIs(t, e, ErrProxyNotFound)
			payload, e := json.Marshal(map[string]any{"type": "sub2api-data", "version": 1, "proxies": []VendorProxyInput{in}})
			require.NoError(t, e)
			req := VendorProxyImportRequest{Format: "json", Content: string(payload)}
			preview, e := s.ImportVendorProxies(tc.ctx, req, true)
			require.NoError(t, e)
			require.Equal(t, 1, preview.InvalidCount)
			require.Nil(t, preview.Rows[0].Proxy)
			require.Equal(t, infraerrors.Reason(ErrProxyNotFound), preview.Rows[0].Code)
			req.InputDigest = preview.InputDigest
			result, e := s.ImportVendorProxies(tc.ctx, req, false)
			require.NoError(t, e)
			require.Equal(t, 1, result.InvalidCount)
			require.Zero(t, result.ValidCount)
			password := "ATTACKER"
			_, e = s.UpdateVendorProxy(tc.ctx, tc.backup, VendorProxyPatch{ConfigVersion: 1, CredentialAction: "update", Password: &password})
			require.ErrorIs(t, e, ErrProxyNotFound)
			_, e = s.GetVendorProxy(tc.ctx, tc.backup)
			require.ErrorIs(t, e, ErrProxyNotFound)
			_, _, e = s.ExportVendorProxies(tc.ctx, VendorProxyExportRequest{Format: "json", IDs: []int64{tc.backup}})
			require.ErrorIs(t, e, ErrProxyNotFound)
		})
	}
	var password string
	require.NoError(t, s.db.QueryRow("SELECT password FROM proxies WHERE id=$1", foreign).Scan(&password))
	require.Equal(t, "SECRET_PASSWORD", password)
	// An otherwise-owned node cannot disguise a cross-tenant hop or direct egress.
	for _, mode := range []string{FallbackModeProxy, FallbackModeDirect} {
		_, err = s.db.Exec("UPDATE proxies SET fallback_mode=$1,backup_proxy_id=$2 WHERE id=$3", mode, foreign, parent.ID)
		require.NoError(t, err)
		in := proxyFieldsInput("chain")
		in.FallbackMode = FallbackModeProxy
		in.BackupProxyID = &parent.ID
		_, err = s.CreateVendorProxy(own, in)
		require.Error(t, err)
	}
}

func TestVendorProxyFallbackLocksCycleAndReferences(t *testing.T) {
	s, ctx, _ := vendorProxyFieldsPostgres(t)
	a, err := s.CreateVendorProxy(ctx, proxyFieldsInput("a"))
	require.NoError(t, err)
	b, err := s.CreateVendorProxy(ctx, proxyFieldsInput("b"))
	require.NoError(t, err)
	mode := FallbackModeProxy
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for _, pair := range [][2]int64{{a.ID, b.ID}, {b.ID, a.ID}} {
		wg.Add(1)
		go func(source, target int64) {
			defer wg.Done()
			_, e := s.UpdateVendorProxy(ctx, source, VendorProxyPatch{ConfigVersion: 1, FallbackMode: &mode, BackupProxyID: json.RawMessage(fmt.Sprint(target))})
			errs <- e
		}(pair[0], pair[1])
	}
	wg.Wait()
	close(errs)
	success := 0
	for e := range errs {
		if e == nil {
			success++
		} else {
			require.Equal(t, "PROXY_BACKUP_SELF", infraerrors.Reason(e))
		}
	}
	require.Equal(t, 1, success)
	var source, target int64
	require.NoError(t, s.db.QueryRow("SELECT id,backup_proxy_id FROM proxies WHERE fallback_mode='proxy'").Scan(&source, &target))
	require.ErrorIs(t, s.DeleteVendorProxy(ctx, target), ErrProxyInUse)
	none := FallbackModeNone
	_, err = s.UpdateVendorProxy(ctx, source, VendorProxyPatch{ConfigVersion: 2, FallbackMode: &none})
	require.NoError(t, err)
	require.NoError(t, s.RegisterVendorProxyReference(ctx, 1, target, "inflight", "exchange", time.Now().Add(time.Minute)))
	require.ErrorIs(t, s.DeleteVendorProxy(ctx, target), ErrProxyInUse)
	require.NoError(t, s.ReleaseVendorProxyReference(ctx, 1, target, "inflight", "exchange"))
	require.NoError(t, s.DeleteVendorProxy(ctx, target))
	_, err = s.UpdateVendorProxy(ctx, source, VendorProxyPatch{ConfigVersion: 3, FallbackMode: &mode, BackupProxyID: json.RawMessage(fmt.Sprint(target))})
	require.ErrorIs(t, err, ErrProxyNotFound)
}

func TestVendorProxyFallbackPreviewRevalidatesAtCommit(t *testing.T) {
	s, ctx, _ := vendorProxyFieldsPostgres(t)
	backup, err := s.CreateVendorProxy(ctx, proxyFieldsInput("backup"))
	require.NoError(t, err)
	in := proxyFieldsInput("import-race")
	in.FallbackMode = FallbackModeProxy
	in.BackupProxyID = &backup.ID
	raw, err := json.Marshal(map[string]any{"type": "sub2api-data", "version": 1, "proxies": []VendorProxyInput{in}})
	require.NoError(t, err)
	req := VendorProxyImportRequest{Format: "json", Content: string(raw)}
	preview, err := s.ImportVendorProxies(ctx, req, true)
	require.NoError(t, err)
	require.Equal(t, 1, preview.ValidCount)
	require.NoError(t, s.DeleteVendorProxy(ctx, backup.ID))
	req.InputDigest = preview.InputDigest
	result, err := s.ImportVendorProxies(ctx, req, false)
	require.NoError(t, err)
	require.Equal(t, 1, result.InvalidCount)
	require.Zero(t, result.ValidCount)
	require.Equal(t, infraerrors.Reason(ErrProxyNotFound), result.Rows[0].Code)
	var count int
	require.NoError(t, s.db.QueryRow("SELECT COUNT(*) FROM proxies WHERE host=$1", in.Host).Scan(&count))
	require.Zero(t, count)
}

func TestVendorProxyFallbackDoesNotReviveDisabledOrExpiredBackups(t *testing.T) {
	s, ctx, child := vendorProxyFieldsPostgres(t)
	live, err := s.CreateVendorProxy(ctx, proxyFieldsInput("live"))
	require.NoError(t, err)
	for _, state := range []string{"inactive", "expired"} {
		in := proxyFieldsInput(state)
		in.FallbackMode = FallbackModeProxy
		in.BackupProxyID = &live.ID
		middle, e := s.CreateVendorProxy(ctx, in)
		require.NoError(t, e)
		_, e = s.db.Exec("UPDATE proxies SET status=$1,expires_at=NOW()-INTERVAL '1 hour' WHERE id=$2", state, middle.ID)
		require.NoError(t, e)
		sourceInput := proxyFieldsInput("source-" + state)
		sourceInput.FallbackMode = FallbackModeProxy
		sourceInput.BackupProxyID = &middle.ID
		source, e := s.CreateVendorProxy(ctx, sourceInput)
		require.NoError(t, e)
		original, e := s.ownedProxy(ctx, source.ID, 1)
		require.NoError(t, e)
		intermediate, e := s.ownedProxy(ctx, middle.ID, 1)
		require.NoError(t, e)
		target, e := s.ownedProxy(ctx, live.ID, 1)
		require.NoError(t, e)
		resolved, change := ResolveProxyFallbackTarget(*original, map[int64]Proxy{middle.ID: *intermediate, live.ID: *target}, time.Now())
		require.True(t, change)
		require.Equal(t, live.ID, *resolved)
		require.Equal(t, state, intermediate.Status)
		// Metadata-only updates preserve disabled state and configured fallback.
		if state == "inactive" {
			warn := 3
			edited, e := s.UpdateVendorProxy(ctx, middle.ID, VendorProxyPatch{ConfigVersion: 1, ExpiryWarnDays: &warn})
			require.NoError(t, e)
			require.Equal(t, "inactive", edited.Status)
			assertProxyFields(t, edited, live.ID, 3)
		}
	}
	// Parent read capability grants neither fallback mutation nor import/export.
	actor, _ := authz.FromContext(ctx)
	read := authz.WithVendorScope(ctx, authz.VendorScope{VendorID: 3, PolicyVersion: 1, ActorUserID: actor.UserID, ReadOnly: true})
	ownChild, e := s.CreateVendorProxy(child, proxyFieldsInput("child-only"))
	require.NoError(t, e)
	_, e = s.GetVendorProxy(read, ownChild.ID)
	require.NoError(t, e)
	_, e = s.CreateVendorProxy(read, proxyFieldsInput("forbidden"))
	require.ErrorIs(t, e, ErrVendorDenied)
	_, e = s.UpdateVendorProxy(read, ownChild.ID, VendorProxyPatch{ConfigVersion: 1})
	require.ErrorIs(t, e, ErrVendorDenied)
	_, e = s.ImportVendorProxies(read, VendorProxyImportRequest{Format: "quick", Content: "http://public.invalid:8080"}, true)
	require.ErrorIs(t, e, ErrVendorDenied)
	_, _, e = s.ExportVendorProxies(read, VendorProxyExportRequest{Format: "json"})
	require.ErrorIs(t, e, ErrVendorDenied)
}

func TestVendorProxyTreeCapacitySerializesParentAndChildCreation(t *testing.T) {
	s, own, child := vendorProxyFieldsPostgres(t)
	_, err := s.db.Exec("UPDATE vendors SET proxy_limit=2 WHERE id=1")
	require.NoError(t, err)
	_, err = s.CreateVendorProxy(own, proxyFieldsInput("already-used"))
	require.NoError(t, err)
	results := make(chan error, 6)
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ctx := own
			if i%2 == 1 {
				ctx = child
			}
			_, e := s.CreateVendorProxy(ctx, proxyFieldsInput(fmt.Sprintf("candidate-%d", i)))
			results <- e
		}(i)
	}
	wg.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		} else {
			require.Equal(t, "PROXY_LIMIT", infraerrors.Reason(err))
		}
	}
	require.Equal(t, 1, success, "one remaining tree slot must not be allocated twice")
	var count int
	require.NoError(t, s.db.QueryRow("SELECT count(*) FROM proxies WHERE vendor_id IN(1,3) AND deleted_at IS NULL").Scan(&count))
	require.Equal(t, 2, count)
	parentPolicy, err := s.loadVendor(own, 1)
	require.NoError(t, err)
	childPolicy, err := s.loadVendor(child, 3)
	require.NoError(t, err)
	require.Equal(t, int64(2), parentPolicy.ProxyTreeCount)
	require.Equal(t, int64(2), parentPolicy.ProxyCount+childPolicy.ProxyCount)
	require.Equal(t, childPolicy.ProxyCount, childPolicy.ProxyTreeCount)
	_, err = s.db.Exec("UPDATE vendors SET proxy_limit=3 WHERE id=1; UPDATE vendors SET proxy_limit=0 WHERE id=3")
	require.NoError(t, err)
	_, err = s.CreateVendorProxy(child, proxyFieldsInput("child-limit"))
	require.NoError(t, err, "zero child cap still permits the final parent slot")
	_, err = s.CreateVendorProxy(own, proxyFieldsInput("parent-still-allowed"))
	require.Equal(t, "PROXY_LIMIT", infraerrors.Reason(err), "zero child cap cannot bypass the finite parent cap")
	_, err = s.db.Exec("UPDATE vendors SET proxy_limit=0 WHERE id=1; UPDATE vendors SET proxy_limit=(SELECT count(*) FROM proxies WHERE vendor_id=3 AND deleted_at IS NULL) WHERE id=3")
	require.NoError(t, err)
	_, err = s.CreateVendorProxy(own, proxyFieldsInput("unlimited-parent"))
	require.NoError(t, err)
	_, err = s.CreateVendorProxy(child, proxyFieldsInput("finite-child-full"))
	require.Equal(t, "PROXY_LIMIT", infraerrors.Reason(err), "a finite child cap still applies under an unlimited parent")
	_, err = s.db.Exec("UPDATE vendors SET proxy_limit=0 WHERE id=3")
	require.NoError(t, err)
	_, err = s.CreateVendorProxy(child, proxyFieldsInput("both-unlimited"))
	require.NoError(t, err)
}

// Snapshot invalidation is covered by repository tests; these tests exercise
// identity collisions and the surrounding real PostgreSQL transactions.
type vendorProxyDedupRepository struct{ ProxyRepository }

func (vendorProxyDedupRepository) InvalidateVendorProxySnapshots(context.Context, VendorProxySQL, int64) error {
	return nil
}

func TestVendorProxyDuplicateIdentityLifecycle(t *testing.T) {
	s, ctx, child := vendorProxyFieldsPostgres(t)
	s.proxies = vendorProxyDedupRepository{}
	in := proxyFieldsInput("shared")
	in.Protocol, in.Host = "socks5h", "2606:4700:4700::1111"
	var platformID, foreignID int64
	for _, seed := range []struct {
		owner any
		id    *int64
	}{{nil, &platformID}, {int64(2), &foreignID}} {
		require.NoError(t, s.db.QueryRow("INSERT INTO proxies(vendor_id,name,protocol,host,port,username,password) VALUES($1,'private',$2,$3,$4,$5,$6) RETURNING id", seed.owner, in.Protocol, in.Host, in.Port, in.Username, in.Password).Scan(seed.id))
	}
	raw, err := json.Marshal(map[string]any{"type": "sub2api-data", "version": 1, "proxies": []VendorProxyInput{in}})
	require.NoError(t, err)
	req := VendorProxyImportRequest{Format: "json", Content: string(raw)}
	preview, err := s.ImportVendorProxies(ctx, req, true)
	require.NoError(t, err)
	require.Equal(t, 1, preview.ValidCount, "preview must not match another owner's inventory")
	own, err := s.CreateVendorProxy(ctx, in)
	require.NoError(t, err, "same endpoint in a different inventory does not grant or prevent ownership")
	_, err = s.CreateVendorProxy(child, in)
	require.NoError(t, err)
	_, err = s.CreateVendorProxy(ctx, in)
	require.Equal(t, "PROXY_DUPLICATE", infraerrors.Reason(err))
	// Historical textual aliases must agree with preview and direct creation.
	_, err = s.db.Exec("UPDATE proxies SET protocol='SOCKS5',host='2606:4700:4700:0:0:0:0:1111' WHERE id=$1", own.ID)
	require.NoError(t, err)
	_, err = s.CreateVendorProxy(ctx, in)
	require.Equal(t, "PROXY_DUPLICATE", infraerrors.Reason(err))
	preview, err = s.ImportVendorProxies(ctx, req, true)
	require.NoError(t, err)
	require.Equal(t, 1, preview.DuplicateCount)
	require.Nil(t, preview.Rows[0].Proxy)
	require.Zero(t, preview.Rows[0].ProxyID)
	req.InputDigest = preview.InputDigest
	result, err := s.ImportVendorProxies(ctx, req, false)
	require.NoError(t, err)
	require.Equal(t, 1, result.DuplicateCount)
	// Distinct protocol, port or credentials at one IP remain legitimate.
	for _, change := range []func(*VendorProxyInput){
		func(p *VendorProxyInput) { p.Protocol = "http" },
		func(p *VendorProxyInput) { p.Port++ },
		func(p *VendorProxyInput) { p.Username += "other" },
		func(p *VendorProxyInput) { p.Password += "other" },
	} {
		other := in
		change(&other)
		p, e := s.CreateVendorProxy(ctx, other)
		require.NoError(t, e)
		_, e = s.UpdateVendorProxy(ctx, p.ID, VendorProxyPatch{ConfigVersion: 1, Protocol: &in.Protocol, Port: &in.Port, Username: &in.Username, Password: &in.Password, CredentialAction: "update"})
		require.Equal(t, "PROXY_DUPLICATE", infraerrors.Reason(e))
		stored, e := s.ownedProxy(ctx, p.ID, 1)
		require.NoError(t, e)
		require.EqualValues(t, 1, stored.ConfigVersion, "collision rolls back without consuming the version")
	}
	anonymous := in
	anonymous.Username, anonymous.Password = "", ""
	_, err = s.CreateVendorProxy(ctx, anonymous)
	require.NoError(t, err)
	_, err = s.UpdateVendorProxy(ctx, own.ID, VendorProxyPatch{ConfigVersion: 1, CredentialAction: "clear"})
	require.Equal(t, "PROXY_DUPLICATE", infraerrors.Reason(err), "clearing auth must check the resulting identity")
	// Pre-existing same-owner duplicates can still be edited and repaired.
	var legacyID int64
	require.NoError(t, s.db.QueryRow("INSERT INTO proxies(vendor_id,name,protocol,host,port,username,password) VALUES(1,'legacy',$1,$2,$3,$4,$5) RETURNING id", in.Protocol, in.Host, in.Port, in.Username, in.Password).Scan(&legacyID))
	name := "renamed"
	edited, err := s.UpdateVendorProxy(ctx, legacyID, VendorProxyPatch{ConfigVersion: 1, Name: &name, CredentialAction: "preserve"})
	require.NoError(t, err)
	require.Equal(t, name, edited.Name)
	tx, err := s.beginProxyMutation(ctx, 1)
	require.NoError(t, err)
	defer tx.Rollback()
	bound, err := ValidateVendorProxyBinding(ctx, tx, 1, legacyID)
	require.NoError(t, err)
	require.Equal(t, in.Password, bound.Password)
	for _, id := range []int64{platformID, foreignID} {
		_, e := ValidateVendorProxyBinding(ctx, tx, 1, id)
		require.ErrorIs(t, e, ErrProxyNotFound)
	}
	_, err = tx.ExecContext(ctx, "UPDATE accounts SET proxy_id=$1 WHERE id=11 AND vendor_id=1", legacyID)
	require.NoError(t, err)
	require.NoError(t, commitVendorProxyMutation(ctx, tx))
	host := "repaired.invalid"
	edited, err = s.UpdateVendorProxy(ctx, legacyID, VendorProxyPatch{ConfigVersion: edited.ConfigVersion, Host: &host})
	require.NoError(t, err)
	require.Equal(t, host, edited.Host)
	for _, id := range []int64{platformID, foreignID} {
		_, e := s.UpdateVendorProxy(ctx, id, VendorProxyPatch{ConfigVersion: 1, Name: &name})
		require.ErrorIs(t, e, ErrProxyNotFound)
	}
	require.NoError(t, s.DeleteVendorProxy(ctx, own.ID))
	_, err = s.CreateVendorProxy(ctx, in)
	require.NoError(t, err, "soft-deleted identities do not reserve an endpoint")
}

func TestVendorProxyDuplicateConcurrentCreateImportAndUpdate(t *testing.T) {
	s, ctx, _ := vendorProxyFieldsPostgres(t)
	s.proxies = vendorProxyDedupRepository{}
	source, err := s.CreateVendorProxy(ctx, proxyFieldsInput("source"))
	require.NoError(t, err)
	in := proxyFieldsInput("destination")
	raw, err := json.Marshal(map[string]any{"type": "sub2api-data", "version": 1, "proxies": []VendorProxyInput{in}})
	require.NoError(t, err)
	req := VendorProxyImportRequest{Format: "json", Content: string(raw)}
	preview, err := s.ImportVendorProxies(ctx, req, true)
	require.NoError(t, err)
	require.Equal(t, 1, preview.ValidCount)
	req.InputDigest = preview.InputDigest
	start := make(chan struct{})
	results := make(chan string, 3)
	go func() {
		<-start
		_, e := s.CreateVendorProxy(ctx, in)
		if e == nil {
			results <- "created"
		} else {
			results <- infraerrors.Reason(e)
		}
	}()
	go func() {
		<-start
		_, e := s.UpdateVendorProxy(ctx, source.ID, VendorProxyPatch{ConfigVersion: 1, Host: &in.Host})
		if e == nil {
			results <- "created"
		} else {
			results <- infraerrors.Reason(e)
		}
	}()
	go func() {
		<-start
		r, e := s.ImportVendorProxies(ctx, req, false)
		if e != nil {
			results <- infraerrors.Reason(e)
			return
		}
		if r.ValidCount == 1 {
			results <- "created"
		} else {
			results <- r.Rows[0].Code
		}
	}()
	close(start)
	success := 0
	for i := 0; i < 3; i++ {
		select {
		case code := <-results:
			if code == "created" {
				success++
			} else {
				require.Equal(t, "PROXY_DUPLICATE", code)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("proxy mutation did not release its owner lock")
		}
	}
	require.Equal(t, 1, success)
	var count int
	require.NoError(t, s.db.QueryRow("SELECT count(*) FROM proxies WHERE vendor_id=1 AND host=$1 AND deleted_at IS NULL", in.Host).Scan(&count))
	require.Equal(t, 1, count)
}
