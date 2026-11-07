//go:build unit

package service

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/stretchr/testify/require"
)

func TestCopyOpenAIUsageFromResponsesUsageTrustsCanonicalCacheCreationValue(t *testing.T) {
	usage := &apicompat.ResponsesUsage{
		InputTokens:              20,
		OutputTokens:             2,
		CacheCreationInputTokens: 0,
		InputTokensDetails: &apicompat.ResponsesInputTokensDetails{
			CachedTokens:            3,
			CacheWriteTokens:        19,
			CacheWriteTokensDetails: &apicompat.CacheWriteTokensDetails{TTL30mTokens: 9, TTL12hTokens: 10},
		},
	}

	got := copyOpenAIUsageFromResponsesUsage(usage)

	require.Equal(t, 20, got.InputTokens)
	require.Equal(t, 3, got.CacheReadInputTokens)
	require.Zero(t, got.CacheCreationInputTokens)
	require.Zero(t, got.CacheCreation30mTokens)
	require.Zero(t, got.CacheCreation12hTokens)
	usage.CacheCreationInputTokens = 19
	got = copyOpenAIUsageFromResponsesUsage(usage)
	require.Equal(t, 9, got.CacheCreation30mTokens)
	require.Equal(t, 10, got.CacheCreation12hTokens)
}
