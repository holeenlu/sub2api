package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// FetchOpenAIModelsList discovers a single account's raw public model catalog.
// API keys use the standard endpoint; OAuth reuses the authenticated, cached
// Codex source. Account mappings and group policy are applied after this cache.
func (s *OpenAIGatewayService) FetchOpenAIModelsList(ctx context.Context, account *Account) (catalogResponse *OpenAIModelsResponse, catalogErr error) {
	var catalogSource *Account
	defer func() {
		if catalogErr == nil {
			s.rememberModelCatalog(account, catalogSource, catalogResponse, false)
		}
	}()
	if s == nil || account == nil {
		return nil, infraerrors.New(http.StatusInternalServerError, "OPENAI_MODELS_ACCOUNT_REQUIRED", "OpenAI account is required")
	}
	credentialAccount, err := resolveCredentialAccount(ctx, s.accountRepo, account)
	if err != nil {
		return nil, fmt.Errorf("resolve model list credentials: %w", err)
	}
	catalogSource = credentialAccount
	if credentialAccount.IsOpenAIOAuth() {
		clientVersion := CodexCanonicalClientVersion()
		if s.settingService != nil {
			clientVersion = s.settingService.GetOpenAICodexClientVersion(ctx)
		}
		response, err := s.FetchCodexModelsManifest(ctx, account, clientVersion, "")
		if err != nil {
			return nil, err
		}
		body, err := standardOpenAIModelsBody(response.Body, true)
		if err != nil {
			return nil, invalidOpenAIModelsList(err)
		}
		return &OpenAIModelsResponse{Body: body, ETag: codexModelsManifestBodyETag(body)}, nil
	}
	req, err := buildOpenAIAPIKeyModelsRequest(ctx, credentialAccount, s.validateUpstreamBaseURL)
	if err != nil {
		return nil, infraerrors.Newf(http.StatusBadGateway, "OPENAI_MODELS_REQUEST_INVALID", "cannot build upstream model list request: %v", err)
	}
	request := openAIModelsRequest{
		url: req.URL.String(), headers: req.Header,
		proxyURL: upstreamModelsProxyURL(account), accountID: account.ID,
		credentialAccountID: credentialAccount.ID, credentialAccount: credentialAccount,
		accountConcurrency: account.Concurrency, useAPIKeyUpstream: true,
		standardModelsList: true,
	}
	response, err := s.fetchCachedOpenAIModels(ctx, request, func(fetchCtx context.Context, etag string) (*OpenAIModelsResponse, error) {
		response, err := s.fetchOpenAIModelsUpstream(fetchCtx, request, etag)
		if err != nil || response.NotModified {
			return response, err
		}
		body, err := standardOpenAIModelsBody(response.Body, false)
		if err != nil {
			return nil, invalidOpenAIModelsList(err)
		}
		response.Body = body
		response.ETag = codexModelsManifestBodyETag(body)
		return response, nil
	}, "")
	if err != nil {
		return nil, err
	}
	if response.NotModified {
		return nil, invalidOpenAIModelsList(fmt.Errorf("upstream returned 304 without a cached catalog"))
	}
	return response, nil
}

func invalidOpenAIModelsList(err error) error {
	return &codexModelsManifestUpstreamError{
		err:       infraerrors.Newf(http.StatusBadGateway, "OPENAI_MODELS_UPSTREAM_INVALID", "invalid upstream model list: %v", err),
		retryable: true,
	}
}

// modelCatalogEntries requires a real array. An empty array is authoritative;
// absent fields, null and error envelopes must not become successful empty lists.
func modelCatalogEntries(body []byte, field string) (map[string]json.RawMessage, []json.RawMessage, error) {
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, nil, err
	}
	raw := bytes.TrimSpace(envelope[field])
	if len(raw) == 0 || raw[0] != '[' {
		return nil, nil, fmt.Errorf("missing or invalid %s array", field)
	}
	var entries []json.RawMessage
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, nil, err
	}
	return envelope, entries, nil
}

// standardOpenAIModelsBody projects either representation onto the OpenAI /v1/models
// shape. Manifest entries are rebuilt to a minimal entry set; plain OpenAI lists keep
// their upstream fields. Both keep display_name, which the admin picker and alias
// projection read.
func standardOpenAIModelsBody(body []byte, fromManifest bool) ([]byte, error) {
	field, idField := "data", "id"
	if fromManifest {
		field, idField = "models", "slug"
	}
	_, entries, err := modelCatalogEntries(body, field)
	if err != nil {
		return nil, err
	}
	models := make([]json.RawMessage, 0, len(entries))
	seen := make(map[string]struct{}, len(entries))
	for _, raw := range entries {
		var entry map[string]json.RawMessage
		if err := json.Unmarshal(raw, &entry); err != nil || entry == nil {
			return nil, fmt.Errorf("model entry must be an object")
		}
		var id string
		if err := json.Unmarshal(entry[idField], &id); err != nil || strings.TrimSpace(id) == "" {
			return nil, fmt.Errorf("model entry has no valid %s", idField)
		}
		id = strings.TrimSpace(id)
		if _, ok := seen[id]; ok || strings.Contains(id, "*") {
			continue
		}
		seen[id] = struct{}{}
		var visibility, purpose string
		_ = json.Unmarshal(entry["visibility"], &visibility)
		_ = json.Unmarshal(entry["model_purpose"], &purpose)
		visibility, purpose = ModelPresentation(id, visibility, purpose)
		if fromManifest {
			// Codex manifest entries carry dozens of client-only fields (instructions,
			// model_messages, reasoning levels) that must not reach the public catalog,
			// so the entry is rebuilt from scratch. The display name is the one manifest
			// field user-facing surfaces need (admin test picker, mapping aliases), so it
			// is carried over explicitly instead of being dropped with the rest.
			var displayName string
			if err := json.Unmarshal(entry["display_name"], &displayName); err == nil {
				displayName = strings.TrimSpace(displayName)
			}
			entry = make(map[string]json.RawMessage)
			if displayName != "" {
				entry["display_name"], _ = json.Marshal(displayName)
			}
		}
		entry["id"], _ = json.Marshal(id)
		if visibility != "" {
			entry["visibility"], _ = json.Marshal(visibility)
		}
		if purpose != "" {
			entry["model_purpose"], _ = json.Marshal(purpose)
		}
		entry["object"] = json.RawMessage(`"model"`)
		if len(entry["created"]) == 0 || string(entry["created"]) == "null" {
			entry["created"] = json.RawMessage(`0`)
		}
		if len(entry["owned_by"]) == 0 || string(entry["owned_by"]) == "null" {
			entry["owned_by"] = json.RawMessage(`"openai"`)
		}
		encoded, err := json.Marshal(entry)
		if err != nil {
			return nil, err
		}
		models = append(models, encoded)
	}
	return json.Marshal(struct {
		Object string            `json:"object"`
		Data   []json.RawMessage `json:"data"`
	}{Object: "list", Data: models})
}

// projectAccountModelsBody applies the same public-name mapping to both model
// representations while retaining the source entry's metadata. It never changes
// the shared response and never synthesizes models absent from this account.
func projectAccountModelsBody(body []byte, account *Account, group *Group, codex bool) ([]byte, error) {
	if account.IsOpenAIPassthroughEnabled() || len(account.GetModelMapping()) == 0 {
		return body, nil
	}
	field, idField := "data", "id"
	if codex {
		field, idField = "models", "slug"
	}
	envelope, entries, err := modelCatalogEntries(body, field)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]json.RawMessage, len(entries))
	candidates := make([]string, 0, len(entries))
	for _, raw := range entries {
		var entry map[string]json.RawMessage
		if json.Unmarshal(raw, &entry) != nil {
			continue
		}
		var id string
		if json.Unmarshal(entry[idField], &id) != nil {
			continue
		}
		id = strings.TrimSpace(id)
		if id == "" || strings.Contains(id, "*") {
			continue
		}
		// Apply the allowlist to public names after mapping, not upstream targets.
		if codex {
			if isCodexDedicatedMediaModel(id) {
				continue
			}
		}
		if _, ok := byID[id]; !ok {
			byID[id] = raw
			candidates = append(candidates, id)
		}
	}
	aliases := make([]string, 0, len(account.GetModelMapping()))
	for alias := range account.GetModelMapping() {
		aliases = append(aliases, alias)
	}
	sort.Strings(aliases)
	candidates = append(candidates, aliases...)
	if group.ModelAllowlistEnabled() {
		candidates = append(candidates, group.ModelAllowlist.Models...)
	}
	projected := make([]json.RawMessage, 0, len(candidates))
	seen := make(map[string]struct{}, len(candidates))
	for _, id := range candidates {
		id = strings.TrimSpace(id)
		if id == "" || strings.Contains(id, "*") {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		target, matched := account.ResolveMappedModel(id)
		raw, available := byID[strings.TrimSpace(target)]
		allowed := matched || (account.IsOpenAIModelMappingAliases() && account.IsModelSupported(id))
		if !available || !allowed {
			continue
		}
		seen[id] = struct{}{}
		var entry map[string]json.RawMessage
		if err := json.Unmarshal(raw, &entry); err != nil {
			return nil, err
		}
		var visibility, purpose string
		_ = json.Unmarshal(entry["visibility"], &visibility)
		_ = json.Unmarshal(entry["model_purpose"], &purpose)
		visibility, purpose = ModelPresentation(target, visibility, purpose)
		if visibility != "" {
			entry["visibility"], _ = json.Marshal(visibility)
		}
		if purpose != "" {
			entry["model_purpose"], _ = json.Marshal(purpose)
		}
		entry[idField], _ = json.Marshal(id)
		if id != target {
			entry["display_name"], _ = json.Marshal(id)
		}
		encoded, err := json.Marshal(entry)
		if err != nil {
			return nil, err
		}
		projected = append(projected, encoded)
	}
	envelope[field], err = json.Marshal(projected)
	if err != nil {
		return nil, err
	}
	return json.Marshal(envelope)
}

// ApplyPinnedCodexModelsMapping applies the selected source account's public
// aliases and account model policy before the manifests are merged. The final
// response still goes through the shared group catalog in
// MergeGroupConfiguredCodexModels, so this source cannot widen group access.
func ApplyPinnedCodexModelsMapping(response *OpenAIModelsResponse, account *Account, group *Group) error {
	if response == nil || group == nil || group.Platform != PlatformOpenAI || !group.CodexModelsManifestConfig.Enabled {
		return nil
	}
	body, err := projectAccountModelsBody(response.Body, account, group, true)
	if err != nil {
		return err
	}
	response.Body = body
	response.ETag = codexModelsManifestBodyETag(body)
	return nil
}
