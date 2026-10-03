package handler

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func writeOpenAIModelsError(c *gin.Context, status int, errorType, message string) {
	c.JSON(status, gin.H{"error": gin.H{"type": errorType, "message": message}})
}

func writeOpenAIModelsResponse(c *gin.Context, manifest *service.OpenAIModelsResponse) {
	if isClientModelCatalog(c) {
		body, err := projectClientModelCatalog(manifest.Body)
		if err != nil || manifest.NotModified {
			writeOpenAIModelsError(c, http.StatusBadGateway, "upstream_error", "Failed to build client model catalog")
			return
		}
		copy := *manifest
		copy.Body, copy.ETag = body, service.CodexModelsManifestETag(body)
		copy.NotModified = service.CodexModelsManifestETagMatches(clientCatalogValidator(c), copy.ETag)
		manifest = &copy
	}
	if c.Param("model") != "" {
		writeRetrievedModel(c, manifest.Body)
		return
	}
	if manifest.ETag != "" {
		c.Header("ETag", manifest.ETag)
	}
	if manifest.NotModified {
		c.Status(http.StatusNotModified)
		c.Writer.WriteHeaderNow()
		return
	}
	c.Data(http.StatusOK, "application/json", manifest.Body)
}

// Both discovery endpoints consume the same final catalogue, after group/platform
// selection and allowlist filtering. Preserve every field on the selected entry.
func writeModelsListResponse(c *gin.Context, models any, capabilities ...map[string]service.ModelListCapabilities) {
	{
		var entries []map[string]json.RawMessage
		encoded, err := json.Marshal(models)
		if err != nil || json.Unmarshal(encoded, &entries) != nil {
			writeOpenAIModelsError(c, http.StatusInternalServerError, "api_error", "Failed to encode model catalogue")
			return
		}
		for _, entry := range entries {
			var id string
			_ = json.Unmarshal(entry["id"], &id)
			if len(capabilities) > 0 {
				fields := capabilities[0][id]
				// The dedicated DTO contains capability fields only. Keep existing
				// identifiers and provider-specific response fields unchanged.
				body, _ := json.Marshal(fields)
				var extra map[string]json.RawMessage
				_ = json.Unmarshal(body, &extra)
				for key, value := range extra {
					entry[key] = value
				}
			}
			var visibility, purpose string
			_ = json.Unmarshal(entry["visibility"], &visibility)
			_ = json.Unmarshal(entry["model_purpose"], &purpose)
			visibility, purpose = service.ModelPresentation(id, visibility, purpose)
			if visibility != "" {
				entry["visibility"], _ = json.Marshal(visibility)
			}
			if purpose != "" {
				entry["model_purpose"], _ = json.Marshal(purpose)
			}
		}
		models = entries
	}
	response := gin.H{"object": "list", "data": models}
	if c.Param("model") == "" && !isClientModelCatalog(c) {
		c.JSON(http.StatusOK, response)
		return
	}
	body, err := json.Marshal(response)
	if err != nil {
		writeOpenAIModelsError(c, http.StatusInternalServerError, "api_error", "Failed to encode model catalogue")
		return
	}
	if isClientModelCatalog(c) {
		body, err = projectClientModelCatalog(body)
		if err != nil {
			writeOpenAIModelsError(c, http.StatusInternalServerError, "api_error", "Failed to build client model catalog")
			return
		}
	}
	if c.Param("model") == "" {
		c.Data(http.StatusOK, "application/json", body)
		return
	}
	writeRetrievedModel(c, body)
}

func writeRetrievedModel(c *gin.Context, body []byte) {
	var catalog struct {
		Data []json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &catalog); err != nil {
		writeOpenAIModelsError(c, http.StatusBadGateway, "upstream_error", "Invalid model catalogue")
		return
	}
	modelID := c.Param("model")
	for _, raw := range catalog.Data {
		var model map[string]json.RawMessage
		if err := json.Unmarshal(raw, &model); err != nil {
			writeOpenAIModelsError(c, http.StatusBadGateway, "upstream_error", "Invalid model catalogue entry")
			return
		}
		var id string
		if err := json.Unmarshal(model["id"], &id); err != nil {
			writeOpenAIModelsError(c, http.StatusBadGateway, "upstream_error", "Invalid model catalogue ID")
			return
		}
		if id == modelID {
			c.Data(http.StatusOK, "application/json", raw)
			return
		}
	}
	c.JSON(http.StatusNotFound, gin.H{"error": gin.H{
		"type": "invalid_request_error", "code": "model_not_found", "param": "model",
		"message": fmt.Sprintf("Model %q does not exist or is not available for this group", modelID),
	}})
}
