package service

// Brand identity defaults.
//
// Brand branches override the VALUES in this file only; the rest of the code
// base refers to these constants instead of hardcoding the product name.
//
// These are the built-in fallbacks used whenever a deployment has not set its
// own `site_name` / `site_subtitle` in the settings table. Runtime settings
// always win; changing these constants only affects fresh installs and the
// nil/empty fallback paths (emails, payment product names, public settings).
const (
	// DefaultSiteName is the product name used as the site-name fallback.
	DefaultSiteName = "TapModels"
	// DefaultSiteTagline is the product tagline used as the site-subtitle
	// fallback.
	DefaultSiteTagline = "Pick a model. Start building."
	// DefaultDocsBaseURL is where the shipped documentation is published.
	DefaultDocsBaseURL = "https://docs.tapmodels.ai"
	// DefaultReleaseRepo is the GitHub "owner/repo" whose releases the online
	// update check and self-update follow. Keep in sync with RELEASE_REPO in
	// frontend/src/config/brand.ts.
	DefaultReleaseRepo = "holeenlu/sub2api"
)
