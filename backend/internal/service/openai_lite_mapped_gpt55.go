package service

import (
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// The account mapping changes the model, not the upstream capability header.
// Apply at the final HTTP request boundary, after account/model resolution.
// Never mutate the ingress headers/body: failover may select a native account.
func applyMappedGPT55LiteCompatibility(req *http.Request, account *Account, body []byte) error {
	if req == nil || account == nil || !account.IsOpenAIOAuthLike() {
		return nil
	}
	if strings.TrimSpace(gjson.GetBytes(body, "model").String()) != "gpt-5.5" {
		return nil
	}
	if !isOpenAIResponsesLiteHeader(req.Header.Get(responsesLiteHeader)) && !isOpenAIResponsesLiteWebSocketPayload(body) {
		return nil
	}
	// The Codex non-Lite endpoint accepts additional_tools, namespaces and
	// reasoning.context=all_turns. Preserve them and all history/tool results.
	changed := false
	if isOpenAIResponsesLiteWebSocketPayload(body) {
		var err error
		body, err = sjson.DeleteBytes(body, "client_metadata."+responsesLiteWSMetadataKey)
		if err != nil {
			return fmt.Errorf("remove mapped GPT-5.5 Lite metadata: %w", err)
		}
		changed = true
	}
	// This boundary actually changes Lite to standard Responses. Apply the
	// standard-only history declaration now, without mutating the ingress
	// request that a later failover could send to a native Lite model.
	if next, added, err := ensureOpenAIOAuthWebSearchToolForHistoryBody(body, false); err != nil {
		return fmt.Errorf("normalize mapped GPT-5.5 search history: %w", err)
	} else if added {
		body, changed = next, true
	}
	if changed {
		if req.Body != nil {
			_ = req.Body.Close()
		}
		req.Body = httputil.NewPrereadBody(body)
		req.ContentLength = int64(len(body))
		req.GetBody = func() (io.ReadCloser, error) { return httputil.NewPrereadBody(body), nil }
	}
	req.Header.Del(responsesLiteHeader)
	return nil
}
