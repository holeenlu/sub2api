package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestModelsListCapabilityExtensionsPreserveListAndRetrieve(t *testing.T) {
	gin.SetMode(gin.TestMode)
	no := false
	caps := map[string]service.ModelListCapabilities{
		"vendor-chat":    {ModelKind: "chat", Reasoning: &no, InputModalities: []string{"text"}, ContextWindow: 12345},
		"gpt-image-test": {ModelKind: "image", OutputModalities: []string{"image"}},
	}
	for _, platform := range []string{service.PlatformOpenAI, service.PlatformAnthropic, service.PlatformGrok, service.PlatformComposite} {
		for _, selected := range []string{"", "vendor-chat", "hidden"} {
			t.Run(platform+"/"+selected, func(t *testing.T) {
				w := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(w)
				if selected != "" {
					c.Params = gin.Params{{Key: "model", Value: selected}}
				}
				writeModelsList(c, platform, []string{"vendor-chat", "gpt-image-test", "names-only"}, caps)
				if selected == "hidden" {
					require.Equal(t, http.StatusNotFound, w.Code)
					return
				}
				require.Equal(t, http.StatusOK, w.Code)
				raw := w.Body.Bytes()
				entry := gjson.ParseBytes(raw)
				if selected == "" {
					require.Equal(t, "list", gjson.GetBytes(raw, "object").String())
					require.Equal(t, int64(3), gjson.GetBytes(raw, "data.#").Int())
					require.Equal(t, "gpt-image-test", gjson.GetBytes(raw, "data.1.id").String())
					require.False(t, gjson.GetBytes(raw, "data.2.context_window").Exists())
					entry = gjson.GetBytes(raw, "data.0")
				}
				require.Equal(t, "vendor-chat", entry.Get("id").String())
				require.True(t, entry.Get("reasoning").Exists())
				require.False(t, entry.Get("reasoning").Bool())
				require.Equal(t, int64(12345), entry.Get("context_window").Int())
				require.Equal(t, "text", entry.Get("input_modalities.0").String())
				require.False(t, entry.Get("supported_reasoning_levels").Exists())
				// Clients unaware of the extension still decode the original fields.
				var legacy struct {
					ID string `json:"id"`
				}
				require.NoError(t, json.Unmarshal([]byte(entry.Raw), &legacy))
				require.Equal(t, "vendor-chat", legacy.ID)
			})
		}
	}
}
