package authz

import (
	"bytes"
	"encoding/json"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"strings"
	"unicode"
)

func FieldName(key string) string {
	return strings.Map(func(r rune) rune {
		if r == '_' || r == '-' || r == '.' {
			return -1
		}
		minimum := r
		for next := unicode.SimpleFold(r); next != r; next = unicode.SimpleFold(next) {
			if next < minimum {
				minimum = next
			}
		}
		return unicode.ToLower(minimum)
	}, strings.TrimSpace(key))
}

// Only these settings can be delegated. The same map gates request fields and
// response fields; an unknown setting is never inherited by a limited admin.
func SettingFieldPermission(key string) string {
	switch FieldName(key) {
	case "sitename", "sitelogo", "sitesubtitle", "contactinfo", "docurl":
		return "settings.general"
	case "loginagreementdocuments", "loginagreementupdatedat":
		return "settings.content"
	}
	return ""
}

// Connection destination and TLS choices have one conservative classification
// shared by preflight and transactional checks, including provider URL maps.
func IsAccountTransportField(key string) bool {
	k := FieldName(key)
	return strings.Contains(k, "url") || strings.HasSuffix(k, "uri") || strings.Contains(k, "endpoint") || strings.Contains(k, "proxy") || strings.Contains(k, "tls") || strings.Contains(k, "ssl") || strings.Contains(k, "host") || strings.Contains(k, "domain") || k == "region" || k == "location"
}

func HasCredentialSecret(value any) bool {
	switch v := value.(type) {
	case map[string]any:
		for key, item := range v {
			for _, secret := range domain.SensitiveCredentialKeys {
				if FieldName(key) == FieldName(secret) {
					return true
				}
			}
			if HasCredentialSecret(item) {
				return true
			}
		}
	case []any:
		for _, item := range v {
			if HasCredentialSecret(item) {
				return true
			}
		}
	}
	return false
}

func FilterResponse(value any, path string, actor Subject, allowNewCredentials bool) any {
	return filterResponse(value, path, actor, allowNewCredentials, strings.HasPrefix(path, "/api/v1/admin/accounts") || path == "/api/v1/admin/proxies/:id/accounts")
}
func filterResponse(value any, path string, actor Subject, allowNewCredentials bool, accountScope bool) any {
	switch v := value.(type) {
	case []any:
		for i, item := range v {
			v[i] = filterResponse(item, path, actor, allowNewCredentials, accountScope)
		}
	case map[string]any:
		_, keyObject := v["user_id"]
		_, hasKey := v["key"]
		accountObject := accountScope
		_, proxyProtocol := v["protocol"]
		_, proxyHost := v["host"]
		_, proxyPort := v["port"]
		for key, item := range v {
			if key == "data" && ((path == "/api/v1/admin/groups/all" && !actor.Can("groups.read")) || (path == "/api/v1/admin/proxies/all" && !actor.Can("proxies.read"))) {
				if entries, ok := item.([]any); ok {
					for _, entry := range entries {
						if fields, ok := entry.(map[string]any); ok {
							for field := range fields {
								switch field {
								case "id", "name", "platform", "status", "subscription_type", "is_exclusive", "protocol":
								default:
									delete(fields, field)
								}
							}
						}
					}
				}
			}
			k := FieldName(key)
			if actor.Role != SuperAdmin && proxyProtocol && proxyHost && proxyPort && k == "username" {
				delete(v, key)
				continue
			}
			if !actor.Can("billing.cost.read") && IsAccountCostField(key, accountObject) {
				delete(v, key)
				continue
			}
			if k == "code" && !actor.Can("redeem_codes.export") && path != "/api/v1/admin/redeem-codes/generate" {
				_, hasType := v["type"]
				_, hasValue := v["value"]
				if hasType && hasValue {
					v[key] = "[redacted]"
					continue
				}
			}
			if k == "assignedbyuser" && actor.Role != SuperAdmin {
				if user, ok := item.(map[string]any); ok {
					if user["role"] == SuperAdmin {
						delete(v, key)
						delete(v, "assigned_by")
						continue
					}
					for field := range user {
						if field != "id" && field != "username" && field != "email" {
							delete(user, field)
						}
					}
				}
			}
			if k == "auditlogs" && actor.Role != SuperAdmin {
				delete(v, key)
				continue
			}
			if keyObject && hasKey && k == "key" && !allowNewCredentials {
				v[key] = "[redacted]"
				continue
			}
			if !allowNewCredentials {
				secret := false
				for _, field := range domain.SensitiveCredentialKeys {
					if FieldName(field) == k {
						secret = true
						break
					}
				}
				switch k {
				case "passwordhash", "totpsecret", "totpsecretencrypted", "clientsecret", "cookies":
					secret = true
				}
				if k == "apikey" {
					if _, object := item.(map[string]any); object {
						secret = false
					}
				}
				if k == "apikeys" {
					if items, ok := item.([]any); ok {
						for _, entry := range items {
							if _, text := entry.(string); text {
								secret = true
							}
						}
					}
				}
				if secret {
					delete(v, key)
					continue
				}
			}
			if k == "requestbody" && strings.Contains(path, "/audit-logs") && actor.Role != SuperAdmin {
				requestPath, _ := v["path"].(string)
				method, _ := v["method"].(string)
				rule, known := RuleFor(method, requestPath)
				raw, ok := item.(string)
				if !known || !rule.Allows(actor) || !ok {
					delete(v, key)
					continue
				}
				var parsed any
				decoder := json.NewDecoder(bytes.NewBufferString(raw))
				decoder.UseNumber()
				if decoder.Decode(&parsed) != nil {
					delete(v, key)
					continue
				}
				if requestPath == "/api/v1/admin/settings" {
					if fields, ok := parsed.(map[string]any); ok {
						for field := range fields {
							permission := SettingFieldPermission(field)
							if permission == "" || !actor.Can(permission+".read") {
								delete(fields, field)
							}
						}
					}
				}
				encoded, err := json.Marshal(FilterResponse(parsed, requestPath, actor, false))
				if err != nil {
					delete(v, key)
					continue
				}
				v[key] = string(encoded)
				continue
			}
			if (k == "requestbody" || k == "responsebody" || k == "upstreamresponsebody" || k == "fullprompt" || k == "prompt") && !strings.Contains(path, "/audit-logs") && !actor.Can("prompt_audit.content.read") {
				delete(v, key)
				continue
			}
			if path == "/api/v1/admin/settings" && key == "data" && actor.Role != SuperAdmin {
				if fields, ok := item.(map[string]any); ok {
					for field := range fields {
						permission := SettingFieldPermission(field)
						if permission == "" || !actor.Can(permission+".read") {
							delete(fields, field)
						}
					}
				}
			}
			childScope := accountObject
			switch k {
			case "groups", "group", "user", "users", "apikey", "apikeys", "assignedbyuser":
				childScope = false
			case "account", "accounts":
				childScope = true
			}
			v[key] = filterResponse(item, path, actor, allowNewCredentials, childScope)
		}
	}
	return value
}

// MarshalResponse projects a copy of the response DTO, preserving shared cache
// objects and exact JSON numbers. The normal response helper owns this boundary.
func MarshalResponse(value any, path string, actor Subject, allowNewCredentials bool) ([]byte, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var copy any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err = decoder.Decode(&copy); err != nil {
		return nil, err
	}
	return json.Marshal(FilterResponse(copy, path, actor, allowNewCredentials))
}
