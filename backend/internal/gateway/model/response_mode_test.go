package model

import "testing"

func TestResponseProbePrincipalNeverUsesFabricatedCustomerKey(t *testing.T) {
	claim := [32]byte{1}
	for _, test := range []struct {
		kind      PrincipalKind
		user, key int64
		probe     string
		claim     [32]byte
		valid     bool
	}{
		{PrincipalCustomer, 1, 2, "", [32]byte{}, true},
		{PrincipalCustomer, 1, 0, "", [32]byte{}, false},
		{PrincipalSystemProbe, 0, 0, "actual-probe-claim", claim, true},
		{PrincipalSystemProbe, 1, 2, "actual-probe-claim", claim, false},
		{PrincipalSystemProbe, 0, 0, "actual-probe-claim", [32]byte{}, false},
		{PrincipalAdministratorProbe, 7, 0, "admin-probe", claim, true},
		{PrincipalAdministratorProbe, 0, 0, "admin-probe", claim, false},
	} {
		if got := ValidExecutionPrincipal(test.kind, test.user, test.key, 0, false, test.probe, test.claim); got != test.valid {
			t.Errorf("kind=%v user=%d key=%d: valid=%v", test.kind, test.user, test.key, got)
		}
	}
}

func TestResponseBillingScopeDoesNotInventBPSRoot(t *testing.T) {
	if !(ModeBinding{BillingScope: "operation"}).Valid("bps") {
		t.Fatal("BPS preparation must allow an unknown canonical billing ID")
	}
	if (ModeBinding{BillingScope: "operation", BillingRoot: "invented-before-send"}).Valid("bps") {
		t.Fatal("BPS must not guess its canonical settlement ID before sending")
	}
	if (ModeBinding{BillingScope: "operation", SessionID: "session", OwnerEpoch: [32]byte{1}}).Valid("cloud") {
		t.Fatal("customer Cloud execution must retain its known root")
	}
}

func TestResponseCustomerPrincipalRequiresExactGroupScope(t *testing.T) {
	for _, tc := range []struct {
		group int64
		bound bool
		valid bool
	}{{0, false, true}, {4, true, true}, {0, true, false}, {-1, true, false}, {4, false, false}} {
		if got := ValidExecutionPrincipal(PrincipalCustomer, 1, 2, tc.group, tc.bound, "", [32]byte{}); got != tc.valid {
			t.Errorf("group=%d bound=%v: valid=%v", tc.group, tc.bound, got)
		}
	}
}
