package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
)

// Both public model lists and Codex-client downloads use the same SIWC permission
// catalog. Unlike the native catalog, expired SIWC entries are revalidated before
// returning: stale-while-revalidate could advertise a revoked permission catalog.
func (s *OpenAIGatewayService) fetchSIWCModelsCatalog(ctx context.Context, account *Account) (*OpenAIModelsResponse, error) {
	if s == nil || account == nil || !account.IsSIWCEnabled() {
		return nil, infraerrors.BadRequest("OPENAI_SIWC_ACCOUNT_REQUIRED", "SIWC account is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	oauth := s.siwcOAuthService()
	if oauth == nil {
		return nil, siwcRequired()
	}
	cache := s.openAITokenProvider.tokenCache
	// Re-read the independent grant even for a fresh catalog cache hit. Neither a
	// missing grant nor a failed refresh may fall back to the native Codex token.
	token, owner, err := oauth.siwcAccessToken(ctx, account, cache)
	if err != nil {
		return nil, err
	}
	proxy, err := oauth.siwcProxy(ctx, account)
	if err != nil {
		return nil, err
	}
	request := openAIModelsRequest{
		url:      "https://api.openai.com/v1/models",
		headers:  http.Header{"Authorization": {"Bearer " + token}, "Accept": {"application/json"}, "User-Agent": {"Sub2API"}},
		proxyURL: proxy, accountID: account.ID, credentialAccountID: account.ID,
		credentialAccount: account, accountConcurrency: account.Concurrency,
	}
	key := "siwc:" + owner + ":" + buildOpenAIModelsCacheKey(request)
	if cached, state := s.openAIModelsCache.get(key, time.Now()); state == openAIModelsCacheFresh {
		return cloneOpenAIModelsResponse(cached), nil
	}
	result := s.openAIModelsCache.refresh.DoChan(key, func() (any, error) {
		fetchCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), codexModelsManifestRequestTimeout)
		defer cancel()
		if err := validateSIWCInferenceGrant(fetchCtx, oauth, account, token, owner); err != nil {
			return nil, err
		}
		cached, state := s.openAIModelsCache.get(key, time.Now())
		if state == openAIModelsCacheFresh {
			return cached, nil
		}
		etag := ""
		if cached != nil {
			etag = cached.upstreamETag
		}
		response, err := s.fetchSIWCModelsUpstream(fetchCtx, request, etag)
		if err != nil {
			// Never retain a catalog after an authoritative authentication failure.
			if infraerrors.Code(err) == http.StatusUnauthorized || infraerrors.Code(err) == http.StatusForbidden {
				s.openAIModelsCache.mu.Lock()
				delete(s.openAIModelsCache.entries, key)
				s.openAIModelsCache.mu.Unlock()
			}
			return nil, err
		}
		if response.NotModified {
			if cached == nil || etag == "" {
				return nil, invalidOpenAIModelsList(fmt.Errorf("SIWC upstream returned 304 without a conditional cached catalog"))
			}
			response = cloneOpenAIModelsResponse(cached)
		} else {
			response.Body, err = filterSIWCModelCatalog(response.Body)
			if err != nil {
				return nil, invalidOpenAIModelsList(err)
			}
			response.ETag = codexModelsManifestBodyETag(response.Body)
		}
		if err := validateSIWCInferenceGrant(fetchCtx, oauth, account, token, owner); err != nil {
			return nil, err
		}
		s.openAIModelsCache.set(key, response, time.Now())
		return response, nil
	})
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case completed := <-result:
		if completed.Err != nil {
			return nil, completed.Err
		}
		response, ok := completed.Val.(*OpenAIModelsResponse)
		if !ok || response == nil {
			return nil, invalidOpenAIModelsList(fmt.Errorf("SIWC catalog result is missing"))
		}
		if err := validateSIWCInferenceGrant(ctx, oauth, account, token, owner); err != nil {
			return nil, err
		}
		return cloneOpenAIModelsResponse(response), nil
	}
}

// validateSIWCInferenceGrant checks the exact previously acquired bearer/owner
// against private durable storage. It never refreshes or substitutes credentials;
// callers use it before dialing/sending and before publishing cached results.
func validateSIWCInferenceGrant(ctx context.Context, oauth *OpenAIOAuthService, account *Account, token, owner string) error {
	if oauth == nil || !account.IsSIWCEnabled() || token == "" || owner == "" {
		return siwcRequired()
	}
	_, _, registration, grant, err := oauth.readSIWCRecord(ctx, account)
	if err != nil {
		return err
	}
	if registration == nil || grant == nil || grant.Rejected || grant.ExpiresAt <= time.Now().Unix() ||
		!openai.HasSIWCPermission(grant.Scope) || grant.AccessToken != token || siwcGrantOwner(registration) != owner {
		return siwcRequired()
	}
	return nil
}

// Reuse the existing HTTP client, proxy and response-size budget, but not the
// Codex auth-error machinery: a SIWC 401 must invalidate only its own grant.
func (s *OpenAIGatewayService) fetchSIWCModelsUpstream(ctx context.Context, request openAIModelsRequest, etag string) (*OpenAIModelsResponse, error) {
	if s.httpUpstream == nil {
		return nil, infraerrors.ServiceUnavailable("OPENAI_SIWC_MODELS_UNAVAILABLE", "SIWC model catalog client is unavailable")
	}
	req, err := http.NewRequestWithContext(WithHTTPUpstreamProfile(ctx, HTTPUpstreamProfileOpenAI), http.MethodGet, request.url, nil)
	if err != nil {
		return nil, err
	}
	req.Header = request.headers.Clone()
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	response, err := s.httpUpstream.Do(req, request.proxyURL, request.accountID, request.accountConcurrency)
	if response != nil && response.Body != nil {
		defer func() { _ = response.Body.Close() }()
	}
	if err != nil || response == nil {
		return nil, infraerrors.New(http.StatusBadGateway, "OPENAI_SIWC_MODELS_FAILED", "SIWC model catalog request failed")
	}
	if response.StatusCode == http.StatusUnauthorized {
		oauth := s.siwcOAuthService()
		if oauth != nil {
			token := strings.TrimPrefix(request.headers.Get("Authorization"), "Bearer ")
			if err := oauth.rejectSIWCAccessToken(ctx, request.credentialAccount, token, s.openAITokenProvider.tokenCache); err != nil {
				return nil, err
			}
		}
		return nil, infraerrors.New(http.StatusUnauthorized, "OPENAI_SIWC_MODELS_UNAUTHORIZED", "SIWC model catalog authorization was rejected")
	}
	if response.StatusCode == http.StatusNotModified {
		return &OpenAIModelsResponse{NotModified: true}, nil
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		status := http.StatusBadGateway
		if response.StatusCode >= 400 && response.StatusCode <= 599 {
			status = response.StatusCode
		}
		return nil, infraerrors.Newf(status, "OPENAI_SIWC_MODELS_FAILED", "SIWC model catalog upstream returned status %d", response.StatusCode)
	}
	if response.Body == nil {
		return nil, invalidOpenAIModelsList(fmt.Errorf("SIWC catalog body is missing"))
	}
	limit := resolveModelsListReadLimit(s.cfg)
	body, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil || int64(len(body)) > limit {
		return nil, invalidOpenAIModelsList(fmt.Errorf("SIWC catalog body could not be read within the configured limit"))
	}
	return &OpenAIModelsResponse{Body: body, upstreamETag: response.Header.Get("ETag")}, nil
}

func filterSIWCModelCatalog(body []byte) ([]byte, error) {
	envelope, entries, err := modelCatalogEntries(body, "models")
	if err != nil {
		return nil, err
	}
	if upstreamError := bytes.TrimSpace(envelope["error"]); len(upstreamError) > 0 && !bytes.Equal(upstreamError, []byte("null")) {
		return nil, fmt.Errorf("SIWC catalog contains an error envelope")
	}
	visible := make([]json.RawMessage, 0, len(entries))
	seen := make(map[string]bool, len(entries))
	for _, raw := range entries {
		var model struct{ Slug, Visibility string }
		if err := json.Unmarshal(raw, &model); err != nil {
			return nil, err
		}
		if model.Slug == "" || strings.TrimSpace(model.Slug) != model.Slug || strings.Contains(model.Slug, "*") {
			return nil, fmt.Errorf("SIWC catalog entry has no valid slug")
		}
		if seen[model.Slug] {
			return nil, fmt.Errorf("duplicate SIWC catalog model")
		}
		seen[model.Slug] = true
		if model.Visibility == "list" {
			visible = append(visible, raw)
		}
	}
	envelope["models"], err = json.Marshal(visible)
	if err != nil {
		return nil, err
	}
	return json.Marshal(envelope)
}

func (s *OpenAIGatewayService) fetchSIWCModelsList(ctx context.Context, account *Account) (*OpenAIModelsResponse, error) {
	response, err := s.fetchSIWCModelsCatalog(ctx, account)
	if err != nil {
		return nil, err
	}
	body, err := standardOpenAIModelsBody(response.Body, true)
	if err != nil {
		return nil, invalidOpenAIModelsList(err)
	}
	return &OpenAIModelsResponse{Body: body, ETag: codexModelsManifestBodyETag(body)}, nil
}
