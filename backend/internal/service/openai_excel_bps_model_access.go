package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

var excelBPSModelID = regexp.MustCompile(`^gpt-[a-z0-9]+(?:[._-][a-z0-9]+)*$`)

// Convert only the official access response. Empty/denied access is authoritative;
// malformed envelopes and HTTP failures cannot manufacture a local allowlist.
func excelBPSAccessModelsBody(raw []byte) ([]byte, error) {
	var envelope struct {
		Allowed *bool `json:"allowed"`
		Catalog *struct {
			Models     *[]json.RawMessage `json:"models"`
			Restricted []string           `json:"restricted_models"`
		} `json:"model_catalog"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil || envelope.Allowed == nil {
		return nil, fmt.Errorf("invalid Excel BPS access response")
	}
	models := make([]map[string]any, 0)
	if *envelope.Allowed {
		if envelope.Catalog == nil || envelope.Catalog.Models == nil {
			return nil, fmt.Errorf("missing Excel BPS model catalog")
		}
		restricted := map[string]bool{}
		for _, id := range envelope.Catalog.Restricted {
			restricted[id] = true
		}
		seen := map[string]bool{}
		for _, raw := range *envelope.Catalog.Models {
			var entry struct {
				ID        string `json:"id"`
				Label     string `json:"label"`
				Available *bool  `json:"available"`
				Enabled   *bool  `json:"enabled"`
				Picker    *bool  `json:"model_picker_enabled"`
				Policy    *struct {
					State string `json:"state"`
				} `json:"policy"`
				Efforts []struct {
					Value string `json:"value"`
				} `json:"efforts"`
			}
			if json.Unmarshal(raw, &entry) != nil || !excelBPSModelID.MatchString(entry.ID) || restricted[entry.ID] || seen[entry.ID] {
				continue
			}
			if entry.Available != nil && !*entry.Available || entry.Enabled != nil && !*entry.Enabled || entry.Picker != nil && !*entry.Picker || entry.Policy != nil && (entry.Policy.State == "disabled" || entry.Policy.State == "unconfigured") {
				continue
			}
			model := map[string]any{"id": entry.ID, "object": "model", "owned_by": "openai", "created": 0}
			if label := strings.TrimSpace(entry.Label); label != "" {
				model["display_name"] = label
			}
			efforts := make([]string, 0)
			effortSeen := map[string]bool{}
			for _, effort := range entry.Efforts {
				switch effort.Value {
				case "low", "medium", "high", "xhigh":
					if !effortSeen[effort.Value] {
						efforts = append(efforts, effort.Value)
						effortSeen[effort.Value] = true
					}
				}
			}
			if entry.Efforts != nil {
				model["reasoning_efforts"] = efforts
				model["supported_reasoning_levels"] = efforts
			}
			models = append(models, model)
			seen[entry.ID] = true
		}
	}
	return json.Marshal(map[string]any{"object": "list", "data": models})
}

// FetchExcelBPSModelsList uses a separate fresh-only cache scoped by bearer,
// account and proxy identity. Expired permissions are never served stale.
func (s *OpenAIGatewayService) FetchExcelBPSModelsList(ctx context.Context, account *Account) (*OpenAIModelsResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s == nil || s.httpUpstream == nil || account == nil || !account.IsOpenAI() || !account.IsExcelBPSEnabled() {
		return nil, fmt.Errorf("excel BPS model discovery is unavailable")
	}
	token, err := s.getExcelBPSAccessToken(ctx, account)
	if err != nil {
		if IsExcelAuthorizationError(err) {
			return nil, err
		}
		return nil, infraerrors.New(502, "EXCEL_BPS_MODELS_AUTH_UNAVAILABLE", "Excel BPS credentials are unavailable")
	}
	accountID := excelBPSAccountID(account, token)
	if accountID == "" {
		return nil, fmt.Errorf("excel BPS model discovery requires an account ID")
	}
	request, err := newExcelBPSRequest(ctx, nil, token, accountID)
	if err != nil {
		return nil, err
	}
	request.Method = http.MethodGet
	request.Body = nil
	request.ContentLength = 0
	request.Header.Set("Accept", "application/json")
	request.URL.Path = "/basispoints/api/responses/access"
	request.URL.RawQuery = "include_models=true"
	proxy := upstreamModelsProxyURL(account)
	identity := openAIModelsRequest{url: request.URL.String(), headers: request.Header, proxyURL: proxy, accountID: account.ID, credentialAccountID: account.ID, standardModelsList: true}
	key := buildOpenAIModelsCacheKey(identity)
	generation := s.excelBPSModelsCache.bind(key, identity, time.Now())
	if cached, state := s.excelBPSModelsCache.get(key, time.Now()); state == openAIModelsCacheFresh {
		return s.excelBPSModelsWithoutRejectedModels(cached, account)
	}
	fetch := func(fetchCtx context.Context) (*OpenAIModelsResponse, error) {
		if cached, state := s.excelBPSModelsCache.get(key, time.Now()); state == openAIModelsCacheFresh {
			return cached, nil
		}
		fetchCtx, cancel := context.WithTimeout(fetchCtx, 5*time.Second)
		defer cancel()

		req := request.Clone(WithHTTPUpstreamRedirectsDisabled(WithHTTPUpstreamProfile(fetchCtx, HTTPUpstreamProfileExcelBPS)))
		response, err := s.httpUpstream.Do(req, proxy, account.ID, account.Concurrency)
		if err != nil {
			if response != nil && response.Body != nil {
				_ = response.Body.Close()
			}
			return nil, infraerrors.New(502, "EXCEL_BPS_MODELS_UPSTREAM_FAILED", "Excel BPS model discovery request failed")
		}
		if response == nil || response.Body == nil {
			return nil, fmt.Errorf("excel BPS model discovery returned no response")
		}
		defer func() { _ = response.Body.Close() }()
		stop := context.AfterFunc(fetchCtx, func() { _ = response.Body.Close() })
		defer stop()
		if response.StatusCode != http.StatusOK {
			return nil, infraerrors.New(502, "EXCEL_BPS_MODELS_UPSTREAM_REJECTED", fmt.Sprintf("Excel BPS model discovery returned HTTP %d", response.StatusCode))
		}
		raw, err := io.ReadAll(io.LimitReader(response.Body, openAIModelsCacheBodyLimit+1))
		if err != nil || len(raw) > openAIModelsCacheBodyLimit {
			return nil, fmt.Errorf("excel BPS model catalog could not be read")
		}
		body, err := excelBPSAccessModelsBody(raw)
		if err != nil {
			return nil, err
		}
		if err := fetchCtx.Err(); err != nil {
			return nil, err
		}
		manifest := &OpenAIModelsResponse{Body: body, ETag: codexModelsManifestBodyETag(body)}
		if !s.excelBPSModelsCache.publish(key, generation, manifest, time.Now()) {
			return nil, errOpenAICatalogChanged
		}
		return manifest, nil
	}

	flightKey := fmt.Sprintf("%s:%d", key, generation)
	ch := s.excelBPSModelsCache.refresh.DoChan(flightKey, func() (any, error) { return fetch(context.Background()) })
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case result := <-ch:
		if result.Err != nil {
			return nil, result.Err
		}
		manifest, ok := result.Val.(*OpenAIModelsResponse)
		if !ok || manifest == nil {
			return nil, fmt.Errorf("excel BPS model discovery returned no catalog")
		}
		return s.excelBPSModelsWithoutRejectedModels(manifest, account)
	}
}

// Model discovery is not proof that inference still permits the model. Apply
// the existing per-account cooldown even to a fresh cached access catalog.
func (s *OpenAIGatewayService) excelBPSModelsWithoutRejectedModels(manifest *OpenAIModelsResponse, account *Account) (*OpenAIModelsResponse, error) {
	result := openAIModelsResponseForClient(manifest, "")
	var catalog struct {
		Object string            `json:"object"`
		Data   []json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(result.Body, &catalog); err != nil {
		return nil, err
	}
	filtered := make([]json.RawMessage, 0, len(catalog.Data))
	for _, raw := range catalog.Data {
		var model struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(raw, &model); err != nil {
			return nil, err
		}
		if !s.isExcelBPSUpstreamModelCoolingDown(account.ID, model.ID) {
			filtered = append(filtered, raw)
		}
	}
	if len(filtered) != len(catalog.Data) {
		catalog.Data = filtered
		body, err := json.Marshal(catalog)
		if err != nil {
			return nil, err
		}
		result.Body, result.ETag = body, codexModelsManifestBodyETag(body)
	}
	return result, nil
}

func mergeExcelBPSAccountModels(native, bps []byte, account *Account) ([]byte, error) {
	_, bpsEntries, err := modelCatalogEntries(bps, "data")
	if err != nil {
		return nil, err
	}
	var nativeEntries []json.RawMessage
	if len(native) > 0 {
		_, nativeEntries, err = modelCatalogEntries(native, "data")
		if err != nil {
			return nil, err
		}
	}
	merged := make([]json.RawMessage, 0, len(nativeEntries)+len(bpsEntries))
	for _, batch := range []struct {
		entries []json.RawMessage
		bps     bool
	}{{nativeEntries, false}, {bpsEntries, true}} {
		for _, raw := range batch.entries {
			var entry struct {
				ID string `json:"id"`
			}
			if json.Unmarshal(raw, &entry) != nil {
				continue
			}
			if account.isExcelBPSUpstreamModelEnabled(entry.ID) == batch.bps {
				merged = append(merged, raw)
			}
		}
	}
	return json.Marshal(map[string]any{"object": "list", "data": merged})
}

// Apply permissions/efforts to raw upstream IDs before public-name projection.
func applyExcelBPSEffortsToManifest(body, access []byte, account *Account) ([]byte, error) {
	envelope, entries, err := modelCatalogEntries(body, "models")
	if err != nil {
		return nil, err
	}
	_, models, err := modelCatalogEntries(access, "data")
	if err != nil {
		return nil, err
	}
	byID := make(map[string]map[string]json.RawMessage, len(models))
	for _, raw := range models {
		var model map[string]json.RawMessage
		_ = json.Unmarshal(raw, &model)
		var id string
		_ = json.Unmarshal(model["id"], &id)
		byID[id] = model
	}
	kept := make([]json.RawMessage, 0, len(entries))
	for _, raw := range entries {
		var model map[string]json.RawMessage
		_ = json.Unmarshal(raw, &model)
		var slug string
		_ = json.Unmarshal(model["slug"], &slug)
		if account.isExcelBPSUpstreamModelEnabled(slug) {
			capability, allowed := byID[slug]
			if !allowed {
				continue
			}
			if label := capability["display_name"]; label != nil {
				model["display_name"] = label
			}
			if levels := capability["reasoning_efforts"]; levels != nil {
				var efforts []string
				_ = json.Unmarshal(levels, &efforts)
				levels := make([]map[string]string, 0, len(efforts))
				for _, effort := range efforts {
					levels = append(levels, map[string]string{"effort": effort, "description": effort})
				}
				model["supported_reasoning_levels"], _ = json.Marshal(levels)
				if len(efforts) > 0 {
					model["default_reasoning_level"], _ = json.Marshal(efforts[0])
				}
			}
			model["multi_agent_version"] = json.RawMessage("null")
			model["multi_agent_reasoning_effort"] = json.RawMessage("null")
			raw, _ = json.Marshal(model)
		}
		kept = append(kept, raw)
	}
	envelope["models"], _ = json.Marshal(kept)
	return json.Marshal(envelope)
}

// Independent BPS discovery applies to the actual selected model route. Native
// models keep their own source; a native source failure cannot invent BPS access.
func (s *OpenAIGatewayService) fetchExcelBPSAccountModels(ctx context.Context, account *Account) (*OpenAIModelsResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !account.hasExcelBPSSelectedModels() {
		if !account.IsExcelOAuth() {
			return s.fetchNativeOpenAIModelsList(ctx, account)
		}
		body := []byte("{\"object\":\"list\",\"data\":[]}")
		return &OpenAIModelsResponse{Body: body, ETag: codexModelsManifestBodyETag(body)}, nil
	}
	access, accessErr := s.FetchExcelBPSModelsList(ctx, account)
	if accessErr != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if account.IsExcelOAuth() {
			return nil, accessErr
		}
		access = &OpenAIModelsResponse{Body: []byte(`{"object":"list","data":[]}`)}
	}
	var native []byte
	if !account.IsExcelOAuth() {
		response, err := s.fetchNativeOpenAIModelsList(ctx, account)
		if err == nil {
			native = response.Body
		} else if ctx.Err() != nil {
			return nil, ctx.Err()
		}
	}
	body, err := mergeExcelBPSAccountModels(native, access.Body, account)
	if err != nil {
		return nil, err
	}
	if accessErr != nil {
		_, entries, parseErr := modelCatalogEntries(body, "data")
		if parseErr != nil || len(entries) == 0 {
			return nil, accessErr
		}
	}
	return &OpenAIModelsResponse{Body: body, ETag: codexModelsManifestBodyETag(body)}, nil
}

func (s *OpenAIGatewayService) fetchExcelBPSCodexManifest(ctx context.Context, account *Account, clientVersion, ifNoneMatch string) (*OpenAIModelsResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !account.hasExcelBPSSelectedModels() {
		if !account.IsExcelOAuth() {
			return s.fetchNativeCodexModelsManifest(ctx, account, clientVersion, ifNoneMatch)
		}
		body := []byte("{\"models\":[]}")
		return openAIModelsResponseForClient(&OpenAIModelsResponse{Body: body, ETag: codexModelsManifestBodyETag(body)}, ifNoneMatch), nil
	}
	access, accessErr := s.FetchExcelBPSModelsList(ctx, account)
	if accessErr != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if account.IsExcelOAuth() {
			return nil, accessErr
		}
		// A BPS catalog failure cannot grant its selected models to Native.
		// Keep those entries absent while trying the independent Native source.
		access = &OpenAIModelsResponse{Body: []byte("{\"object\":\"list\",\"data\":[]}")}
	}
	bpsBody, err := mergeExcelBPSAccountModels(nil, access.Body, account)
	if err != nil {
		return nil, err
	}
	_, entries, err := modelCatalogEntries(bpsBody, "data")
	if err != nil {
		return nil, err
	}
	var body []byte
	if len(entries) == 0 {
		body = []byte(`{"models":[]}`)
	} else {
		body = convertOpenAIModelListToCodexManifestForAccount(bpsBody, account)
	}
	body, err = applyExcelBPSEffortsToManifest(body, access.Body, account)
	if err != nil {
		return nil, err
	}
	if !account.IsExcelOAuth() {
		native, nativeErr := s.fetchNativeCodexModelsManifest(ctx, account, clientVersion, "")
		if nativeErr == nil {
			envelope, models, err := modelCatalogEntries(native.Body, "models")
			if err != nil {
				return nil, err
			}
			kept := make([]json.RawMessage, 0, len(models))
			for _, raw := range models {
				var entry struct {
					Slug string `json:"slug"`
				}
				_ = json.Unmarshal(raw, &entry)
				if !account.isExcelBPSUpstreamModelEnabled(entry.Slug) {
					kept = append(kept, raw)
				}
			}
			envelope["models"], _ = json.Marshal(kept)
			nativeBody, _ := json.Marshal(envelope)
			body, err = mergeCodexModelsManifestBodies([][]byte{nativeBody, body})
			if err != nil {
				return nil, err
			}
		} else if ctx.Err() != nil {
			return nil, ctx.Err()
		}
	}
	if accessErr != nil {
		_, available, parseErr := modelCatalogEntries(body, "models")
		if parseErr != nil || len(available) == 0 {
			return nil, accessErr
		}
	}
	manifest := &OpenAIModelsResponse{Body: body, ETag: codexModelsManifestBodyETag(body)}
	return openAIModelsResponseForClient(manifest, ifNoneMatch), nil
}
