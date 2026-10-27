//go:build unit

package service

import "testing"

func TestModelPolicySeedMatchesUnifiedCatalogSemantics(t *testing.T) {
 for _, platform := range []string{PlatformAnthropic, PlatformOpenAI} {
  policy := modelPolicySeed(platform)
  if len(policy.Models) == 0 { t.Fatalf("%s seed is empty", platform) }
  if policy.CatalogMode != "policy_only" { t.Fatalf("%s catalog mode = %q", platform, policy.CatalogMode) }
  for id, row := range policy.Models {
   if id != row.ID || row.Kind == "" { t.Fatalf("invalid %s row %#v", platform, row) }
  }
 }
}

func TestModelPolicyDisabledEntryRejectsAliasButUnknownKeepsDefault(t *testing.T) {
 policy := modelPolicySeed(PlatformAnthropic)
 var id string
 for key := range policy.Models { id = key; break }
 row := policy.Models[id]
 row.Enabled = false
 row.Aliases = append(row.Aliases, "disabled-alias")
 policy.Models[id] = row
 if policy.Allows("disabled-alias") { t.Fatal("disabled alias was accepted") }
 if !policy.Allows("unlisted-custom-model") { t.Fatal("unknown model should follow default policy") }
}

func TestGroupModelAllowlistEmptyMeansNoAdditionalRestriction(t *testing.T) {
 cfg := GroupModelAllowlist{Enabled: true}
 if !cfg.Allows("any-model") { t.Fatal("empty group selection should inherit") }
 if !(&Group{ModelAllowlist: cfg}).ModelAllowlistEnabled() { t.Fatal("empty selection should disable extra restriction") }
}
