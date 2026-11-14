//go:build integration

package handler_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
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
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

// Mount the production registration functions and use the real service/repo
// against an isolated migrated PostgreSQL database. Only authentication is
// supplied by the fixture; no ManagementRequest flag masks repository gaps.
// All supplier fixtures are inactive, so even mounted probes cannot dial them.
func TestVendorProxyMountedRouter(t *testing.T) {
	ctx := context.Background()
	pg, err := tcpostgres.Run(ctx, "postgres:18.1-alpine3.23", tcpostgres.WithDatabase("vendor_proxy_routes"), tcpostgres.WithUsername("test"), tcpostgres.WithPassword("local-test-only"), tcpostgres.BasicWaitStrategies())
	require.NoError(t, err)
	t.Cleanup(func() { _ = pg.Terminate(ctx) })
	dsn, err := pg.ConnectionString(ctx, "sslmode=disable", "TimeZone=UTC")
	require.NoError(t, err)
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, repository.ApplyMigrations(ctx, db))
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	rdb := redis.NewClient(&redis.Options{Addr: miniredis.RunT(t).Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	settings := service.NewSettingService(repository.NewSettingRepository(client), nil)
	_, err = db.ExecContext(ctx, "INSERT INTO settings(key,value) VALUES('vendor_console_enabled','true') ON CONFLICT(key) DO UPDATE SET value='true'")
	require.NoError(t, err)
	repo := repository.NewProxyRepository(client, db)
	cfg := &config.Config{Timezone: "UTC"}
	native := service.NewAdminService(cfg, nil, nil, nil, repo, nil, nil, nil, nil, nil, nil, nil, nil, client, settings, nil, nil, nil, nil, nil, nil, nil, nil)
	svc := service.NewVendorService(db, client, nil, repo, nil, nil, native, settings, nil, nil, rdb, nil, nil, cfg)
	actors := map[string]authz.Subject{}
	for name, role := range map[string]string{"super": authz.SuperAdmin, "admin": authz.Admin, "supplier-a": authz.VendorLevel1, "supplier-b": authz.VendorLevel1} {
		var id, generation int64
		err = db.QueryRowContext(ctx, "INSERT INTO users(email,password_hash,role,status) VALUES($1,'test-only',$2,'active') RETURNING id,session_generation", name+"@invalid", role).Scan(&id, &generation)
		require.NoError(t, err)
		actors[name] = authz.Subject{UserID: id, Role: role, SessionGeneration: generation}
	}
	vendors := map[string]int64{}
	for _, name := range []string{"supplier-a", "supplier-b"} {
		var id int64
		err = db.QueryRowContext(ctx, "INSERT INTO vendors(user_id,name,proxy_limit) VALUES($1,$2,30) RETURNING id", actors[name].UserID, name).Scan(&id)
		require.NoError(t, err)
		vendors[name] = id
	}
	proxyIDs := map[string][]int64{}
	for name, vendorID := range vendors {
		n := 1
		if name == "supplier-a" {
			n = 12
		}
		for i := 0; i < n; i++ {
			var id int64
			err = db.QueryRowContext(ctx, "INSERT INTO proxies(name,protocol,host,port,status,username,password,vendor_id,region_label_country_code) VALUES($1,'http','fixture.invalid',$2,'inactive','SECRET_USER','SECRET_PASSWORD',$3,'US') RETURNING id", fmt.Sprintf("%s-%02d", name, i), 8000+i, vendorID).Scan(&id)
			require.NoError(t, err)
			proxyIDs[name] = append(proxyIDs[name], id)
		}
	}
	var platformID int64
	err = db.QueryRowContext(ctx, "INSERT INTO proxies(name,protocol,host,port,status) VALUES('platform-only','http','127.0.0.1',8080,'active') RETURNING id").Scan(&platformID)
	require.NoError(t, err)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	h := &handler.Handlers{Admin: &handler.AdminHandlers{Proxy: adminhandler.NewProxyHandler(native)}, Vendor: handler.NewVendorHandler(svc, nil, nil), VendorProxy: handler.NewVendorProxyHandler(svc)}
	authenticate := func(c *gin.Context) {
		actor, ok := actors[c.GetHeader("X-Test-Actor")]
		if !ok {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		c.Request = c.Request.WithContext(authz.WithSubject(c.Request.Context(), actor))
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: actor.UserID})
		c.Set(string(middleware.ContextKeyUserRole), string(actor.Role))
		c.Next()
	}
	pass := func(c *gin.Context) { c.Next() }
	routes.RegisterAdminRoutes(router.Group("/api/v1"), h, middleware.AdminAuthMiddleware(authenticate), middleware.AuditLogMiddleware(pass), middleware.StepUpAuthMiddleware(pass), nil, nil)
	routes.RegisterVendorRoutes(router.Group("/api/v1"), h, middleware.JWTAuthMiddleware(authenticate), nil)
	request := func(t *testing.T, actor, method, path, body string, status int) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		r := httptest.NewRequest(method, "/api/v1"+path, strings.NewReader(body))
		r.Header.Set("X-Test-Actor", actor)
		r.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, r)
		require.Equal(t, status, w.Code, "%s %s: %s", method, path, w.Body.String())
		require.NotContains(t, w.Body.String(), "SECRET", path)
		return w
	}
	list := func(t *testing.T, w *httptest.ResponseRecorder) service.VendorProxyList {
		t.Helper()
		var envelope struct {
			Data service.VendorProxyList `json:"data"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &envelope))
		return envelope.Data
	}
	aID, bID := proxyIDs["supplier-a"][0], proxyIDs["supplier-b"][0]
	selected := fmt.Sprintf("?vendor_id=%d", vendors["supplier-a"])
	t.Run("aggregate and selected inventory scopes", func(t *testing.T) {
		all := list(t, request(t, "super", "GET", "/admin/proxies?vendor_id=0&page_size=10", "", 200))
		require.Equal(t, 13, all.Total)
		require.Equal(t, 13, all.Summary.Total)
		require.Len(t, all.Items, 10)
		for _, p := range all.Items {
			require.NotNil(t, p.VendorID)
			require.NotEqual(t, platformID, p.ID)
		}
		filtered := list(t, request(t, "super", "GET", "/admin/proxies?vendor_id=0&search=supplier-b", "", 200))
		require.Equal(t, 1, filtered.Total)
		require.Equal(t, 13, filtered.Summary.Total)
		one := list(t, request(t, "super", "GET", "/admin/proxies"+selected+"&page_size=20", "", 200))
		require.Equal(t, 12, one.Total)
		for _, p := range one.Items {
			require.Equal(t, vendors["supplier-a"], *p.VendorID)
		}
	})
	t.Run("resource owner dispatch and mismatch", func(t *testing.T) {
		request(t, "super", "GET", fmt.Sprintf("/admin/proxies/%d?vendor_id=0", aID), "", 200)
		request(t, "super", "GET", fmt.Sprintf("/admin/proxies/%d%s", aID, selected), "", 200)
		request(t, "super", "GET", fmt.Sprintf("/admin/proxies/%d%s", bID, selected), "", 404)
		request(t, "super", "PUT", fmt.Sprintf("/admin/proxies/%d%s", bID, selected), `{"name":"forbidden","config_version":1}`, 404)
		request(t, "super", "DELETE", fmt.Sprintf("/admin/proxies/%d%s", bID, selected), "", 404)
		request(t, "super", "GET", fmt.Sprintf("/admin/proxies/%d?vendor_id=0", platformID), "", 404)
		for _, suffix := range []string{"test", "quality-check"} {
			w := request(t, "super", "POST", fmt.Sprintf("/admin/proxies/%d/%s?vendor_id=0", aID, suffix), `{"config_version":1}`, 200)
			require.Contains(t, w.Body.String(), `"skipped"`)
			request(t, "super", "POST", fmt.Sprintf("/admin/proxies/%d/%s%s", bID, suffix, selected), `{"config_version":1}`, 404)
		}
	})
	t.Run("supplier routes retain owner scope", func(t *testing.T) {
		mine := list(t, request(t, "supplier-a", "GET", "/vendor/proxies?page_size=20", "", 200))
		require.Equal(t, 12, mine.Total)
		request(t, "supplier-a", "GET", fmt.Sprintf("/vendor/proxies/%d", aID), "", 200)
		request(t, "supplier-a", "GET", fmt.Sprintf("/vendor/proxies/%d", bID), "", 404)
		request(t, "supplier-a", "PATCH", fmt.Sprintf("/vendor/proxies/%d", bID), `{"name":"forbidden","config_version":1}`, 404)
		request(t, "supplier-a", "GET", fmt.Sprintf("/vendor/proxies?vendor_id=%d", vendors["supplier-b"]), "", 403)
		request(t, "supplier-a", "POST", "/vendor/proxies/export", fmt.Sprintf(`{"format":"json","ids":[%d]}`, bID), 404)
	})
	t.Run("selected export cannot override trusted scope", func(t *testing.T) {
		w := request(t, "super", "POST", "/admin/proxies/export"+selected, `{"format":"json","filter":{"page":1,"page_size":10}}`, 200)
		var inventory struct {
			Proxies []json.RawMessage `json:"proxies"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &inventory))
		require.Len(t, inventory.Proxies, 12, "export covers full selected scope, not just page")
		request(t, "super", "POST", "/admin/proxies/export"+selected, fmt.Sprintf(`{"format":"json","filter":{"vendor_id":%d}}`, vendors["supplier-b"]), 404)
		request(t, "super", "POST", "/admin/proxies/export"+selected, fmt.Sprintf(`{"format":"json","ids":[%d]}`, bID), 404)
	})
	t.Run("native inventories exclude supplier objects", func(t *testing.T) {
		for _, actor := range []string{"admin", "super"} {
			for _, path := range []string{"/admin/proxies", "/admin/proxies/all", "/admin/proxies/all?with_count=true", "/admin/proxies/data"} {
				w := request(t, actor, "GET", path, "", 200)
				require.Contains(t, w.Body.String(), "platform-only")
				require.NotContains(t, w.Body.String(), "supplier-")
			}
			exported := request(t, actor, "GET", fmt.Sprintf("/admin/proxies/data?ids=%d,%d", aID, platformID), "", 200)
			require.Contains(t, exported.Body.String(), "platform-only")
			require.NotContains(t, exported.Body.String(), "supplier-")
		}
		for _, suffix := range []string{"", "/stats", "/accounts"} {
			request(t, "admin", "GET", fmt.Sprintf("/admin/proxies/%d%s", aID, suffix), "", 404)
		}
		for _, suffix := range []string{"test", "quality-check"} {
			request(t, "admin", "POST", fmt.Sprintf("/admin/proxies/%d/%s", aID, suffix), "{}", 404)
		}
		request(t, "admin", "PUT", fmt.Sprintf("/admin/proxies/%d", aID), `{"name":"forbidden"}`, 404)
		request(t, "admin", "DELETE", fmt.Sprintf("/admin/proxies/%d", aID), "", 404)
		w := request(t, "admin", "POST", "/admin/proxies/batch-delete", fmt.Sprintf(`{"ids":[%d]}`, aID), 200)
		require.Contains(t, w.Body.String(), `"skipped"`)
		request(t, "admin", "GET", "/admin/proxies?vendor_id=0", "", 403)
		request(t, "admin", "GET", "/admin/proxies"+selected, "", 403)
		request(t, "admin", "POST", "/admin/proxies/export"+selected, `{"format":"json"}`, 403)
		request(t, "admin", "POST", "/admin/proxies/batch-delete?vendor_id=0", fmt.Sprintf(`{"ids":[%d]}`, aID), 403)
		request(t, "admin", "GET", fmt.Sprintf("/admin/proxies/%d", platformID), "", 200)
		request(t, "admin", "PUT", fmt.Sprintf("/admin/proxies/%d", platformID), `{"name":"platform-updated"}`, 200)
		var name string
		require.NoError(t, db.QueryRowContext(ctx, "SELECT name FROM proxies WHERE id=$1 AND deleted_at IS NULL", aID).Scan(&name))
		require.Equal(t, "supplier-a-00", name)
	})
	t.Run("aggregate export alias", func(t *testing.T) {
		w := request(t, "super", "POST", "/admin/proxies/export?vendor_id=0", `{"format":"json","filter":{"search":"supplier-","page_size":10}}`, 200)
		var inventory struct {
			Proxies []json.RawMessage `json:"proxies"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &inventory))
		require.Len(t, inventory.Proxies, 13)
		require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
		w = request(t, "super", "POST", "/admin/proxies/export?vendor_id=0", fmt.Sprintf(`{"format":"json","filter":{"vendor_id":%d}}`, vendors["supplier-b"]), 200)
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &inventory))
		require.Len(t, inventory.Proxies, 1)
		w = request(t, "super", "POST", "/admin/proxies/export?vendor_id=0", fmt.Sprintf(`{"format":"csv","ids":[%d,%d]}`, aID, bID), 200)
		require.True(t, strings.HasPrefix(w.Body.String(), "\xef\xbb\xbf"))
		require.Contains(t, w.Body.String(), "supplier-a-00")
		require.Contains(t, w.Body.String(), "supplier-b-00")
		request(t, "super", "POST", "/admin/proxies/export?vendor_id=0", fmt.Sprintf(`{"format":"json","ids":[%d]}`, platformID), 404)
	})
	t.Run("aggregate batch delete resolves each owner and protects references", func(t *testing.T) {
		create := func(vID int64, name string) int64 {
			var id int64
			require.NoError(t, db.QueryRowContext(ctx, "INSERT INTO proxies(name,protocol,host,port,status,vendor_id) VALUES($1,'http','delete-fixture.invalid',8080,'inactive',$2) RETURNING id", name, vID).Scan(&id))
			return id
		}
		freeA := create(vendors["supplier-a"], "free-a")
		freeB := create(vendors["supplier-b"], "free-b")
		blocked := create(vendors["supplier-a"], "oauth-referenced")
		require.NoError(t, svc.RegisterVendorProxyReference(ctx, vendors["supplier-a"], blocked, "oauth", "pending-session", time.Now().Add(time.Minute)))
		w := request(t, "super", "POST", "/admin/proxies/batch-delete?vendor_id=0", fmt.Sprintf(`{"ids":[%d,%d,%d,%d,-1,%d]}`, freeA, freeB, blocked, platformID, freeA), 200)
		var result struct {
			Data service.VendorProxyBatchResult `json:"data"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
		require.Equal(t, 2, result.Data.SuccessCount)
		require.Equal(t, 3, result.Data.FailedCount)
		require.Len(t, result.Data.Items, 5)
		require.Equal(t, "PROXY_IN_USE", result.Data.Items[2].Code)
		for _, id := range []int64{freeA, freeB, blocked, platformID} {
			var deleted bool
			require.NoError(t, db.QueryRowContext(ctx, "SELECT deleted_at IS NOT NULL FROM proxies WHERE id=$1", id).Scan(&deleted))
			require.Equal(t, id == freeA || id == freeB, deleted)
		}
		request(t, "super", "POST", "/admin/proxies/batch-delete?vendor_id=0", fmt.Sprintf(`{"ids":[%d,%d]}`, freeA, freeB), 200)
	})
	t.Run("stale administrator subject cannot retain aggregate access", func(t *testing.T) {
		_, err := db.ExecContext(ctx, "UPDATE users SET role='admin' WHERE id=$1", actors["super"].UserID)
		require.NoError(t, err)
		request(t, "super", "GET", "/admin/proxies?vendor_id=0", "", 403)
		request(t, "super", "POST", "/admin/proxies/export?vendor_id=0", `{"format":"json"}`, 403)
		request(t, "super", "POST", "/admin/proxies/batch-delete?vendor_id=0", fmt.Sprintf(`{"ids":[%d]}`, aID), 403)
	})
}
