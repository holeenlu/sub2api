package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// A preview keeps connection secrets only inside the encrypted import receipt.
// No proxy is persisted until confirmation; source IDs are never local bindings.
type vendorAccountImportProxy struct {
	Input  VendorProxyInput
	Backup *vendorAccountImportProxy
}

func (p *vendorAccountImportProxy) proxy(vendorID int64) *Proxy {
	in := p.Input
	return &Proxy{VendorID: &vendorID, Name: in.Name, Protocol: in.Protocol, Host: in.Host, Port: in.Port, Username: in.Username, Password: in.Password, Status: in.Status, ExpiresAt: in.ExpiresAt, ExpiryWarnDays: *in.ExpiryWarnDays, FallbackMode: in.FallbackMode}
}

func accountDataProxyKey(p AccountDataProxy) string {
	if p.ProxyKey != "" {
		return p.ProxyKey
	}
	return NativeDataProxyKey(p.Protocol, p.Host, p.Port, p.Username, p.Password)
}

func vendorAccountBundleProxy(proxies []AccountDataProxy, key string, visiting map[string]bool) (*vendorAccountImportProxy, error) {
	if visiting == nil {
		visiting = map[string]bool{}
	}
	if visiting[key] || len(visiting) >= vendorAccountImportLimit {
		return nil, vendorAccountInputError("Cyclic or excessive bundled proxy chain")
	}
	visiting[key] = true
	defer delete(visiting, key)
	var source *AccountDataProxy
	for _, candidate := range proxies {
		if accountDataProxyKey(candidate) != key {
			continue
		}
		if source != nil {
			identity := func(p AccountDataProxy) string {
				return proxyEndpointKey(VendorProxyInput{Protocol: p.Protocol, Host: p.Host, Port: p.Port, Username: p.Username, Password: p.Password})
			}
			if identity(*source) != identity(candidate) || source.FallbackMode != candidate.FallbackMode || source.BackupProxyName != candidate.BackupProxyName {
				return nil, vendorAccountInputError("Ambiguous bundled proxy key")
			}
			continue
		}
		copy := candidate
		source = &copy
	}
	if source == nil {
		return nil, vendorAccountInputError("Bundled proxy not found")
	}
	if source.FallbackMode != "" && source.FallbackMode != FallbackModeNone && source.FallbackMode != FallbackModeProxy {
		return nil, vendorAccountInputError("Supplier proxy fallback cannot use direct egress")
	}
	// Reuse the native supplier parser for endpoint, credentials and expiry.
	// A backup is resolved by its portable name, never by a source database ID.
	connection := *source
	connection.FallbackMode, connection.BackupProxyName = FallbackModeNone, ""
	raw, err := json.Marshal(struct {
		Proxies []AccountDataProxy `json:"proxies"`
	}{[]AccountDataProxy{connection}})
	if err != nil {
		return nil, err
	}
	inputs, errs, err := parseVendorProxyImport(VendorProxyImportRequest{Format: "json", Content: string(raw)})
	if err != nil {
		return nil, err
	}
	if errs[0] != nil {
		return nil, errs[0]
	}
	plan := &vendorAccountImportProxy{Input: inputs[0]}
	// The native account format uses zero when expiry_warn_days is omitted.
	warnDays := source.ExpiryWarnDays
	plan.Input.ExpiryWarnDays = &warnDays
	if source.Status != "" {
		plan.Input.Status = source.Status
	}
	if err = validateVendorProxyInput(&plan.Input); err != nil {
		return nil, err
	}
	if source.FallbackMode == FallbackModeProxy {
		var backupKey string
		for _, candidate := range proxies {
			if source.BackupProxyName == "" || candidate.Name != source.BackupProxyName {
				continue
			}
			if backupKey != "" && backupKey != accountDataProxyKey(candidate) {
				return nil, vendorAccountInputError("Ambiguous backup proxy name")
			}
			backupKey = accountDataProxyKey(candidate)
		}
		if backupKey == "" {
			return nil, vendorAccountInputError("Bundled backup proxy not found")
		}
		plan.Backup, err = vendorAccountBundleProxy(proxies, backupKey, visiting)
		if err != nil {
			return nil, err
		}
	}
	return plan, nil
}

func (s *VendorService) vendorAccountImportExistingProxy(ctx context.Context, vendorID int64, in VendorProxyInput) (*Proxy, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT "+vendorProxyColumns+" FROM proxies p WHERE p.vendor_id=$1 AND p.deleted_at IS NULL AND p.port=$2 AND COALESCE(p.username,'')=$3 AND COALESCE(p.password,'')=$4 ORDER BY p.id", vendorID, in.Port, in.Username, in.Password)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		p, e := scanVendorProxy(rows)
		if e != nil {
			return nil, e
		}
		if proxyEndpointKey(VendorProxyInput{Protocol: p.Protocol, Host: p.Host, Port: p.Port, Username: p.Username, Password: p.Password}) == proxyEndpointKey(in) {
			return p, nil
		}
	}
	return nil, rows.Err()
}

func (s *VendorService) vendorAccountImportEgress(ctx context.Context, tx *sql.Tx, v *Vendor, plan *vendorAccountImportProxy) (string, int64, error) {
	p := plan.proxy(v.ID)
	if !p.IsActive() || p.IsExpired(time.Now()) {
		return "", 0, ErrVendorProxyUnavailable
	}
	if err := ValidateVendorResourceCapacity(ctx, tx, v.ID, "proxies"); err != nil {
		return "", 0, err
	}
	// VendorID retains the shared transport's DNS/IP and redirect restrictions
	// even though the preview has no persisted proxy ID yet.
	return p.URL(), 0, nil
}

func (s *VendorService) confirmVendorAccountImportProxy(ctx context.Context, vendorID int64, plan *vendorAccountImportProxy) (*Proxy, error) {
	if existing, err := s.vendorAccountImportExistingProxy(ctx, vendorID, plan.Input); err != nil || existing != nil {
		return existing, err
	}
	in := plan.Input
	if plan.Backup != nil {
		backup, err := s.confirmVendorAccountImportProxy(ctx, vendorID, plan.Backup)
		if err != nil {
			return nil, err
		}
		in.FallbackMode, in.BackupProxyID = FallbackModeProxy, &backup.ID
	}
	created, err := s.CreateVendorProxy(ctx, in)
	if infraerrors.Reason(err) == "PROXY_DUPLICATE" {
		// Another import may have created the same endpoint after the preview.
		existing, e := s.vendorAccountImportExistingProxy(ctx, vendorID, in)
		if e == nil && existing == nil {
			e = ErrVendorProxyUnavailable
		}
		return existing, e
	}
	if err != nil {
		return nil, err
	}
	return s.ownedProxy(ctx, created.ID, vendorID)
}

func exportVendorAccountProxy(ctx context.Context, tx *sql.Tx, vendorID, id int64, bundle *VendorAccountBundle, exported map[int64]string) (string, error) {
	if key, ok := exported[id]; ok {
		if key == "" {
			return "", vendorAccountInputError("Cyclic proxy backup chain")
		}
		return key, nil
	}
	if len(exported) >= VendorProxyMaxRows {
		return "", vendorAccountInputError("Too many bundled proxies")
	}
	exported[id] = ""
	p, err := scanVendorProxy(tx.QueryRowContext(ctx, "SELECT "+vendorProxyColumns+" FROM proxies p WHERE p.id=$1 AND p.vendor_id=$2 AND p.deleted_at IS NULL", id, vendorID))
	if err != nil {
		return "", ErrProxyNotFound
	}
	row := AccountDataProxy{ProxyKey: NativeDataProxyKey(p.Protocol, p.Host, p.Port, p.Username, p.Password), Name: p.Name, Protocol: p.Protocol, Host: p.Host, Port: p.Port, Username: p.Username, Password: p.Password, Status: p.Status, FallbackMode: p.FallbackMode, ExpiryWarnDays: p.ExpiryWarnDays}
	if p.ExpiresAt != nil {
		seconds := p.ExpiresAt.Unix()
		row.ExpiresAt = &seconds
	}
	if p.FallbackMode == FallbackModeProxy && p.BackupProxyID != nil {
		key, e := exportVendorAccountProxy(ctx, tx, vendorID, *p.BackupProxyID, bundle, exported)
		if e != nil {
			return "", e
		}
		for _, backup := range bundle.Proxies {
			if backup.ProxyKey == key {
				row.BackupProxyName = backup.Name
				break
			}
		}
	}
	bundle.Proxies = append(bundle.Proxies, row)
	exported[id] = row.ProxyKey
	return row.ProxyKey, nil
}
