//go:build unit

package repository

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRedeemLockKey(t *testing.T) {
	tests := []struct {
		name     string
		code     string
		expected string
	}{
		{
			name:     "normal_code",
			code:     "ABC123",
			expected: "redeem:lock:ABC123",
		},
		{
			name:     "empty_code",
			code:     "",
			expected: "redeem:lock:",
		},
		{
			name:     "code_with_special_chars",
			code:     "CODE-2024:test",
			expected: "redeem:lock:CODE-2024:test",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := redeemLockKey(tc.code)
			require.Equal(t, tc.expected, got)
		})
	}
}
