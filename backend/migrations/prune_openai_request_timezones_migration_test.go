package migrations

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMigration242PrunesUnsupportedOpenAIRequestTimezones(t *testing.T) {
	content, err := FS.ReadFile("242_prune_openai_request_timezones.sql")
	require.NoError(t, err)
	sql := string(content)
	require.Contains(t, sql, "SET extra = extra - 'openai_request_timezone'")
	require.Contains(t, sql, "WHERE platform = 'openai'")
	require.Contains(t, sql, "extra ? 'openai_request_timezone'")
	require.Contains(t, sql, "jsonb_typeof(extra->'openai_request_timezone') IS DISTINCT FROM 'string'")
	require.Contains(t, sql, "extra->>'openai_request_timezone' NOT IN (")

	matches := regexp.MustCompile(`(?m)^\s+'([^']+)'[,]?$`).FindAllStringSubmatch(sql, -1)
	options := make([]string, 0, len(matches))
	for _, match := range matches {
		options = append(options, match[1])
	}
	require.Len(t, options, 30)
	// The historical SQL snapshot is independent of the retired runtime option list.
}
