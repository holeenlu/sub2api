package access

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestResourceInGroupNeverBroadensEmptyScope(t *testing.T) {
	zero, group := int64(0), int64(7)
	for _, tc := range []struct {
		name        string
		group       *int64
		memberships []int64
		want        bool
	}{
		{"unbound only", nil, nil, true}, {"absent is not global", nil, []int64{7}, false},
		{"empty pool remains empty", &group, nil, false}, {"different group", &group, []int64{8}, false},
		{"bound group", &group, []int64{8, 7}, true}, {"explicit zero not absent", &zero, nil, false},
		{"explicit zero membership", &zero, []int64{0}, true},
	} {
		t.Run(tc.name, func(t *testing.T) { require.Equal(t, tc.want, ResourceInGroup(tc.group, tc.memberships)) })
	}
}
