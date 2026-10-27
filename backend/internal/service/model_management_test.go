//go:build unit

package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestModelPolicySeedMatchesUnifiedCatalogSemantics(t *testing.T) {
	for _, platform := range []string{PlatformAnthropic, PlatformOpenAI} {
		policy := modelPolicySeed(platform)
		require.NoError(t, validateModelPolicy(policy))
		if len(policy.Models) == 0 {
			t.Fatalf("%s seed is empty", platform)
		}
		if policy.CatalogMode != "policy_only" {
			t.Fatalf("%s catalog mode = %q", platform, policy.CatalogMode)
		}
		for id, row := range policy.Models {
			if id != row.ID || row.Kind == "" {
				t.Fatalf("invalid %s row %#v", platform, row)
			}
		}
	}
}

func TestModelPolicyDisabledEntryRejectsAliasButUnknownKeepsDefault(t *testing.T) {
	policy := modelPolicySeed(PlatformAnthropic)
	var id string
	for key := range policy.Models {
		id = key
		break
	}
	row := policy.Models[id]
	row.Enabled = false
	row.Aliases = append(row.Aliases, "disabled-alias")
	policy.Models[id] = row
	if policy.Allows("disabled-alias") {
		t.Fatal("disabled alias was accepted")
	}
	if !policy.Allows("unlisted-custom-model") {
		t.Fatal("unknown model should follow default policy")
	}
}

func TestGroupModelAllowlistEmptyMeansNoAdditionalRestriction(t *testing.T) {
	cfg := GroupModelAllowlist{Enabled: true}
	if !cfg.Allows("any-model") {
		t.Fatal("empty group selection should inherit")
	}
	if (&Group{ModelAllowlist: cfg}).ModelAllowlistEnabled() {
		t.Fatal("empty selection should disable extra restriction")
	}
}

// Exercise the same read/edit/save/read flow as the page, including the full
// seed rather than a reduced hand-built policy that misses built-in aliases.
func TestModelPolicySaveSeedEdits(t *testing.T) {
	for _, platform := range []string{PlatformAnthropic, PlatformOpenAI} {
		for _, edit := range []string{"unchanged", "enabled", "params"} {
			t.Run(platform+"/"+edit, func(t *testing.T) {
				repo := &modelPolicySaveRepo{settingRepoStub: settingRepoStub{values: map[string]string{}}}
				svc := NewSettingService(repo, nil)
				ctx := context.Background()
				policy, err := svc.GetModelPolicy(ctx, platform, true)
				require.NoError(t, err)
				id := "gpt-6.1-sol"
				if platform == PlatformAnthropic {
					id = "claude-haiku-4-5-20251001"
				}
				row := policy.Models[id]
				if edit == "enabled" {
					row.Enabled = false
				}
				if edit == "params" && platform == PlatformAnthropic {
					row.Params.MaxTokensDefault = 4096
				}
				policy.Models[id] = row
				saved, err := svc.SaveModelPolicy(ctx, platform, *policy)
				require.NoError(t, err)
				require.Equal(t, int64(1), saved.Version)
				loaded, err := svc.GetModelPolicy(ctx, platform, true)
				require.NoError(t, err)
				require.Equal(t, saved, loaded)
				require.Equal(t, row.Enabled, loaded.Models[id].Enabled)
				require.Equal(t, row.Params.MaxTokensDefault, loaded.Models[id].Params.MaxTokensDefault)
				if platform == PlatformAnthropic {
					canonical, _, ok := loaded.Resolve("claude-haiku-4-5")
					require.True(t, ok)
					require.Equal(t, id, canonical)
					require.Equal(t, row.Enabled, loaded.Allows("claude-haiku-4-5"))
				}
			})
		}
	}
}

func TestModelPolicyAliasValidation(t *testing.T) {
	for _, tc := range []struct {
		name    string
		change  func(*ModelPolicy)
		message string
	}{
		{"declared builtin redirect", func(*ModelPolicy) {}, ""},
		{"undeclared shadow", func(p *ModelPolicy) { delete(p.Aliases, "claude-haiku-4-5") }, "Alias shadows another model"},
		{"conflicting redirect", func(p *ModelPolicy) { p.Aliases["claude-haiku-4-5"] = "claude-haiku-5-5" }, "Alias shadows another model"},
		{"duplicate alias", func(p *ModelPolicy) {
			for _, id := range []string{"claude-sonnet-4-6", "claude-opus-4-6"} {
				row := p.Models[id]
				row.Aliases = append(row.Aliases, "duplicate-alias")
				p.Models[id] = row
			}
		}, "Duplicate model alias"},
		{"whitespace alias", func(p *ModelPolicy) {
			row := p.Models["auto"]
			row.Aliases = []string{"invalid alias"}
			p.Models["auto"] = row
		}, "Invalid alias"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			policy := modelPolicySeed(PlatformAnthropic)
			tc.change(policy)
			err := validateModelPolicy(policy)
			if tc.message == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tc.message)
			}
		})
	}
}

type modelPolicySaveRepo struct{ settingRepoStub }

func (r *modelPolicySaveRepo) CompareAndSwapModelSettings(ctx context.Context, key string, expected int64, value string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	var current struct {
		Version int64 `json:"version"`
	}
	if raw := r.values[key]; raw != "" {
		if err := json.Unmarshal([]byte(raw), &current); err != nil {
			return err
		}
	}
	if current.Version != expected {
		return ErrModelPolicyConflict
	}
	r.values[key] = value
	return nil
}
