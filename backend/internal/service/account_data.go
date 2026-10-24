package service

import (
	"fmt"
	"strings"
)

// AccountDataProxy is the native migration format shared by administrator and
// supplier account transfers. Unlike inventory DTOs, an explicit account backup
// includes proxy authentication so that its proxy_key can be restored.
type AccountDataProxy struct {
	ProxyKey        string `json:"proxy_key"`
	Name            string `json:"name"`
	Protocol        string `json:"protocol"`
	Host            string `json:"host"`
	Port            int    `json:"port"`
	Username        string `json:"username,omitempty"`
	Password        string `json:"password,omitempty"`
	Status          string `json:"status"`
	ExpiresAt       *int64 `json:"expires_at,omitempty"`
	FallbackMode    string `json:"fallback_mode,omitempty"`
	BackupProxyName string `json:"backup_proxy_name,omitempty"`
	BackupProxyKey  string `json:"backup_proxy_key,omitempty"`
	ExpiryWarnDays  int    `json:"expiry_warn_days,omitempty"`
}

func NativeDataProxyKey(protocol, host string, port int, username, password string) string {
	return fmt.Sprintf("%s|%s|%d|%s|%s", strings.TrimSpace(protocol), strings.TrimSpace(host), port, strings.TrimSpace(username), strings.TrimSpace(password))
}

func ValidateNativeDataVersion(kind string, version int) error {
	if kind != "" && kind != "sub2api-data" && kind != "sub2api-bundle" {
		return fmt.Errorf("unsupported data type: %s", kind)
	}
	if version != 0 && version != 1 && version != 2 {
		return fmt.Errorf("unsupported data version: %d", version)
	}
	return nil
}
