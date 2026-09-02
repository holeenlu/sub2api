package runtime_test

import (
	"testing"

	"entgo.io/ent"
	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/ent/account"
	"github.com/Wei-Shaw/sub2api/ent/group"
	_ "github.com/Wei-Shaw/sub2api/ent/runtime"
	"github.com/Wei-Shaw/sub2api/ent/schema"
)

// ent/runtime/runtime.go binds every generated Default/Validator variable by
// *position* in the schema's field slice (`groupFields[43].Descriptor()`).
// Inserting a schema field without re-running `go generate ./ent` shifts every
// later index by one, so each binding silently reads its neighbour — or panics
// during init() when that neighbour has another type.
//
// Both failure modes only surface when the process starts, which is how a
// commit that compiled cleanly once shipped a runtime.go whose group defaults
// were off by one. Importing this package runs that init(), and the tables
// below re-derive the same defaults *by name*, so a stale runtime.go fails
// `make test-unit` instead of the next deploy.
//
// See also `make verify-generate`, which catches the same class of drift for
// every generated file rather than just the bound defaults.

func TestGroupDefaultsMatchSchemaByName(t *testing.T) {
	defaults := schemaFieldDefaults(schema.Group{}.Fields())

	// Fields declared after the insertion-prone tail of the Group schema: the
	// bool run in particular would keep type-asserting cleanly while reading
	// the wrong neighbour.
	requireSchemaDefault(t, defaults, "model_routing_enabled", group.DefaultModelRoutingEnabled)
	requireSchemaDefault(t, defaults, "mcp_xml_inject", group.DefaultMcpXMLInject)
	requireSchemaDefault(t, defaults, "supported_model_scopes", group.DefaultSupportedModelScopes)
	requireSchemaDefault(t, defaults, "sort_order", group.DefaultSortOrder)
	requireSchemaDefault(t, defaults, "allow_messages_dispatch", group.DefaultAllowMessagesDispatch)
	requireSchemaDefault(t, defaults, "allow_live", group.DefaultAllowLive)
	requireSchemaDefault(t, defaults, "force_openai_fast", group.DefaultForceOpenaiFast)
	requireSchemaDefault(t, defaults, "free_openai_fast", group.DefaultFreeOpenaiFast)
}

func TestAccountDefaultsMatchSchemaByName(t *testing.T) {
	defaults := schemaFieldDefaults(schema.Account{}.Fields())

	requireSchemaDefault(t, defaults, "schedulable", account.DefaultSchedulable)
}

func requireSchemaDefault(t *testing.T, defaults map[string]any, field string, bound any) {
	t.Helper()
	want, ok := defaults[field]
	require.Truef(t, ok, "schema field %q declares no default; update this test alongside the schema", field)
	require.Equalf(t, want, bound,
		"generated default for %q does not match the schema — ent/runtime is stale, run `make generate`", field)
}

func schemaFieldDefaults(fields []ent.Field) map[string]any {
	defaults := make(map[string]any, len(fields))
	for _, f := range fields {
		descriptor := f.Descriptor()
		if descriptor.Default == nil {
			continue
		}
		defaults[descriptor.Name] = descriptor.Default
	}
	return defaults
}
