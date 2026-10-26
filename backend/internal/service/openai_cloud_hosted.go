package service

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image/png"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

type cloudHostedState struct {
	kind      string
	published bool
}

type cloudHostedOutput struct {
	upstreamID string
	item       map[string]any
	bytes      int
}

// Hosted tools are executed by Cloud. Keep their starts private until a complete
// result is verified. A concurrent client RPC may be needed for Cloud to finish,
// so yielding that RPC must not expose an unfinished hosted item or wait for it.
func (a *cloudSessionActor) handleHostedItem(params json.RawMessage, completed bool) error {
	a.turnProducedOutput = true
	a.handoffBeforeInference = false
	var event struct {
		Item struct {
			ID            string          `json:"id"`
			Type          string          `json:"type"`
			Status        string          `json:"status"`
			Result        string          `json:"result"`
			RevisedPrompt string          `json:"revisedPrompt"`
			Failure       json.RawMessage `json:"failure"`
			Action        map[string]any  `json:"action"`
			Results       []struct {
				URL   string `json:"url"`
				Title string `json:"title"`
			} `json:"results"`
		} `json:"item"`
	}
	if json.Unmarshal(params, &event) != nil {
		return errCloudRPCProtocol
	}
	upstream := event.Item
	if a.protocol == nil || upstream.ID == "" || len(upstream.ID) > 512 {
		return errCloudRPCProtocol
	}
	kind, prefix := "web_search_call", "ws_cloud_"
	switch upstream.Type {
	case "webSearch":
		if !a.protocol.webSearch {
			return errors.New("Cloud attempted undeclared web search")
		}
	case "imageGeneration":
		if !a.protocol.imageGeneration {
			return errors.New("Cloud attempted undeclared image generation")
		}
		kind, prefix = "image_generation_call", "ig_cloud_"
	default:
		return errCloudRPCProtocol
	}
	if index, exists := a.items[upstream.ID]; exists && a.output[index]["type"] != kind {
		return errors.New("Cloud reused an output identity")
	}
	if previous, exists := a.hostedStates[upstream.ID]; exists {
		if previous.kind != kind {
			return errors.New("Cloud reused a hosted output identity")
		}
	} else {
		if len(a.hostedStates) >= 1024 {
			return errors.New("Cloud hosted item limit exceeded")
		}
		if a.hostedStates == nil {
			a.hostedStates = make(map[string]*cloudHostedState)
		}
		a.hostedStates[upstream.ID] = &cloudHostedState{kind: kind}
	}
	encodedUpstream, _ := json.Marshal(upstream)
	hostedDigest := cloudDigest(encodedUpstream)
	if prior, seen := a.rawItems["hosted:"+upstream.ID]; seen {
		if completed && prior != hostedDigest {
			return errors.New("Cloud repeated an inconsistent hosted result")
		}
		return nil
	}
	if !completed {
		return nil
	}

	item := map[string]any{"type": kind, "id": prefix + uuid.NewString(), "status": "completed"}
	var imageSize, imageTier string
	if kind == "web_search_call" {
		action, err := cloudSearchAction(upstream.Action)
		if err != nil {
			return err
		}
		if a.protocol.includeSearchSources {
			sources := make([]any, 0, len(upstream.Results))
			for _, result := range upstream.Results {
				if cloudPublicURL(result.URL) {
					sources = append(sources, map[string]any{"type": "url", "url": result.URL, "title": result.Title})
				}
			}
			action["sources"] = sources
		}
		item["action"] = action
	} else {
		if upstream.Status != "completed" || len(upstream.Failure) > 0 && string(upstream.Failure) != "null" || upstream.Result == "" {
			return errors.New("Cloud image generation did not produce a completed image")
		}
		var err error
		imageSize, err = cloudGeneratedPNGSize(upstream.Result)
		if err != nil {
			return err
		}
		var ok bool
		imageTier, ok = ClassifyImageBillingTier(imageSize)
		if !ok {
			return errors.New("Cloud image billing dimensions are unavailable")
		}
		item["result"], item["revised_prompt"], item["output_format"], item["size"] = upstream.Result, upstream.RevisedPrompt, "png", imageSize
	}

	// Count only validated completions, once per upstream ID over the whole
	// turn. Save these facts even when a later output-buffer limit fails.
	if kind == "web_search_call" {
		a.hostedUsage.WebSearchCalls++
	} else {
		a.hostedUsage.ImageCount++
		if a.hostedUsage.ImageSizes == nil {
			a.hostedUsage.ImageSizes = make(map[string]int)
		}
		a.hostedUsage.ImageSizes[imageTier]++
		a.hostedUsage.ImageOutputSizes = append(a.hostedUsage.ImageOutputSizes, imageSize)
	}
	if a.rawItems == nil {
		a.rawItems = make(map[string]string)
	}
	a.rawItems["hosted:"+upstream.ID] = hostedDigest
	if a.record.Usage != nil {
		a.record.Usage = a.withHostedUsage(a.record.Usage)
	}
	if err := a.save(nil, nil); err != nil {
		return err
	}
	raw, _ := json.Marshal(item)
	if a.hostedBufferedBytes+a.bufferedEventBytes+len(raw) > OpenAICloudMaxResultBytes {
		return errors.New("Cloud buffered hosted output exceeded its limit")
	}
	a.hostedReady = append(a.hostedReady, cloudHostedOutput{upstreamID: upstream.ID, item: item, bytes: len(raw)})
	a.hostedBufferedBytes += len(raw)
	return a.flushHostedOutputs()
}

func cloudGeneratedPNGSize(encoded string) (string, error) {
	if len(encoded) == 0 || len(encoded) > OpenAICloudMaxResultBytes || strings.ContainsAny(encoded, "\r\n") {
		return "", errors.New("Cloud image result exceeds its size or encoding limits")
	}
	data, err := base64.StdEncoding.Strict().DecodeString(encoded)
	if err != nil {
		return "", errors.New("Cloud image result is not valid base64")
	}
	config, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width <= 0 || config.Height <= 0 || config.Width > 8192 || config.Height > 8192 || int64(config.Width)*int64(config.Height) > 16<<20 {
		return "", errors.New("Cloud generated PNG dimensions are invalid")
	}
	reader := bytes.NewReader(data)
	if _, err := png.Decode(reader); err != nil || reader.Len() != 0 {
		return "", errors.New("Cloud generated PNG is incomplete or has trailing data")
	}
	return strconv.Itoa(config.Width) + "x" + strconv.Itoa(config.Height), nil
}

func (a *cloudSessionActor) hasPendingHosted() bool {
	for _, state := range a.hostedStates {
		if !state.published {
			return true
		}
	}
	return false
}

// A completion received between client HTTP requests remains queued. The next
// response publishes its entire lifecycle in completion order before yielding
// any more client tools. The raw completion digest already prevents rebilling.
func (a *cloudSessionActor) flushHostedOutputs() error {
	if a.requestID == "" {
		return nil
	}
	for len(a.hostedReady) > 0 {
		ready := a.hostedReady[0]
		state := a.hostedStates[ready.upstreamID]
		if state == nil || state.published || ready.item["type"] != state.kind {
			return errCloudRPCProtocol
		}
		if len(a.output) >= 1024 || a.outputBytes+ready.bytes > OpenAICloudMaxResultBytes {
			return errors.New("Cloud hosted output exceeded its limit")
		}
		if _, exists := a.items[ready.upstreamID]; exists {
			return errors.New("Cloud reused an output identity")
		}
		index := len(a.output)
		if a.items == nil {
			a.items = make(map[string]int)
		}
		a.items[ready.upstreamID] = index
		a.output = append(a.output, ready.item)
		a.outputBytes += ready.bytes
		kind := ready.item["type"].(string)
		initial := map[string]any{"id": ready.item["id"], "type": kind, "status": "in_progress"}
		if err := a.emit("response.output_item.added", map[string]any{"output_index": index, "item": initial}); err != nil {
			return err
		}
		if err := a.emit("response."+kind+".in_progress", map[string]any{"output_index": index, "item_id": ready.item["id"]}); err != nil {
			return err
		}
		if err := a.emit("response."+kind+".completed", map[string]any{"output_index": index, "item_id": ready.item["id"]}); err != nil {
			return err
		}
		if err := a.emit("response.output_item.done", map[string]any{"output_index": index, "item": ready.item}); err != nil {
			return err
		}
		state.published = true
		// The client may replay this public item as history; keep it for the
		// same comparison as messages and tool calls (id/status are ignored).
		a.history = append(a.history, ready.item)
		a.hostedBufferedBytes -= ready.bytes
		a.hostedReady[0] = cloudHostedOutput{}
		a.hostedReady = a.hostedReady[1:]
	}
	return nil
}

func cloudPublicURL(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && (parsed.Scheme == "https" || parsed.Scheme == "http") && parsed.Hostname() != "" && parsed.User == nil && len(value) <= 8192
}
func cloudSearchAction(value map[string]any) (map[string]any, error) {
	out := make(map[string]any)
	kind, _ := value["type"].(string)
	switch kind {
	case "search":
		out["type"] = "search"
		query, _ := value["query"].(string)
		queries, has := value["queries"].([]any)
		if query == "" && !has {
			return nil, errors.New("Cloud search action is incomplete")
		}
		if query != "" {
			out["query"] = query
		}
		if has {
			for _, q := range queries {
				if _, ok := q.(string); !ok {
					return nil, errCloudRPCProtocol
				}
			}
			out["queries"] = queries
		}
	case "openPage", "open_page":
		link, _ := value["url"].(string)
		if !cloudPublicURL(link) {
			return nil, errCloudRPCProtocol
		}
		out["type"], out["url"] = "open_page", link
	case "findInPage", "find_in_page":
		link, _ := value["url"].(string)
		pattern, ok := value["pattern"].(string)
		if !cloudPublicURL(link) || !ok {
			return nil, errCloudRPCProtocol
		}
		out["type"], out["url"], out["pattern"] = "find_in_page", link, pattern
	default:
		return nil, errors.New("Cloud search action is unsupported")
	}
	return out, nil
}
func (a *cloudSessionActor) withHostedUsage(tokens *OpenAICloudUsage) *OpenAICloudUsage {
	if tokens == nil {
		return nil
	}
	copy := *tokens
	copy.WebSearchCalls, copy.ImageCount = a.hostedUsage.WebSearchCalls, a.hostedUsage.ImageCount
	copy.ImageSizes = cloneImageSizeBreakdown(a.hostedUsage.ImageSizes)
	copy.ImageOutputSizes = append([]string(nil), a.hostedUsage.ImageOutputSizes...)
	return &copy
}

func (a *cloudSessionActor) handleRawItem(raw json.RawMessage) error {
	var item map[string]any
	if len(raw) > OpenAICloudMaxResultBytes || json.Unmarshal(raw, &item) != nil {
		return errCloudRPCProtocol
	}
	kind, _ := item["type"].(string)
	switch kind {
	case "message":
		rawID, _ := item["id"].(string)
		rawDigest := cloudDigest(raw)
		if prior, seen := a.rawItems["message:"+rawID]; seen {
			if prior != rawDigest {
				return errors.New("Cloud repeated an inconsistent raw message")
			}
			return nil
		}
		if item["role"] != "assistant" {
			return nil
		}
		phase := ""
		if value := item["phase"]; value != nil {
			if !cloudValidMessagePhase(value) {
				return errors.New("Cloud assistant message phase is invalid")
			}
			phase = value.(string)
		}
		parts, ok := item["content"].([]any)
		if !ok || len(parts) != 1 {
			return errors.New("Cloud raw assistant content is unsupported")
		}
		part, ok := parts[0].(map[string]any)
		if !ok || part["type"] != "output_text" {
			return errors.New("Cloud raw assistant content is unsupported")
		}
		text, ok := part["text"].(string)
		if !ok {
			return errCloudRPCProtocol
		}
		annotations, err := cloudPublicAnnotations(part["annotations"], text)
		if err != nil {
			return err
		}
		id, _ := item["id"].(string)
		if _, exists := a.items[id]; !exists {
			// Current Cloud uses different wire/UI identifiers in some revisions.
			// A UI fallback may already belong to a closed downstream response.
			// Retain only unmatched identities across segments of this turn.
			if observed, exists := a.fallbackMessages[id]; exists {
				if observed != text {
					return errors.New("Cloud raw message differs from streamed text")
				}
			} else {
				matched := ""
				for _, candidates := range []map[string]string{a.pendingMessages, a.fallbackMessages} {
					for pending, body := range candidates {
						if body == text {
							if matched != "" && matched != pending {
								return errors.New("Cloud raw message identity is ambiguous")
							}
							matched = pending
						}
					}
				}
				if matched == "" {
					return errors.New("Cloud raw message has no matching streamed item")
				}
				id = matched
			}
		}
		if _, current := a.items[id]; current {
			if err := a.completeMessage(id, text, annotations, phase); err != nil {
				return err
			}
		}
		// A prior segment is immutable: late metadata cannot rewrite history the
		// caller already received, and must not emit a second copy of its text.
		a.fallbackMessageBytes -= len(a.fallbackMessages[id])
		delete(a.fallbackMessages, id)
		if a.rawItems == nil {
			a.rawItems = make(map[string]string)
		}
		a.rawItems["message:"+rawID] = rawDigest
		return nil
	case "reasoning", "compaction":
		a.turnProducedOutput = true
		a.handoffBeforeInference = false
		id, _ := item["id"].(string)
		if id == "" || a.requestID == "" {
			return errCloudRPCProtocol
		}
		output := map[string]any{"id": id, "type": kind, "status": "completed"}
		if encrypted, ok := item["encrypted_content"].(string); ok && encrypted != "" {
			output["encrypted_content"] = encrypted
		}
		if kind == "reasoning" {
			summary, ok := item["summary"].([]any)
			if !ok {
				return errCloudRPCProtocol
			}
			public := make([]any, 0, len(summary))
			for _, rawPart := range summary {
				part, ok := rawPart.(map[string]any)
				if !ok || part["type"] != "summary_text" {
					return errCloudRPCProtocol
				}
				text, ok := part["text"].(string)
				if !ok {
					return errCloudRPCProtocol
				}
				public = append(public, map[string]any{"type": "summary_text", "text": text})
			}
			output["summary"] = public
		}
		encoded, _ := json.Marshal(output)
		digest := cloudDigest(encoded)
		if a.rawItems == nil {
			a.rawItems = make(map[string]string)
		}
		if prior, exists := a.rawItems[id]; exists {
			if prior != digest {
				return errors.New("Cloud repeated an inconsistent raw item")
			}
			return nil
		}
		if len(a.output) >= 1024 || a.outputBytes+len(encoded) > OpenAICloudMaxResultBytes {
			return errors.New("Cloud raw output exceeds its limit")
		}
		a.rawItems[id] = digest
		a.outputBytes += len(encoded)
		index := len(a.output)
		a.output = append(a.output, output)
		initial := map[string]any{"id": id, "type": kind, "status": "in_progress"}
		if kind == "reasoning" {
			initial["summary"] = []any{}
		}
		if err := a.emit("response.output_item.added", map[string]any{"output_index": index, "item": initial}); err != nil {
			return err
		}
		// Native Responses streams each summary part. Cloud receives the item
		// complete, so it publishes each part whole, in the native event order.
		summary, _ := output["summary"].([]any)
		for summaryIndex, rawPart := range summary {
			text, _ := rawPart.(map[string]any)["text"].(string)
			fields := func(extra map[string]any) map[string]any {
				event := map[string]any{"item_id": id, "output_index": index, "summary_index": summaryIndex}
				for key, value := range extra {
					event[key] = value
				}
				return event
			}
			for _, step := range []struct {
				kind   string
				fields map[string]any
			}{
				{"response.reasoning_summary_part.added", fields(map[string]any{"part": map[string]any{"type": "summary_text", "text": ""}})},
				{"response.reasoning_summary_text.delta", fields(map[string]any{"delta": text})},
				{"response.reasoning_summary_text.done", fields(map[string]any{"text": text})},
				{"response.reasoning_summary_part.done", fields(map[string]any{"part": map[string]any{"type": "summary_text", "text": text}})},
			} {
				if err := a.emit(step.kind, step.fields); err != nil {
					return err
				}
			}
		}
		if err := a.emit("response.output_item.done", map[string]any{"output_index": index, "item": output}); err != nil {
			return err
		}
		history := make(map[string]any, len(output))
		for k, v := range output {
			if k != "status" {
				history[k] = v
			}
		}
		a.history = append(a.history, history)
		return nil
	default:
		// Function RPCs are mapped independently; raw custom calls are private
		// hosted implementation details, not declarations for the caller to run.
		return nil
	}
}

func cloudPublicAnnotations(raw any, text string) ([]any, error) {
	if raw == nil {
		return []any{}, nil
	}
	list, ok := raw.([]any)
	if !ok || len(list) > 1024 {
		return nil, errCloudRPCProtocol
	}
	out := make([]any, 0, len(list))
	for _, v := range list {
		annotation, ok := v.(map[string]any)
		if !ok || annotation["type"] != "url_citation" {
			return nil, errors.New("Cloud returned an unsupported citation type")
		}
		link, ok := annotation["url"].(string)
		if !ok || !cloudPublicURL(link) {
			return nil, errCloudRPCProtocol
		}
		title, ok := annotation["title"].(string)
		if !ok {
			return nil, errCloudRPCProtocol
		}
		start, ok1 := annotation["start_index"].(float64)
		end, ok2 := annotation["end_index"].(float64)
		if !ok1 || !ok2 || start < 0 || end < start || end > float64(len(text)) || start != float64(int(start)) || end != float64(int(end)) {
			return nil, errCloudRPCProtocol
		}
		out = append(out, map[string]any{"type": "url_citation", "url": link, "title": title, "start_index": int(start), "end_index": int(end)})
	}
	return out, nil
}
func (a *cloudSessionActor) completeMessage(id, text string, annotations []any, phase string) error {
	index, exists := a.items[id]
	if !exists {
		return errCloudRPCProtocol
	}
	item := a.output[index]
	if item["type"] != "message" {
		return errCloudRPCProtocol
	}
	part := item["content"].([]any)[0].(map[string]any)
	if part["text"] != text {
		return errors.New("Cloud raw message differs from streamed text")
	}
	if phase != "" {
		if previous, exists := item["phase"]; exists && previous != phase {
			return errors.New("Cloud assistant message phase changed")
		}
		item["phase"] = phase
	}
	if item["status"] == "completed" {
		// A raw completion can follow the UI-only fallback. The retained public
		// ID ties its observed phase to the exact history item, even when two
		// assistant messages contain the same text.
		if phase != "" {
			for _, raw := range a.history {
				history, ok := raw.(map[string]any)
				if ok && history["type"] == "message" && history["id"] == item["id"] {
					history["phase"] = phase
					break
				}
			}
		}
		return nil
	}
	part["annotations"] = annotations
	item["status"] = "completed"
	delete(a.pendingMessages, id)
	for i, annotation := range annotations {
		if err := a.emit("response.output_text.annotation.added", map[string]any{"output_index": index, "item_id": item["id"], "content_index": 0, "annotation_index": i, "annotation": annotation}); err != nil {
			return err
		}
	}
	if err := a.emit("response.output_text.done", map[string]any{"output_index": index, "item_id": item["id"], "content_index": 0, "text": text}); err != nil {
		return err
	}
	if err := a.emit("response.content_part.done", map[string]any{"output_index": index, "item_id": item["id"], "content_index": 0, "part": part}); err != nil {
		return err
	}
	if err := a.emit("response.output_item.done", map[string]any{"output_index": index, "item": item}); err != nil {
		return err
	}
	history := map[string]any{"type": "message", "id": item["id"], "role": "assistant", "content": []any{map[string]any{"type": "output_text", "text": text, "annotations": annotations}}}
	if phase != "" {
		history["phase"] = phase
	}
	a.history = append(a.history, history)
	if a.record.Status == "awaiting_tools" && a.hasPendingTools() && !a.drainingBuffered {
		return a.finishAwaiting()
	}
	return nil
}
func (a *cloudSessionActor) flushPendingMessages() error {
	ids := make([]string, 0, len(a.pendingMessages))
	for id := range a.pendingMessages {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return a.items[ids[i]] < a.items[ids[j]] })
	for _, id := range ids {
		text := a.pendingMessages[id]
		if len(a.fallbackMessages) >= 1024 || a.fallbackMessageBytes+len(text) > OpenAICloudMaxResultBytes {
			return errors.New("Cloud unmatched message metadata exceeded its buffer limit")
		}
		if err := a.completeMessage(id, text, []any{}, ""); err != nil {
			return err
		}
		if a.fallbackMessages == nil {
			a.fallbackMessages = make(map[string]string)
		}
		a.fallbackMessages[id] = text
		a.fallbackMessageBytes += len(text)
	}
	return nil
}
func (a *cloudSessionActor) setPublicFailure(err error) {
	status, code, message, ok := cloudPublicError(err)
	if !ok {
		status, code, message = http.StatusBadGateway, "cloud_execution_failed", "Cloud execution failed; the request was not replayed."
	}
	a.errorStatus = status
	// Both the public SSE envelope and the shared WS bridge need the mapped
	// client status; a protocol-specific code alone may default to HTTP 502.
	a.publicFailure = map[string]any{"code": code, "message": message, "status_code": status}
	for key, value := range cloudPublicErrorFields(err, code, message) {
		if key == "type" || key == "param" {
			a.publicFailure[key] = value
		}
	}
	var handshake *cloudHandshakeError
	if errors.As(err, &handshake) {
		a.publicFailure["upstream_http_status"] = handshake.StatusCode
	}
	var operation *cloudOperationError
	if errors.As(err, &operation) && operation.Method != "" {
		a.publicFailure["upstream_method"] = operation.Method
	}
	var upstream *cloudUpstreamError
	var rpc *cloudRPCError
	if errors.As(err, &rpc) {
		a.publicFailure["upstream_rpc_code"] = rpc.Code
		if method := cloudDiagnosticMethod(rpc.Method); method != "" {
			a.publicFailure["upstream_method"] = method
		}
		upstream = rpc.Upstream
	} else {
		_ = errors.As(err, &upstream)
	}
	if upstream != nil {
		if code := cloudKnownErrorCode(upstream.Code); code != "" {
			a.publicFailure["upstream_error_code"] = code
		}
		if upstream.Code == "cloud_message_too_large" {
			a.publicFailure["upstream_grpc_status"] = upstream.GRPCStatus
			a.publicFailure["upstream_message_bytes"] = upstream.MessageBytes
			a.publicFailure["upstream_message_limit_bytes"] = upstream.MessageLimitBytes
		}
		if upstream.HTTPStatus > 0 {
			a.publicFailure["upstream_http_status"] = upstream.HTTPStatus
		}
		if upstream.CodexErrorInfo != "" {
			a.publicFailure["upstream_error_info"] = upstream.CodexErrorInfo
		}
	}
	a.publicFailure["history_recovery_attempts"] = a.historyRecoveries
	a.publicFailure["output_observed"] = a.turnProducedOutput
	if a.record != nil {
		a.publicFailure["usage_observed"] = a.record.Usage != nil
	}

}
func (a *cloudSessionActor) terminateError(err error, fallback string, mayHaveExecuted bool) {
	a.setPublicFailure(err)
	if a.publicFailure != nil {
		fallback = a.publicFailure["message"].(string)
	}
	if !mayHaveExecuted {
		// No turn/start has been attempted: zero consumption is proven by the
		// gateway control flow, not invented from a missing upstream usage event.
		if a.previousThreadUsage != nil {
			a.lowerBound = true // A confirmed earlier segment remains billable.
		} else {
			a.record.Usage = &OpenAICloudUsage{}
			a.rejectedBeforeInference = true
		}
	}
	if a.responseID == "" {
		a.responseID = "resp_cloud_" + uuid.NewString()
	}
	a.terminate("failed", fallback, mayHaveExecuted)
}
