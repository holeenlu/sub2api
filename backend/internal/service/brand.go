package service

import "github.com/Wei-Shaw/sub2api/internal/projectinfo"

// Neutral project defaults for deployments without explicit website settings.
//
// These are the built-in fallbacks used whenever a deployment has not set its
// own `site_name` / `site_subtitle` in the settings table. Runtime settings
// always win; changing these constants only affects fresh installs and the
// nil/empty fallback paths (emails, payment product names, public settings).
const (
	// DefaultSiteName is the product name used as the site-name fallback.
	DefaultSiteName = projectinfo.Name
	// DefaultSiteTagline is the product tagline used as the site-subtitle
	// fallback.
	DefaultSiteTagline = "Subscription to API Conversion Platform"
	// DefaultDocsBaseURL stays empty until a project documentation site exists.
	// Deployments can supply their own doc_url through the existing settings.
	DefaultDocsBaseURL = ""
)
