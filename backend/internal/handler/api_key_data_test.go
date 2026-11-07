//go:build unit

package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAPIKeyDataHandlerRejectsUnauthenticatedAndMalformedRequests(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		method, path, body string
		auth               bool
		status             int
	}{
		{"GET", "/keys/export", "", false, http.StatusUnauthorized},
		{"POST", "/keys/import", "{}", false, http.StatusUnauthorized},
		{"GET", "/keys/export?ids=abc", "", true, http.StatusBadRequest},
		{"GET", "/keys/export?group_id=-1", "", true, http.StatusBadRequest},
		{"POST", "/keys/import", "{bad", true, http.StatusBadRequest},
		{"POST", "/keys/import", `{"data":{"type":"sub2api-data","version":1,"keys":[]}}`, true, http.StatusBadRequest},
		{"POST", "/keys/import", strings.Repeat(" ", 5<<20) + "{}", true, http.StatusBadRequest},
	} {
		h := NewAPIKeyHandler(&service.APIKeyService{})
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		c.Request.Header.Set("Content-Type", "application/json")
		if tc.auth {
			c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 7})
		}
		if tc.method == "GET" {
			h.ExportData(c)
		} else {
			h.ImportData(c)
		}
		require.Equal(t, tc.status, recorder.Code, tc.path)
	}
}
