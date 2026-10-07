package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/authz"
)

type adminObjectRepository interface {
	ResolveAdminTargetUsers(context.Context, string, []int64) ([]int64, error)
	CheckAdminAccountTransport(context.Context, []int64, map[string]any) error
	CheckAdminProxyTransport(context.Context, []int64, map[string]any) error
}

func adminInputID(value any) (int64, error) {
	var raw string
	switch v := value.(type) {
	case string:
		raw = v
	case json.Number:
		raw = string(v)
	case float64:
		raw = strconv.FormatFloat(v, 'f', -1, 64)
	default:
		return 0, fmt.Errorf("Invalid object ID")
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("Invalid object ID")
	}
	return id, nil
}

func collectAdminIDs(value any, names map[string]string, out map[string][]int64) error {
	switch v := value.(type) {
	case map[string]any:
		for key, item := range v {
			if kind, ok := names[authz.FieldName(key)]; ok && item != nil {
				values, ok := item.([]any)
				if !ok {
					values = []any{item}
				}
				for _, value := range values {
					id, err := adminInputID(value)
					if err != nil {
						return err
					}
					out[kind] = append(out[kind], id)
				}
			} else if authz.FieldName(key) == "entries" || authz.FieldName(key) == "items" || authz.FieldName(key) == "users" || authz.FieldName(key) == "updates" {
				if err := collectAdminIDs(item, names, out); err != nil {
					return err
				}
			}
		}
	case []any:
		for _, item := range v {
			if err := collectAdminIDs(item, names, out); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *UserService) CheckAdminResourceTargets(ctx context.Context, path, id, userID string, body map[string]any) ([]int64, error) {
	actor, ok := authz.FromContext(ctx)
	if !ok || actor.Role != RoleAdmin {
		return nil, nil
	}
	repo, ok := s.userRepo.(adminObjectRepository)
	if !ok {
		return nil, ErrAdminPolicyUnavailable
	}
	scoped := false
	for _, prefix := range []string{"/api/v1/admin/users", "/api/v1/admin/subscriptions", "/api/v1/admin/payment/orders", "/api/v1/admin/affiliates", "/api/v1/admin/redeem-codes", "/api/v1/admin/api-keys", "/api/v1/admin/risk-control/users", "/api/v1/admin/user-attributes/batch"} {
		if strings.HasPrefix(path, prefix) {
			scoped = true
		}
	}
	if strings.Contains(path, "/rate-multipliers") || strings.Contains(path, "/rpm-overrides") {
		scoped = true
	}
	if !scoped {
		return nil, nil
	}
	targets := map[string][]int64{}
	names := map[string]string{"userid": "users", "userids": "users", "subscriptionid": "subscriptions", "subscriptionids": "subscriptions", "apikeyid": "api_keys", "apikeyids": "api_keys"}
	if strings.Contains(path, "/subscriptions") {
		names["ids"] = "subscriptions"
	}
	if err := collectAdminIDs(body, names, targets); err != nil {
		return nil, err
	}
	if userID != "" {
		uid, err := adminInputID(userID)
		if err != nil {
			return nil, err
		}
		targets["users"] = append(targets["users"], uid)
	}
	if id != "" {
		kind := ""
		switch {
		case strings.Contains(path, "/rate-multipliers") || strings.Contains(path, "/rpm-overrides"):
			kind = "group_rates"
		case strings.HasPrefix(path, "/api/v1/admin/users/"), strings.HasPrefix(path, "/api/v1/admin/risk-control/users/"):
			kind = "users"
		case strings.HasPrefix(path, "/api/v1/admin/subscriptions/"):
			kind = "subscriptions"
		case strings.HasPrefix(path, "/api/v1/admin/api-keys/"):
			kind = "api_keys"
		case strings.HasPrefix(path, "/api/v1/admin/payment/orders/"):
			kind = "orders"
		case strings.HasPrefix(path, "/api/v1/admin/redeem-codes/"):
			kind = "redeem_codes"
		}
		if kind != "" {
			n, err := adminInputID(id)
			if err != nil {
				return nil, err
			}
			targets[kind] = append(targets[kind], n)
		}
	}
	var all []int64
	for kind, ids := range targets {
		if len(ids) > 10000 {
			return nil, fmt.Errorf("Too many target objects")
		}
		userIDs, err := repo.ResolveAdminTargetUsers(ctx, kind, ids)
		if err != nil {
			return nil, err
		}
		for _, uid := range userIDs {
			if uid == actor.UserID && (strings.Contains(path, "/refund") || strings.HasSuffix(path, "/balance") || strings.HasSuffix(path, "/withdraw")) {
				return nil, ErrAdminPermissionDenied
			}
			all = append(all, uid)
			user, err := s.userRepo.GetByIDIncludeDeleted(ctx, uid)
			if err != nil || user == nil || user.Role == RoleSuperAdmin {
				return nil, ErrAdminPermissionDenied
			}
		}
	}
	return all, nil
}

func adminTransportIDs(id string, body map[string]any) ([]int64, error) {
	targets := map[string][]int64{}
	if err := collectAdminIDs(body, map[string]string{"ids": "objects", "accountids": "objects", "proxyids": "objects"}, targets); err != nil {
		return nil, err
	}
	if id != "" {
		n, err := adminInputID(id)
		if err != nil {
			return nil, err
		}
		targets["objects"] = append(targets["objects"], n)
	}
	return targets["objects"], nil
}
func (s *UserService) CheckAdminAccountTransport(ctx context.Context, path, id string, body map[string]any) error {
	repo, ok := s.userRepo.(adminObjectRepository)
	if !ok {
		return ErrAdminPolicyUnavailable
	}
	ids, err := adminTransportIDs(id, body)
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil
	}
	return repo.CheckAdminAccountTransport(ctx, ids, body)
}
func (s *UserService) CheckAdminProxyTransport(ctx context.Context, id string, body map[string]any) error {
	repo, ok := s.userRepo.(adminObjectRepository)
	if !ok {
		return ErrAdminPolicyUnavailable
	}
	ids, err := adminTransportIDs(id, body)
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil
	}
	return repo.CheckAdminProxyTransport(ctx, ids, body)
}
