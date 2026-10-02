package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// ErrNoPinnedCodexModelsAccounts means that an enabled source has no account
// that can be used after membership, status, scheduling and expiry checks.
// Temporary rate-limit, overload and cooldown state is intentionally ignored:
// this source controls discovery only, not inference scheduling.
var ErrNoPinnedCodexModelsAccounts = errors.New("no usable pinned codex models manifest accounts")

func isPinnedCodexModelsAccountUsable(account *Account) bool {
	if account == nil || !account.IsActive() || !account.Schedulable {
		return false
	}
	return !account.AutoPauseOnExpired || account.ExpiresAt == nil || account.ExpiresAt.After(time.Now())
}

// mergeCodexModelsManifestBodies keeps the first response envelope and unions
// model descriptors in configured account order. A duplicate slug is resolved
// by the first successful account; malformed envelopes fail closed.
func mergeCodexModelsManifestBodies(bodies [][]byte) ([]byte, error) {
	if len(bodies) == 0 {
		return nil, errors.New("no codex models manifest bodies to merge")
	}
	var base map[string]json.RawMessage
	if err := json.Unmarshal(bodies[0], &base); err != nil {
		return nil, fmt.Errorf("decode base codex models manifest: %w", err)
	}
	if base == nil {
		return nil, errors.New("base codex models manifest is not a JSON object")
	}

	mergedModels := make([]json.RawMessage, 0, len(bodies)*8)
	seenSlug := make(map[string]struct{})
	seenSlugless := make(map[string]struct{})
	for _, body := range bodies {
		var envelope map[string]json.RawMessage
		if err := json.Unmarshal(body, &envelope); err != nil {
			return nil, fmt.Errorf("decode codex models manifest body: %w", err)
		}
		modelsRaw, ok := envelope["models"]
		if !ok {
			continue
		}
		var models []json.RawMessage
		if err := json.Unmarshal(modelsRaw, &models); err != nil {
			return nil, fmt.Errorf("decode codex models manifest models array: %w", err)
		}
		for _, raw := range models {
			var model struct {
				Slug string `json:"slug"`
			}
			slug := ""
			if err := json.Unmarshal(raw, &model); err == nil {
				slug = strings.TrimSpace(model.Slug)
			}
			if slug != "" {
				if _, exists := seenSlug[slug]; exists {
					continue
				}
				seenSlug[slug] = struct{}{}
			} else {
				fingerprint := string(bytes.TrimSpace(raw))
				if _, exists := seenSlugless[fingerprint]; exists {
					continue
				}
				seenSlugless[fingerprint] = struct{}{}
			}
			mergedModels = append(mergedModels, raw)
		}
	}

	encodedModels, err := json.Marshal(mergedModels)
	if err != nil {
		return nil, fmt.Errorf("encode merged codex models: %w", err)
	}
	base["models"] = encodedModels
	merged, err := json.Marshal(base)
	if err != nil {
		return nil, fmt.Errorf("encode merged codex models manifest: %w", err)
	}
	return merged, nil
}

// FetchPinnedCodexModelsManifest fetches only the configured OpenAI accounts,
// in parallel, and merges successful responses in configuration order. The
// caller applies catalog governance after this source stage.
func (s *OpenAIGatewayService) FetchPinnedCodexModelsManifest(ctx context.Context, group *Group, clientVersion string) (*OpenAIModelsResponse, *Account, error) {
	results, err := s.fetchPinnedOpenAIModels(ctx, group, func(ctx context.Context, account *Account) (*OpenAIModelsResponse, error) {
		manifest, err := s.FetchCodexModelsManifest(ctx, account, clientVersion, "")
		if err != nil {
			return nil, err
		}
		if err := s.CompleteAPIKeyCodexModelsManifestForClient(manifest, account); err != nil {
			return nil, err
		}
		if err := ApplyPinnedCodexModelsMapping(manifest, account, group); err != nil {
			return nil, err
		}
		return manifest, nil
	})
	if err != nil {
		return nil, nil, err
	}
	bodies := make([][]byte, 0, len(results))
	for _, result := range results {
		bodies = append(bodies, result.response.Body)
	}
	merged, err := mergeCodexModelsManifestBodies(bodies)
	if err != nil {
		return nil, nil, fmt.Errorf("merge pinned codex models manifests: %w", err)
	}
	return &OpenAIModelsResponse{Body: merged, ETag: codexModelsManifestBodyETag(merged)}, results[0].account, nil
}

type pinnedOpenAIModelsResult struct {
	account  *Account
	response *OpenAIModelsResponse
}

// fetchPinnedOpenAIModels centralizes membership and partial-failure policy.
// It deliberately does not use scheduler selection, so discovery remains
// deterministic even while a selected account is temporarily rate-limited.
func (s *OpenAIGatewayService) fetchPinnedOpenAIModels(ctx context.Context, group *Group, fetch func(context.Context, *Account) (*OpenAIModelsResponse, error)) ([]pinnedOpenAIModelsResult, error) {
	if s == nil || s.accountRepo == nil || group == nil {
		return nil, ErrNoPinnedCodexModelsAccounts
	}
	cfg := group.CodexModelsManifestConfig
	if group.Platform != PlatformOpenAI || !cfg.Enabled || len(cfg.AccountIDs) == 0 {
		return nil, ErrNoPinnedCodexModelsAccounts
	}
	members, err := s.accountRepo.ListByGroup(ctx, group.ID)
	if err != nil {
		return nil, fmt.Errorf("load pinned codex models manifest accounts: %w", err)
	}
	memberByID := make(map[int64]Account, len(members))
	for _, member := range members {
		memberByID[member.ID] = member
	}
	usable := make([]Account, 0, len(cfg.AccountIDs))
	for _, id := range cfg.AccountIDs {
		member, ok := memberByID[id]
		if !ok || member.Platform != PlatformOpenAI || !isPinnedCodexModelsAccountUsable(&member) {
			continue
		}
		usable = append(usable, member)
	}
	if len(usable) == 0 {
		return nil, ErrNoPinnedCodexModelsAccounts
	}

	results := make([]pinnedOpenAIModelsResult, len(usable))
	fetchErrs := make([]error, len(usable))
	var wg sync.WaitGroup
	for i := range usable {
		index := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			response, fetchErr := fetch(ctx, &usable[index])
			results[index] = pinnedOpenAIModelsResult{account: &usable[index], response: response}
			fetchErrs[index] = fetchErr
		}()
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	successes := make([]pinnedOpenAIModelsResult, 0, len(results))
	failedIDs := make([]int64, 0)
	var lastErr error
	for i, result := range results {
		if fetchErrs[i] != nil || result.response == nil {
			failedIDs = append(failedIDs, usable[i].ID)
			if fetchErrs[i] != nil {
				lastErr = fetchErrs[i]
			}
			continue
		}
		successes = append(successes, result)
	}
	if len(successes) == 0 {
		if lastErr == nil {
			lastErr = infraerrors.New(http.StatusBadGateway, "OPENAI_MODELS_UPSTREAM_FAILED", "pinned model discovery accounts all failed")
		}
		return nil, lastErr
	}
	if len(failedIDs) > 0 {
		slog.Warn("openai_models_pinned_partial_failure", "group_id", group.ID, "failed_account_ids", failedIDs)
	}
	return successes, nil
}
