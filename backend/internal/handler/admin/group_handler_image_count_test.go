//go:build unit

package admin

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestGroupImageCountBindingAndPermission(t *testing.T) {
	for _, target := range []any{&CreateGroupRequest{}, &UpdateGroupRequest{}} {
		field, ok := reflect.TypeOf(target).Elem().FieldByName("ImageCountBilling")
		require.True(t, ok)
		require.Equal(t, "billing.rates.update", field.Tag.Get("authz"))
	}
	for _, enabled := range []bool{false, true} {
		body, err := json.Marshal(map[string]any{"name": "images", "platform": "openai", "allow_image_generation": true, "image_count_billing": enabled})
		require.NoError(t, err)
		var create CreateGroupRequest
		require.NoError(t, bindGroupPlatformJSON(t, &create, string(body)))
		require.Equal(t, enabled, create.ImageCountBilling)
		var update UpdateGroupRequest
		require.NoError(t, bindGroupPlatformJSON(t, &update, string(body)))
		require.NotNil(t, update.ImageCountBilling)
		require.Equal(t, enabled, *update.ImageCountBilling)
	}
	var omitted UpdateGroupRequest
	require.NoError(t, bindGroupPlatformJSON(t, &omitted, `{"name":"unchanged"}`))
	require.Nil(t, omitted.ImageCountBilling, "omission must not disable an existing setting")
}

func TestGroupImageCountDTOProjection(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		group := &service.Group{ID: 7, Platform: service.PlatformOpenAI, AllowImageGeneration: true, ImageCountBilling: enabled}
		encoded, err := json.Marshal(dto.GroupFromService(group))
		require.NoError(t, err)
		var payload map[string]any
		require.NoError(t, json.Unmarshal(encoded, &payload))
		require.Equal(t, enabled, payload["image_count_billing"], "UI must receive explicit false as well as true")
	}
}
