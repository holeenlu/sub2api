package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"strings"
	"time"
	"unicode"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/proxyurl"
)

type VendorProxyImportRequest struct {
	Format      string `json:"format"`
	Content     string `json:"content"`
	InputDigest string `json:"input_digest"`
}
type VendorProxyImportRow struct {
	Line    int             `json:"line"`
	Status  string          `json:"status"`
	Code    string          `json:"code,omitempty"`
	Proxy   *VendorProxyDTO `json:"proxy,omitempty"`
	ProxyID int64           `json:"proxy_id,omitempty"`
}
type VendorProxyImportResult struct {
	Rows           []VendorProxyImportRow `json:"rows"`
	ValidCount     int                    `json:"valid_count"`
	DuplicateCount int                    `json:"duplicate_count"`
	InvalidCount   int                    `json:"invalid_count"`
	InputDigest    string                 `json:"input_digest"`
}

// Inventory fields deliberately exclude source IDs, owner, authentication,
// status and test reports. Backup IDs remain subject to direct-owner validation on import.
type vendorProxyInventory struct {
	FallbackMode   string `json:"fallback_mode"`
	BackupProxyID  *int64 `json:"backup_proxy_id"`
	ExpiryWarnDays int    `json:"expiry_warn_days"`
	Name           string `json:"name"`
	Protocol       string `json:"protocol"`
	Host           string `json:"host"`
	Port           int    `json:"port"`
	Country        string `json:"region_label_country_code,omitempty"`
	City           string `json:"region_label_city,omitempty"`
	ExpiresAt      *int64 `json:"expires_at,omitempty"`
	Notes          string `json:"vendor_notes,omitempty"`
}

func proxyInputDigest(req VendorProxyImportRequest) string {
	sum := sha256.Sum256([]byte(req.Format + "\x00" + req.Content))
	return hex.EncodeToString(sum[:])
}
func proxyEndpointKey(in VendorProxyInput) string {
	normalizeVendorProxyEndpoint(&in)
	b, _ := json.Marshal([]any{in.Protocol, in.Host, in.Port, in.Username, in.Password})
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func parseVendorProxyImport(req VendorProxyImportRequest) ([]VendorProxyInput, []error, error) {
	if len(req.Content) > VendorProxyMaxBytes {
		return nil, nil, infraerrors.New(422, "PROXY_IMPORT_TOO_LARGE", "proxy import exceeds size limit")
	}
	var inputs []VendorProxyInput
	var errs []error
	add := func(in VendorProxyInput, err error) {
		if err == nil {
			err = validateVendorProxyInput(&in)
		}
		inputs = append(inputs, in)
		errs = append(errs, err)
	}
	switch req.Format {
	case "quick":
		for _, line := range strings.Split(req.Content, "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			_, u, err := proxyurl.Parse(line)
			if err != nil || u == nil {
				add(VendorProxyInput{}, infraerrors.New(422, "PROXY_URL_INVALID", "invalid proxy URL"))
				continue
			}
			port, e := strconv.Atoi(u.Port())
			if e != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
				add(VendorProxyInput{}, infraerrors.New(422, "PROXY_URL_INVALID", "proxy URL requires explicit port and no query or fragment"))
				continue
			}
			in := VendorProxyInput{Name: u.Host, Protocol: u.Scheme, Host: u.Hostname(), Port: port}
			if u.User != nil {
				in.Username = u.User.Username()
				in.Password, _ = u.User.Password()
			}
			add(in, nil)
		}
	case "json":
		var payload struct {
			Type    string            `json:"type"`
			Version int               `json:"version"`
			Proxies []json.RawMessage `json:"proxies"`
		}
		if err := json.Unmarshal([]byte(req.Content), &payload); err != nil {
			return nil, nil, infraerrors.New(422, "PROXY_IMPORT_INVALID", "invalid native JSON document")
		}
		if (payload.Type != "sub2api-data" && payload.Type != "sub2api-bundle") || payload.Version != 1 {
			return nil, nil, infraerrors.New(422, "PROXY_IMPORT_VERSION", "unsupported native type or version")
		}
		if len(payload.Proxies) > VendorProxyMaxRows {
			return nil, nil, infraerrors.New(422, "PROXY_IMPORT_TOO_LARGE", "too many proxies")
		}
		for _, raw := range payload.Proxies {
			// Ignore source ownership and status; only this input
			// whitelist can reach creation. Native authentication is accepted on
			// import but never emitted by preview/export.
			var row struct {
				FallbackMode   string          `json:"fallback_mode"`
				BackupProxyID  *int64          `json:"backup_proxy_id"`
				ExpiryWarnDays *int            `json:"expiry_warn_days"`
				Name           string          `json:"name"`
				Protocol       string          `json:"protocol"`
				Host           string          `json:"host"`
				Port           int             `json:"port"`
				Username       string          `json:"username"`
				Password       string          `json:"password"`
				Country        string          `json:"region_label_country_code"`
				City           string          `json:"region_label_city"`
				Notes          string          `json:"vendor_notes"`
				Expires        json.RawMessage `json:"expires_at"`
			}
			if e := json.Unmarshal(raw, &row); e != nil {
				add(VendorProxyInput{}, infraerrors.New(422, "PROXY_INPUT_INVALID", "invalid proxy row"))
				continue
			}
			in := VendorProxyInput{FallbackMode: row.FallbackMode, BackupProxyID: row.BackupProxyID, ExpiryWarnDays: row.ExpiryWarnDays, Name: row.Name, Protocol: row.Protocol, Host: row.Host, Port: row.Port, Username: row.Username, Password: row.Password, RegionLabelCountryCode: row.Country, RegionLabelCity: row.City, VendorNotes: row.Notes, Status: StatusActive}
			var dateErr error
			if len(row.Expires) > 0 && string(row.Expires) != "null" {
				var seconds int64
				if e := json.Unmarshal(row.Expires, &seconds); e == nil {
					t := time.Unix(seconds, 0).UTC()
					in.ExpiresAt = &t
				} else {
					var t time.Time
					if e = json.Unmarshal(row.Expires, &t); e != nil {
						dateErr = infraerrors.New(422, "PROXY_EXPIRY_INVALID", "invalid expiry timestamp")
					} else {
						in.ExpiresAt = &t
					}
				}
			}
			add(in, dateErr)
		}
	default:
		return nil, nil, infraerrors.New(422, "PROXY_IMPORT_FORMAT", "format must be quick or json")
	}
	if len(inputs) == 0 || len(inputs) > VendorProxyMaxRows {
		return nil, nil, infraerrors.New(422, "PROXY_IMPORT_TOO_LARGE", "import requires 1 to 1000 proxies")
	}
	return inputs, errs, nil
}

func (s *VendorService) ImportVendorProxies(ctx context.Context, req VendorProxyImportRequest, preview bool) (*VendorProxyImportResult, error) {
	vID, err := s.proxyScope(ctx)
	if err != nil {
		return nil, err
	}
	digest := proxyInputDigest(req)
	if !preview && req.InputDigest != digest {
		return nil, infraerrors.Conflict("PROXY_PREVIEW_CHANGED", "preview the unchanged input before importing")
	}
	inputs, errs, err := parseVendorProxyImport(req)
	if err != nil {
		return nil, err
	}
	r := &VendorProxyImportResult{Rows: []VendorProxyImportRow{}, InputDigest: digest}
	seen := map[string]bool{}
	rows, err := s.db.QueryContext(ctx, "SELECT protocol,host,port,COALESCE(username,''),COALESCE(password,'') FROM proxies WHERE vendor_id=$1 AND deleted_at IS NULL", vID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var in VendorProxyInput
		if err = rows.Scan(&in.Protocol, &in.Host, &in.Port, &in.Username, &in.Password); err != nil {
			rows.Close()
			return nil, err
		}
		seen[proxyEndpointKey(in)] = true
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	// Previews share the native owner lock with writes and expiry. Confirmation
	// revalidates inside CreateVendorProxy's transaction, never trusts this view.
	var previewTx *sql.Tx
	if preview {
		for i, in := range inputs {
			if errs[i] == nil && in.BackupProxyID != nil {
				previewTx, err = s.beginProxyMutation(ctx, vID)
				if err != nil {
					return nil, err
				}
				defer previewTx.Rollback()
				break
			}
		}
	}
	for i, in := range inputs {
		if previewTx != nil && errs[i] == nil {
			errs[i] = validateVendorProxyFallback(ctx, previewTx, vID, 0, in)
		}
		if ctx.Err() != nil {
			for j := i; j < len(inputs); j++ {
				r.Rows = append(r.Rows, VendorProxyImportRow{Line: j + 1, Status: "invalid", Code: "PROXY_IMPORT_CANCELLED"})
				r.InvalidCount++
			}
			break
		}
		row := VendorProxyImportRow{Line: i + 1, Status: "valid"}
		key := proxyEndpointKey(in)
		if errs[i] != nil {
			row.Status = "invalid"
			row.Code = infraerrors.Reason(errs[i])
			r.InvalidCount++
		} else if seen[key] {
			row.Status = "duplicate"
			row.Code = "PROXY_DUPLICATE"
			r.DuplicateCount++
		} else {
			p := Proxy{FallbackMode: in.FallbackMode, BackupProxyID: in.BackupProxyID, ExpiryWarnDays: *in.ExpiryWarnDays, Name: in.Name, Protocol: in.Protocol, Host: in.Host, Port: in.Port, Username: in.Username, Password: in.Password, VendorNotes: in.VendorNotes, RegionLabelCountryCode: in.RegionLabelCountryCode, RegionLabelCity: in.RegionLabelCity, ExpiresAt: in.ExpiresAt, Status: in.Status, ConfigVersion: 1}
			d := vendorProxyDTO(&p, nil, time.Now())
			row.Proxy = &d
			if !preview {
				created, e := s.CreateVendorProxy(ctx, in)
				if e != nil {
					row.Status = "invalid"
					row.Code = infraerrors.Reason(e)
					if row.Code == "" {
						row.Code = "PROXY_IMPORT_FAILED"
					}
					if row.Code == "PROXY_DUPLICATE" {
						row.Status = "duplicate"
						r.DuplicateCount++
					} else {
						r.InvalidCount++
					}
				} else {
					row.Status = "created"
					row.Proxy = created
					row.ProxyID = created.ID
					r.ValidCount++
					seen[key] = true
				}
			} else {
				r.ValidCount++
				seen[key] = true
			}
		}
		r.Rows = append(r.Rows, row)
		if !preview && row.Status != "created" {
			s.VendorAudit(ctx, vID, "proxy.import.row", 0, strconv.Itoa(row.Line)+":"+row.Status+":"+row.Code)
		}
	}
	if !preview {
		s.VendorAudit(ctx, vID, "proxy.import", 0, "created="+strconv.Itoa(r.ValidCount)+",duplicate="+strconv.Itoa(r.DuplicateCount)+",invalid="+strconv.Itoa(r.InvalidCount))
	}
	return r, nil
}

type VendorProxyExportRequest struct {
	Format string            `json:"format"`
	IDs    []int64           `json:"ids"`
	Filter VendorProxyFilter `json:"filter"`
}

func proxyCSVCell(s string) string {
	trim := strings.TrimLeftFunc(s, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) || r == 0xfeff })
	if strings.HasPrefix(s, "\t") || strings.HasPrefix(s, "\r") || strings.HasPrefix(s, "\n") || len(trim) > 0 && strings.ContainsRune("=+-@", rune(trim[0])) {
		return "'" + s
	}
	return s
}

func (s *VendorService) ExportVendorProxies(ctx context.Context, req VendorProxyExportRequest) ([]byte, string, error) {
	if err := requireVendorWriteScope(ctx); err != nil {
		return nil, "", err
	}
	vID, err := s.proxyReadScope(ctx, req.Filter.VendorID)
	if err != nil {
		return nil, "", err
	}
	if len(req.IDs) > VendorProxyMaxRows {
		return nil, "", infraerrors.New(422, "PROXY_SELECTION_INVALID", "too many selected proxies")
	}
	all, err := s.vendorProxyRows(ctx, vID)
	if err != nil {
		return nil, "", err
	}
	selected := []VendorProxyDTO{}
	if len(req.IDs) > 0 {
		byID := map[int64]VendorProxyDTO{}
		for _, p := range all {
			byID[p.ID] = p
		}
		seen := map[int64]bool{}
		for _, id := range req.IDs {
			if seen[id] {
				continue
			}
			seen[id] = true
			p, ok := byID[id]
			if !ok {
				return nil, "", ErrProxyNotFound
			}
			selected = append(selected, p)
		}
	} else {
		selected, err = filterVendorProxies(all, req.Filter, s.proxyLocation())
		if err != nil {
			return nil, "", err
		}
	}
	var b []byte
	contentType := ""
	switch req.Format {
	case "json":
		inventory := make([]vendorProxyInventory, 0, len(selected))
		for _, p := range selected {
			row := vendorProxyInventory{FallbackMode: p.FallbackMode, BackupProxyID: p.BackupProxyID, ExpiryWarnDays: p.ExpiryWarnDays, Name: p.Name, Protocol: p.Protocol, Host: p.Host, Port: p.Port, Country: p.RegionLabelCountryCode, City: p.RegionLabelCity, Notes: p.VendorNotes}
			if p.ExpiresAt != nil {
				seconds := p.ExpiresAt.Unix()
				row.ExpiresAt = &seconds
			}
			inventory = append(inventory, row)
		}
		b, err = json.Marshal(struct {
			Type    string                 `json:"type"`
			Version int                    `json:"version"`
			Proxies []vendorProxyInventory `json:"proxies"`
		}{"sub2api-data", 1, inventory})
		contentType = "application/json"
	case "csv":
		var buffer bytes.Buffer
		buffer.WriteString("\xef\xbb\xbf")
		w := csv.NewWriter(&buffer)
		_ = w.Write([]string{"id", "name", "protocol", "host", "port", "country_code", "city", "expires_at", "vendor_notes", "fallback_mode", "backup_proxy_id", "expiry_warn_days"})
		for _, p := range selected {
			expiry := ""
			if p.ExpiresAt != nil {
				expiry = p.ExpiresAt.Format(time.RFC3339)
			}
			backup := ""
			if p.BackupProxyID != nil {
				backup = strconv.FormatInt(*p.BackupProxyID, 10)
			}
			row := []string{strconv.FormatInt(p.ID, 10), p.Name, p.Protocol, p.Host, strconv.Itoa(p.Port), p.RegionLabelCountryCode, p.RegionLabelCity, expiry, p.VendorNotes, p.FallbackMode, backup, strconv.Itoa(p.ExpiryWarnDays)}
			for i := range row {
				row[i] = proxyCSVCell(row[i])
			}
			_ = w.Write(row)
		}
		w.Flush()
		err = w.Error()
		b = buffer.Bytes()
		contentType = "text/csv; charset=utf-8"
	default:
		return nil, "", infraerrors.New(422, "PROXY_EXPORT_FORMAT", "format must be csv or json")
	}
	if err != nil {
		return nil, "", err
	}
	if _, err = s.proxyReadScope(ctx, req.Filter.VendorID); err != nil {
		return nil, "", err
	}
	s.VendorAudit(ctx, vID, "proxy.export", 0, "success:count="+strconv.Itoa(len(selected)))
	return b, contentType, nil
}
