package admin

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// Resolve backup dependencies before creating a primary proxy. This preserves
// fallback chains regardless of JSON order and fails on missing/ambiguous links.
func (h *AccountHandler) importDataProxies(ctx context.Context, input []DataProxy, result *DataImportResult) (map[string]int64, error) {
	existing, err := h.listAllProxies(ctx)
	if err != nil {
		return nil, err
	}
	available := map[string]int64{}
	byIdentity := map[string]service.Proxy{}
	names := map[string][]string{}
	for _, p := range existing {
		key := buildProxyKey(p.Protocol, p.Host, p.Port, p.Username, p.Password)
		available[key], byIdentity[key] = p.ID, p
		names[p.Name] = append(names[p.Name], key)
	}
	rows := map[string]DataProxy{}
	order := []string{}
	importedNames := map[string][]string{}
	for _, p := range input {
		key := p.ProxyKey
		if key == "" {
			key = buildProxyKey(p.Protocol, p.Host, p.Port, p.Username, p.Password)
			p.ProxyKey = key
		}
		if previous, exists := rows[key]; exists {
			if !reflect.DeepEqual(previous, p) {
				return nil, errors.New("Conflicting definitions for one proxy in the backup")
			}
			continue
		}
		rows[key] = p
		order = append(order, key)
		importedNames[p.Name] = append(importedNames[p.Name], key)
	}
	state := map[string]int{}
	failures := map[string]error{}
	var restore func(string) (int64, error)
	restore = func(key string) (int64, error) {
		row, exists := rows[key]
		if !exists {
			if id, ok := available[key]; ok {
				return id, nil
			}
			return 0, errors.New("Backup proxy reference was not found")
		}
		if state[key] == 2 {
			return available[key], failures[key]
		}
		if state[key] == 1 {
			return 0, errors.New("Proxy fallback chain contains a cycle")
		}
		state[key] = 1
		canonical := buildProxyKey(row.Protocol, row.Host, row.Port, row.Username, row.Password)
		fail := func(err error) (int64, error) {
			state[key] = 2
			failures[key] = err
			delete(available, key)
			delete(available, canonical)
			return 0, err
		}
		if err := validateDataProxy(row); err != nil {
			return fail(err)
		}
		mode := row.FallbackMode
		if mode == "" {
			mode = service.FallbackModeNone
		}
		if mode != service.FallbackModeNone && mode != service.FallbackModeProxy && mode != service.FallbackModeDirect {
			return fail(errors.New("Invalid proxy fallback mode"))
		}
		var backupID *int64
		if mode == service.FallbackModeProxy {
			backupKey := row.BackupProxyKey
			if backupKey == "" {
				matches := importedNames[row.BackupProxyName]
				if len(matches) == 0 {
					matches = names[row.BackupProxyName]
				}
				if len(matches) != 1 {
					return fail(errors.New("Backup proxy name is missing or ambiguous"))
				}
				backupKey = matches[0]
			}
			id, e := restore(backupKey)
			if e != nil {
				return fail(e)
			}
			backupID = &id
		}
		var expires *time.Time
		if row.ExpiresAt != nil && *row.ExpiresAt > 0 {
			v := time.Unix(*row.ExpiresAt, 0).UTC()
			expires = &v
		}
		p, reused := byIdentity[canonical]
		if p.VendorID != nil {
			return fail(errors.New("Supplier proxies require scoped supplier import"))
		}
		if !reused {
			created, e := h.adminService.CreateProxy(ctx, &service.CreateProxyInput{Name: defaultProxyName(row.Name), Protocol: row.Protocol, Host: row.Host, Port: row.Port, Username: row.Username, Password: row.Password, ExpiresAt: expires, FallbackMode: mode, BackupProxyID: backupID, ExpiryWarnDays: row.ExpiryWarnDays})
			if e != nil {
				return fail(e)
			}
			p = *created
		}
		status := normalizeProxyStatus(row.Status)
		if status == "" {
			status = p.Status
		}
		if status == "" {
			status = service.StatusActive
		}
		_, err := h.adminService.UpdateProxy(ctx, p.ID, &service.UpdateProxyInput{Name: defaultProxyName(row.Name), Protocol: row.Protocol, Host: row.Host, Port: row.Port, Username: &row.Username, Password: &row.Password, Status: status, ExpiresAt: expires, ClearExpiresAt: expires == nil, FallbackMode: mode, BackupProxyID: backupID, ClearBackupID: backupID == nil, ExpiryWarnDays: &row.ExpiryWarnDays})
		if err != nil {
			return fail(fmt.Errorf("Proxy settings could not be restored: %w", err))
		}
		state[key] = 2
		available[key] = p.ID
		available[canonical] = p.ID
		byIdentity[canonical] = p
		if reused {
			result.ProxyReused++
		} else {
			result.ProxyCreated++
		}
		return p.ID, nil
	}
	for _, key := range order {
		if _, err := restore(key); err != nil {
			result.ProxyFailed++
			result.Errors = append(result.Errors, DataImportError{Kind: "proxy", Name: rows[key].Name, Message: err.Error()})
		}
	}
	return available, nil
}
