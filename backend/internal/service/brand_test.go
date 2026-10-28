//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestBrandDefaultsFreshInstall(t *testing.T) {
	require.Equal(t, "sub2api", DefaultSiteName)
	require.Empty(t, DefaultDocsBaseURL)

	repo := &forwardedIPMigrationRepoStub{values: map[string]string{}}
	svc := NewSettingService(repo, &config.Config{})
	require.NoError(t, svc.InitializeDefaultSettings(context.Background()))
	require.Equal(t, "sub2api", repo.values[SettingKeySiteName])
	require.Empty(t, repo.values[SettingKeyDocURL])
	require.Empty(t, repo.values[SettingKeyContactInfo])

	public, err := svc.GetPublicSettings(context.Background())
	require.NoError(t, err)
	require.Equal(t, "sub2api", public.SiteName)
	require.Empty(t, public.DocURL)
}

func TestBrandParsedSettingsUseNeutralFallbacks(t *testing.T) {
	svc := NewSettingService(&settingPublicRepoStub{}, &config.Config{})
	for _, values := range []map[string]string{nil, {}, {SettingKeySiteName: "", SettingKeySiteSubtitle: ""}} {
		settings := svc.parseSettings(values)
		require.Equal(t, "sub2api", settings.SiteName)
		require.Equal(t, DefaultSiteTagline, settings.SiteSubtitle)
		require.Empty(t, settings.DocURL)
		require.Empty(t, settings.ContactInfo)
	}
}

func TestBrandExplicitSettingsRemainAuthoritative(t *testing.T) {
	for _, name := range []string{"KDAN", "TapModels", "Tokensavy", "Example deployment"} {
		t.Run(name, func(t *testing.T) {
			repo := &forwardedIPMigrationRepoStub{values: map[string]string{
				SettingKeyRegistrationEnabled: "true",
				SettingKeySiteName:            name,
				SettingKeySiteLogo:            "https://example.com/logo.svg",
				SettingKeySiteSubtitle:        "Deployment subtitle",
				SettingKeyAPIBaseURL:          "https://example.com/v1",
				SettingKeyDocURL:              "https://example.com/docs",
				SettingKeyContactInfo:         "support@example.com",
			}}
			svc := NewSettingService(repo, &config.Config{})
			require.NoError(t, svc.InitializeDefaultSettings(context.Background()))
			require.Nil(t, repo.updates, "existing settings must not be reseeded")
			require.Equal(t, name, svc.GetSiteName(context.Background()))

			settings := svc.parseSettings(repo.values)
			public, err := svc.GetPublicSettings(context.Background())
			require.NoError(t, err)
			require.Equal(t, name, settings.SiteName)
			require.Equal(t, name, public.SiteName)
			require.Equal(t, repo.values[SettingKeySiteLogo], public.SiteLogo)
			require.Equal(t, repo.values[SettingKeySiteSubtitle], public.SiteSubtitle)
			require.Equal(t, repo.values[SettingKeyAPIBaseURL], public.APIBaseURL)
			require.Equal(t, repo.values[SettingKeyDocURL], public.DocURL)
			require.Equal(t, repo.values[SettingKeyContactInfo], public.ContactInfo)
			require.Equal(t, public.DocURL, settings.DocURL)
		})
	}
}
