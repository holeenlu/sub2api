package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVendorOpenAIAuthorizationInputBoundary(t *testing.T) {
	for _, tc := range []struct {
		name, platform, mode, action string
		target                       int64
		valid                        bool
	}{
		{"legacy create defaults native", PlatformOpenAI, "", "", 0, true},
		{"legacy target defaults reauthorize", PlatformOpenAI, "", "", 7, true},
		{"BPS attach", PlatformOpenAI, "bps", "attach", 7, true},
		{"forged mode", PlatformOpenAI, "excel-admin", "create", 0, false},
		{"case variant", PlatformOpenAI, "BPS", "create", 0, false},
		{"Claude cannot claim BPS", PlatformAnthropic, "bps", "create", 0, false},
		{"attach requires existing target", PlatformOpenAI, "bps", "attach", 0, false},
		{"create cannot overwrite target", PlatformOpenAI, "native", "create", 7, false},
		{"reauthorize requires target", PlatformOpenAI, "native", "reauthorize", 0, false},
		{"forged action", PlatformOpenAI, "native", "transfer", 7, false},
		{"Claude cannot attach a parallel grant", PlatformAnthropic, "", "attach", 7, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := VendorAccountInput{Platform: tc.platform, AuthorizationMode: tc.mode, AuthorizationAction: tc.action, TargetAccountID: tc.target}
			err := normalizeVendorAuthorizationInput(&in)
			if !tc.valid {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			if tc.mode == "" {
				require.Equal(t, VendorOpenAIModeNative, in.AuthorizationMode)
			}
			if tc.action == "" {
				if tc.target == 0 {
					require.Equal(t, "create", in.AuthorizationAction)
				} else {
					require.Equal(t, "reauthorize", in.AuthorizationAction)
				}
			}
		})
	}
}

func TestVendorOpenAIAuthorizationPolicyNormalization(t *testing.T) {
	for _, tc := range []struct {
		name     string
		platform string
		input    []string
		want     []string
		invalid  bool
	}{
		{"legacy native only", PlatformOpenAI, nil, []string{"native"}, false},
		{"BPS only", PlatformOpenAI, []string{"bps"}, []string{"bps"}, false},
		{"both", PlatformOpenAI, []string{"native", "bps"}, []string{"native", "bps"}, false},
		{"explicit empty is not legacy", PlatformOpenAI, []string{}, nil, true},
		{"duplicate is invalid", PlatformOpenAI, []string{"bps", "bps"}, nil, true},
		{"unknown is invalid", PlatformOpenAI, []string{"native", "admin"}, nil, true},
		{"Claude removes OpenAI capabilities", PlatformAnthropic, []string{"native", "bps"}, []string{}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := &Vendor{Platforms: []string{tc.platform}, OpenAIAuthorizationModes: tc.input}
			err := normalizeVendorOpenAIModes(v)
			if tc.invalid {
				require.ErrorIs(t, err, ErrVendorInput)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, v.OpenAIAuthorizationModes)
		})
	}
}

func TestVendorOpenAIAuthorizationParentIntersection(t *testing.T) {
	for _, tc := range []struct {
		name, platform string
		parentModes    []string
		childModes     []string
		want           []string
	}{
		{"native parent", PlatformOpenAI, []string{"native"}, []string{"native", "bps"}, []string{"native"}},
		{"BPS parent", PlatformOpenAI, []string{"bps"}, []string{"native", "bps"}, []string{"bps"}},
		{"child cannot inherit ungranted mode", PlatformOpenAI, []string{"native", "bps"}, []string{"bps"}, []string{"bps"}},
		{"no intersection does not fall back native", PlatformOpenAI, []string{"native"}, []string{"bps"}, []string{}},
		{"Claude parent blocks both modes", PlatformAnthropic, nil, []string{"native", "bps"}, []string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			parent := &Vendor{Platforms: []string{tc.platform}, OpenAIAuthorizationModes: tc.parentModes}
			child := &Vendor{Platforms: []string{PlatformOpenAI}, OpenAIAuthorizationModes: tc.childModes}
			applyVendorParentPolicy(child, parent)
			require.Equal(t, tc.want, child.OpenAIAuthorizationModes)
			for _, mode := range []string{"native", "bps"} {
				err := validateVendorAuthorization(child, VendorAccountInput{Platform: PlatformOpenAI, AuthorizationMode: mode})
				if stringsContain(tc.want, mode) {
					require.NoError(t, err)
				} else {
					require.ErrorIs(t, err, ErrVendorDenied)
				}
			}
		})
	}
}

func TestVendorOpenAIAuthorizationImportVerifiedGrantsMustMatch(t *testing.T) {
	// These are already-verified service structures, never a substitute for
	// authenticating caller-supplied JWT claims against the upstream profile.
	primary := vendorOpenAITestCredential("native", "verified-workspace")
	for _, field := range []string{"same", "workspace", "chatgpt_user_id", "email", "missing"} {
		t.Run(field, func(t *testing.T) {
			bps := vendorOpenAITestCredential("bps", "verified-workspace")
			switch field {
			case "same":
				bps.Credentials["email"] = "OWNER@example.invalid"
			case "workspace":
				bps.Identity = "other-workspace"
			case "missing":
				bps = nil
			default:
				bps.Credentials[field] = "different-person"
			}
			err := vendorOpenAIGrantsMatch(primary, bps)
			if field == "same" {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, ErrVendorIdentityBound)
			}
		})
	}
}
