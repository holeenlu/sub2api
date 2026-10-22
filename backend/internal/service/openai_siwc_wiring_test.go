package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

type siwcBrandSettingRepo struct {
	SettingRepository
	name string
	err  error
	key  string
}

func (r *siwcBrandSettingRepo) GetValue(_ context.Context, key string) (string, error) {
	r.key = key
	return r.name, r.err
}
func TestSIWCWiringUsesExistingSiteNameAndBranchFallback(t *testing.T) {
	for _, tc := range []struct {
		name     string
		value    string
		err      error
		expected string
	}{{"runtime setting", " My Deployed App ", nil, "My Deployed App"}, {"empty setting", "", nil, DefaultSiteName}, {"unavailable", "", errors.New("offline"), DefaultSiteName}} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &siwcBrandSettingRepo{name: tc.value, err: tc.err}
			s := ProvideOpenAIOAuthService(nil, nil, nil, nil, nil, nil, nil, repo)
			defer s.Stop()
			require.Equal(t, SettingKeySiteName, repo.key)
			require.Equal(t, tc.expected, s.siwcAgentName)
		})
	}
	s := ProvideOpenAIOAuthService(nil, nil, nil, nil, nil, nil, nil, nil)
	defer s.Stop()
	require.Equal(t, DefaultSiteName, s.siwcAgentName)
}
