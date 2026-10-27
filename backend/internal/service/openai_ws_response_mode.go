package service

import (
	"context"
	"errors"
	"time"
)

type ResponseMode int64

const (
	ResponseModeUnknown ResponseMode = iota
	ResponseModeNative
	ResponseModeBPS
	ResponseModeCloud
)

type responseModeBinding struct {
	mode      ResponseMode
	expiresAt time.Time
}

var ErrResponseModeUnavailable = errors.New("response producer mode is unavailable")

// The existing shared index can store integer routing metadata. A separate
// namespaced key keeps old account-affinity consumers intact. Shared reads are
// authoritative: a cache failure must not turn a known mode into an unknown one.
func (s *defaultOpenAIWSStateStore) BindResponseMode(ctx context.Context, groupID int64, responseID string, mode ResponseMode, ttl time.Duration) error {
	id := normalizeOpenAIWSResponseID(responseID)
	if id == "" || mode < ResponseModeNative || mode > ResponseModeCloud {
		return errors.New("invalid response mode binding")
	}
	ttl = normalizeOpenAIWSTTL(ttl)
	if s.cache != nil {
		cacheCtx, cancel := withOpenAIWSStateStoreRedisTimeout(ctx)
		defer cancel()
		return s.cache.SetSessionAccountID(cacheCtx, groupID, "mode:v1:"+openAIWSResponseAccountCacheKey(id), int64(mode), ttl)
	}
	s.maybeCleanup()
	key := openAIWSResponseAccountMapKey(groupID, id)
	s.responseModeMu.Lock()
	defer s.responseModeMu.Unlock()
	if s.responseModes == nil {
		s.responseModes = make(map[string]responseModeBinding)
	}
	ensureBindingCapacity(s.responseModes, key, openAIWSStateStoreMaxEntriesPerMap)
	s.responseModes[key] = responseModeBinding{mode: mode, expiresAt: time.Now().Add(ttl)}
	return nil
}

func (s *defaultOpenAIWSStateStore) GetResponseMode(ctx context.Context, groupID int64, responseID string) (ResponseMode, error) {
	id := normalizeOpenAIWSResponseID(responseID)
	if id == "" {
		return ResponseModeUnknown, nil
	}
	if s.cache != nil {
		cacheCtx, cancel := withOpenAIWSStateStoreRedisTimeout(ctx)
		defer cancel()
		value, err := s.cache.GetSessionAccountID(cacheCtx, groupID, "mode:v1:"+openAIWSResponseAccountCacheKey(id))
		if errors.Is(err, ErrStickySessionNotFound) || err == nil && value == 0 {
			return s.missingResponseMode(cacheCtx, groupID, id)
		}
		if err != nil {
			return ResponseModeUnknown, err
		}
		if value < int64(ResponseModeUnknown) || value > int64(ResponseModeCloud) {
			return ResponseModeUnknown, errors.New("invalid stored response mode")
		}
		return ResponseMode(value), nil
	}
	s.maybeCleanup()
	s.responseModeMu.RLock()
	binding := s.responseModes[openAIWSResponseAccountMapKey(groupID, id)]
	s.responseModeMu.RUnlock()
	if !time.Now().Before(binding.expiresAt) {
		return s.missingResponseMode(ctx, groupID, id)
	}
	return binding.mode, nil
}

// An external unknown response retains the provider's existing resolution
// contract. A gateway-owned response with missing producer metadata cannot be
// reclassified as native after eviction, expiry or an older cache publication.
func (s *defaultOpenAIWSStateStore) missingResponseMode(ctx context.Context, groupID int64, id string) (ResponseMode, error) {
	var accountID int64
	var err error
	if s.cache != nil {
		accountID, err = s.cache.GetSessionAccountID(ctx, groupID, openAIWSResponseAccountCacheKey(id))
	} else {
		accountID, err = s.GetResponseAccount(ctx, groupID, id)
	}
	if err != nil && !errors.Is(err, ErrStickySessionNotFound) {
		return ResponseModeUnknown, err
	}
	if accountID > 0 {
		return ResponseModeUnknown, ErrResponseModeUnavailable
	}
	_, _, owned, ownerErr := s.GetHTTPResponseOwner(ctx, groupID, id)
	if ownerErr != nil {
		return ResponseModeUnknown, ownerErr
	}
	if owned {
		return ResponseModeUnknown, ErrResponseModeUnavailable
	}
	return ResponseModeUnknown, nil
}
