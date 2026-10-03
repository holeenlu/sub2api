//go:build unit

package admin

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAccountHandlerCatalogPolicyWriteBoundaries(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const policy = `"model_catalog_policy":{"mode":"fixed","models":[],"excluded":["blocked"]}`
	const create = `{"name":"test","platform":"anthropic","type":"apikey","credentials":{"api_key":"test"},` + policy + `}`
	for _, kind := range []string{"create", "batch", "bulk"} {
		t.Run(kind, func(t *testing.T) {
			stub := newStubAdminService()
			h := NewAccountHandler(stub, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
			router := gin.New()
			body := create
			switch kind {
			case "create":
				router.POST("/accounts", h.Create)
			case "batch":
				router.POST("/accounts", h.BatchCreate)
				body = `{"accounts":[` + create + `]}`
			case "bulk":
				router.POST("/accounts", h.BulkUpdate)
				body = `{"account_ids":[1,2],` + policy + `}`
			}
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/accounts", bytes.NewBufferString(body))
			req.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(rec, req)
			require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
			require.Empty(t, stub.createdAccounts)
			require.Nil(t, stub.lastBulkUpdateAccountInput)
		})
	}
}
