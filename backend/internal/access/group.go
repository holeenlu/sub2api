package access

import (
	"slices"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/domain"
)

// GroupBinding contains the existing user's group grants. Vendor is supplied
// by the identity boundary; this package does not interpret roles. The zero
// value allows public groups only. AllowedGroups is borrowed read-only for the
// synchronous decision, never retained or mutated.
type GroupBinding struct {
	Vendor               bool
	RestrictPublicGroups bool
	AllowedGroups        []int64
}

// CanBindGroup decides standard-group binding without fetching identity or
// subscription state. IDs are compared exactly, including an explicit zero.
func CanBindGroup(binding GroupBinding, groupID int64, isExclusive bool) bool {
	if binding.Vendor {
		return false
	}
	if !isExclusive && !binding.RestrictPublicGroups {
		return true
	}
	return slices.Contains(binding.AllowedGroups, groupID)
}

// Group contains only the group facts consumed by API-key authorization. Status
// is the persisted value: deleted is case-insensitive, active is exact. A nil
// *Group means the bound group could not be loaded; Group{} is an inactive group.
// Subscription is the existing subscription-type classification, not evidence
// of a valid subscription.
type Group struct {
	ID           int64
	Status       string
	IsExclusive  bool
	Subscription bool
}

// GroupAvailability classifies group existence and activity, not user identity
// or entitlement. GroupAvailable is also the result for an unbound key.
type GroupAvailability uint8

const (
	GroupAvailable GroupAvailability = iota
	GroupDeleted
	GroupDisabled
)

// APIKeyGroupAvailable checks the key's group reference. Bound records GroupID
// presence, so an explicit zero is bound. An unbound key ignores any group edge.
// The caller still owns authentication and any required-group check.
func APIKeyGroupAvailable(bound bool, group *Group) GroupAvailability {
	if !bound {
		return GroupAvailable
	}
	if group == nil || strings.EqualFold(group.Status, "deleted") {
		return GroupDeleted
	}
	if group.Status != domain.StatusActive {
		return GroupDisabled
	}
	return GroupAvailable
}

// APIKeyGroupAllowed applies the existing group entitlement check. Missing
// identity and group edges are handled by the caller's authentication and
// availability checks, not granted here. Subscription groups defer entitlement
// to the existing subscription check, including its current ordering relative
// to vendor identity rejection. This decision grants neither a session nor a
// reusable dispatch permission.
func APIKeyGroupAllowed(bound bool, binding *GroupBinding, group *Group) bool {
	if !bound || binding == nil || group == nil {
		return true
	}
	if group.Subscription {
		return true
	}
	return CanBindGroup(*binding, group.ID, group.IsExclusive)
}
