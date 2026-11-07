package service

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"time"
)

const (
	FallbackModeNone   = "none"
	FallbackModeProxy  = "proxy"
	FallbackModeDirect = "direct"
)

type Proxy struct {
	VendorID               *int64
	CreatedBy              *int64
	VendorNotes            string
	RegionLabelCountryCode string
	RegionLabelCity        string
	ConfigVersion          int64
	ID                     int64
	Name                   string
	Protocol               string
	Host                   string
	Port                   int
	Username               string
	Password               string
	Status                 string
	CreatedAt              time.Time
	UpdatedAt              time.Time
	ExpiresAt              *time.Time
	FallbackMode           string
	BackupProxyID          *int64
	ExpiryWarnDays         int
}

func (p *Proxy) IsActive() bool {
	return p.Status == StatusActive
}

// IsExpired 报告代理是否已过期（基于 expires_at，与 status 无关）。
func (p *Proxy) IsExpired(now time.Time) bool {
	return p.ExpiresAt != nil && !p.ExpiresAt.After(now)
}

func (p *Proxy) URL() string {
	u := &url.URL{
		Scheme: p.Protocol,
		Host:   net.JoinHostPort(p.Host, strconv.Itoa(p.Port)),
	}
	if p.Username != "" && p.Password != "" || p.VendorID != nil && (p.Username != "" || p.Password != "") {
		u.User = url.UserPassword(p.Username, p.Password)
	}
	if p.VendorID != nil {
		// Internal transport policy and pool generation; never included in safe DTOs.
		u.Fragment = fmt.Sprintf("vendor-proxy:%d:%d:%d", *p.VendorID, p.ID, p.ConfigVersion)
	}
	return u.String()
}

// SameProxyOwner excludes platform and cross-vendor fallbacks.
func SameProxyOwner(a, b *int64) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}

type ProxyWithAccountCount struct {
	Proxy
	AccountCount   int64
	LatencyMs      *int64
	LatencyStatus  string
	LatencyMessage string
	IPAddress      string
	Country        string
	CountryCode    string
	Region         string
	City           string
	QualityStatus  string
	QualityScore   *int
	QualityGrade   string
	QualitySummary string
	QualityChecked *int64
}

type ProxyAccountSummary struct {
	ID       int64
	Name     string
	Platform string
	Type     string
	Notes    *string
	Status   string
}
