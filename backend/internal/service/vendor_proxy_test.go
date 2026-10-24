package service

import (
	"context"
	"encoding/json"
	"github.com/Wei-Shaw/sub2api/internal/authz"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
	"time"
)

func TestVendorProxyTrustedScopeCannotBeReplaced(t *testing.T) {
	s := &VendorService{}
	for _, role := range []string{authz.User, authz.Admin, authz.SuperAdmin} {
		ctx := authz.WithSubject(context.Background(), authz.Subject{UserID: 3, Role: role})
		ctx = authz.WithVendorScope(ctx, authz.VendorScope{VendorID: 7, ActorUserID: 3, Admin: role == authz.SuperAdmin})
		_, err := s.proxyReadScope(ctx, 8)
		require.ErrorIs(t, err, ErrProxyNotFound)
		_, err = s.proxyReadScope(ctx, -1)
		require.ErrorIs(t, err, ErrProxyNotFound)
	}
}

func TestVendorProxyAggregateScopeDoesNotGrantAdministratorRole(t *testing.T) {
	s := &VendorService{}
	for _, role := range []string{authz.User, authz.Admin} {
		ctx := authz.WithSubject(context.Background(), authz.Subject{UserID: 3, Role: role})
		ctx = authz.WithVendorScope(ctx, authz.VendorScope{Admin: true, ActorUserID: 3})
		_, err := s.BatchDeleteVendorProxies(ctx, []int64{1})
		require.ErrorIs(t, err, ErrVendorDenied)
	}
}

func vendorProxyTestInput() VendorProxyInput {
	return VendorProxyInput{Name: "proxy", Protocol: "HTTP", Host: "EXAMPLE.invalid", Port: 8080, Username: "user", Password: "secret", Status: StatusActive}
}
func TestVendorProxyInputRejectsPrivateAndMalformed(t *testing.T) {
	for _, host := range []string{"127.0.0.1", "10.0.0.1", "::1", "::ffff:127.0.0.1", "169.254.169.254", "fc00::1", "example.invalid/path", "user@host.invalid", "host.invalid?x", "fe80::1%eth0"} {
		t.Run(host, func(t *testing.T) {
			in := vendorProxyTestInput()
			in.Host = host
			require.Error(t, validateVendorProxyInput(&in))
		})
	}
	in := vendorProxyTestInput()
	require.NoError(t, validateVendorProxyInput(&in))
	require.Equal(t, "http", in.Protocol)
	require.Equal(t, "example.invalid", in.Host)
	in.Port = 65536
	require.Error(t, validateVendorProxyInput(&in))
}
func TestVendorProxyCredentialActions(t *testing.T) {
	p := &Proxy{Name: "proxy", Protocol: "http", Host: "example.invalid", Port: 8080, Username: "original-user", Password: "original-password", Status: StatusActive}
	empty := ""
	newPassword := "new-password"
	for _, patch := range []VendorProxyPatch{{}, {CredentialAction: "preserve", Password: &empty}, {CredentialAction: "update", Password: &empty}} {
		in, err := applyVendorProxyPatch(p, patch)
		require.NoError(t, err)
		require.Equal(t, p.Password, in.Password)
		require.Equal(t, p.Username, in.Username)
	}
	in, err := applyVendorProxyPatch(p, VendorProxyPatch{CredentialAction: "update", Password: &newPassword})
	require.NoError(t, err)
	require.Equal(t, newPassword, in.Password)
	in, err = applyVendorProxyPatch(p, VendorProxyPatch{CredentialAction: "clear"})
	require.NoError(t, err)
	require.Empty(t, in.Username)
	require.Empty(t, in.Password)
	_, err = applyVendorProxyPatch(p, VendorProxyPatch{Password: &newPassword})
	require.Error(t, err)
}

func TestVendorProxyCanonicalEndpointIdentity(t *testing.T) {
	base := vendorProxyTestInput()
	for _, tc := range []struct {
		name   string
		change func(*VendorProxyInput)
		same   bool
	}{
		{"host spelling", func(p *VendorProxyInput) { p.Host = " example.INVALID. " }, true},
		{"protocol spelling", func(p *VendorProxyInput) { p.Protocol = " http " }, true},
		{"different protocol", func(p *VendorProxyInput) { p.Protocol = "https" }, false},
		{"different port", func(p *VendorProxyInput) { p.Port++ }, false},
		{"different username", func(p *VendorProxyInput) { p.Username += " " }, false},
		{"different password", func(p *VendorProxyInput) { p.Password += " " }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			other := base
			tc.change(&other)
			require.Equal(t, tc.same, proxyEndpointKey(base) == proxyEndpointKey(other))
		})
	}
	base.Protocol, base.Host = "SOCKS5", "2606:4700:4700:0:0:0:0:1111"
	other := base
	other.Protocol, other.Host = "socks5h", "2606:4700:4700::1111"
	require.Equal(t, proxyEndpointKey(base), proxyEndpointKey(other))
	base.Host, other.Host = "::ffff:8.8.8.8", "8.8.8.8"
	require.Equal(t, proxyEndpointKey(base), proxyEndpointKey(other))
}
func TestVendorProxySnapshotVersionAndLabels(t *testing.T) {
	now := time.Now()
	id := int64(7)
	p := &Proxy{ID: 3, VendorID: &id, Name: "proxy", Host: "example.invalid", Protocol: "http", Port: 8080, Username: "SECRET_USER", Password: "SECRET_PASSWORD", Status: StatusActive, ConfigVersion: 2, RegionLabelCountryCode: "US", RegionLabelCity: "label"}
	info := &ProxyLatencyInfo{Success: true, CountryCode: "JP", City: "detected", ConfigVersion: 1, UpdatedAt: now}
	d := vendorProxyDTO(p, info, now)
	require.Equal(t, "label", d.RegionSource)
	require.Equal(t, "untested", d.EffectiveStatus)
	info.ConfigVersion = 2
	d = vendorProxyDTO(p, info, now)
	require.Equal(t, "detected", d.RegionSource)
	require.Equal(t, "JP", d.CountryCode)
	info.Quality = &ProxyQualityCheckResult{CheckedAt: now.Add(-25 * time.Hour).Unix(), Items: []ProxyQualityCheckItem{{Status: "fail", Message: "SECRET_PASSWORD"}}}
	require.Nil(t, vendorProxyDTO(p, info, now).Quality)
	info.Quality.CheckedAt = now.Unix()
	d = vendorProxyDTO(p, info, now)
	require.NotContains(t, d.Quality.Items[0].Message, "SECRET")
	info.UpdatedAt = now.Add(-25 * time.Hour)
	d = vendorProxyDTO(p, info, now)
	require.Equal(t, "label", d.RegionSource)
	b, err := json.Marshal(d)
	require.NoError(t, err)
	require.NotContains(t, string(b), "SECRET_")
	require.NotContains(t, d.Endpoint, "@")
	require.NotContains(t, d.Endpoint, "vendor-proxy")
	p.Status = "inactive"
	p.ExpiresAt = &now
	require.Equal(t, "inactive", vendorProxyDTO(p, nil, now).EffectiveStatus)
}
func TestVendorProxyFilterDatesNullLatencyAndStableSort(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)
	start, err := time.ParseInLocation("2006-01-02", "2026-10-15", loc)
	require.NoError(t, err)
	ms := int64(12)
	items := []VendorProxyDTO{{ID: 1, CreatedAt: start.Add(-time.Nanosecond)}, {ID: 2, Name: "needle", Host: "one.invalid", CreatedAt: start, LatencyMs: &ms, AccountCount: 1, CountryCode: "JP"}, {ID: 3, CreatedAt: start.AddDate(0, 0, 1).Add(-time.Nanosecond)}, {ID: 4, CreatedAt: start.AddDate(0, 0, 1)}}
	out, err := filterVendorProxies(items, VendorProxyFilter{StartDate: "2026-10-15", EndDate: "2026-10-15", SortBy: "latency", SortOrder: "desc"}, loc)
	require.NoError(t, err)
	require.Len(t, out, 2)
	require.Equal(t, int64(2), out[0].ID)
	out, err = filterVendorProxies(items, VendorProxyFilter{Country: "JP", Occupancy: "occupied", Search: "needle"}, loc)
	require.NoError(t, err)
	require.Len(t, out, 1)
	for _, f := range []VendorProxyFilter{{StartDate: "2026-02-30"}, {StartDate: "2026-10-16", EndDate: "2026-10-15"}, {SortBy: "password"}, {Occupancy: "anything"}} {
		_, err = filterVendorProxies(items, f, loc)
		require.Error(t, err)
	}
}
func TestVendorProxyImportNativeWhitelistAndQuickSecrets(t *testing.T) {
	native, nativeErrors, err := parseVendorProxyImport(VendorProxyImportRequest{Format: "json", Content: `{"proxies":[{"name":"admin export","protocol":"http","host":"public.invalid","port":8080,"username":"user","password":"password"}]}`})
	require.NoError(t, err)
	require.NoError(t, nativeErrors[0])
	require.Equal(t, "password", native[0].Password)
	req := VendorProxyImportRequest{Format: "json", Content: `{"type":"sub2api-data","version":1,"proxies":[{"name":"native","protocol":"HTTP","host":"PUBLIC.invalid","port":8080,"username":"secret-user","password":"secret-pass","vendor_id":999,"status":"active","fallback_mode":"direct","config_version":999,"expires_at":"2026-02-30T00:00:00Z"},{"name":"clean","protocol":"http","host":"clean.invalid","port":80,"owner":"other","status":"failed"}]}`}
	inputs, errs, err := parseVendorProxyImport(req)
	require.NoError(t, err)
	require.Len(t, inputs, 2)
	require.Error(t, errs[0])
	require.NoError(t, errs[1])
	require.Equal(t, StatusActive, inputs[1].Status)
	inputs, errs, err = parseVendorProxyImport(VendorProxyImportRequest{Format: "quick", Content: "http://user:p%40ss@EXAMPLE.invalid:8080\nhttp://127.0.0.1:80\nsocks5://[2606:4700:4700::1111]:1080"})
	require.NoError(t, err)
	require.Equal(t, "p@ss", inputs[0].Password)
	require.NoError(t, errs[0])
	require.Error(t, errs[1])
	require.NoError(t, errs[2])
	require.Equal(t, "socks5h", inputs[2].Protocol)
	for _, raw := range []string{`{"type":"sub2api-vendor-proxy-demo","version":1,"proxies":[]}`, `{"type":"sub2api-data","version":2,"proxies":[]}`} {
		_, _, err = parseVendorProxyImport(VendorProxyImportRequest{Format: "json", Content: raw})
		require.Error(t, err)
	}
	before := proxyInputDigest(req)
	req.Content += " "
	require.NotEqual(t, before, proxyInputDigest(req))
}
func TestVendorProxyCSVFormulaProtection(t *testing.T) {
	for _, s := range []string{"=SUM(1,2)", " +SUM(1,2)", "@cmd", "-1+2", "\tformula", "\n=bad", "\r=bad"} {
		require.True(t, strings.HasPrefix(proxyCSVCell(s), "'"))
	}
	require.Equal(t, "ordinary", proxyCSVCell("ordinary"))
}
func TestVendorProxyFallbackCannotCrossOwnerOrGoDirect(t *testing.T) {
	a, b := int64(1), int64(2)
	backup := int64(4)
	start := Proxy{ID: 3, VendorID: &a, FallbackMode: FallbackModeProxy, BackupProxyID: &backup}
	for _, owner := range []*int64{nil, &b} {
		_, change := ResolveProxyFallbackTarget(start, map[int64]Proxy{4: {ID: 4, VendorID: owner, Status: StatusActive}}, time.Now())
		require.False(t, change)
	}
	target, change := ResolveProxyFallbackTarget(start, map[int64]Proxy{4: {ID: 4, VendorID: &a, Status: StatusActive}}, time.Now())
	require.True(t, change)
	require.Equal(t, backup, *target)
	start.FallbackMode = FallbackModeDirect
	_, change = ResolveProxyFallbackTarget(start, nil, time.Now())
	require.False(t, change)
}

func TestVendorProxyNativeFieldsPatchAndDefaults(t *testing.T) {
	in := vendorProxyTestInput()
	require.NoError(t, validateVendorProxyInput(&in))
	require.Equal(t, FallbackModeNone, in.FallbackMode)
	require.Nil(t, in.BackupProxyID)
	require.Equal(t, 7, *in.ExpiryWarnDays)
	id := int64(12)
	p := &Proxy{Name: in.Name, Protocol: in.Protocol, Host: in.Host, Port: in.Port, Status: StatusActive, Username: " user ", Password: " p@ss:word ", FallbackMode: FallbackModeProxy, BackupProxyID: &id, ExpiryWarnDays: 0}
	preserved, err := applyVendorProxyPatch(p, VendorProxyPatch{})
	require.NoError(t, err)
	require.Equal(t, FallbackModeProxy, preserved.FallbackMode)
	require.Equal(t, p.BackupProxyID, preserved.BackupProxyID)
	require.Zero(t, *preserved.ExpiryWarnDays)
	require.Equal(t, p.Password, preserved.Password)
	mode := FallbackModeNone
	cleared, err := applyVendorProxyPatch(p, VendorProxyPatch{FallbackMode: &mode, BackupProxyID: json.RawMessage("null")})
	require.NoError(t, err)
	require.Nil(t, cleared.BackupProxyID)
	require.Equal(t, FallbackModeNone, cleared.FallbackMode)
	for _, raw := range []string{"null", "0", "-1", `"12"`, "1.5", "{}"} {
		_, err = applyVendorProxyPatch(p, VendorProxyPatch{BackupProxyID: json.RawMessage(raw)})
		require.Error(t, err, raw)
	}
	for _, mode := range []string{FallbackModeDirect, "unexpected"} {
		in.FallbackMode = mode
		require.Error(t, validateVendorProxyInput(&in))
	}
	in.FallbackMode = FallbackModeNone
	negative := -1
	in.ExpiryWarnDays = &negative
	require.Error(t, validateVendorProxyInput(&in))
	d := vendorProxyDTO(p, nil, time.Now())
	require.Equal(t, p.FallbackMode, d.FallbackMode)
	require.Equal(t, p.BackupProxyID, d.BackupProxyID)
	require.Zero(t, d.ExpiryWarnDays)
	payload, err := json.Marshal(d)
	require.NoError(t, err)
	require.NotContains(t, string(payload), p.Password)
	require.NotContains(t, string(payload), p.Username)
}
