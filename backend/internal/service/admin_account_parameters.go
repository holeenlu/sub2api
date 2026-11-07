package service

import (
	"context"
	"maps"
)

type accountLifecycleEditKey struct{}
type accountLifecycleEdit struct{ Status, Schedulable bool }

func withAccountLifecycleEdit(ctx context.Context, status, schedulable bool) context.Context {
	return context.WithValue(ctx, accountLifecycleEditKey{}, accountLifecycleEdit{status, schedulable})
}
func AccountLifecycleEditFromContext(ctx context.Context) (bool, bool) {
	edit, _ := ctx.Value(accountLifecycleEditKey{}).(accountLifecycleEdit)
	return edit.Status, edit.Schedulable
}

// Bulk editing uses a key patch, while the single editor sends the complete
// configuration. Merge each target independently; null explicitly removes a
// setting and never copies another account's credentials or runtime state.
func mergeAccountParameterPatch(current, patch map[string]any) map[string]any {
	result := maps.Clone(current)
	if result == nil {
		result = make(map[string]any)
	}
	for key, value := range patch {
		if value == nil {
			delete(result, key)
		} else {
			result[key] = value
		}
	}
	return result
}

func (s *adminServiceImpl) bulkUpdateAccountParameters(ctx context.Context, input *BulkUpdateAccountsInput) (*BulkUpdateAccountsResult, error) {
	ids := input.AccountIDs
	if len(ids) == 0 && input.Filters != nil {
		var err error
		ids, err = s.resolveBulkUpdateTargetIDs(ctx, input.Filters)
		if err != nil {
			return nil, err
		}
	}
	ids = vendorUniqueIDs(ids)
	result := &BulkUpdateAccountsResult{SuccessIDs: []int64{}, FailedIDs: []int64{}, Results: []BulkUpdateAccountResult{}}
	if len(ids) == 0 {
		return result, nil
	}
	accounts, err := s.accountRepo.GetByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	byID := make(map[int64]*Account, len(accounts))
	for _, account := range accounts {
		if account != nil {
			byID[account.ID] = account
		}
	}
	// Reject missing/foreign targets before writing anything.
	if len(byID) != len(ids) {
		return nil, ErrAccountNotFound
	}
	for _, id := range ids {
		update := &UpdateAccountInput{ParameterPatch: true, Name: input.Name, Notes: input.Notes, Type: input.Type,
			ProxyID: input.ProxyID, Concurrency: input.Concurrency, Priority: input.Priority,
			RateMultiplier: input.RateMultiplier, LoadFactor: input.LoadFactor, Status: input.Status, Schedulable: input.Schedulable,
			GroupIDs: input.GroupIDs, ExpiresAt: input.ExpiresAt, AutoPauseOnExpired: input.AutoPauseOnExpired,
			ProbeEnabled: input.ProbeEnabled, RateSyncEnabled: input.RateSyncEnabled,
			SkipMixedChannelCheck: input.SkipMixedChannelCheck}
		if input.Credentials != nil {
			update.Credentials = maps.Clone(input.Credentials)
		}
		if input.Extra != nil {
			update.Extra = maps.Clone(input.Extra)
		}
		_, err := s.UpdateAccount(ctx, id, update)
		entry := BulkUpdateAccountResult{AccountID: id, Success: err == nil}
		if err != nil {
			entry.Error = err.Error()
			result.Failed++
			result.FailedIDs = append(result.FailedIDs, id)
		} else {
			result.Success++
			result.SuccessIDs = append(result.SuccessIDs, id)
		}
		result.Results = append(result.Results, entry)
	}
	return result, nil
}
