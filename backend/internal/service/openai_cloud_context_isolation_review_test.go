package service

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// Ported from the maintenance contract at main@c2a10f2aa. This checks protocol
// construction and client-history preservation, not remote App Server execution.
func TestCloudThreadKeepsClientPermissionContext(t *testing.T) {
	for _, mode := range []string{"read-only", "workspace-write", "danger-full-access"} {
		t.Run(mode, func(t *testing.T) {
			permissions := "<permissions instructions>Filesystem sandboxing is `" + mode + "`. Approval policy is on-request.</permissions instructions>"
			environment := "<environment_context><cwd>/client/workspace</cwd><filesystem>" + mode + "</filesystem></environment_context>"
			history := []any{
				map[string]any{"type": "message", "role": "developer", "content": []any{map[string]any{"type": "input_text", "text": permissions}}},
				map[string]any{"type": "message", "role": "user", "content": []any{map[string]any{"type": "input_text", "text": environment}}},
			}
			raw, err := json.Marshal(map[string]any{"model": "m", "input": append(append([]any{}, history...), map[string]any{"role": "user", "content": "Edit the project using the client tool."}), "tools": []any{map[string]any{"type": "function", "name": "apply_patch", "parameters": map[string]any{"type": "object"}}}})
			require.NoError(t, err)
			p, err := parseCloudProtocolRequest(raw)
			require.NoError(t, err)
			require.Equal(t, history, cloudInjectionHistory(p), "the client's permission instructions and environment must survive unchanged")
			params := cloudThreadParams(p)
			config := params["config"].(map[string]any)
			require.Equal(t, false, config["include_permissions_instructions"], "Cloud's sandbox must not become instructions for tools executed by the client")
			require.Equal(t, false, config["include_environment_context"], "the Cloud host is not the client's filesystem")
			require.Equal(t, ":read-only", params["permissions"], "do not expand the Cloud host's execution permissions")
			require.Equal(t, "never", params["approvalPolicy"])
			for _, feature := range []string{"shell_tool", "unified_exec", "apply_patch", "js_repl", "plugins", "apps", "multi_agent"} {
				require.Equal(t, false, config["features."+feature])
			}
			require.Empty(t, params["environments"])
			require.Len(t, params["dynamicTools"], 1, "client tools remain available under the client executor's policy")
		})
	}
}
