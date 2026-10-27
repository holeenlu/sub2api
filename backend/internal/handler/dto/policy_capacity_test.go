package dto

import (
	"encoding/json"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestPolicyCapacityFullAndLiteProjection(t *testing.T) {
	a := &service.Account{ID: 7, Extra: map[string]any{service.PolicyCapacityUnavailableUntilKey: time.Now().Add(time.Hour).UnixMilli()}}
	full := AccountFromService(a)
	lite := AccountListItemFromAccount(full)
	require.NotNil(t, full.PolicyCapacityUnavailableUntil)
	require.Equal(t, full.PolicyCapacityUnavailableUntil, lite.PolicyCapacityUnavailableUntil)
	for _, value := range []any{full, lite} {
		encoded, err := json.Marshal(value)
		require.NoError(t, err)
		require.Contains(t, string(encoded), "policy_capacity_unavailable_until")
	}
	a.Extra[service.PolicyCapacityUnavailableUntilKey] = time.Now().Add(-time.Second).UnixMilli()
	require.Nil(t, AccountFromService(a).PolicyCapacityUnavailableUntil)
	require.Nil(t, AccountListItemFromAccount(AccountFromServiceShallow(a)).PolicyCapacityUnavailableUntil)
}
