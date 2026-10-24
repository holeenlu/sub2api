package handler_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	_ "github.com/Wei-Shaw/sub2api/ent/runtime"
	"github.com/Wei-Shaw/sub2api/internal/authz"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/server/routes"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

// Exercise the mounted production handlers, dispatch, service and generated Ent
// against the full migration runner. Only login authentication is supplied by
// the fixture; ownership, current roles, pagination and accounting use real SQL.
func TestVendorUsagePostgresMountedHistoryAndAdminDispatch(t *testing.T) {
	db := vendorHTTPPostgres(t)
	var err error
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	settings := service.NewSettingService(repository.NewSettingRepository(client), nil)
	_, err = db.Exec("INSERT INTO settings(key,value) VALUES('vendor_console_enabled','true') ON CONFLICT(key) DO UPDATE SET value='true'")
	require.NoError(t, err)
	svc := service.NewVendorService(db, client, nil, nil, nil, nil, nil, settings, nil, nil, nil, nil, nil, &config.Config{Timezone: "UTC"})
	actors := map[string]authz.Subject{}
	for name, role := range map[string]string{"super": authz.SuperAdmin, "admin": authz.Admin, "supplier-a": authz.VendorLevel1, "supplier-b": authz.VendorLevel1, "consumer": authz.User} {
		var id, generation int64
		err = db.QueryRow("INSERT INTO users(email,password_hash,role,status) VALUES($1,'test-only',$2,'active') RETURNING id,session_generation", name+"@invalid", role).Scan(&id, &generation)
		require.NoError(t, err)
		actors[name] = authz.Subject{UserID: id, Role: role, SessionGeneration: generation}
	}
	vendors := map[string]int64{}
	for _, name := range []string{"supplier-a", "supplier-b"} {
		var id int64
		err = db.QueryRow("INSERT INTO vendors(user_id,name,platforms) VALUES($1,$2,'[\"openai\"]') RETURNING id", actors[name].UserID, name).Scan(&id)
		require.NoError(t, err)
		vendors[name] = id
	}
	var groupID, keyID int64
	require.NoError(t, db.QueryRow("INSERT INTO groups(name,platform,status) VALUES('served','openai','active') RETURNING id").Scan(&groupID))
	_, err = db.Exec("INSERT INTO vendor_groups(vendor_id,group_id,is_default) VALUES($1,$3,true),($2,$3,true)", vendors["supplier-a"], vendors["supplier-b"], groupID)
	require.NoError(t, err)
	require.NoError(t, db.QueryRow("INSERT INTO api_keys(user_id,key,name,group_id) VALUES($1,'SECRET_CONSUMER_KEY','private consumer key',$2) RETURNING id", actors["consumer"].UserID, groupID).Scan(&keyID))
	account := func(name string, vendorID int64, deleted bool) int64 {
		var id int64
		err := db.QueryRow("INSERT INTO accounts(name,platform,type,status,vendor_id,credentials,extra) VALUES($1,'openai','oauth','active',$2,'{}','{}') RETURNING id", name, vendorID).Scan(&id)
		require.NoError(t, err)
		if deleted {
			_, err = db.Exec("UPDATE accounts SET deleted_at=NOW() WHERE id=$1", id)
			require.NoError(t, err)
		}
		return id
	}
	currentID := account("A-current", vendors["supplier-a"], false)
	deletedID := account("A-deleted", vendors["supplier-a"], true)
	otherID := account("B-private", vendors["supplier-b"], false)
	usage := func(request string, accountID int64, owner any, name, cost string, at time.Time) {
		_, err := db.Exec(`INSERT INTO usage_logs(user_id,api_key_id,account_id,request_id,model,group_id,vendor_id_snapshot,account_name_snapshot,group_name_snapshot,platform_snapshot,created_at,input_tokens,output_tokens,total_cost,actual_cost)
VALUES($1,$2,$3,$4,'requested',$5,$6,$7,'served','openai',$8,100,40,$9,77777)`, actors["consumer"].UserID, keyID, accountID, request, groupID, owner, name, at, cost)
		require.NoError(t, err)
	}
	usage("current", currentID, vendors["supplier-a"], "A-current", "1", time.Now().Add(-time.Second))
	old := time.Date(2020, 1, 1, 12, 0, 0, 0, time.UTC)
	usage("deleted", deletedID, vendors["supplier-a"], "A-deleted", "2", old)
	usage("foreign", otherID, vendors["supplier-b"], "B-private", "88888", old)
	usage("legacy", deletedID, nil, "legacy", "99999", old)
	_, err = db.Exec("UPDATE usage_logs SET requested_model='claude-fable-5',model='claude-fable-5',upstream_model='claude-opus-4-6' WHERE request_id='current'; UPDATE usage_logs SET requested_model='claude-opus-4-6',model='claude-opus-4-6',upstream_model='claude-fable-5' WHERE request_id='deleted'")
	require.NoError(t, err)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	h := &handler.Handlers{Admin: &handler.AdminHandlers{}, Vendor: handler.NewVendorHandler(svc, nil, nil), VendorAccount: handler.NewVendorAccountHandler(svc), VendorProxy: handler.NewVendorProxyHandler(svc), VendorUsage: handler.NewVendorUsageHandler(svc), VendorSettlement: handler.NewVendorSettlementHandler(svc)}
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
	request := func(actor, path string, status int) service.VendorUsageReport {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/api/v1"+path, nil)
		r.Header.Set("X-Test-Actor", actor)
		router.ServeHTTP(w, r)
		require.Equal(t, status, w.Code, "%s: %s", path, w.Body.String())
		for _, private := range []string{"SECRET_CONSUMER_KEY", "consumer@invalid", "api_key_id", "user_id", "request_id", "actual_cost", "77777"} {
			require.NotContains(t, w.Body.String(), private)
		}
		if status != http.StatusOK {
			return service.VendorUsageReport{}
		}
		require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
		var envelope struct {
			Data service.VendorUsageReport `json:"data"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &envelope))
		return envelope.Data
	}
	first := request("supplier-a", "/vendor/usage/accounts?all_history=true&page_size=1", 200)
	require.Equal(t, int64(2), first.Total)
	require.Equal(t, int64(2), first.Summary.Requests)
	require.Equal(t, "3", first.Summary.CostUSD)
	require.Equal(t, "1", first.Summary.OpusCostUSD)
	require.Equal(t, "2", first.Summary.FableCostUSD)
	require.Len(t, first.Items, 1)
	require.Equal(t, deletedID, first.Items[0].AccountID)
	require.Equal(t, "supplier-a", first.Items[0].VendorName)
	require.True(t, first.Items[0].AccountDeleted)
	require.False(t, first.Items[0].AccountArchived, "legacy tombstones are not archives")
	require.Equal(t, "2", first.Items[0].FableCostUSD)
	opusFirst := request("supplier-a", "/vendor/usage/accounts?all_history=true&sort_by=opus_cost_usd&order=desc&page_size=1", 200)
	require.Equal(t, currentID, opusFirst.Items[0].AccountID)
	require.Equal(t, first.Summary, opusFirst.Summary)
	cheapFirst := request("super", fmt.Sprintf("/admin/vendors/usage?dimension=account&all_history=true&vendor_id=%d&sort_by=cost_usd&order=asc&page_size=1", vendors["supplier-a"]), 200)
	require.Equal(t, currentID, cheapFirst.Items[0].AccountID)
	emptyPage := request("supplier-a", "/vendor/usage/accounts?all_history=true&page=3&page_size=1", 200)
	require.Empty(t, emptyPage.Items)
	require.Equal(t, first.Total, emptyPage.Total)
	require.Equal(t, first.Summary, emptyPage.Summary)
	second := request("supplier-a", fmt.Sprintf("/vendor/usage/accounts?all_history=true&page=2&page_size=1&snapshot_at=%s&watermark=%d", url.QueryEscape(first.SnapshotAt.Format(time.RFC3339Nano)), first.Watermark), 200)
	require.Equal(t, first.Summary, second.Summary)
	require.Len(t, second.Items, 1)
	require.Equal(t, currentID, second.Items[0].AccountID)
	recent := request("supplier-a", "/vendor/usage/accounts", 200)
	require.Equal(t, int64(1), recent.Summary.Requests)
	admin := request("super", fmt.Sprintf("/admin/vendors/usage?dimension=account&all_history=true&vendor_id=%d&page_size=1", vendors["supplier-a"]), 200)
	require.Equal(t, first.Summary, admin.Summary)
	require.Equal(t, first.Total, admin.Total)
	require.Equal(t, "supplier-a", admin.Items[0].VendorName)
	for _, owner := range []int64{0, vendors["supplier-a"]} {
		detail := request("super", fmt.Sprintf("/admin/accounts/%d/usage?vendor_id=%d&all_history=true", deletedID, owner), 200)
		require.Equal(t, int64(1), detail.Summary.Requests)
		require.Equal(t, "2", detail.Summary.CostUSD)
	}
	request("admin", fmt.Sprintf("/admin/accounts/%d/usage?vendor_id=0&all_history=true", deletedID), 403)
	request("super", fmt.Sprintf("/admin/accounts/%d/usage?vendor_id=%d&all_history=true", otherID, vendors["supplier-a"]), 404)
	request("supplier-a", fmt.Sprintf("/vendor/accounts/%d/usage?all_history=true", otherID), 404)
	request("supplier-a", fmt.Sprintf("/vendor/usage/accounts?all_history=true&vendor_id=%d", vendors["supplier-b"]), 403)
	for _, query := range []string{"all_history=true&start_date=2020-01-01", "all_history=true&end_date=2020-01-01", "all_history=true&page_size=101", "all_history=true&page=100001", "all_history=maybe", "sort_by=cost_usd;DROP", "sort_by=actual_cost", "sort_by=date", "order=descending", "sort_by=cost_usd&sort_by=requests", "order=asc&order=desc"} {
		request("supplier-a", "/vendor/usage/accounts?"+query, 422)
	}
	requestPath := fmt.Sprintf("/admin/vendors/usage?dimension=request&all_history=true&vendor_id=%d&page_size=1", vendors["supplier-a"])
	records := request("super", requestPath, 200)
	require.Equal(t, first.Summary, records.Summary)
	require.Equal(t, int64(2), records.Total)
	require.Equal(t, "request", records.Dimension)
	require.Len(t, records.Items, 1)
	record := records.Items[0]
	require.Positive(t, record.UsageLogID)
	require.NotNil(t, record.CreatedAt)
	require.Equal(t, currentID, record.AccountID)
	require.Equal(t, "openai", record.Platform)
	require.Equal(t, "unknown", record.Result)
	require.Nil(t, record.SuccessfulRequests)
	require.Nil(t, record.FailedRequests)
	require.Equal(t, "1", record.OpusCostUSD)
	request("supplier-a", requestPath, 403)
	request("admin", requestPath, 403)
	for _, endpoint := range []string{"summary", "trend", "accounts"} {
		request("supplier-a", "/vendor/usage/"+endpoint+"?dimension=request", 422)
	}
	historyRecord := request("super", requestPath+fmt.Sprintf("&account_id=%d&snapshot_at=%s&watermark=%d", deletedID, url.QueryEscape(records.SnapshotAt.Format(time.RFC3339Nano)), records.Watermark), 200)
	require.Equal(t, int64(1), historyRecord.Total)
	require.True(t, historyRecord.Items[0].AccountDeleted)
	require.Equal(t, "2", historyRecord.Items[0].FableCostUSD)
	for _, query := range []string{"dimension=request&sort_by=date", "dimension=request&page_size=101", "dimension=account&sort_by=created_at"} {
		request("super", "/admin/vendors/usage?"+query, 422)
	}
	_, err = db.Exec("UPDATE accounts SET extra=jsonb_build_object('vendor_archived_at',NOW()) WHERE id=$1", currentID)
	require.NoError(t, err)
	// The picker uses this all-history account query. Live archive metadata must
	// stay distinct from tombstones even while reusing a billing snapshot.
	picker := request("supplier-a", fmt.Sprintf("/vendor/usage/accounts?all_history=true&snapshot_at=%s&watermark=%d", url.QueryEscape(first.SnapshotAt.Format(time.RFC3339Nano)), first.Watermark), 200)
	require.Equal(t, first.Summary, picker.Summary)
	require.Len(t, picker.Items, 2)
	for _, row := range picker.Items {
		require.Equal(t, row.AccountID == currentID, row.AccountArchived)
		require.Equal(t, row.AccountID == deletedID, row.AccountDeleted)
	}
	for _, dimension := range []string{"account", "account_date", "request"} {
		report := request("super", fmt.Sprintf("/admin/vendors/usage?dimension=%s&all_history=true&vendor_id=%d&account_id=%d", dimension, vendors["supplier-a"], currentID), 200)
		require.Len(t, report.Items, 1)
		require.True(t, report.Items[0].AccountArchived)
		require.False(t, report.Items[0].AccountDeleted)
		require.Equal(t, "active", report.Items[0].AccountStatus, "archive does not replace provider status")
		require.Equal(t, "1", report.Summary.CostUSD)
	}
	_, err = db.Exec("UPDATE users SET role='admin' WHERE id=$1", actors["super"].UserID)
	require.NoError(t, err)
	request("super", fmt.Sprintf("/admin/accounts/%d/usage?vendor_id=0&all_history=true", deletedID), 403)
}

func vendorHTTPPostgres(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("VENDOR_SETTLEMENT_TEST_DSN")
	if dsn == "" {
		t.Skip("VENDOR_SETTLEMENT_TEST_DSN not set")
	}
	u, err := url.Parse(dsn)
	require.NoError(t, err)
	require.Contains(t, []string{"127.0.0.1", "localhost"}, u.Hostname())
	require.Equal(t, "/vendor_settlement_test", u.Path)
	base, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, base.Close()) })
	database := fmt.Sprintf("vendor_usage_http_%d", time.Now().UnixNano())
	_, err = base.Exec("CREATE DATABASE " + database)
	require.NoError(t, err)
	u.Path = "/" + database
	db, err := sql.Open("postgres", u.String())
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, db.Close())
		_, err := base.Exec("DROP DATABASE " + database)
		require.NoError(t, err)
	})
	require.NoError(t, repository.ApplyMigrations(context.Background(), db))
	return db
}
