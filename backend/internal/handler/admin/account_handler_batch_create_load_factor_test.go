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

// 批量创建与单个创建接收同一份 CreateAccountRequest，负载因子必须一并透传：
// setup-token 批量导入只走这个端点，管理员填了却被静默丢掉，账号会按默认权重调度。
func TestBatchCreateForwardsLoadFactor(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stub := newStubAdminService()
	handler := NewAccountHandler(stub, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	router := gin.New()
	router.POST("/accounts/batch", handler.BatchCreate)

	body := `{"accounts":[
		{"name":"weighted","platform":"anthropic","type":"setup-token","credentials":{"access_token":"sk-ant-oat01-x"},"load_factor":3},
		{"name":"default","platform":"anthropic","type":"setup-token","credentials":{"access_token":"sk-ant-oat01-y"}}
	]}`
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/accounts/batch", bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")

	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Len(t, stub.createdAccounts, 2)
	require.NotNil(t, stub.createdAccounts[0].LoadFactor)
	require.Equal(t, 3, *stub.createdAccounts[0].LoadFactor)
	require.Nil(t, stub.createdAccounts[1].LoadFactor, "未填写时保持 nil，由服务层落默认值")
}
