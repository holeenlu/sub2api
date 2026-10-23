package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/authz"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/server/routes"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestVendorSettlementPostgresMountedBilateralAPI(t *testing.T) {
	db := vendorHTTPPostgres(t)
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	settings := service.NewSettingService(repository.NewSettingRepository(client), nil)
	_, err := db.Exec("INSERT INTO settings(key,value) VALUES('vendor_console_enabled','true') ON CONFLICT(key) DO UPDATE SET value='true'")
	require.NoError(t, err)
	svc := service.NewVendorService(db, client, nil, nil, nil, nil, nil, settings, nil, nil, nil, nil, nil, &config.Config{JWT: config.JWTConfig{Secret: "local-http-bilateral-test-only-key"}, Timezone: "UTC"})
	actors := map[string]authz.Subject{}
	for name, role := range map[string]string{"root": authz.SuperAdmin, "l1": authz.VendorLevel1, "l2": authz.VendorLevel2, "other": authz.VendorLevel1, "customer": authz.User} {
		var id, generation int64
		require.NoError(t, db.QueryRow("INSERT INTO users(email,password_hash,role,status) VALUES($1,'test-only',$2,'active') RETURNING id,session_generation", name+"@invalid", role).Scan(&id, &generation))
		actors[name] = authz.Subject{UserID: id, Role: role, SessionGeneration: generation}
	}
	var l1, l2, other, groupID, accountID, keyID int64
	require.NoError(t, db.QueryRow("INSERT INTO vendors(user_id,name,platforms,default_coefficient,contract_version) VALUES($1,'L1','[\"openai\"]',1.8,7) RETURNING id", actors["l1"].UserID).Scan(&l1))
	require.NoError(t, db.QueryRow("INSERT INTO vendors(user_id,name,platforms,default_coefficient,contract_version,tier,parent_vendor_id) VALUES($1,'L2','[\"openai\"]',0.9,3,2,$2) RETURNING id", actors["l2"].UserID, l1).Scan(&l2))
	require.NoError(t, db.QueryRow("INSERT INTO vendors(user_id,name,platforms) VALUES($1,'Other','[\"openai\"]') RETURNING id", actors["other"].UserID).Scan(&other))
	require.NoError(t, db.QueryRow("INSERT INTO groups(name,platform,status) VALUES('served','openai','active') RETURNING id").Scan(&groupID))
	_, err = db.Exec("INSERT INTO vendor_groups(vendor_id,group_id,is_default) VALUES($1,$3,true),($2,$3,true)", l1, l2, groupID)
	require.NoError(t, err)
	require.NoError(t, db.QueryRow("INSERT INTO accounts(name,platform,type,status,vendor_id,credentials,extra) VALUES('child-account','openai','oauth','active',$1,'{}','{}') RETURNING id", l2).Scan(&accountID))
	require.NoError(t, db.QueryRow("INSERT INTO api_keys(user_id,key,name,group_id) VALUES($1,'PRIVATE-CUSTOMER-KEY','consumer',$2) RETURNING id", actors["customer"].UserID, groupID).Scan(&keyID))
	// Exercise the existing producer snapshot and native batched persistence,
	// rather than relying on an artificial settlement-only usage writer.
	usage := &service.UsageLog{UserID: actors["customer"].UserID, APIKeyID: keyID, AccountID: accountID, RequestID: "PRIVATE-REQUEST", Model: "model", GroupID: &groupID, TotalCost: 100, ActualCost: 77777, CreatedAt: time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)}
	usage.CaptureVendorSnapshot(&service.Account{ID: accountID, VendorID: &l2, Name: "child-account", Platform: "openai"}, &service.Group{ID: groupID, Name: "served"})
	inserted, err := repository.NewUsageLogRepository(client, db).Create(context.Background(), usage)
	require.NoError(t, err)
	require.True(t, inserted)
	stored, err := client.UsageLog.Get(context.Background(), usage.ID)
	require.NoError(t, err)
	require.Equal(t, l2, *stored.VendorIDSnapshot)
	require.Equal(t, l1, *stored.VendorRootIDSnapshot)
	require.Equal(t, l1, *stored.VendorParentIDSnapshot)
	require.Equal(t, actors["customer"].UserID, stored.UserID)
	// Migration 276 guarantees the owner hierarchy cannot move during async usage.
	_, err = db.Exec("UPDATE vendors SET parent_vendor_id=$1 WHERE id=$2", other, l2)
	require.Error(t, err)
	_, err = db.Exec("UPDATE accounts SET vendor_id=$1 WHERE id=$2", other, accountID)
	require.Error(t, err)
	var savedRoot, savedParent int64
	require.NoError(t, db.QueryRow("SELECT vendor_root_id_snapshot,vendor_parent_id_snapshot FROM usage_logs WHERE account_id=$1", accountID).Scan(&savedRoot, &savedParent))
	require.Equal(t, l1, savedRoot)
	require.Equal(t, l1, savedParent)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := &handler.Handlers{Admin: &handler.AdminHandlers{}, Vendor: handler.NewVendorHandler(svc, nil, nil), VendorAccount: handler.NewVendorAccountHandler(svc), VendorProxy: handler.NewVendorProxyHandler(svc), VendorUsage: handler.NewVendorUsageHandler(svc), VendorSettlement: handler.NewVendorSettlementHandler(svc)}
	auth := func(c *gin.Context) {
		a := actors[c.GetHeader("X-Test-Actor")]
		c.Request = c.Request.WithContext(authz.WithSubject(context.Background(), a))
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: a.UserID})
		c.Set(string(middleware.ContextKeyUserRole), a.Role)
		c.Next()
	}
	pass := func(c *gin.Context) { c.Next() }
	routes.RegisterAdminRoutes(r.Group("/api/v1"), h, middleware.AdminAuthMiddleware(auth), middleware.AuditLogMiddleware(pass), middleware.StepUpAuthMiddleware(pass), nil, nil)
	routes.RegisterVendorRoutes(r.Group("/api/v1"), h, middleware.JWTAuthMiddleware(auth), nil)
	request := func(actor, method, path string, body any, status int) json.RawMessage {
		b, e := json.Marshal(body)
		require.NoError(t, e)
		req := httptest.NewRequest(method, "/api/v1"+path, bytes.NewReader(b))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Test-Actor", actor)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		require.Equal(t, status, w.Code, "%s %s: %s", method, path, w.Body.String())
		for _, secret := range []string{"PRIVATE-CUSTOMER-KEY", "PRIVATE-REQUEST", "77777", "api_key_id", "user_id", "actual_cost"} {
			require.NotContains(t, w.Body.String(), secret)
		}
		if status >= 400 {
			return nil
		}
		require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
		if w.Header().Get("Content-Type") == "text/csv; charset=utf-8" {
			return w.Body.Bytes()
		}
		var envelope struct{ Data json.RawMessage }
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &envelope))
		return envelope.Data
	}
	input := func(id int64) service.VendorSettlementInput {
		return service.VendorSettlementInput{VendorIDs: []int64{id}, StartDate: "2026-10-01", EndDate: "2026-10-01", Timezone: "UTC", ExchangeRate: "6.7", ExchangeRateSource: "agreed", CoefficientUnit: "CNY/USD", SettlementCurrency: "USDT"}
	}
	create := func(actor, base string, id int64) service.VendorSettlement {
		var previews []service.VendorSettlementPreview
		require.NoError(t, json.Unmarshal(request(actor, "POST", base+"/preview", input(id), 200), &previews))
		require.Empty(t, previews[0].Error)
		var result []service.VendorSettlementDraftResult
		require.NoError(t, json.Unmarshal(request(actor, "POST", base, map[string]any{"drafts": []service.VendorSettlementDraftInput{{PreviewToken: previews[0].PreviewToken, IdempotencyKey: "draft"}}}, 200), &result))
		require.Empty(t, result[0].Error)
		require.NotNil(t, result[0].Statement)
		return *result[0].Statement
	}
	parent := create("root", "/admin/vendors/settlements", l1)
	child := create("l1", "/vendor/payables", l2)
	require.Equal(t, "26.87", parent.Total)
	require.Equal(t, "13.43", child.Total)
	// AdminGet/AdminExport retain the legacy "receivable" hint. The actual
	// super-admin capability must still return complete draft evidence on both
	// edges, without letting a supplier enter that administration path.
	for _, statement := range []service.VendorSettlement{parent, child} {
		path := fmt.Sprintf("/admin/vendors/settlements/%d", statement.ID)
		var detail service.VendorSettlement
		require.NoError(t, json.Unmarshal(request("root", "GET", path, nil, 200), &detail))
		require.Equal(t, statement.SnapshotHash, detail.SnapshotHash)
		require.Equal(t, statement.PayerVendorID, detail.PayerVendorID)
		require.Equal(t, statement.UsageCount, int64(len(detail.Allocations)))
		require.Equal(t, "draft", detail.Status)
		request("root", "GET", path+"/export", nil, 200)
		request("l1", "GET", path, nil, 403)
	}
	request("l1", "GET", fmt.Sprintf("/vendor/payables?payer_vendor_id=%d", other), nil, 403)
	forged := input(l2)
	forged.PayerVendorID = &other
	request("l1", "POST", "/vendor/payables/preview", forged, 403)
	forged.PayerVendorID = &l1
	request("root", "POST", "/admin/vendors/settlements/preview", forged, 403)
	request("l2", "POST", "/vendor/payables", map[string]any{"drafts": []service.VendorSettlementDraftInput{{PreviewToken: "untrusted", IdempotencyKey: "denied"}}}, 403)
	request("l2", "POST", "/vendor/payables/preview", input(l2), 403)
	request("l2", "GET", "/vendor/payables", nil, 403)
	request("l2", "GET", fmt.Sprintf("/vendor/settlements/%d", child.ID), nil, 404)
	request("other", "GET", fmt.Sprintf("/vendor/payables/%d", child.ID), nil, 404)
	request("l1", "GET", fmt.Sprintf("/vendor/payables?vendor_id=%d", l2), nil, 200)
	request("l1", "GET", fmt.Sprintf("/vendor/payables?vendor_id=%d", other), nil, 403)
	request("l1", "GET", fmt.Sprintf("/vendor/payables/%d/export", child.ID), nil, 200)
	request("l1", "GET", fmt.Sprintf("/vendor/settlements/%d", child.ID), nil, 404)
	request("l1", "POST", fmt.Sprintf("/vendor/settlements/%d/confirm", parent.ID), map[string]any{"version": 1}, 404)
	request("l1", "POST", fmt.Sprintf("/vendor/payables/%d/confirm", parent.ID), map[string]any{"version": 1}, 404)
	require.NoError(t, json.Unmarshal(request("root", "POST", fmt.Sprintf("/admin/vendors/settlements/%d/confirm", parent.ID), map[string]any{"version": 1}, 200), &parent))
	require.NoError(t, json.Unmarshal(request("l1", "POST", fmt.Sprintf("/vendor/payables/%d/confirm", child.ID), map[string]any{"version": 1}, 200), &child))
	request("l1", "GET", fmt.Sprintf("/vendor/settlements/%d", parent.ID), nil, 200)
	request("l2", "GET", fmt.Sprintf("/vendor/settlements/%d", parent.ID), nil, 404)
	request("l2", "GET", fmt.Sprintf("/vendor/settlements/%d/export", parent.ID), nil, 404)
	raw := request("l2", "GET", fmt.Sprintf("/vendor/settlements/%d", child.ID), nil, 200)
	require.NotContains(t, string(raw), "1.8")
	require.NotContains(t, string(raw), "26.87")
	pay := service.VendorSettlementPaymentInput{Version: child.Version, Amount: "1", PaidAt: time.Now(), Channel: "offline", Reference: "receipt", IdempotencyKey: "payment"}
	request("l2", "POST", fmt.Sprintf("/vendor/payables/%d/payments", child.ID), pay, 403)
	require.NoError(t, json.Unmarshal(request("l1", "POST", fmt.Sprintf("/vendor/payables/%d/payments", child.ID), pay, 200), &child))
	require.Equal(t, "1.00", child.Paid)
	reverse := service.VendorSettlementAdjustmentInput{Version: child.Version, Amount: "1", Reason: "correct receipt", IdempotencyKey: "reverse"}
	require.NoError(t, json.Unmarshal(request("l1", "POST", fmt.Sprintf("/vendor/payables/%d/payments/%d/reverse", child.ID, child.Payments[0].ID), reverse, 200), &child))
	require.Equal(t, "0.00", child.Paid)
	_, err = db.Exec("UPDATE vendors SET status='disabled' WHERE id=$1", l1)
	require.NoError(t, err)
	request("l1", "GET", "/vendor/payables", nil, 403)
	request("l2", "GET", "/vendor/settlements", nil, 403)
	request("l2", "GET", "/vendor/usage/accounts", nil, 403)
	var consumer int64
	require.NoError(t, db.QueryRow("SELECT user_id FROM usage_logs WHERE account_id=$1", accountID).Scan(&consumer))
	require.Equal(t, actors["customer"].UserID, consumer)
}
