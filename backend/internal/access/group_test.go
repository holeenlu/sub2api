package access

import (
	"slices"
	"testing"
)

func TestCanBindGroup(t *testing.T) {
	const (
		publicGroupA    int64 = 10
		publicGroupB    int64 = 11
		exclusiveGroupA int64 = 20
	)

	for _, tc := range []struct {
		name        string
		binding     GroupBinding
		groupID     int64
		isExclusive bool
		want        bool
	}{
		{
			name:    "public group is bindable by default",
			binding: GroupBinding{},
			groupID: publicGroupA,
			want:    true,
		},
		{
			name:        "exclusive group needs an explicit grant",
			binding:     GroupBinding{},
			groupID:     exclusiveGroupA,
			isExclusive: true,
			want:        false,
		},
		{
			name:        "granted exclusive group is bindable",
			binding:     GroupBinding{AllowedGroups: []int64{exclusiveGroupA}},
			groupID:     exclusiveGroupA,
			isExclusive: true,
			want:        true,
		},
		{
			name:    "an unrestricted user keeps every public group even with a grant list",
			binding: GroupBinding{AllowedGroups: []int64{exclusiveGroupA}},
			groupID: publicGroupA,
			want:    true,
		},
		{
			name:    "a restricted user keeps the public groups listed for them",
			binding: GroupBinding{RestrictPublicGroups: true, AllowedGroups: []int64{publicGroupA}},
			groupID: publicGroupA,
			want:    true,
		},
		{
			name:    "a restricted user loses the public groups not listed for them",
			binding: GroupBinding{RestrictPublicGroups: true, AllowedGroups: []int64{publicGroupA}},
			groupID: publicGroupB,
			want:    false,
		},
		{
			name:    "a restricted user with no list keeps no public group",
			binding: GroupBinding{RestrictPublicGroups: true},
			groupID: publicGroupA,
			want:    false,
		},
		{
			name:        "restricting public groups does not widen exclusive access",
			binding:     GroupBinding{RestrictPublicGroups: true, AllowedGroups: []int64{publicGroupA}},
			groupID:     exclusiveGroupA,
			isExclusive: true,
			want:        false,
		},
		{
			name:        "a restricted user can still hold both kinds of grant",
			binding:     GroupBinding{RestrictPublicGroups: true, AllowedGroups: []int64{publicGroupA, exclusiveGroupA}},
			groupID:     exclusiveGroupA,
			isExclusive: true,
			want:        true,
		},
		{name: "vendor public denied", binding: GroupBinding{Vendor: true}, groupID: 10},
		{name: "vendor grant denied", binding: GroupBinding{Vendor: true, AllowedGroups: []int64{20}}, groupID: 20, isExclusive: true},
		{name: "explicit zero grant", binding: GroupBinding{AllowedGroups: []int64{0}}, isExclusive: true, want: true},
		{name: "zero is not a wildcard", binding: GroupBinding{AllowedGroups: []int64{0}}, groupID: 20, isExclusive: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := slices.Clone(tc.binding.AllowedGroups)
			if got := CanBindGroup(tc.binding, tc.groupID, tc.isExclusive); got != tc.want {
				t.Fatalf("CanBindGroup = %v, want %v", got, tc.want)
			}
			if !slices.Equal(before, tc.binding.AllowedGroups) {
				t.Fatal("binding decision mutated grants")
			}
		})
	}
}

func TestAPIKeyGroupAvailable(t *testing.T) {
	for _, tc := range []struct {
		name  string
		bound bool
		group *Group
		want  GroupAvailability
	}{
		{name: "unbound", want: GroupAvailable},
		{name: "unbound ignores deleted edge", group: &Group{Status: "deleted"}, want: GroupAvailable},
		{name: "bound missing edge", bound: true, want: GroupDeleted},
		{name: "deleted case insensitive", bound: true, group: &Group{Status: "DeLeTeD"}, want: GroupDeleted},
		{name: "disabled", bound: true, group: &Group{Status: "disabled"}, want: GroupDisabled},
		{name: "unknown status", bound: true, group: &Group{Status: "future"}, want: GroupDisabled},
		{name: "zero value group", bound: true, group: &Group{}, want: GroupDisabled},
		{name: "active is case sensitive", bound: true, group: &Group{Status: "ACTIVE"}, want: GroupDisabled},
		{name: "deleted is not trimmed", bound: true, group: &Group{Status: " deleted "}, want: GroupDisabled},
		{name: "active", bound: true, group: &Group{ID: 10, Status: "active"}, want: GroupAvailable},
		{name: "explicit zero active", bound: true, group: &Group{ID: 0, Status: "active"}, want: GroupAvailable},
		{name: "subscription still requires active", bound: true, group: &Group{Subscription: true, Status: "disabled"}, want: GroupDisabled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := APIKeyGroupAvailable(tc.bound, tc.group); got != tc.want {
				t.Fatalf("availability = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestAPIKeyGroupAllowed(t *testing.T) {
	for _, tc := range []struct {
		name    string
		bound   bool
		binding *GroupBinding
		group   *Group
		want    bool
	}{
		{name: "unbound ignores denied edge", binding: &GroupBinding{}, group: &Group{IsExclusive: true}, want: true},
		{name: "identity check stays at caller", bound: true, group: &Group{IsExclusive: true}, want: true},
		{name: "missing edge checked separately", bound: true, binding: &GroupBinding{}, want: true},
		{name: "public", bound: true, binding: &GroupBinding{}, group: &Group{}, want: true},
		{name: "restricted public", bound: true, binding: &GroupBinding{RestrictPublicGroups: true}, group: &Group{}},
		{name: "exclusive revoked", bound: true, binding: &GroupBinding{}, group: &Group{ID: 20, IsExclusive: true}},
		{name: "loaded edge ID determines grant", bound: true, binding: &GroupBinding{AllowedGroups: []int64{20}}, group: &Group{ID: 20, IsExclusive: true}, want: true},
		{name: "standard vendor denied", bound: true, binding: &GroupBinding{Vendor: true}, group: &Group{}},
		{name: "subscription defers entitlement", bound: true, binding: &GroupBinding{RestrictPublicGroups: true}, group: &Group{Subscription: true, IsExclusive: true}, want: true},
		{name: "subscription does not replace vendor authentication", bound: true, binding: &GroupBinding{Vendor: true}, group: &Group{Subscription: true}, want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := APIKeyGroupAllowed(tc.bound, tc.binding, tc.group); got != tc.want {
				t.Fatalf("group allowed = %v, want %v", got, tc.want)
			}
		})
	}
}
