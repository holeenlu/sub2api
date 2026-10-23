package authz

import "context"

// OpenAIAuthorizationModeSQL is shared by supply admission and its management
// projection. Aliases and mode are internal constants, never request fragments.
func OpenAIAuthorizationModeSQL(a, v, mode string) string {
	if mode != "native" && mode != "bps" {
		return "FALSE"
	}
	policy := v + `.platforms ? 'openai' AND ` + v + `.openai_authorization_modes ? '` + mode + `'
 AND (` + v + `.tier=1 OR EXISTS(SELECT 1 FROM vendors auth_parent WHERE auth_parent.id=` + v + `.parent_vendor_id AND auth_parent.platforms ? 'openai' AND auth_parent.openai_authorization_modes ? '` + mode + `'))`
	grant := `COALESCE(` + a + `.credentials->>'client_id','')<>'app_fnr0pYvVwwFDocDumLG3H2Bp' AND COALESCE(` + a + `.credentials->>'access_token','')<>'' AND COALESCE(` + a + `.extra->>'vendor_native_disabled','false')<>'true'`
	if mode == "bps" {
		grant = `COALESCE(` + a + `.extra->>'openai_excel_bps','false')='true' AND LOWER(COALESCE(` + a + `.credentials->>'plan_type',''))<>'free' AND ((` + a + `.credentials->>'client_id'='app_fnr0pYvVwwFDocDumLG3H2Bp' AND COALESCE(` + a + `.credentials->>'access_token','')<>'') OR EXISTS(SELECT 1 FROM openai_excel_grants auth_grant WHERE auth_grant.account_id=` + a + `.id AND auth_grant.ciphertext<>''))`
	}
	return "(" + policy + " AND " + grant + ")"
}

// VendorScope is created only after live user, policy and session validation.
// It does not grant any administrative capability.
type VendorScope struct {
	VendorID      int64
	PolicyVersion int64
	ActorUserID   int64
	Admin         bool
	ReadOnly      bool // Direct-child inventory/usage only; never credentials or mutations.
	Children      bool // Inventory only; VendorID identifies the authenticated parent.
}
type vendorScopeKey struct{}

func WithVendorScope(ctx context.Context, scope VendorScope) context.Context {
	return context.WithValue(ctx, vendorScopeKey{}, scope)
}
func VendorScopeFromContext(ctx context.Context) (VendorScope, bool) {
	v, ok := ctx.Value(vendorScopeKey{}).(VendorScope)
	return v, ok
}

// ActiveVendorSQL is shared by authentication, ownership reads and dispatch.
// Aliases must be trusted identifiers supplied by the implementation.
func ActiveVendorSQL(v, u string) string {
	return "(" + v + ".status='active' AND " + u + ".status='active' AND " + u + ".deleted_at IS NULL AND ((" + v + ".tier=1 AND " + v + ".parent_vendor_id IS NULL AND " + u + ".role='vendor_level1') OR (" + v + ".tier=2 AND " + u + ".role='vendor_level2' AND EXISTS (SELECT 1 FROM vendors vp_parent JOIN users vu_parent ON vu_parent.id=vp_parent.user_id WHERE vp_parent.id=" + v + ".parent_vendor_id AND vp_parent.tier=1 AND vp_parent.parent_vendor_id IS NULL AND vp_parent.status='active' AND vu_parent.role='vendor_level1' AND vu_parent.status='active' AND vu_parent.deleted_at IS NULL))))"
}

type supplierOriginKey struct{}

func WithSupplierOrigin(ctx context.Context, supplier bool) context.Context {
	return context.WithValue(ctx, supplierOriginKey{}, supplier)
}
func SupplierOrigin(ctx context.Context) (bool, bool) {
	v, ok := ctx.Value(supplierOriginKey{}).(bool)
	return v, ok
}
