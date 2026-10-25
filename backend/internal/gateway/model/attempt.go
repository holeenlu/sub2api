// Package model contains immutable values shared by gateway domains.
// Values never carry credentials, service entities or transport connections.
package model

// ResourceOwner identifies the physical credential owner, including its vendor.
// Account shadows must resolve their parent before constructing this value.
// A zero value denotes an absent owner; it does not grant access to a resource.
type ResourceOwner struct {
	AccountID int64
	VendorID  int64
}

// AttemptTarget is a comparable recovery identity, not permission to dispatch.
// Digests retain the existing target separation without storing credentials or
// request bodies. An absent target is represented by nil at the consumer port.
// Limit must be positive for a target to consume an attempt. It remains part of
// the identity to preserve the existing per-target recovery budget contract.
type AttemptTarget struct {
	Owner       ResourceOwner
	Mode        string
	Endpoint    string
	Model       string
	ProxyDigest [32]byte
	GrantDigest [32]byte
	InputDigest [32]byte
	Limit       int
}
