package service

import (
	"context"
	"errors"
	"html"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
)

const APIKeyDataLimit = 1000

// Local IDs and user records are deliberately excluded from portable key backups.
type APIKeyDataGroup struct {
	Name     string `json:"name"`
	Platform string `json:"platform"`
}

type APIKeyDataRow struct {
	Name          string           `json:"name"`
	Key           string           `json:"key"`
	Group         *APIKeyDataGroup `json:"group"`
	Status        string           `json:"status"`
	IPWhitelist   []string         `json:"ip_whitelist"`
	IPBlacklist   []string         `json:"ip_blacklist"`
	Quota         float64          `json:"quota"`
	QuotaUsed     float64          `json:"quota_used"`
	ExpiresAt     *time.Time       `json:"expires_at"`
	RateLimit5h   float64          `json:"rate_limit_5h"`
	RateLimit1d   float64          `json:"rate_limit_1d"`
	RateLimit7d   float64          `json:"rate_limit_7d"`
	Usage5h       float64          `json:"usage_5h"`
	Usage1d       float64          `json:"usage_1d"`
	Usage7d       float64          `json:"usage_7d"`
	Window5hStart *time.Time       `json:"window_5h_start"`
	Window1dStart *time.Time       `json:"window_1d_start"`
	Window7dStart *time.Time       `json:"window_7d_start"`
}

type APIKeyData struct {
	Type       string          `json:"type"`
	Version    int             `json:"version"`
	ExportedAt time.Time       `json:"exported_at"`
	Keys       []APIKeyDataRow `json:"keys"`
}

type APIKeyDataImportError struct {
	Index   int    `json:"index"` // One-based row number; never echo credential material.
	Message string `json:"message"`
}

type APIKeyDataImportResult struct {
	Created int                     `json:"created"`
	Skipped int                     `json:"skipped"`
	Failed  int                     `json:"failed"`
	Errors  []APIKeyDataImportError `json:"errors"`
}

func (s *APIKeyService) ExportData(ctx context.Context, userID int64, ids []int64, filters APIKeyListFilters) (*APIKeyData, error) {
	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if user.IsVendor() {
		return nil, ErrVendorDenied
	}
	if len(ids) > APIKeyDataLimit {
		return nil, apiKeyDataInvalid("Export at most 1000 keys at a time")
	}
	var keys []APIKey
	if len(ids) > 0 {
		seen := make(map[int64]bool, len(ids))
		for _, id := range ids {
			if id <= 0 {
				return nil, apiKeyDataInvalid("Invalid key selection")
			}
			if seen[id] {
				continue
			}
			seen[id] = true
			key, err := s.apiKeyRepo.GetByID(ctx, id)
			if err != nil {
				return nil, err
			}
			keys = append(keys, *key)
		}
	} else {
		// Match the personal Key list; the repository rechecks live roles.
		filters.IncludeSuperAdminKeys = user.IsSuperAdmin()
		var page *pagination.PaginationResult
		keys, page, err = s.apiKeyRepo.ListByUserID(ctx, userID, pagination.PaginationParams{Page: 1, PageSize: APIKeyDataLimit, SortBy: "id", SortOrder: "asc"}, filters)
		if err != nil {
			return nil, err
		}
		if page.Total > APIKeyDataLimit {
			return nil, apiKeyDataInvalid("Export at most 1000 keys at a time; narrow the filters or select keys")
		}
	}
	// Reuse the same live management scope as read/update/delete, including
	// peer superadmin Keys. Never infer authority from the supplied filters.
	var foreignIDs []int64
	for _, key := range keys {
		if key.UserID != userID {
			foreignIDs = append(foreignIDs, key.ID)
		}
	}
	if len(foreignIDs) > 0 {
		allowed, err := s.VerifyOwnership(ctx, userID, foreignIDs)
		if err != nil {
			return nil, err
		}
		if len(allowed) != len(foreignIDs) {
			return nil, ErrAPIKeyNotFound
		}
	}
	data := &APIKeyData{Type: "sub2api-keys", Version: 1, ExportedAt: time.Now().UTC(), Keys: make([]APIKeyDataRow, 0, len(keys))}
	for _, key := range keys {
		row := APIKeyDataRow{
			Name: html.UnescapeString(key.Name), Key: key.Key, Status: key.Status,
			IPWhitelist: key.IPWhitelist, IPBlacklist: key.IPBlacklist,
			Quota: key.Quota, QuotaUsed: key.QuotaUsed, ExpiresAt: key.ExpiresAt,
			RateLimit5h: key.RateLimit5h, RateLimit1d: key.RateLimit1d, RateLimit7d: key.RateLimit7d,
			Usage5h: key.EffectiveUsage5h(), Usage1d: key.EffectiveUsage1d(), Usage7d: key.EffectiveUsage7d(),
			Window5hStart: key.Window5hStart, Window1dStart: key.Window1dStart, Window7dStart: key.Window7dStart,
		}
		if key.GroupID != nil {
			if key.Group == nil {
				return nil, apiKeyDataInvalid("A key group is unavailable for backup")
			}
			row.Group = &APIKeyDataGroup{Name: key.Group.Name, Platform: key.Group.Platform}
		}
		data.Keys = append(data.Keys, row)
	}
	return data, nil
}

func apiKeyDataInvalid(message string) error {
	return infraerrors.BadRequest("API_KEY_DATA_INVALID", message)
}

func (row *APIKeyDataRow) validate() error {
	if strings.TrimSpace(row.Name) == "" || len(html.EscapeString(row.Name)) > 100 || row.Key == "" || len(row.Key) > MaxAPIKeyCredentialBytes {
		return apiKeyDataInvalid("A valid key (at most 128 bytes) and a name (at most 100 bytes) are required")
	}
	switch row.Status {
	case StatusActive, "inactive", StatusAPIKeyDisabled, StatusAPIKeyQuotaExhausted, StatusAPIKeyExpired:
	default:
		return apiKeyDataInvalid("Invalid key status")
	}
	for _, value := range []float64{row.QuotaUsed, row.Usage5h, row.Usage1d, row.Usage7d} {
		if err := validateAPIKeyLimit(value); err != nil {
			return err
		}
	}
	return nil
}

// Restore all state before the single INSERT, so a disabled/expired key is never
// temporarily usable and a failed import cannot leave a partially configured key.
func (row *APIKeyDataRow) restore(key *APIKey) {
	key.Status, key.QuotaUsed, key.ExpiresAt = row.Status, row.QuotaUsed, row.ExpiresAt
	key.Usage5h, key.Usage1d, key.Usage7d = row.Usage5h, row.Usage1d, row.Usage7d
	key.Window5hStart, key.Window1dStart, key.Window7dStart = row.Window5hStart, row.Window1dStart, row.Window7dStart
}

func (s *APIKeyService) ImportData(ctx context.Context, userID int64, data APIKeyData) (*APIKeyDataImportResult, error) {
	if data.Type != "sub2api-keys" || data.Version != 1 || len(data.Keys) == 0 || len(data.Keys) > APIKeyDataLimit {
		return nil, apiKeyDataInvalid("Expected a sub2api-keys version 1 backup containing 1 to 1000 keys")
	}
	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if user.IsVendor() {
		return nil, ErrVendorDenied
	}
	var groups []Group
	for _, row := range data.Keys {
		if row.Group != nil {
			groups, err = s.GetAvailableGroups(ctx, userID)
			if err != nil {
				return nil, err
			}
			break
		}
	}
	result := &APIKeyDataImportResult{Errors: []APIKeyDataImportError{}}
	for index, row := range data.Keys {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		err := row.validate()
		if err == nil {
			err = s.ValidateCustomKey(row.Key)
		}
		if err == nil {
			existing, lookupErr := s.apiKeyRepo.GetByKey(ctx, row.Key)
			if lookupErr == nil && existing.UserID == userID {
				result.Skipped++
				continue
			}
			if lookupErr != nil && !errors.Is(lookupErr, ErrAPIKeyNotFound) {
				err = lookupErr
			}
		}
		var groupID *int64
		if err == nil && row.Group != nil {
			for _, group := range groups {
				if group.Name == row.Group.Name && group.Platform == row.Group.Platform {
					if groupID != nil {
						err = apiKeyDataInvalid("Destination group name and platform are ambiguous")
						break
					}
					id := group.ID
					groupID = &id
				}
			}
			if err == nil && groupID == nil {
				err = apiKeyDataInvalid("No permitted destination group matches this name and platform")
			}
		}
		if err == nil {
			_, err = s.Create(ctx, userID, CreateAPIKeyRequest{
				Name: row.Name, CustomKey: &row.Key, GroupID: groupID,
				IPWhitelist: row.IPWhitelist, IPBlacklist: row.IPBlacklist,
				Quota: row.Quota, RateLimit5h: row.RateLimit5h, RateLimit1d: row.RateLimit1d, RateLimit7d: row.RateLimit7d,
				importData: &row,
			})
		}
		if err != nil {
			result.Failed++
			result.Errors = append(result.Errors, APIKeyDataImportError{Index: index + 1, Message: infraerrors.Message(err)})
		} else {
			result.Created++
		}
	}
	return result, nil
}
