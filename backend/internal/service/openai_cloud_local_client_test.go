//go:build cloudlocal

package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// This opt-in interoperability check runs the installed official client against
// the production catalog and Cloud gateway, with only the upstream RPC replaced.
// Its isolated client home has a dummy key, never copies a user's credentials,
// and is removed by the test. No request body or client output is persisted.
func TestCloudLocalCodexClientUsesAdvertisedContract(t *testing.T) {
	bin := os.Getenv("SUB2API_CODEX_LOCAL_BIN")
	if bin == "" {
		t.Skip("set SUB2API_CODEX_LOCAL_BIN to test an installed official client; no process was started")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	svc, in, store, wire := cloudForwardFixture(t, nil)
	const model = "gpt-6.1-sol"
	in.Account.Credentials["model_mapping"] = map[string]any{model: "gpt-5.1"}
	manifest, err := BuildCodexModelsManifest([]string{model})
	require.NoError(t, err)
	manifest, _, err = restrictCloudCodexModelsManifest(manifest, []Account{*in.Account}, &Group{Platform: PlatformOpenAI})
	require.NoError(t, err)
	var requests, catalogs, declaredTools atomic.Int32
	upstreamDone := make(chan struct{})
	go func() {
		defer close(upstreamDone)
		for {
			select {
			case <-ctx.Done():
				return
			case raw := <-wire.writes:
				var call cloudRPCMessage
				if json.Unmarshal(raw, &call) != nil {
					return
				}
				var result any = map[string]any{}
				switch call.Method {
				case "initialize", "thread/inject_items", "thread/archive":
				case "initialized":
					continue
				case "thread/start":
					declaredTools.Store(int32(len(gjson.GetBytes(call.Params, "dynamicTools.0.tools").Array())))
					result = map[string]any{"model": "gpt-5.1", "thread": map[string]any{"id": "thread"}}
				case "turn/start":
					result = map[string]any{"turn": map[string]any{"id": "turn"}}
				default:
					return
				}
				cloudTestRead(wire, map[string]any{"id": call.ID, "result": result})
				if call.Method == "turn/start" {
					cloudWireComplete(wire)
				}
			}
		}
	}()
	t.Cleanup(func() { cancel(); <-upstreamDone })
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/v1/models") {
			catalogs.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(manifest)
			return
		}
		if r.URL.Path != "/v1/responses" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		requests.Add(1)
		raw, err := io.ReadAll(io.LimitReader(r.Body, OpenAICloudMaxRetainedBytes+1))
		if err != nil || len(raw) > OpenAICloudMaxRetainedBytes {
			http.Error(w, "invalid local probe request", http.StatusBadRequest)
			return
		}
		requestCtx, err := svc.PrepareCloudContinuation(r.Context(), in.User.ID, in.APIKey.ID, *in.APIKey.GroupID, raw, r.Header)
		if err == nil {
			requestCtx, err = svc.CaptureCloudBillingSnapshot(requestCtx, in)
		}
		if err != nil {
			http.Error(w, "local probe admission failed", http.StatusBadRequest)
			return
		}
		c, _ := gin.CreateTestContext(w)
		c.Request = r.WithContext(requestCtx)
		c.Set("api_key", in.APIKey)
		SetOpenAIHTTPResponseOwner(c, in.User.ID, in.APIKey.ID)
		_, _ = svc.Forward(requestCtx, c, in.Account, raw)
	}))
	defer server.Close()
	clientHome := t.TempDir()
	configuration := fmt.Sprintf("model_provider = \"OpenAI\"\nmodel = %q\n[model_providers.OpenAI]\nname = \"OpenAI\"\nbase_url = %q\nenv_key = \"CLOUD_LOCAL_DUMMY\"\nwire_api = \"responses\"\nmodel_catalog_url = %q\nsupports_websockets = false\n", model, server.URL+"/v1", server.URL+"/v1/models")
	version, err := exec.CommandContext(ctx, bin, "--version").Output()
	require.NoError(t, err)
	fileCatalog := strings.Contains(string(version), "0.159.")
	if fileCatalog {
		// The older CLI does not fetch the provider catalog during this single
		// ephemeral turn. Its supported file override consumes the same bytes.
		catalogPath := filepath.Join(clientHome, "catalog.json")
		require.NoError(t, os.WriteFile(catalogPath, manifest, 0600))
		configuration = fmt.Sprintf("model_catalog_json = %q\n", catalogPath) + configuration
	}
	require.NoError(t, os.WriteFile(filepath.Join(clientHome, "config.toml"), []byte(configuration), 0600))
	command := exec.CommandContext(ctx, bin, "exec", "--ephemeral", "--skip-git-repo-check", "--sandbox", "read-only", "Reply with the single word ok. Do not invoke tools.")
	command.Dir = clientHome
	command.Env = append(os.Environ(), "CODEX_HOME="+clientHome, "CLOUD_LOCAL_DUMMY=local-probe")
	var output bytes.Buffer
	command.Stdout, command.Stderr = &output, &output
	err = command.Run()
	require.NoError(t, err, "isolated local client output: %s", output.String())
	require.EqualValues(t, 1, requests.Load(), "the client must complete without a retry")
	if !fileCatalog {
		require.NotEmpty(t, store.snapshot().ConversationHash, "the real desktop client must supply a native stable session header")
		require.Positive(t, catalogs.Load(), "the production Cloud catalog must actually be read")
	}
	require.Positive(t, declaredTools.Load(), "the complete client catalog must reach thread/start")
	t.Logf("client=%s requests=%d catalog_http=%d catalog_file=%t mapped_tools=%d upstream=local_rpc_fixture", strings.TrimSpace(string(version)), requests.Load(), catalogs.Load(), fileCatalog, declaredTools.Load())
}
