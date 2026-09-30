package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"
)

const ModelCatalogSnapshotExtraKey = "model_catalog_snapshot"
const ModelCatalogSettingsKey = "model_catalog_settings"
const ModelCatalogRegistryKey = "model_catalog_registry"

var ErrModelCatalogUnavailable = errors.New("model catalog unavailable")
var ErrModelCatalogBusy = errors.New("model catalog refresh already running")
var ErrModelCatalogScopeChanged = errors.New("model catalog credential scope changed")

// Entry describes scoped upstream facts. It is not an administrator grant.
type CatalogFieldEvidence struct {
	Value      json.RawMessage `json:"value"`
	State      string          `json:"state"`
	Source     string          `json:"source"`
	Revision   string          `json:"revision"`
	ObservedAt time.Time       `json:"observed_at"`
	ExpiresAt  time.Time       `json:"expires_at"`
}
type ModelCatalogEntry struct {
	UpstreamNamespace   string                          `json:"upstream_namespace,omitempty"`
	SourceAccountID     int64                           `json:"source_account_id,omitempty"`
	Observations        []CatalogMediaObservation       `json:"observations,omitempty"`
	Fields              map[string]CatalogFieldEvidence `json:"fields,omitempty"`
	ID                  string                          `json:"id"`
	DisplayName         string                          `json:"display_name"`
	Platform            string                          `json:"platform"`
	Kind                string                          `json:"kind"`
	Lifecycle           string                          `json:"lifecycle"`
	ShutdownDate        string                          `json:"shutdown_date,omitempty"`
	Access              string                          `json:"access"`
	Source              string                          `json:"source"`
	Metadata            UpstreamModelMetadata           `json:"metadata"`
	Missing             []string                        `json:"missing"`
	Endpoints           []string                        `json:"endpoints"`
	RecommendedPriority int                             `json:"recommended_priority,omitempty"`
	CodexModel          json.RawMessage                 `json:"codex_model,omitempty"`
}

type ModelCatalogSnapshot struct {
	Warnings      []string            `json:"warnings,omitempty"`
	Policy        *ModelCatalogPolicy `json:"policy,omitempty"`
	Revision      string              `json:"revision"`
	ScopeRevision string              `json:"-"`
	AccountID     int64               `json:"account_id,omitempty"`
	Platform      string              `json:"platform"`
	UpdatedAt     time.Time           `json:"updated_at"`
	CheckedAt     time.Time           `json:"checked_at"`
	Status        string              `json:"status"`
	LastError     string              `json:"last_error,omitempty"`
	PriceRevision string              `json:"price_revision,omitempty"`
	Models        []ModelCatalogEntry `json:"models"`
}

type ModelCatalogRelease struct {
	ID               int64     `json:"id"`
	Revision         string    `json:"revision"`
	PreviousRevision string    `json:"previous_revision,omitempty"`
	PriceRevision    string    `json:"price_revision,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	Operation        string    `json:"operation"`
}

type ModelCatalogRepository interface {
	PricingAudits(context.Context, bool) ([]map[string]any, error)
	ObserveMedia(context.Context, int64, string, CatalogMediaObservation) error
	MediaObservations(context.Context, int64, string) ([]CatalogMediaObservation, error)
	SaveJob(context.Context, ModelCatalogJob) error
	ReadJob(context.Context, string) (*ModelCatalogJob, error)
	Current(context.Context, string) (*ModelCatalogSnapshot, error)
	Claim(context.Context, string, int64, string, string, string, time.Time, bool) (bool, error)
	Publish(context.Context, string, string, *ModelCatalogSnapshot, time.Time) error
	Fail(context.Context, string, string, string, time.Time) error
	ListPlatform(context.Context, string) ([]ModelCatalogSnapshot, error)
	History(context.Context, string, int) ([]ModelCatalogRelease, error)
	Rollback(context.Context, string, string, string) error
	SavePrices(context.Context, string, json.RawMessage) error
}

type ModelCatalogSettings struct {
	DeletionAlertPercent    int     `json:"deletion_alert_percent"`
	PriorityAccountIDs      []int64 `json:"priority_account_ids,omitempty"`
	PriorityIntervalSeconds int     `json:"priority_interval_seconds"`
	PriceIntervalSeconds    int     `json:"price_interval_seconds"`
	Enabled                 bool    `json:"enabled"`
	IntervalSeconds         int     `json:"interval_seconds"`
	TimeoutSeconds          int     `json:"timeout_seconds"`
	Concurrency             int     `json:"concurrency"`
	StaleSeconds            int     `json:"stale_seconds"`
}

// Selection/alias edits re-project facts, not invalidate upstream discovery.
func modelCatalogScope(account, source *Account) string {
	if account == nil || source == nil {
		return ""
	}
	credentials := make(map[string]any)
	for key, value := range source.Credentials {
		if key == "model_mapping" || key == "compact_model_mapping" || key == "model_mapping_mode" {
			continue
		}
		credentials[key] = value
	}
	catalogStableOAuthCredentials(source, credentials)
	proxyID := account.ProxyID
	if proxyID == nil {
		proxyID = source.ProxyID
	}
	return modelCatalogHash(struct {
		Credentials    map[string]any
		SourceID       int64
		Platform, Type string
		ProxyID        *int64
	}{credentials, source.ID, account.Platform, source.Type, proxyID})
}

func modelCatalogHash(v any) string {
	body, _ := json.Marshal(v)
	hash := sha256.Sum256(body)
	return hex.EncodeToString(hash[:])
}

func modelCatalogSourceKey(accountID int64) string {
	return "account:" + strconv.FormatInt(accountID, 10)
}

func modelCatalogEntryKind(id string, m UpstreamModelMetadata) string {
	if m.ModelKind != "" {
		return m.ModelKind
	}
	for _, kind := range []string{"image", "video", "audio"} {
		if stringSliceContains(m.OutputModalities, kind) {
			return kind
		}
	}
	lower := strings.ToLower(id)
	switch {
	case strings.HasPrefix(lower, "gpt-image-"), strings.HasPrefix(lower, "dall-e"), strings.Contains(lower, "imagen"), strings.Contains(lower, "imagine-image"):
		return "image"
	case strings.HasPrefix(lower, "sora"), strings.HasPrefix(lower, "veo"), strings.Contains(lower, "imagine-video"):
		return "video"
	case strings.Contains(lower, "embedding"):
		return "embedding"
	case strings.Contains(lower, "tts"), strings.Contains(lower, "whisper"), strings.Contains(lower, "audio"), strings.Contains(lower, "transcrib"):
		return "audio"
	default:
		return "chat"
	}
}

func modelCatalogLifecycle(m UpstreamModelMetadata, now time.Time) string {
	if m.ShutdownDate != "" {
		if at, err := time.Parse("2006-01-02", m.ShutdownDate); err == nil && !now.Before(at) {
			return "retired"
		}
	}
	if m.Deprecated != nil && *m.Deprecated {
		return "deprecated"
	}
	return "active"
}

func modelCatalogMissing(kind string, m UpstreamModelMetadata) []string {
	missing := []string{}
	if kind != "chat" {
		return missing
	}
	if m.Reasoning == nil {
		missing = append(missing, "reasoning")
	}
	if m.Reasoning != nil && *m.Reasoning && len(m.SupportedReasoningLevels) == 0 {
		missing = append(missing, "supported_reasoning_levels")
	}
	if len(m.InputModalities) == 0 {
		missing = append(missing, "input_modalities")
	}
	if m.ContextWindow <= 0 {
		missing = append(missing, "context_window")
	}
	return missing
}

func modelCatalogNormalizeEntries(entries []ModelCatalogEntry) []ModelCatalogEntry {
	seen := make(map[string]bool)
	out := make([]ModelCatalogEntry, 0, len(entries))
	for _, entry := range entries {
		entry.ID = strings.TrimSpace(entry.ID)
		if entry.ID == "" || strings.Contains(entry.ID, "*") || seen[entry.Platform+"\x00"+entry.ID] {
			continue
		}
		seen[entry.Platform+"\x00"+entry.ID] = true
		if entry.DisplayName == "" {
			entry.DisplayName = entry.ID
		}
		if entry.Missing == nil {
			entry.Missing = []string{}
		}
		if entry.Endpoints == nil {
			entry.Endpoints = []string{}
		}
		out = append(out, entry)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].RecommendedPriority != out[j].RecommendedPriority {
			return out[i].RecommendedPriority < out[j].RecommendedPriority
		}
		return out[i].ID < out[j].ID
	})
	return out
}
