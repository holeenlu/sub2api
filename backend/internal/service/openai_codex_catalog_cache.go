package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// A short retry window absorbs failed discovery bursts without extending LKG TTL.
const openAICatalogFailureTTL = 5 * time.Second

var errOpenAICatalogChanged = infraerrors.New(http.StatusServiceUnavailable, "OPENAI_CODEX_MODELS_SCOPE_CHANGED", "models discovery credentials or cache generation changed")

// Unlike capability scope, live discovery is bound to the actual credentials.
// Ordinary token rotation must not inherit an old credential's authentication
// failure. Agent task recovery changes its task, not the signing identity.
func openAICatalogCredentialScope(account *Account) string {
	if account == nil {
		return ""
	}
	credentials := account.Credentials
	if account.IsOpenAIAgentIdentity() {
		credentials = make(map[string]any, len(account.Credentials))
		for key, value := range account.Credentials {
			if key != "task_id" {
				credentials[key] = value
			}
		}
	}
	raw, err := json.Marshal(struct {
		ID                               int64
		Platform, Type, Identity, Status string
		Schedulable                      bool
		Credentials                      map[string]any
	}{account.ID, account.Platform, account.Type, codexCapabilityScope(account), account.Status, account.Schedulable, credentials})
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%x", sha256.Sum256(raw))
}

// Re-read the credential owner before cache use/send and after asynchronous I/O.
// A repository outage is not credential revocation and must not destroy LKG.
func (s *OpenAIGatewayService) validateOpenAICatalogCredential(ctx context.Context, request openAIModelsRequest) error {
	if request.credentialAccount == nil {
		// Cloud has its own grant authority and supplies X-Cloud-Generation.
		return nil
	}
	current := request.credentialAccount
	if s.accountRepo != nil {
		var err error
		current, err = s.accountRepo.GetByID(ctx, request.credentialAccountID)
		if err != nil && !errors.Is(err, ErrAccountNotFound) {
			return fmt.Errorf("revalidate models discovery credential: %w", err)
		}
	}
	if current == nil || current.ID != request.credentialAccountID || request.credentialScope == "" ||
		openAICatalogCredentialScope(current) != request.credentialScope {
		s.openAIModelsCache.invalidateCredential(request.credentialAccountID, request.credentialScope)
		return errOpenAICatalogChanged
	}
	return nil
}

func (c *openAIModelsCache) invalidateCredential(accountID int64, scope string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for key, entry := range c.entries {
		if entry.credentialAccountID == accountID && entry.credentialScope == scope {
			delete(c.entries, key)
		}
	}
}

// bind allocates a bounded local lifetime for this request profile. Different
// credential scopes have distinct keys. Only the authority recheck may revoke
// one: an older caller must never roll back a newer caller's observed scope.
func (c *openAIModelsCache) bind(key string, request openAIModelsRequest, now time.Time) uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = make(map[string]openAIModelsCacheEntry)
	}
	if entry, ok := c.entries[key]; ok && (now.Before(entry.staleUntil) || now.Before(entry.retryAfter)) {
		return entry.generation
	}
	var rejection openAIModelsCacheEntry
	for candidate, entry := range c.entries {
		if !now.Before(entry.staleUntil) && !now.Before(entry.retryAfter) {
			delete(c.entries, candidate)
			continue
		}
		if request.credentialScope != "" && entry.credentialAccountID == request.credentialAccountID &&
			entry.credentialScope == request.credentialScope && entry.manifest == nil && now.Before(entry.retryAfter) {
			var upstreamErr *codexModelsManifestUpstreamError
			if errors.As(entry.refreshErr, &upstreamErr) && (upstreamErr.statusCode == 401 || upstreamErr.statusCode == 403) {
				rejection = entry
			}
		}
	}
	if len(c.entries) >= openAIModelsCacheMaxEntries {
		oldest := ""
		var order uint64
		for candidate, entry := range c.entries {
			if oldest == "" || entry.order < order {
				oldest, order = candidate, entry.order
			}
		}
		delete(c.entries, oldest)
	}
	c.nextOrder++
	c.nextGeneration++
	c.entries[key] = openAIModelsCacheEntry{
		order: c.nextOrder, generation: c.nextGeneration, staleUntil: now.Add(openAIModelsCacheStaleTTL),
		credentialAccountID: request.credentialAccountID, credentialScope: request.credentialScope,
		refreshErr: rejection.refreshErr, retryAfter: rejection.retryAfter,
	}
	return c.nextGeneration
}

func (c *openAIModelsCache) refreshAllowed(key string, generation uint64, now time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok || entry.generation != generation {
		return errOpenAICatalogChanged
	}
	if now.Before(entry.retryAfter) {
		return entry.refreshErr
	}
	return nil
}

func (c *openAIModelsCache) recordFailure(key string, generation uint64, err error, now time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok || entry.generation != generation {
		return false
	}
	var upstreamErr *codexModelsManifestUpstreamError
	if !errors.As(err, &upstreamErr) {
		return true
	}
	if upstreamErr.statusCode == http.StatusUnauthorized || upstreamErr.statusCode == http.StatusForbidden {
		// A discovery rejection invalidates only this credential's catalogs.
		// It does not disable custom API keys or infer global account failure.
		for candidate, sibling := range c.entries {
			if candidate != key && (entry.credentialScope == "" || sibling.credentialAccountID != entry.credentialAccountID || sibling.credentialScope != entry.credentialScope) {
				continue
			}
			c.nextGeneration++
			sibling.generation = c.nextGeneration
			sibling.manifest = nil
			sibling.refreshErr, sibling.retryAfter = err, now.Add(openAICatalogFailureTTL)
			sibling.expiresAt, sibling.staleUntil = time.Time{}, sibling.retryAfter
			c.entries[candidate] = sibling
		}
	} else if upstreamErr.retryable && infraerrors.Reason(err) == "OPENAI_CODEX_MODELS_UPSTREAM_FAILED" {
		entry.refreshErr, entry.retryAfter = err, now.Add(openAICatalogFailureTTL)
		c.entries[key] = entry
	}
	return true
}

// publish is the only success writer. Even a 304 must own the same local
// generation; late successes cannot resurrect a revoked or evicted catalog.
func (c *openAIModelsCache) publish(key string, generation uint64, manifest *OpenAIModelsResponse, now time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok || entry.generation != generation {
		return false
	}
	if manifest.NotModified || len(manifest.Body) > openAIModelsCacheBodyLimit ||
		len(manifest.upstreamSourceBody) > openAIModelsCacheBodyLimit-len(manifest.Body) {
		// A successful, uncacheable replacement must not leave old capabilities.
		delete(c.entries, key)
		return true
	}
	c.nextOrder++
	entry.manifest, entry.order = cloneOpenAIModelsResponse(manifest), c.nextOrder
	entry.expiresAt, entry.staleUntil = now.Add(openAIModelsCacheTTL), now.Add(openAIModelsCacheStaleTTL)
	entry.refreshErr, entry.retryAfter = nil, time.Time{}
	c.entries[key] = entry
	return true
}
