package handler

import (
	"encoding/json"
	"fmt"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

const clientCatalogValidatorKey = "client_catalog_if_none_match"

func isClientModelCatalog(c *gin.Context) bool {
	return c.Query("catalog_view") == "client"
}

// The client projection has its own ETag. Do not send that validator to the
// full-catalog cache; compare it only after projecting the final response.
func prepareClientCatalogValidation(c *gin.Context) {
	if isClientModelCatalog(c) {
		c.Set(clientCatalogValidatorKey, c.GetHeader("If-None-Match"))
		c.Request.Header.Del("If-None-Match")
	}
}

func clientCatalogValidator(c *gin.Context) string {
	if validator, exists := c.Get(clientCatalogValidatorKey); exists {
		return validator.(string)
	}
	return c.GetHeader("If-None-Match")
}

// Discovery and request authorization use the full catalog. This projection
// is exclusively for a user's downloaded or remotely configured client list.
func projectClientModelCatalog(body []byte) ([]byte, error) {
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, err
	}
	field, idField := "models", "slug"
	if _, ok := envelope[field]; !ok {
		field, idField = "data", "id"
	}
	var models []map[string]json.RawMessage
	if raw, ok := envelope[field]; !ok || json.Unmarshal(raw, &models) != nil {
		return nil, fmt.Errorf("invalid model catalog")
	}
	visible := make([]map[string]json.RawMessage, 0, len(models))
	for _, model := range models {
		var id, visibility, purpose string
		_ = json.Unmarshal(model[idField], &id)
		_ = json.Unmarshal(model["visibility"], &visibility)
		_ = json.Unmarshal(model["model_purpose"], &purpose)
		visibility, _ = service.ModelPresentation(id, visibility, purpose)
		if visibility != "hide" {
			visible = append(visible, model)
		}
	}
	envelope[field], _ = json.Marshal(visible)
	return json.Marshal(envelope)
}
