package admin

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGroupRequestsDecodeNoAccountFallback(t *testing.T) {
	var createReq CreateGroupRequest
	require.NoError(t, json.Unmarshal([]byte(`{"name":"primary","fallback_group_id_on_no_account":7}`), &createReq))
	require.NotNil(t, createReq.FallbackGroupIDOnNoAccount)
	require.Equal(t, int64(7), *createReq.FallbackGroupIDOnNoAccount)

	// Update 用 0 表示清除，必须与「未传」区分开。
	var updateReq UpdateGroupRequest
	require.NoError(t, json.Unmarshal([]byte(`{"fallback_group_id_on_no_account":0}`), &updateReq))
	require.NotNil(t, updateReq.FallbackGroupIDOnNoAccount)
	require.Zero(t, *updateReq.FallbackGroupIDOnNoAccount)

	var omitted UpdateGroupRequest
	require.NoError(t, json.Unmarshal([]byte(`{}`), &omitted))
	require.Nil(t, omitted.FallbackGroupIDOnNoAccount)
}
