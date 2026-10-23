package migrations

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSIWCMigrationKeepsIndependentRegistrationAndUsageHistory(t *testing.T) {
	raw, err := os.ReadFile("284_openai_siwc_grants.sql")
	require.NoError(t, err)
	sql := string(raw)
	for _, fragment := range []string{"openai_siwc_installation", "singleton BOOLEAN PRIMARY KEY", "CHECK (singleton)", "openai_siwc_grants", "REFERENCES accounts(id) ON DELETE CASCADE", "registration_ciphertext TEXT NOT NULL", "credentials_ciphertext TEXT NOT NULL DEFAULT ''", "DROP CONSTRAINT IF EXISTS usage_logs_authorization_mode_check", "CHECK (authorization_mode IN ('native', 'bps', 'siwc'))"} {
		require.Contains(t, sql, fragment)
	}
	require.NotContains(t, strings.ToUpper(sql), "UPDATE ACCOUNTS")
	require.NotContains(t, strings.ToUpper(sql), "UPDATE USAGE_LOGS")
	require.NotContains(t, sql, "openai_oauth_reauth")
	require.NotContains(t, sql, "openai_excel_grants")
	original, err := os.ReadFile("279_openai_usage_authorization_mode.sql")
	require.NoError(t, err)
	require.Contains(t, string(original), "CHECK (authorization_mode IN ('native', 'bps'))")
	require.Contains(t, string(original), "usage authorization mode is immutable")
}
