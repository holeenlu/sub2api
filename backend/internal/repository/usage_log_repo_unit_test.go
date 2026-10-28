//go:build unit

package repository

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestSafeDateFormat(t *testing.T) {
	tests := []struct {
		name        string
		granularity string
		expected    string
	}{
		// 合法值
		{"hour", "hour", "YYYY-MM-DD HH24:00"},
		{"day", "day", "YYYY-MM-DD"},
		{"week", "week", "IYYY-IW"},
		{"month", "month", "YYYY-MM"},

		// 非法值回退到默认
		{"空字符串", "", "YYYY-MM-DD"},
		{"未知粒度 year", "year", "YYYY-MM-DD"},
		{"未知粒度 minute", "minute", "YYYY-MM-DD"},

		// 恶意字符串
		{"SQL 注入尝试", "'; DROP TABLE users; --", "YYYY-MM-DD"},
		{"带引号", "day'", "YYYY-MM-DD"},
		{"带括号", "day)", "YYYY-MM-DD"},
		{"Unicode", "日", "YYYY-MM-DD"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := safeDateFormat(tc.granularity)
			require.Equal(t, tc.expected, got, "safeDateFormat(%q)", tc.granularity)
		})
	}
}

func TestBuildUsageLogBatchInsertQuery_UsesConflictDoNothing(t *testing.T) {
	log := &service.UsageLog{
		UserID:                 1,
		APIKeyID:               2,
		AccountID:              3,
		RequestID:              "req-batch-no-update",
		Model:                  "gpt-5",
		InputTokens:            10,
		CacheCreationTokens:    34,
		CacheCreation30mTokens: 13,
		CacheCreation12hTokens: 21,
		OutputTokens:           5,
		TotalCost:              1.2,
		ActualCost:             1.2,
		CreatedAt:              time.Now().UTC(),
	}
	prepared := prepareUsageLogInsert(log)

	legacy := *log
	legacy.RequestID = "req-legacy-unknown-ttl"
	legacy.CacheCreation30mTokens = 0
	legacy.CacheCreation12hTokens = 0
	legacyPrepared := prepareUsageLogInsert(&legacy)
	key := usageLogBatchKey(log.RequestID, log.APIKeyID)
	legacyKey := usageLogBatchKey(legacy.RequestID, legacy.APIKeyID)
	batchQuery, batchArgs := buildUsageLogBatchInsertQuery([]string{key, legacyKey}, map[string]usageLogInsertPrepared{
		key: prepared, legacyKey: legacyPrepared,
	})
	bestEffortQuery, bestEffortArgs := buildUsageLogBestEffortInsertQuery([]usageLogInsertPrepared{prepared, legacyPrepared})
	columns := strings.Split(usageLogSelectColumns, ", ")[1:]
	for _, tc := range []struct {
		name    string
		query   string
		args    []any
		indexed bool
	}{
		{"batch", batchQuery, batchArgs, true},
		{"best_effort", bestEffortQuery, bestEffortArgs, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Contains(t, tc.query, "ON CONFLICT (request_id, api_key_id) DO NOTHING")
			require.NotContains(t, strings.ToUpper(tc.query), "DO UPDATE")
			for _, pattern := range []string{
				`(?s)WITH input \((.*?)\) AS`,
				`(?s)INSERT INTO usage_logs \((.*?)\)`,
				`(?s)\)\s+SELECT\s+(.*?)\s+FROM input`,
			} {
				match := regexp.MustCompile(pattern).FindStringSubmatch(tc.query)
				require.Len(t, match, 2)
				got := strings.Split(match[1], ",")
				for i := range got {
					got[i] = strings.TrimSpace(got[i])
				}
				if got[0] == "input_idx" {
					got = got[1:]
				}
				require.Equal(t, columns, got, "all CTE/INSERT/SELECT columns must share the scanner order")
			}
			placeholders := regexp.MustCompile(`\$(\d+)(?:::([a-z]+))?`).FindAllStringSubmatch(tc.query, -1)
			width := len(usageLogInsertArgTypes)
			if tc.indexed {
				width++
			}
			require.Len(t, tc.args, 2*width)
			require.Len(t, placeholders, len(tc.args))
			for i, ph := range placeholders {
				require.Equal(t, strconv.Itoa(i+1), ph[1])
				column := i % width
				if tc.indexed {
					if column == 0 {
						require.Empty(t, ph[2])
						require.Equal(t, i/width, tc.args[i])
						continue
					}
					column--
				}
				require.Equal(t, usageLogInsertArgTypes[column], ph[2])
				want := prepared.args
				if i >= width {
					want = legacyPrepared.args
				}
				require.Equal(t, want[column], tc.args[i])
			}
			require.Equal(t, []any{13, 21}, tc.args[width-3:width-1])
			require.Equal(t, []any{0, 0}, tc.args[len(tc.args)-3:len(tc.args)-1])
		})
	}
}
