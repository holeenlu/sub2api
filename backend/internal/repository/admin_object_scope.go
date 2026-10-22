package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/authz"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

func (r *userRepository) ResolveAdminTargetUsers(ctx context.Context, kind string, ids []int64) ([]int64, error) {
	queries := map[string]string{
		"users":         "SELECT id FROM users WHERE id=ANY($1)",
		"group_rates":   "SELECT user_id FROM user_group_rate_multipliers WHERE group_id=ANY($1)",
		"api_keys":      "SELECT user_id FROM api_keys WHERE id=ANY($1)",
		"subscriptions": "SELECT user_id FROM user_subscriptions WHERE id=ANY($1)",
		"orders":        "SELECT user_id FROM payment_orders WHERE id=ANY($1)",
		"redeem_codes":  "SELECT used_by FROM redeem_codes WHERE id=ANY($1) AND used_by IS NOT NULL",
	}
	query, ok := queries[kind]
	if !ok {
		return nil, service.ErrAdminPermissionDenied
	}
	rows, err := clientFromContext(ctx, r.client).QueryContext(ctx, query, pq.Array(ids))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var users []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		users = append(users, id)
	}
	return users, rows.Err()
}

func (r *userRepository) CheckAdminAccountTransport(ctx context.Context, ids []int64, body map[string]any) error {
	return r.checkAdminAccountTransport(ctx, ids, body, false)
}

func (r *userRepository) checkAdminAccountTransport(ctx context.Context, ids []int64, body map[string]any, replacing bool) error {
	if len(ids) == 0 {
		return nil
	}
	rows, err := clientFromContext(ctx, r.client).QueryContext(ctx, `SELECT COALESCE(credentials::text,'{}'),COALESCE(extra::text,'{}'),COALESCE(proxy_id,0),rate_multiplier FROM accounts WHERE id=ANY($1) AND deleted_at IS NULL`, pq.Array(ids))
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var raw, extraRaw string
		var proxyID int64
		var rate float64
		if err := rows.Scan(&raw, &extraRaw, &proxyID, &rate); err != nil {
			return err
		}
		var credentials, extra map[string]any
		if json.Unmarshal([]byte(raw), &credentials) != nil || json.Unmarshal([]byte(extraRaw), &extra) != nil {
			return fmt.Errorf("Invalid account configuration")
		}
		for key, value := range body {
			switch authz.FieldName(key) {
			case "ratemultiplier":
				if value != nil {
					next, err := strconv.ParseFloat(fmt.Sprint(value), 64)
					if err != nil {
						return service.ErrAdminPermissionDenied
					}
					actor, _ := authz.FromContext(ctx)
					if next != rate && !actor.Can("billing.rates.update") {
						return service.ErrAdminPermissionDenied
					}
				}
			case "proxyid":
				newID := int64(0)
				if value != nil {
					n, err := strconv.ParseInt(fmt.Sprint(value), 10, 64)
					if err != nil {
						return service.ErrAdminPermissionDenied
					}
					newID = n
				}
				if newID != proxyID {
					return service.ErrAdminPermissionDenied
				}
			case "credentials", "extra":
				current := credentials
				if authz.FieldName(key) == "extra" {
					current = extra
				}
				if fields, ok := value.(map[string]any); ok {
					if replacing && authz.FieldName(key) == "credentials" {
						for field, old := range current {
							if authz.IsAccountTransportField(field) {
								if !reflect.DeepEqual(old, fields[field]) {
									return service.ErrAdminPermissionDenied
								}
							}
						}
					}
					for field, next := range fields {
						if authz.IsAccountTransportField(field) {
							if !reflect.DeepEqual(current[field], next) {
								return service.ErrAdminPermissionDenied
							}
						}
					}
				}
			}
		}
	}
	return rows.Err()
}

func (r *userRepository) CheckAdminProxyTransport(ctx context.Context, ids []int64, body map[string]any) error {
	// A fallback proxy can receive the same saved account credential as its
	// primary. Traverse that existing relationship instead of adding approval state.
	rows, err := clientFromContext(ctx, r.client).QueryContext(ctx, `WITH RECURSIVE used(id) AS (
 SELECT proxy_id FROM accounts WHERE proxy_id IS NOT NULL AND deleted_at IS NULL
 UNION SELECT proxy_fallback_origin_id FROM accounts WHERE proxy_fallback_origin_id IS NOT NULL AND deleted_at IS NULL
 UNION SELECT p.backup_proxy_id FROM proxies p JOIN used u ON p.id=u.id
 WHERE p.backup_proxy_id IS NOT NULL AND p.deleted_at IS NULL
 ) SELECT to_jsonb(p)::text FROM proxies p WHERE p.id=ANY($1) AND p.id IN (SELECT id FROM used)`, pq.Array(ids))
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return err
		}
		var current map[string]any
		if json.Unmarshal([]byte(raw), &current) != nil {
			return service.ErrAdminPermissionDenied
		}
		for key, value := range body {
			k := authz.FieldName(key)
			if k == "deletingproxy" {
				return service.ErrAdminPermissionDenied
			}
			switch k {
			case "host", "port", "protocol", "username", "password", "fallbackmode", "backupproxyid", "expiresat":
				var previous any
				for field, old := range current {
					if authz.FieldName(field) == k {
						previous = old
						break
					}
				}
				if (k == "username" || k == "password") && ((previous == nil && value == "") || (previous == "" && value == nil)) {
					continue
				}
				if k == "expiresat" {
					oldTime, oldErr := time.Parse(time.RFC3339Nano, fmt.Sprint(previous))
					newTime, newErr := time.Parse(time.RFC3339Nano, fmt.Sprint(value))
					if oldErr == nil && newErr == nil && oldTime.Equal(newTime) {
						continue
					}
					if seconds, err := strconv.ParseInt(fmt.Sprint(value), 10, 64); oldErr == nil && err == nil && oldTime.Unix() == seconds {
						continue
					}
				}
				// Compare numbers independently of json.Number/float64 decoding.
				if fmt.Sprint(previous) != fmt.Sprint(value) {
					return service.ErrAdminPermissionDenied
				}
			}
		}
	}
	return rows.Err()
}
