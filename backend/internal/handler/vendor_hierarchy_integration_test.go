package handler_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"fmt"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	_ "github.com/Wei-Shaw/sub2api/ent/runtime"
	"github.com/Wei-Shaw/sub2api/internal/authz"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	adminhandler "github.com/Wei-Shaw/sub2api/internal/handler/admin"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/server/routes"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	_ "github.com/lib/pq"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

// Real migrations and production route registration in an explicitly opt-in,
// disposable local database. Only the authentication identity is supplied here.
func TestVendorHierarchyMountedRoutes(t *testing.T) {
	dsn := os.Getenv("VENDOR_HIERARCHY_TEST_DSN")
	if dsn == "" {
		t.Skip("VENDOR_HIERARCHY_TEST_DSN not set")
	}
	u, err := url.Parse(dsn)
	require.NoError(t, err)
	require.Contains(t, []string{"127.0.0.1", "localhost"}, u.Hostname())
	require.Equal(t, "/vendor_settlement_test", u.Path)
	control, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = control.Close() })
	name := fmt.Sprintf("vendor_routes_%d", time.Now().UnixNano())
	_, err = control.Exec("CREATE DATABASE " + name)
	require.NoError(t, err)
	u.Path = "/" + name
	db, err := sql.Open("postgres", u.String())
	require.NoError(t, err)
	t.Cleanup(func() {
		db.Close()
		_, e := control.Exec("DROP DATABASE " + name + " WITH (FORCE)")
		require.NoError(t, e)
	})
	ctx := context.Background()
	require.NoError(t, repository.ApplyMigrations(ctx, db))
	require.NoError(t, repository.ApplyMigrations(ctx, db))
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	settings := service.NewSettingService(repository.NewSettingRepository(client), nil)
	_, err = db.Exec("INSERT INTO settings(key,value) VALUES('vendor_console_enabled','true'),('vendor_portal_origin','https://supplier.example.invalid') ON CONFLICT(key) DO UPDATE SET value=EXCLUDED.value")
	require.NoError(t, err)
	accounts := repository.NewAccountRepository(client, db, nil)
	proxies := repository.NewProxyRepository(client, db)
	users := repository.NewUserRepository(client, db)
	native := service.NewAdminService(&config.Config{}, users, repository.NewGroupRepository(client, db).(service.AdminGroupRepository), accounts.(service.AdminAccountRepository), proxies, nil, nil, nil, nil, nil, nil, nil, nil, client, settings, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	cache := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: cache.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	svc := service.NewVendorService(db, client, accounts, proxies, nil, users, native, settings, nil, nil, rdb, nil, nil, &config.Config{Timezone: "UTC"})
	actors := map[string]authz.Subject{}
	for name, role := range map[string]string{"super": authz.SuperAdmin, "admin": authz.Admin, "customer": authz.User} {
		var id, generation int64
		require.NoError(t, db.QueryRow("INSERT INTO users(email,password_hash,role,status) VALUES($1,'test-only',$2,'active') RETURNING id,session_generation", name+"@example.invalid", role).Scan(&id, &generation))
		actors[name] = authz.Subject{UserID: id, Role: role, SessionGeneration: generation}
	}
	var groupID int64
	require.NoError(t, db.QueryRow("INSERT INTO groups(name,platform,status) VALUES('Supply group','openai','active') RETURNING id").Scan(&groupID))
	h := &handler.Handlers{Admin: &handler.AdminHandlers{Account: adminhandler.NewAccountHandler(native, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)}, Vendor: handler.NewVendorHandler(svc, nil, nil), VendorAccount: handler.NewVendorAccountHandler(svc), VendorProxy: handler.NewVendorProxyHandler(svc), VendorUsage: handler.NewVendorUsageHandler(svc), VendorSettlement: &handler.VendorSettlementHandler{}}
	authenticate := func(c *gin.Context) {
		a, ok := actors[c.GetHeader("X-Test-Actor")]
		if !ok {
			c.AbortWithStatus(401)
			return
		}
		c.Request = c.Request.WithContext(authz.WithSubject(c.Request.Context(), a))
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: a.UserID})
		c.Set(string(middleware.ContextKeyUserRole), a.Role)
		c.Next()
	}
	pass := func(c *gin.Context) { c.Next() }
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.VendorOrigin(settings))
	routes.RegisterAdminRoutes(r.Group("/api/v1"), h, middleware.AdminAuthMiddleware(authenticate), middleware.AuditLogMiddleware(pass), middleware.StepUpAuthMiddleware(pass), nil, nil)
	routes.RegisterVendorRoutes(r.Group("/api/v1"), h, middleware.JWTAuthMiddleware(authenticate), nil)
	paths := map[string]bool{}
	for _, route := range r.Routes() {
		paths[route.Method+" "+route.Path] = true
	}
	for _, path := range []string{"POST /api/v1/admin/vendors/settlements", "POST /api/v1/admin/vendors/settlements/:id/payments/:payment_id/reverse", "GET /api/v1/vendor/payables", "POST /api/v1/vendor/payables/:id/payments/:payment_id/reverse", "GET /api/v1/vendor/settlements"} {
		require.True(t, paths[path], path)
	}
	require.False(t, paths["POST /api/v1/admin/vendors/payables"])
	require.False(t, paths["POST /api/v1/vendor/settlements"])
	request := func(actor, method, path, body string, status int) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, "https://supplier.example.invalid/api/v1"+path, strings.NewReader(body))
		req.Header.Set("X-Test-Actor", actor)
		req.Header.Set("Content-Type", "application/json")
		out := httptest.NewRecorder()
		r.ServeHTTP(out, req)
		require.Equal(t, status, out.Code, method+" "+path+": "+out.Body.String())
		if actor == "super" && strings.Contains(path, "view=management") {
			require.NotContains(t, out.Body.String(), "SECRET_access")
			require.NotContains(t, out.Body.String(), "SECRET_refresh")
		} else {
			require.NotContains(t, out.Body.String(), "SECRET")
		}
		return out
	}
	decode := func(out *httptest.ResponseRecorder) service.Vendor {
		var envelope struct {
			Data service.Vendor `json:"data"`
		}
		require.NoError(t, json.Unmarshal(out.Body.Bytes(), &envelope))
		return envelope.Data
	}
	create := func(actor, email string, tier int, parent *int64) service.Vendor {
		t.Helper()
		input := map[string]any{"email": email, "password": "SECRET_password123", "name": "  " + email + "  ", "tier": tier, "platforms": []string{"openai"}, "group_ids": []int64{groupID}, "allow_direct": true}
		if parent != nil {
			input["parent_vendor_id"] = *parent
		}
		data, _ := json.Marshal(input)
		path := "/admin/vendors"
		if actor != "super" {
			path = "/vendor/vendors"
		}
		v := decode(request(actor, "POST", path, string(data), 200))
		role := authz.VendorLevel1
		if tier == 2 {
			role = authz.VendorLevel2
		}
		var gen int64
		require.NoError(t, db.QueryRow("SELECT session_generation FROM users WHERE id=$1", v.UserID).Scan(&gen))
		actors[email] = authz.Subject{UserID: v.UserID, Role: role, SessionGeneration: gen}
		return v
	}
	parent := create("super", "parent@example.invalid", 1, nil)
	require.Zero(t, parent.AccountLimit)
	require.Zero(t, parent.ProxyLimit)
	require.Equal(t, service.DefaultAccountConcurrency, parent.CreationDefaults.Concurrency)
	require.Equal(t, service.DefaultAccountPriority, parent.CreationDefaults.Priority)
	require.Equal(t, parent.Name, parent.Username)
	require.Equal(t, "parent@example.invalid", parent.Username)
	child := create(parent.Email, "child@example.invalid", 2, nil)
	require.Equal(t, parent.ID, *child.ParentVendorID)
	require.Equal(t, child.Name, child.Username)
	other := create("super", "other@example.invalid", 1, nil)
	listIDs := func(out *httptest.ResponseRecorder) []int64 {
		var envelope struct {
			Data struct {
				Items []service.Vendor `json:"items"`
				Total int64            `json:"total"`
			} `json:"data"`
		}
		require.NoError(t, json.Unmarshal(out.Body.Bytes(), &envelope))
		ids := []int64{}
		for _, v := range envelope.Data.Items {
			ids = append(ids, v.ID)
		}
		return ids
	}
	require.ElementsMatch(t, []int64{parent.ID, other.ID}, listIDs(request("super", "GET", "/admin/vendors?tier=1&status=active", "", 200)))
	require.Equal(t, []int64{child.ID}, listIDs(request("super", "GET", fmt.Sprintf("/admin/vendors?tier=2&parent_vendor_id=%d", parent.ID), "", 200)))
	require.Equal(t, []int64{child.ID}, listIDs(request(parent.Email, "GET", "/vendor/vendors?tier=2", "", 200)))
	request(parent.Email, "GET", fmt.Sprintf("/vendor/vendors?parent_vendor_id=%d", other.ID), "", 403)
	request("super", "GET", "/admin/vendors?tier=3", "", 400)
	request("super", "GET", "/admin/vendors?parent_vendor_id=0", "", 400)

	request("admin", "GET", "/admin/vendors", "", 403)
	request("super", "GET", "/admin/users", "", 403)
	request(child.Email, "GET", "/vendor/vendors", "", 403)
	request(parent.Email, "GET", fmt.Sprintf("/vendor/vendors/%d", child.ID), "", 200)
	request(parent.Email, "GET", fmt.Sprintf("/vendor/vendors/%d", other.ID), "", 403)
	request(parent.Email, "PATCH", fmt.Sprintf("/vendor/vendors/%d", child.ID), `{"name":"Renamed"}`, 200)
	edited, err := svc.ManagedVendor(authz.WithSubject(ctx, actors["super"]), child.ID)
	require.NoError(t, err)
	require.Zero(t, edited.AccountLimit)
	require.Equal(t, child.ContractVersion, edited.ContractVersion)
	require.Greater(t, edited.PolicyVersion, child.PolicyVersion)
	request(parent.Email, "PATCH", fmt.Sprintf("/vendor/vendors/%d", child.ID), fmt.Sprintf(`{"policy_version":%d,"name":"Stale"}`, child.PolicyVersion), 409)
	for _, body := range []string{`{"password":"SECRET"}`, `{"email":"move@example.invalid"}`, `{"username":"move"}`, `{"contract_version":88}`, `{"user_id":9}`} {
		request(parent.Email, "PATCH", fmt.Sprintf("/vendor/vendors/%d", child.ID), body, 400)
	}
	request(parent.Email, "PATCH", fmt.Sprintf("/vendor/vendors/%d", child.ID), fmt.Sprintf(`{"parent_vendor_id":%d}`, other.ID), 403)
	request("super", "POST", "/admin/vendors", `{"username":"separate-login-name"}`, 400)
	body := fmt.Sprintf(`{"email":"parent@example.invalid","password":"SECRET_password123","name":"duplicate","tier":1,"platforms":["openai"],"group_ids":[%d]}`, groupID)
	request("super", "POST", "/admin/vendors", body, 409)
	var accountID int64
	require.NoError(t, db.QueryRow(`INSERT INTO accounts(name,platform,type,credentials,extra,vendor_id,status) VALUES('Child account','openai','oauth','{"access_token":"SECRET_access","refresh_token":"SECRET_refresh"}','{"secret":"SECRET_extra"}',$1,'disabled') RETURNING id`, child.ID).Scan(&accountID))
	chosen := fmt.Sprintf("?vendor_id=%d", child.ID)
	// Use actual account DTO and repository paths: no credentials in parent reads.
	request(parent.Email, "GET", fmt.Sprintf("/vendor/accounts/%d", accountID)+chosen, "", 200)
	request(parent.Email, "GET", "/vendor/accounts"+chosen, "", 200)
	request(other.Email, "GET", "/vendor/accounts"+chosen, "", 403)
	request(child.Email, "GET", "/vendor/accounts?net=1&vendor_id="+fmt.Sprint(parent.ID), "", 403)
	for _, path := range []string{"/vendor/accounts/export", fmt.Sprintf("/vendor/accounts/%d/probe", accountID), "/vendor/proxies/export", "/vendor/proxies/1/test"} {
		request(parent.Email, "POST", path+chosen, "{}", 403)
	}
	request(parent.Email, "PATCH", fmt.Sprintf("/vendor/accounts/%d", accountID)+chosen, "{}", 403)
	request(parent.Email, "GET", "/vendor/vendors"+chosen, "", 403)
	request(parent.Email, "GET", "/vendor/accounts?vendor_id=0", "", 403)
	request(parent.Email, "GET", "/vendor/accounts?vendor_id=abc", "", 403)

	t.Run("aggregate inventory preserves ownership and read boundaries", func(t *testing.T) {
		sibling := create(parent.Email, "sibling@example.invalid", 2, nil)
		outsider := create(other.Email, "outsider@example.invalid", 2, nil)
		var apiKey int64
		require.NoError(t, db.QueryRow("INSERT INTO api_keys(user_id,key,name,status) VALUES($1,'inventory-test','inventory','active') RETURNING id", actors["customer"].UserID).Scan(&apiKey))
		for _, owner := range []service.Vendor{parent, sibling, outsider} {
			var id int64
			require.NoError(t, db.QueryRow("INSERT INTO accounts(name,platform,type,vendor_id,status) VALUES($1,'openai','oauth',$2,'disabled') RETURNING id", owner.Name, owner.ID).Scan(&id))
			_, e := db.Exec("INSERT INTO usage_logs(user_id,api_key_id,account_id,request_id,model,vendor_id_snapshot,input_tokens,total_cost,created_at) VALUES($1,$2,$3,$4,'test-model',$5,10,2,NOW()-INTERVAL '1 second')", actors["customer"].UserID, apiKey, id, fmt.Sprint(id), owner.ID)
			require.NoError(t, e)
		}
		for _, owner := range []service.Vendor{parent, child, sibling, outsider} {
			_, e := db.Exec("INSERT INTO proxies(name,protocol,host,port,username,password,vendor_id) VALUES($1,'http','proxy.example.invalid',8080,'SECRET_user','SECRET_password',$2)", owner.Name, owner.ID)
			require.NoError(t, e)
		}
		// More than one page of child proxies, plus unrelated owners to exclude.
		_, e := db.Exec("INSERT INTO proxies(name,protocol,host,port,vendor_id) SELECT 'Child extra '||n,'http','extra.example.invalid',8080,$1 FROM generate_series(1,10) n", sibling.ID)
		require.NoError(t, e)
		accounts := func(actor, path string) service.VendorAccountList {
			var result struct {
				Data service.VendorAccountList `json:"data"`
			}
			require.NoError(t, json.Unmarshal(request(actor, "GET", path, "", 200).Body.Bytes(), &result))
			return result.Data
		}
		proxies := func(actor, path string) service.VendorProxyList {
			var result struct {
				Data service.VendorProxyList `json:"data"`
			}
			require.NoError(t, json.Unmarshal(request(actor, "GET", path, "", 200).Body.Bytes(), &result))
			return result.Data
		}
		a := accounts(parent.Email, "/vendor/accounts?scope=children&page_size=1&sort_by=id&sort_order=asc")
		require.EqualValues(t, 2, a.Total)
		require.Len(t, a.Items, 1)
		require.Equal(t, child.ID, a.Items[0].VendorID)
		require.Equal(t, "Renamed", a.Items[0].VendorName)
		require.EqualValues(t, 2, a.Overview.Total)
		require.EqualValues(t, 1, a.Overview.AllTimeUsage.Requests)
		require.Equal(t, "2", a.Overview.AllTimeUsage.CostUSD)
		next := accounts(parent.Email, "/vendor/accounts?scope=children&page_size=1&page=2&sort_by=id&sort_order=asc")
		require.Equal(t, sibling.ID, next.Items[0].VendorID)
		require.EqualValues(t, 1, next.Items[0].AllTimeUsage.Requests)
		require.Equal(t, a.Overview.AllTimeUsage.CostUSD, next.Overview.AllTimeUsage.CostUSD)
		require.EqualValues(t, 1, accounts(parent.Email, "/vendor/accounts?scope=children&search=sibling").Total)
		require.EqualValues(t, 1, accounts(parent.Email, "/vendor/accounts"+chosen).Total)
		require.EqualValues(t, 4, accounts("super", "/admin/accounts?vendor_id=0").Total)
		p := proxies(parent.Email, "/vendor/proxies?scope=children&page_size=10&sort_by=created_at&sort_order=asc")
		require.Equal(t, 12, p.Total)
		require.Equal(t, 12, p.Summary.Total)
		require.Len(t, p.Items, 10)
		for _, item := range p.Items {
			require.Contains(t, []int64{child.ID, sibling.ID}, *item.VendorID)
			require.NotEmpty(t, item.VendorName)
		}
		require.Len(t, proxies(parent.Email, "/vendor/proxies?scope=children&page_size=10&page=2").Items, 2)
		require.Equal(t, 1, proxies(parent.Email, "/vendor/proxies"+chosen).Total)
		root := proxies("super", "/admin/proxies?vendor_id=0")
		require.Equal(t, 14, root.Total)
		for _, item := range root.Items {
			require.NotEmpty(t, item.VendorName)
		}
		request(parent.Email, "GET", fmt.Sprintf("/vendor/proxies/%d?vendor_id=%d", p.Items[0].ID, *p.Items[0].VendorID), "", 200)
		request(parent.Email, "GET", fmt.Sprintf("/vendor/accounts/%d/usage?vendor_id=%d&all_history=true", next.Items[0].ID, sibling.ID), "", 200)
		for _, path := range []string{"/vendor/accounts?scope=children", "/vendor/proxies?scope=children"} {
			request(child.Email, "GET", path, "", 403)
			request("customer", "GET", path, "", 403)
			request(parent.Email, "GET", path+"&vendor_id="+fmt.Sprint(outsider.ID), "", 403)
			request(parent.Email, "POST", path, "{}", 403)
		}
		for _, path := range []string{"/vendor/accounts/1?scope=children", "/vendor/proxies/1?scope=children", "/vendor/usage/summary?scope=children", "/vendor/me?scope=children", "/vendor/accounts?scope=all"} {
			request(parent.Email, "GET", path, "", 403)
		}
		request(parent.Email, "DELETE", fmt.Sprintf("/vendor/accounts/%d?scope=children", accountID), "", 403)
		request(parent.Email, "GET", fmt.Sprintf("/vendor/proxies?vendor_id=%d", outsider.ID), "", 403)
		// No children is an empty inventory, never a fallback to own or all rows.
		empty := create("super", "empty@example.invalid", 1, nil)
		require.Zero(t, accounts(empty.Email, "/vendor/accounts?scope=children").Total)
		require.Zero(t, proxies(empty.Email, "/vendor/proxies?scope=children").Total)
	})

	t.Run("native creator is immutable creation-time attribution", func(t *testing.T) {
		created := &service.Account{Name: "creator snapshot", Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey,
			Credentials: map[string]any{"api_key": "fixture-only"}, Status: service.StatusActive, Schedulable: true, Concurrency: 10, Priority: 1, AutoPauseOnExpired: true}
		require.NoError(t, accounts.Create(authz.WithSubject(ctx, actors["super"]), created))
		persisted, e := accounts.GetByID(authz.WithSubject(ctx, actors["super"]), created.ID)
		require.NoError(t, e)
		require.Equal(t, actors["super"].UserID, *persisted.CreatedBy)
		require.Equal(t, "super@example.invalid", persisted.CreatedByName)
		require.Equal(t, authz.SuperAdmin, persisted.CreatedByRole)
		_, e = db.Exec("UPDATE users SET username='renamed operator' WHERE id=$1", actors["super"].UserID)
		require.NoError(t, e)
		_, e = db.Exec("UPDATE accounts SET created_by_role='user' WHERE id=$1", created.ID)
		require.Error(t, e)
		persisted, e = accounts.GetByID(authz.WithSubject(ctx, actors["super"]), created.ID)
		require.NoError(t, e)
		require.Equal(t, "super@example.invalid", persisted.CreatedByName)
	})

	// Same mounted root route used by the supplier portal, through native account
	// updates. Scheduling edits must not resurrect disabled supply or change price.
	var oldRate string
	var oldCredentials string
	require.NoError(t, db.QueryRow("SELECT rate_multiplier::text,credentials::text FROM accounts WHERE id=$1", accountID).Scan(&oldRate, &oldCredentials))
	rootEdit := fmt.Sprintf("/admin/accounts/%d", accountID) + chosen + "&view=management"
	request("super", "PUT", rootEdit, `{"concurrency":23,"priority":0,"notes":"native scheduling edit"}`, 200)
	var concurrency, priority int
	var state, rate, credentials, notes string
	require.NoError(t, db.QueryRow("SELECT concurrency,priority,status,rate_multiplier::text,credentials::text,COALESCE(notes,'') FROM accounts WHERE id=$1", accountID).Scan(&concurrency, &priority, &state, &rate, &credentials, &notes))
	require.Equal(t, 23, concurrency)
	require.Zero(t, priority)
	require.Equal(t, "disabled", state)
	require.Equal(t, oldRate, rate)
	require.Equal(t, oldCredentials, credentials)
	require.Equal(t, "native scheduling edit", notes)
	var outbox int
	require.NoError(t, db.QueryRow("SELECT count(*) FROM scheduler_outbox WHERE account_id=$1 AND event_type='account_changed'", accountID).Scan(&outbox))
	require.Positive(t, outbox)
	request(child.Email, "PATCH", fmt.Sprintf("/vendor/accounts/%d", accountID), `{"concurrency":1}`, 422)
	request(child.Email, "PATCH", fmt.Sprintf("/vendor/accounts/%d", accountID), `{"priority":1}`, 422)
	request(parent.Email, "PATCH", fmt.Sprintf("/vendor/accounts/%d", accountID)+chosen, `{"concurrency":1,"priority":1}`, 403)
	request("super", "PUT", rootEdit, `{"rate_multiplier":3}`, 200)

	request("super", "PATCH", fmt.Sprintf("/admin/vendors/%d", child.ID), `{"default_concurrency":37,"default_priority":4}`, 400)
	require.NoError(t, db.QueryRow("SELECT concurrency,priority FROM accounts WHERE id=$1", accountID).Scan(&concurrency, &priority))
	require.Equal(t, 23, concurrency, "changing creation defaults never backfills existing accounts")
	require.Zero(t, priority)

	request("super", "PUT", rootEdit, `{"credentials":{"model_mapping":{"gpt-5.2":"gpt-5.2"}},"extra":{"session_id_masking_enabled":true}}`, 200)
	var mapping string
	require.NoError(t, db.QueryRow("SELECT credentials->'model_mapping'->>'gpt-5.2' FROM accounts WHERE id=$1", accountID).Scan(&mapping))
	require.Equal(t, "gpt-5.2", mapping)
	request("super", "PUT", rootEdit, `{"credentials":{"access_token":"replacement"}}`, 403)
	bulkPath := "/admin/accounts/bulk-update?vendor_id=0&view=management"
	request("super", "POST", bulkPath, fmt.Sprintf(`{"account_ids":[%d],"priority":7,"notes":"batch metadata","expires_at":0,"auto_pause_on_expired":false}`, accountID), 200)
	require.NoError(t, db.QueryRow("SELECT priority,COALESCE(notes,'') FROM accounts WHERE id=$1", accountID).Scan(&priority, &notes))
	require.Equal(t, 7, priority)
	require.Equal(t, "batch metadata", notes)
	request("admin", "POST", bulkPath, fmt.Sprintf(`{"account_ids":[%d],"priority":99}`, accountID), 403)
	request(child.Email, "POST", bulkPath, fmt.Sprintf(`{"account_ids":[%d],"priority":99}`, accountID), 403)
	request("super", "POST", fmt.Sprintf("/admin/accounts/bulk-update?vendor_id=%d&view=management", parent.ID), fmt.Sprintf(`{"account_ids":[%d],"priority":99}`, accountID), 404)

	// Native failure must roll back ordinary fields in the same request as well.
	_, err = db.Exec("CREATE FUNCTION reject_test_scheduling() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.priority=333 THEN RAISE EXCEPTION 'test native write rejected'; END IF; RETURN NEW; END; $$; CREATE TRIGGER reject_test_scheduling BEFORE UPDATE OF priority ON accounts FOR EACH ROW EXECUTE FUNCTION reject_test_scheduling()")
	require.NoError(t, err)
	request("super", "PUT", rootEdit, `{"priority":333,"notes":"must roll back"}`, 500)
	require.NoError(t, db.QueryRow("SELECT priority,COALESCE(notes,'') FROM accounts WHERE id=$1", accountID).Scan(&priority, &notes))
	require.Equal(t, 7, priority)
	require.Equal(t, "batch metadata", notes)
	_, err = db.Exec("UPDATE accounts SET extra=extra||jsonb_build_object('vendor_archived_at',NOW()) WHERE id=$1", accountID)
	require.NoError(t, err)
	request("super", "PUT", rootEdit, `{"concurrency":99,"priority":9}`, 409)
	require.NoError(t, db.QueryRow("SELECT concurrency,priority FROM accounts WHERE id=$1", accountID).Scan(&concurrency, &priority))
	require.Equal(t, 23, concurrency)
	require.Equal(t, 7, priority)
	request(parent.Email, "PATCH", fmt.Sprintf("/vendor/vendors/%d", child.ID), `{"status":"disabled"}`, 200)
	request(parent.Email, "GET", fmt.Sprintf("/vendor/accounts/%d", accountID)+chosen, "", 200)
	request(child.Email, "GET", "/vendor/me", "", 403)
	request("super", "PATCH", fmt.Sprintf("/admin/vendors/%d", parent.ID), `{"status":"disabled"}`, 200)
	request(parent.Email, "GET", "/vendor/accounts"+chosen, "", 403)
	request(parent.Email, "GET", "/vendor/accounts?scope=children", "", 403)
	request(parent.Email, "GET", "/vendor/proxies?scope=children", "", 403)
	// User identity validation shares exactly the live ancestor predicate.
	checker := users.(interface {
		ValidateVendorIdentity(context.Context, int64, string) error
	})
	require.ErrorIs(t, checker.ValidateVendorIdentity(ctx, child.UserID, authz.VendorLevel2), service.ErrVendorDenied)
}
