package admin

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func newApplyOAuthCredentialsRouter(stub *stubAdminService) *gin.Engine {
	gin.SetMode(gin.TestMode)
	handler := NewAccountHandler(stub, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	router := gin.New()
	router.POST("/accounts/:id/apply-oauth-credentials", handler.ApplyOAuthCredentials)
	return router
}

// 重新授权必须走整套替换的 ApplyOAuthCredentials，而不是通用 UpdateAccount：后者的
// "缺失即保留"会把上一套 refresh_token 留在直接导入的 setup-token 账号上。
func TestApplyOAuthCredentialsUsesTokenReplacingServicePath(t *testing.T) {
	stub := newStubAdminService()
	stub.getAccountResult = &service.Account{
		ID:       1,
		Platform: service.PlatformAnthropic,
		Type:     service.AccountTypeOAuth,
	}
	router := newApplyOAuthCredentialsRouter(stub)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/accounts/1/apply-oauth-credentials", bytes.NewBufferString(
		`{"type":"setup-token","credentials":{"access_token":"sk-ant-oat01-new","token_type":"Bearer","scope":"user:inference"}}`,
	))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, 1, stub.applyOAuthCredentialsCalls)
	require.Zero(t, stub.updateAccountCalls, "re-auth must not go through the merge-preserving UpdateAccount path")
	require.NotNil(t, stub.lastApplyOAuthCredentialsInput)
	require.Equal(t, service.AccountTypeSetupToken, stub.lastApplyOAuthCredentialsInput.Type)
	require.Equal(t, map[string]any{
		"access_token": "sk-ant-oat01-new",
		"token_type":   "Bearer",
		"scope":        "user:inference",
	}, stub.lastApplyOAuthCredentialsInput.Credentials)
}

func TestApplyOAuthCredentialsRejectsNonOAuthAccountBeforeServiceCall(t *testing.T) {
	stub := newStubAdminService()
	stub.getAccountResult = &service.Account{
		ID:       1,
		Platform: service.PlatformAnthropic,
		Type:     service.AccountTypeAPIKey,
	}
	router := newApplyOAuthCredentialsRouter(stub)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/accounts/1/apply-oauth-credentials", bytes.NewBufferString(
		`{"type":"oauth","credentials":{"access_token":"new"}}`,
	))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Contains(t, recorder.Body.String(), "NOT_OAUTH")
	require.Zero(t, stub.applyOAuthCredentialsCalls)
}
