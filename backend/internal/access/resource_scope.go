package access

// ResourceInGroup evaluates the existing scheduling membership contract. An
// absent group admits only unbound resources; an explicit zero is compared as
// zero, never interpreted as all groups. Input slices are read-only snapshots.
func ResourceInGroup(groupID *int64, memberships []int64) bool {
	if groupID == nil {
		return len(memberships) == 0
	}
	for _, id := range memberships {
		if id == *groupID {
			return true
		}
	}
	return false
}
