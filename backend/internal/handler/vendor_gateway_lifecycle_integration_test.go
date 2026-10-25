package handler_test

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/authz"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/pkg/httpclient"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/server/routes"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/imroc/req/v3"
	"github.com/redis/go-redis/v9"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

// Only the external provider is replaced. Enrollment, ownership checks, native
// API-key authentication, scheduling, billing, reports and settlement previews
// run against the real implementations and a fully migrated disposable database.
type vendorLifecycleProvider struct {
	service.HTTPUpstream
	server *httptest.Server
	calls  atomic.Int64
}

func (p *vendorLifecycleProvider) ExchangeCode(context.Context, string, string, string, string, string) (*openai.TokenResponse, error) {
	return &openai.TokenResponse{AccessToken: "fixture-access", RefreshToken: "fixture-refresh", ExpiresIn: 3600}, nil
}
func (p *vendorLifecycleProvider) RefreshToken(ctx context.Context, token, proxy string) (*openai.TokenResponse, error) {
	return p.ExchangeCode(ctx, "", "", "", proxy, "")
}
func (p *vendorLifecycleProvider) RefreshTokenWithClientID(ctx context.Context, token, proxy, clientID string) (*openai.TokenResponse, error) {
	return p.RefreshToken(ctx, token, proxy)
}
func (p *vendorLifecycleProvider) Do(r *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	p.calls.Add(1)
	copy := r.Clone(r.Context())
	u, _ := url.Parse(p.server.URL)
	copy.URL.Scheme, copy.URL.Host, copy.Host = u.Scheme, u.Host, u.Host
	return p.server.Client().Do(copy)
}

func TestVendorGatewayPostgresEnrollmentConsumptionLifecycle(t *testing.T) {
	db := vendorHTTPPostgres(t)
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	rdb := redis.NewClient(&redis.Options{Addr: miniredis.RunT(t).Addr()})
	t.Cleanup(func() { require.NoError(t, rdb.Close()) })
	cfg := &config.Config{RunMode: config.RunModeStandard, Timezone: "UTC", JWT: config.JWTConfig{Secret: "vendor-lifecycle-local-test-only-secret"}}
	cfg.Default.RateMultiplier = 1
	cfg.Gateway.MaxAccountSwitches = 1
	settings := service.NewSettingService(repository.NewSettingRepository(client), nil)
	_, err := db.Exec("INSERT INTO settings(key,value) VALUES('vendor_console_enabled','true') ON CONFLICT(key) DO UPDATE SET value='true'")
	require.NoError(t, err)
	accounts := repository.NewAccountRepository(client, db, nil)
	proxies := repository.NewProxyRepository(client, db)
	users := repository.NewUserRepository(client, db)
	groups := repository.NewGroupRepository(client, db)
	keys := repository.NewAPIKeyRepository(client, db)
	subscriptions := repository.NewUserSubscriptionRepository(client)
	rates := repository.NewUserGroupRateRepository(db)
	usage := repository.NewUsageLogRepository(client, db)
	provider := &vendorLifecycleProvider{}
	quotaReset := time.Now().UTC().Add(time.Hour).Truncate(time.Second).Unix()
	provider.server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/backend-api/accounts/check/v4-2023-04-27":
			_, _ = w.Write([]byte(`{"accounts":{"fixture-workspace":{"account":{"account_id":"fixture-workspace","plan_type":"plus","is_default":true}}}}`))
		case "/backend-api/me":
			_, _ = w.Write([]byte(`{"email":"verified@fixture.invalid"}`))
		case "/backend-api/codex/responses":
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("x-codex-primary-used-percent", "42")
			w.Header().Set("x-codex-primary-window-minutes", "300")
			w.Header().Set("x-codex-primary-reset-after-seconds", "3600")
			w.Header().Set("x-codex-secondary-used-percent", "63")
			w.Header().Set("x-codex-secondary-window-minutes", "10080")
			w.Header().Set("x-codex-secondary-reset-after-seconds", "90000")
			w.Header().Set("x-request-id", fmt.Sprintf("vendor-lifecycle-%d", provider.calls.Load()))
			_, _ = fmt.Fprintf(w, "event: response.completed\ndata: %s\n\n", `{"type":"response.completed","response":{"id":"resp_fixture","status":"completed","model":"gpt-5.2","output":[{"id":"msg_fixture","type":"message","role":"assistant","content":[{"type":"output_text","text":"fixture reply"}]}],"usage":{"input_tokens":1000,"input_tokens_details":{"cached_tokens":200},"output_tokens":1000,"total_tokens":2000}}}`)
		default:
			_, _ = w.Write([]byte("{}"))
		}
	}))
	t.Cleanup(provider.server.Close)
	oauth := service.NewOpenAIOAuthService(proxies, provider, rdb)
	t.Cleanup(oauth.Stop)
	oauth.SetPrivacyClientFactory(func(string) (*req.Client, error) {
		return req.C().SetTimeout(5 * time.Second).SetDialTLS(func(ctx context.Context, network, _ string) (net.Conn, error) {
			// Trust only this httptest server; never change process/global trust.
			d := tls.Dialer{Config: provider.server.Client().Transport.(*http.Transport).TLSClientConfig}
			return d.DialContext(ctx, network, provider.server.Listener.Addr().String())
		}), nil
	})
	// The native quota probe uses the shared HTTP client. Redirect only this
	// test's pooled transport to the same trusted TLS fixture, then restore it.
	probeClient, err := httpclient.GetClient(httpclient.Options{Timeout: 15 * time.Second, ResponseHeaderTimeout: 10 * time.Second})
	require.NoError(t, err)
	previousTransport := probeClient.Transport
	transport := provider.server.Client().Transport.(*http.Transport).Clone()
	transport.DialTLSContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		d := tls.Dialer{Config: transport.TLSClientConfig}
		return d.DialContext(ctx, network, provider.server.Listener.Addr().String())
	}
	probeClient.Transport = transport
	t.Cleanup(func() { probeClient.Transport = previousTransport; transport.CloseIdleConnections() })
	accountUsage := service.NewAccountUsageService(accounts, usage, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	native := service.NewAdminService(cfg, users, groups.(service.AdminGroupRepository), accounts.(service.AdminAccountRepository), proxies, nil, nil, nil, nil, nil, nil, nil, nil, client, settings, nil, nil, nil, nil, nil, nil, nil, nil)
	vendors := service.NewVendorService(db, client, accounts, proxies, groups, users, native, settings, nil, oauth, rdb, nil, accountUsage, cfg)
	actors := map[string]authz.Subject{}
	for name, role := range map[string]string{"root": authz.SuperAdmin, "consumer": authz.User} {
		var id int64
		require.NoError(t, db.QueryRow("INSERT INTO users(email,password_hash,role,status,balance,concurrency) VALUES($1,'fixture',$2,'active',10,5) RETURNING id", name+"@fixture.invalid", role).Scan(&id))
		actors[name] = authz.Subject{UserID: id, Role: role}
	}
	var groupID int64
	require.NoError(t, db.QueryRow(`INSERT INTO groups(name,platform,status,rate_multiplier,model_allowlist) VALUES('Lifecycle group','openai','active',2,'{"enabled":true,"models":["gpt-5.2"]}') RETURNING id`).Scan(&groupID))
	gin.SetMode(gin.TestMode)
	router := gin.New()
	h := &handler.Handlers{Admin: &handler.AdminHandlers{}, Vendor: handler.NewVendorHandler(vendors, nil, nil), VendorAccount: handler.NewVendorAccountHandler(vendors), VendorProxy: handler.NewVendorProxyHandler(vendors), VendorUsage: handler.NewVendorUsageHandler(vendors), VendorSettlement: handler.NewVendorSettlementHandler(vendors)}
	actorAuth := func(c *gin.Context) {
		a := actors[c.GetHeader("X-Test-Actor")]
		c.Request = c.Request.WithContext(authz.WithSubject(c.Request.Context(), a))
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: a.UserID})
		c.Set(string(middleware.ContextKeyUserRole), a.Role)
		c.Next()
	}
	pass := func(c *gin.Context) { c.Next() }
	routes.RegisterAdminRoutes(router.Group("/api/v1"), h, middleware.AdminAuthMiddleware(actorAuth), middleware.AuditLogMiddleware(pass), middleware.StepUpAuthMiddleware(pass), nil, nil)
	routes.RegisterVendorRoutes(router.Group("/api/v1"), h, middleware.JWTAuthMiddleware(actorAuth), nil)
	request := func(actor, method, path string, body any, want int) json.RawMessage {
		t.Helper()
		b, e := json.Marshal(body)
		require.NoError(t, e)
		r := httptest.NewRequest(method, "/api/v1"+path, bytes.NewReader(b))
		r.Header.Set("X-Test-Actor", actor)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		require.Equal(t, want, w.Code, "%s %s: %s", method, path, w.Body.String())
		require.NotContains(t, w.Body.String(), "fixture-access")
		require.NotContains(t, w.Body.String(), "fixture-refresh")
		var out struct{ Data json.RawMessage }
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
		return out.Data
	}
	createVendor := func(actor, name string, tier int, parent *int64) service.Vendor {
		in := map[string]any{"name": name, "email": name + "@fixture.invalid", "password": "fixture-password", "tier": tier, "platforms": []string{"openai"}, "group_ids": []int64{groupID}, "allow_direct": true, "default_coefficient": "1.8"}
		if parent != nil {
			in["parent_vendor_id"] = *parent
			in["default_coefficient"] = "0.9"
		}
		path := "/admin/vendors"
		if actor != "root" {
			path = "/vendor/vendors"
		}
		var v service.Vendor
		require.NoError(t, json.Unmarshal(request(actor, "POST", path, in, 200), &v))
		role := authz.VendorLevel1
		if tier == 2 {
			role = authz.VendorLevel2
		}
		actors[name] = authz.Subject{UserID: v.UserID, Role: role}
		return v
	}
	parent := createVendor("root", "parent", 1, nil)
	child := createVendor("parent", "child", 2, &parent.ID)
	var account service.VendorAccountDTO
	require.NoError(t, json.Unmarshal(request("child", "POST", "/vendor/accounts", map[string]any{"name": "Lifecycle supply", "platform": "openai", "method": "refresh_token", "token": "fixture-initial-refresh", "group_ids": []int64{groupID}, "direct": true}, 200), &account))
	require.Positive(t, account.ID)
	require.Equal(t, "verified@fixture.invalid", account.Email)
	require.Equal(t, child.ID, account.VendorID)
	var creatorID int64
	var creatorName, creatorRole string
	var createdConcurrency, createdPriority int
	require.NoError(t, db.QueryRow("SELECT created_by,created_by_name,created_by_role,concurrency,priority FROM accounts WHERE id=$1", account.ID).Scan(&creatorID, &creatorName, &creatorRole, &createdConcurrency, &createdPriority))
	require.Equal(t, actors["child"].UserID, creatorID)
	require.Equal(t, authz.VendorLevel2, creatorRole)
	require.NotEmpty(t, creatorName)
	require.Equal(t, service.DefaultAccountConcurrency, createdConcurrency)
	require.Equal(t, service.DefaultAccountPriority, createdPriority)

	require.True(t, account.Schedulable)
	require.NotNil(t, account.Quota.FiveHour, "enrollment fetches native provider quota")
	require.NotNil(t, account.Quota.SevenDay)
	require.Equal(t, 42.0, account.Quota.FiveHour.Utilization)
	require.Equal(t, 63.0, account.Quota.SevenDay.Utilization)
	path := fmt.Sprintf("/vendor/accounts/%d", account.ID)
	request("child", "PATCH", path, map[string]any{"name": "Lifecycle renamed", "vendor_notes": "created through the production handler"}, 200)
	request("parent", "PATCH", path+fmt.Sprintf("?vendor_id=%d", child.ID), map[string]any{"name": "forbidden"}, 403)
	request("child", "POST", path+"/refresh", map[string]any{}, 200)
	var quota service.VendorAccountDTO
	require.NoError(t, json.Unmarshal(request("child", "POST", path+"/usage-refresh", map[string]any{}, 200), &quota))
	require.NotNil(t, quota.Quota.FiveHour)
	require.NotNil(t, quota.Quota.SevenDay)
	require.Equal(t, 42.0, quota.Quota.FiveHour.Utilization)
	require.Equal(t, 63.0, quota.Quota.SevenDay.Utilization)
	require.InDelta(t, quotaReset, quota.Quota.FiveHour.ResetsAt.Unix(), 5)
	require.InDelta(t, quotaReset+86400, quota.Quota.SevenDay.ResetsAt.Unix(), 5)
	request("child", "POST", path+"/sync", map[string]any{}, 200)
	for _, actor := range []string{"child", "parent"} {
		p := path
		if actor == "parent" {
			p += fmt.Sprintf("?vendor_id=%d", child.ID)
		}
		var saved service.VendorAccountDTO
		require.NoError(t, json.Unmarshal(request(actor, "GET", p, nil, 200), &saved))
		require.NotNil(t, saved.Quota.FiveHour)
		require.Equal(t, 42.0, saved.Quota.FiveHour.Utilization)
		require.NotNil(t, saved.Quota.SevenDay)
		require.Equal(t, 63.0, saved.Quota.SevenDay.Utilization)
	}

	keyString := "sk-vendor-lifecycle-terminal-fixture-only"
	_, err = db.Exec("INSERT INTO api_keys(user_id,key,name,group_id,status) VALUES($1,$2,'terminal',$3,'active')", actors["consumer"].UserID, keyString, groupID)
	require.NoError(t, err)
	keyService := service.NewAPIKeyService(keys, users, groups, subscriptions, rates, nil, cfg)
	concurrency := service.NewConcurrencyService(repository.NewConcurrencyCache(rdb, 5, 30))
	billingCache := service.NewBillingCacheService(repository.NewBillingCache(rdb), users, subscriptions, keys, repository.NewUserRPMCache(rdb), rates, cfg, nil)
	t.Cleanup(billingCache.Stop)
	billing := service.NewBillingService(cfg, nil)
	gateway := service.NewOpenAIGatewayService(accounts, usage, repository.NewUsageBillingRepository(client, db), users, subscriptions, rates, repository.NewGatewayCache(rdb), cfg, nil, concurrency, billing, nil, billingCache, provider, service.NewDeferredService(accounts, nil, time.Minute), service.NewOpenAITokenProvider(accounts, nil, oauth), nil, nil, nil, nil, settings, nil)
	t.Cleanup(gateway.StopBPS403Recovery)
	t.Cleanup(gateway.CloseOpenAIWSPool)
	gh := handler.NewOpenAIGatewayHandler(gateway, concurrency, billingCache, keyService, nil, nil, nil, nil, cfg)
	router.POST("/v1/responses", gin.HandlerFunc(middleware.NewAPIKeyAuthMiddleware(keyService, nil, cfg)), gh.Responses)
	app := httptest.NewServer(router)
	t.Cleanup(app.Close)
	consume := func(want int) {
		t.Helper()
		r, e := http.NewRequest("POST", app.URL+"/v1/responses", strings.NewReader(`{"model":"gpt-5.2","input":"reply fixture","stream":false}`))
		require.NoError(t, e)
		r.Header.Set("Authorization", "Bearer "+keyString)
		r.Header.Set("Content-Type", "application/json")
		res, e := app.Client().Do(r)
		require.NoError(t, e)
		defer res.Body.Close()
		var result map[string]any
		require.NoError(t, json.NewDecoder(res.Body).Decode(&result))
		require.Equal(t, want, res.StatusCode, "%v", result)
	}
	consume(200)
	require.EqualValues(t, 1, provider.calls.Load())
	var total, charged, stats, balance string
	var consumerID, leafID, rootID, input, output, cached int64
	require.NoError(t, db.QueryRow("SELECT user_id,vendor_id_snapshot,vendor_root_id_snapshot,input_tokens,output_tokens,cache_read_tokens,total_cost::text,actual_cost::text,COALESCE(account_stats_cost,total_cost*account_rate_multiplier)::text FROM usage_logs WHERE account_id=$1", account.ID).Scan(&consumerID, &leafID, &rootID, &input, &output, &cached, &total, &charged, &stats))
	require.Equal(t, actors["consumer"].UserID, consumerID)
	require.Equal(t, child.ID, leafID)
	require.Equal(t, parent.ID, rootID)
	require.EqualValues(t, 800, input)
	require.EqualValues(t, 1000, output)
	require.EqualValues(t, 200, cached)
	baseCost := decimal.RequireFromString("0.015435") // 800*1.75e-6 + 200*0.175e-6 + 1000*14e-6.
	require.True(t, decimal.RequireFromString(total).Equal(baseCost), total)
	require.True(t, decimal.RequireFromString(charged).Equal(baseCost.Mul(decimal.NewFromInt(2))), charged)
	require.True(t, decimal.RequireFromString(stats).Equal(baseCost), stats)
	require.NoError(t, db.QueryRow("SELECT balance::text FROM users WHERE id=$1", consumerID).Scan(&balance))
	require.True(t, decimal.RequireFromString(balance).Equal(decimal.NewFromInt(10).Sub(decimal.RequireFromString(charged))), balance)
	for _, actor := range []string{"root", "parent", "child"} {
		p := "/vendor/usage/accounts?all_history=true"
		if actor == "root" {
			p = "/admin/vendors/usage?all_history=true"
		}
		var report service.VendorUsageReport
		require.NoError(t, json.Unmarshal(request(actor, "GET", p, nil, 200), &report))
		require.EqualValues(t, 1, report.Summary.Requests)
		require.True(t, decimal.RequireFromString(report.Summary.CostUSD).Equal(decimal.RequireFromString(stats)))
	}
	date := time.Now().UTC().Format(time.DateOnly)
	for _, edge := range []struct {
		actor, path string
		vendor      int64
		price       string
	}{{"root", "/admin/vendors/settlements/preview", parent.ID, "1.8"}, {"parent", "/vendor/payables/preview", child.ID, "0.9"}} {
		var previews []service.VendorSettlementPreview
		require.NoError(t, json.Unmarshal(request(edge.actor, "POST", edge.path, map[string]any{"vendor_ids": []int64{edge.vendor}, "start_date": date, "end_date": date, "timezone": "UTC", "exchange_rate": "6.7", "exchange_rate_source": "fixture", "coefficient_unit": "CNY/USD", "settlement_currency": "USDT"}, 200), &previews))
		require.Len(t, previews, 1)
		require.Empty(t, previews[0].Error)
		require.NotNil(t, previews[0].Statement)
		require.EqualValues(t, 1, previews[0].Statement.UsageCount)
		require.True(t, decimal.RequireFromString(previews[0].Statement.CostUSD).Equal(decimal.RequireFromString(stats)))
		want := decimal.RequireFromString(stats).Mul(decimal.RequireFromString(edge.price)).Div(decimal.RequireFromString("6.7")).Round(2)
		require.True(t, decimal.RequireFromString(previews[0].Statement.Total).Equal(want), previews[0].Statement.Total)
		base := strings.TrimSuffix(edge.path, "/preview")
		var drafts []service.VendorSettlementDraftResult
		require.NoError(t, json.Unmarshal(request(edge.actor, "POST", base, map[string]any{"drafts": []service.VendorSettlementDraftInput{{PreviewToken: previews[0].PreviewToken, IdempotencyKey: "terminal-" + edge.actor}}}, 200), &drafts))
		require.Len(t, drafts, 1)
		require.Empty(t, drafts[0].Error)
		require.NotNil(t, drafts[0].Statement)
		statementPath := fmt.Sprintf("%s/%d", base, drafts[0].Statement.ID)
		// Today's real request may be reserved, but an unfinished billing day
		// must not be confirmed. Closed-period payment/reversal is covered by
		// TestVendorSettlementPostgresMountedBilateralAPI.
		request(edge.actor, "POST", statementPath+"/confirm", map[string]any{"version": 1}, 409)
		request(edge.actor, "POST", statementPath+"/void", map[string]any{"version": 1, "reason": "release open-period acceptance reservation"}, 200)
		encoded, e := json.Marshal(previews[0])
		require.NoError(t, e)
		require.NotContains(t, string(encoded), keyString)
	}
	var credentials string
	require.NoError(t, db.QueryRow("SELECT credentials::text FROM accounts WHERE id=$1", account.ID).Scan(&credentials))
	for _, state := range []struct{ name, sql, blocker string }{{"disabled", "status='disabled'", "platform_disabled"}, {"error", "status='error'", "credential_error"}, {"unschedulable", "schedulable=false", "platform_disabled"}, {"expired", "expires_at=NOW()-INTERVAL '1 hour'", "expired"}, {"quota cooldown", "rate_limit_reset_at=NOW()+INTERVAL '1 hour'", "quota_cooldown"}, {"overload", "overload_until=NOW()+INTERVAL '1 hour'", "upstream_cooldown"}, {"temporary quarantine", "temp_unschedulable_until=NOW()+INTERVAL '1 hour'", "temporarily_unavailable"}, {"authorization missing", "credentials=credentials-'access_token'", "authorization_required"}} {
		t.Run(state.name, func(t *testing.T) {
			t.Cleanup(func() {
				_, e := db.Exec("UPDATE accounts SET status='active',schedulable=true,expires_at=NULL,rate_limit_reset_at=NULL,overload_until=NULL,temp_unschedulable_until=NULL,credentials=$2::jsonb WHERE id=$1", account.ID, credentials)
				require.NoError(t, e)
			})
			_, e := db.Exec("UPDATE accounts SET "+state.sql+" WHERE id=$1", account.ID)
			require.NoError(t, e)
			var dto service.VendorAccountDTO
			require.NoError(t, json.Unmarshal(request("child", "GET", path, nil, 200), &dto))
			require.Contains(t, dto.Blockers, state.blocker)
			require.False(t, dto.Schedulable)
			fresh, e := accounts.GetByID(t.Context(), account.ID)
			require.NoError(t, e)
			require.ErrorIs(t, accounts.(interface {
				ValidateVendorSupply(context.Context, *service.Account, *int64) error
			}).ValidateVendorSupply(t.Context(), fresh, &groupID), service.ErrVendorSupplyUnavailable)
		})
	}
	request("child", "POST", path+"/pause", map[string]any{}, 200)
	consume(503)
	require.EqualValues(t, 1, provider.calls.Load(), "paused supply must not reach the provider")
	request("child", "POST", path+"/resume", map[string]any{}, 200)
	consume(200)
	require.EqualValues(t, 2, provider.calls.Load(), "resumed supply must become usable again")
	request("child", "DELETE", path, nil, 200)
	request("child", "PATCH", path, map[string]any{"name": "restore"}, 409)
	request("child", "POST", path+"/resume", map[string]any{}, 409)
	var archived service.VendorAccountDTO
	require.NoError(t, json.Unmarshal(request("child", "GET", path, nil, 200), &archived))
	require.True(t, archived.Archived)
	consume(503)
	require.EqualValues(t, 2, provider.calls.Load())
	var count int
	require.NoError(t, db.QueryRow("SELECT count(*) FROM usage_logs WHERE account_id=$1", account.ID).Scan(&count))
	require.Equal(t, 2, count)
	var history service.VendorAccountUsage
	require.NoError(t, json.Unmarshal(request("child", "GET", path+"/usage?all_history=true", nil, 200), &history))
	require.True(t, history.AccountArchived)
	require.EqualValues(t, 2, history.Summary.Requests)
	require.True(t, decimal.RequireFromString(history.Summary.CostUSD).Equal(baseCost.Mul(decimal.NewFromInt(2))))
	require.NoError(t, db.QueryRow("SELECT balance::text FROM users WHERE id=$1", consumerID).Scan(&balance))
	require.True(t, decimal.RequireFromString(balance).Equal(decimal.NewFromInt(10).Sub(baseCost.Mul(decimal.NewFromInt(4)))), "only the two accepted requests are charged: %s", balance)
}
