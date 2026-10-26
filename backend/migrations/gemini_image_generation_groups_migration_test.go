package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGeminiImageGenerationMigrationPreservesSavedSettings(t *testing.T) {
	content, err := FS.ReadFile("246_enable_gemini_image_generation_groups.sql")
	require.NoError(t, err)

	var statements []string
	for _, line := range strings.Split(string(content), "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "--") {
			statements = append(statements, line)
		}
	}
	sql := strings.Join(strings.Fields(strings.Join(statements, " ")), " ")
	require.Equal(t,
		"SELECT 1;",
		sql)
}
