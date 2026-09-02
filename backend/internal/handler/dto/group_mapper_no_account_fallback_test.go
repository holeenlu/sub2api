package dto

import (
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

// 与同族的 fallback_group_id / fallback_group_id_on_invalid_request 一样，兜底
// 分组 ID 走用户侧与管理端共用的基础映射，两侧都返回。
func TestGroupMapperCarriesNoAccountFallback(t *testing.T) {
	fallbackID := int64(9)
	group := &service.Group{
		ID: 7, Name: "primary", Platform: service.PlatformAnthropic, Status: service.StatusActive,
		FallbackGroupIDOnNoAccount: &fallbackID,
	}

	userJSON, err := json.Marshal(GroupFromService(group))
	require.NoError(t, err)
	require.Contains(t, string(userJSON), `"fallback_group_id_on_no_account":9`)

	adminJSON, err := json.Marshal(GroupFromServiceAdmin(group))
	require.NoError(t, err)
	require.Contains(t, string(adminJSON), `"fallback_group_id_on_no_account":9`)

	group.FallbackGroupIDOnNoAccount = nil
	unsetJSON, err := json.Marshal(GroupFromServiceAdmin(group))
	require.NoError(t, err)
	require.Contains(t, string(unsetJSON), `"fallback_group_id_on_no_account":null`)
}
