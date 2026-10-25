package service

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/tidwall/gjson"
)

func (a *cloudSessionActor) handleEvent(event cloudRPCMessage) error {
	// Establish ownership before decoding typed payloads. A future or malformed
	// item from another account thread must not terminate this actor.
	thread := gjson.GetBytes(event.Params, "threadId")
	if !thread.Exists() && len(event.ID) > 0 && a.rpc != nil {
		// This actor owns its connection, so a server request without a thread
		// is addressed to it alone. Decline it at once so the server never waits
		// on it; it is not part of the running turn and does not end it.
		_ = a.rpc.send(a.ctx, map[string]any{"id": event.ID, "error": map[string]any{"code": -32601, "message": "Sub2API does not authorize this server request"}})
		return nil
	}
	if thread.Type != gjson.String || thread.String() != a.record.ThreadID || a.record.ThreadID == "" {
		return nil
	}
	turn := gjson.GetBytes(event.Params, "turnId")
	if event.Method == "turn/completed" {
		turn = gjson.GetBytes(event.Params, "turn.id")
	}
	if turn.Exists() && (turn.Type != gjson.String || turn.String() != a.record.TurnID) {
		return nil
	}

	var p struct {
		ThreadID  string          `json:"threadId"`
		TurnID    string          `json:"turnId"`
		ItemID    string          `json:"itemId"`
		Delta     string          `json:"delta"`
		CallID    string          `json:"callId"`
		Tool      string          `json:"tool"`
		Namespace *string         `json:"namespace"`
		Arguments json.RawMessage `json:"arguments"`
		Item      struct {
			ID   string `json:"id"`
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"item"`
		Turn struct {
			ID     string          `json:"id"`
			Status string          `json:"status"`
			Error  json.RawMessage `json:"error"`
		} `json:"turn"`
		TokenUsage struct {
			Total json.RawMessage `json:"total"`
		} `json:"tokenUsage"`
	}
	if json.Unmarshal(event.Params, &p) != nil {
		return errors.New("Cloud event payload is invalid")
	}
	// Cloud may broadcast notifications for other threads on this account,
	// including archived or injected history. Filter before classification/output.
	turnID := p.TurnID
	if event.Method == "turn/completed" {
		turnID = p.Turn.ID
	}
	if p.ThreadID != "" && p.ThreadID != a.record.ThreadID || turnID != "" && turnID != a.record.TurnID {
		// A foreign server request belongs to another connection's actor. Do not
		// answer it: sending an error could cancel that actor's pending client tool.
		return nil
	}
	if event.Method == "model/rerouted" || event.Method == "thread/model/changed" {
		if p.ThreadID == "" {
			return nil
		}
		a.usageTainted = true
		return errors.New("Cloud changed the selected model")
	}
	switch event.Method {
	case "item/tool/call", "item/started", "item/completed", "item/agentMessage/delta", "thread/tokenUsage/updated", "turn/completed", "rawResponseItem/completed", "error":
		if p.ThreadID == "" || turnID == "" {
			return errors.New("Cloud event ownership is missing")
		}
	default:
		if len(event.ID) == 0 {
			return nil
		}
	}
	if len(event.ID) > 0 && event.Method != "item/tool/call" {
		_ = a.rpc.send(a.ctx, map[string]any{"id": event.ID, "error": map[string]any{"code": -32601, "message": "Sub2API does not authorize this server request"}})
		return errors.New("Cloud requested an undeclared server capability")
	}
	if a.record.Status == "completed" || a.record.Status == "failed" || a.record.Status == "interrupted" || a.record.Status == "unknown" {
		return nil
	}
	// Client tool responses close one downstream response while the upstream turn
	// remains alive. Retain interleaved text/reasoning until the next response.
	if a.requestID == "" && a.record.Status == "awaiting_tools" && cloudBuffersDuringToolWait(event) {
		if len(a.bufferedEvents) >= 256 || a.bufferedEventBytes+a.hostedBufferedBytes+len(event.Params) > OpenAICloudMaxResultBytes {
			return errors.New("Cloud interleaved output exceeded its buffer limit")
		}
		a.bufferedEvents = append(a.bufferedEvents, event)
		a.bufferedEventBytes += len(event.Params)
		return nil
	}
	switch event.Method {
	case "error":
		notice, err := cloudParseErrorNotification(event.Params)
		if err != nil {
			return err
		}
		if notice.WillRetry {
			return nil
		}
		// Persist the safe classification; only turn/completed proves the end.
		a.setPublicFailure(notice.Err)
		a.publicFailure["upstream_method"] = "error"
		return nil
	case "rawResponseItem/completed":
		var raw struct {
			Item json.RawMessage `json:"item"`
		}
		if json.Unmarshal(event.Params, &raw) != nil {
			return errCloudRPCProtocol
		}
		return a.handleRawItem(raw.Item)
	case "item/tool/call":
		a.turnProducedOutput = true
		a.handoffBeforeInference = false
		if len(event.ID) == 0 || p.CallID == "" || p.Tool == "" || !a.protocol.toolNames[p.Tool] || p.Namespace == nil || *p.Namespace != cloudClientToolNamespace || !json.Valid(p.Arguments) {
			if len(event.ID) > 0 {
				_ = a.rpc.send(a.ctx, map[string]any{"id": event.ID, "error": map[string]any{"code": -32601, "message": "Sub2API only permits declared client tools"}})
			}
			return errors.New("Cloud requested an undeclared or invalid tool")
		}
		order := 0
		for _, tool := range a.tools {
			if string(tool.RPCID) == string(event.ID) {
				return errors.New("Cloud repeated a pending tool RPC")
			}
			order = max(order, tool.Order+1)
		}
		if a.compacting {
			return a.declineCompactionTool(event.ID)
		}
		// Answered calls leave the registry, so this bounds only the calls
		// waiting for client results at once, not a turn's total.
		if len(a.tools) >= 256 {
			_ = a.rpc.send(a.ctx, map[string]any{"id": event.ID, "error": map[string]any{"code": -32601, "message": "Sub2API accepts at most 256 unanswered client tool calls"}})
			return errors.New("Cloud requested more than 256 unanswered client tool calls")
		}
		if err := a.flushPendingMessages(); err != nil {
			return err
		}
		id := "call_cloud_" + uuid.NewString()
		a.retainedBytes += len(p.Arguments)
		if a.retainedBytes > OpenAICloudMaxRetainedBytes {
			return errors.New("Cloud retained session bytes exceeded their limit")
		}
		tool := &cloudPendingTool{RPCID: event.ID, CallID: id, Name: p.Tool, Arguments: string(p.Arguments), Order: order}
		a.tools[id] = tool
		if err := a.save([]string{id}, nil); err != nil {
			return errors.New("Cloud tool ownership could not be persisted")
		}
		if a.requestID != "" {
			return a.finishAwaiting()
		}
	case "thread/tokenUsage/updated":
		a.handoffBeforeInference = false
		if a.usageTainted {
			return errors.New("Cloud model changed; subsequent usage cannot be attributed")
		}
		total, err := cloudParseUsage(p.TokenUsage.Total)
		if err != nil {
			return err
		}
		step, err := cloudUsageDifference(total, a.threadTotal)
		if err != nil {
			return err
		}
		usage, err := cloudUsageDifference(total, a.turnBase)
		if err != nil {
			return err
		}
		// Each new total follows one sampling step; a repeated one confirms none.
		if step.TotalTokens > 0 {
			copy := *step
			a.lastStep, a.lastStepPublished = &copy, false
		}
		a.threadTotal = total
		a.record.Usage = a.withHostedUsage(cloudSumTokenUsage(a.previousThreadUsage, usage))
		if err := a.save(nil, nil); err != nil {
			return errors.New("Cloud measured usage could not be persisted")
		}
	case "item/started":
		if p.Item.Type != "userMessage" && p.Item.Type != "contextCompaction" {
			a.turnProducedOutput = true
			a.handoffBeforeInference = false
		}
		if p.Item.Type == "contextCompaction" {
			a.threadCompacted = true
		}
		if p.Item.Type == "webSearch" || p.Item.Type == "imageGeneration" {
			return a.handleHostedItem(event.Params, false)
		}
		if p.Item.Type == "agentMessage" {
			return a.startMessage(p.Item.ID)
		}
		if cloudForbiddenItem(p.Item.Type) {
			return errors.New("Cloud attempted an undeclared built-in tool")
		}
	case "item/agentMessage/delta":
		a.turnProducedOutput = true
		a.handoffBeforeInference = false
		if a.requestID == "" {
			return errors.New("Cloud emitted text without an active response")
		}
		if err := a.startMessage(p.ItemID); err != nil {
			return err
		}
		index := a.items[p.ItemID]
		item := a.output[index]
		part := item["content"].([]any)[0].(map[string]any)
		a.outputBytes += len(p.Delta)
		if a.outputBytes > OpenAICloudMaxResultBytes {
			return errors.New("Cloud response exceeded its size limit")
		}
		part["text"] = part["text"].(string) + p.Delta
		return a.emit("response.output_text.delta", map[string]any{"output_index": index, "item_id": item["id"], "content_index": 0, "delta": p.Delta})
	case "item/completed":
		if p.Item.Type != "userMessage" && p.Item.Type != "contextCompaction" {
			a.turnProducedOutput = true
			a.handoffBeforeInference = false
		}
		if p.Item.Type == "contextCompaction" {
			a.threadCompacted = true
		}
		if p.Item.Type == "webSearch" || p.Item.Type == "imageGeneration" {
			return a.handleHostedItem(event.Params, true)
		}
		if cloudForbiddenItem(p.Item.Type) {
			return errors.New("Cloud completed an undeclared built-in tool")
		}
		if p.Item.Type != "agentMessage" {
			return nil
		}
		if err := a.startMessage(p.Item.ID); err != nil {
			return err
		}
		index := a.items[p.Item.ID]
		item := a.output[index]
		if item["status"] == "completed" {
			return nil
		}
		part := item["content"].([]any)[0].(map[string]any)
		observed := part["text"].(string)
		if !strings.HasPrefix(p.Item.Text, observed) {
			return errors.New("Cloud final text does not match its streamed output")
		}
		if len(p.Item.Text) > len(observed) {
			delta := p.Item.Text[len(observed):]
			a.outputBytes += len(delta)
			if a.outputBytes > OpenAICloudMaxResultBytes {
				return errors.New("Cloud response exceeded its size limit")
			}
			part["text"] = p.Item.Text
			if err := a.emit("response.output_text.delta", map[string]any{"output_index": index, "item_id": item["id"], "content_index": 0, "delta": delta}); err != nil {
				return err
			}
		}
		if a.pendingMessages == nil {
			a.pendingMessages = make(map[string]string)
		}
		a.pendingMessages[p.Item.ID] = p.Item.Text
		return nil

	case "turn/completed":
		if err := a.flushPendingMessages(); err != nil {
			return err
		}
		if upstream := cloudParseTurnError(p.Turn.Error); upstream != nil {
			if p.Turn.Status == "failed" && a.recoverEncryptedHistory(upstream, "turn/completed", a.checkHistoryRecoveryDispatch) {
				a.startTurn(a.protocol, a.checkHistoryRecoveryDispatch)
				return nil
			}
			if a.record.Status != "running" && a.record.Status != "awaiting_tools" {
				return nil
			}
			a.setPublicFailure(upstream)
			a.publicFailure["upstream_method"] = "turn/completed"
		}
		if p.Turn.Status != "completed" {
			status := "failed"
			if p.Turn.Status == "interrupted" {
				status = "interrupted"
			}
			unknown := true
			message := "Cloud turn did not complete successfully; remaining usage is unconfirmed"
			if !a.turnProducedOutput && a.record.Usage == nil && cloudRejectedBeforeInference(cloudParseTurnError(p.Turn.Error)) {
				// An explicit input/auth/capacity rejection before any model/tool output
				// proves no inference; an absent usage event on its own never does.
				a.rejectedBeforeInference = true
				a.record.Usage = &OpenAICloudUsage{}
				unknown = false
			} else if a.record.Usage != nil {
				// The upstream confirmed how the turn ended, so the cumulative usage
				// it reported covers every completed sampling step. Bill it as the
				// native gateway bills an incomplete/failed response; the discarded
				// in-flight step is not estimated and the row is marked partial.
				a.lowerBound = true
				unknown = false
				message = "Cloud turn ended early; its confirmed usage is billed as a lower bound"
			}
			a.terminate(status, message, unknown)
			return nil
		}
		if (len(p.Turn.Error) > 0 && string(p.Turn.Error) != "null") || a.hasPendingTools() || a.hasPendingHosted() || a.record.Usage == nil {
			a.terminate("unknown", "Cloud terminal output or measured usage is incomplete", true)
			return nil
		}
		for _, item := range a.output {
			if item["status"] != "completed" {
				return errors.New("Cloud completed before its output was complete")
			}
		}
		a.record.Status = "completed"
		a.record.Failure = ""
		if a.requestID == "" {
			return errors.New("Cloud completed without an active response")
		}
		if a.compacting {
			return a.finishCompaction()
		}
		return a.finish("completed", false, true, a.record.Usage, nil)
	}
	return nil
}

func cloudForbiddenItem(kind string) bool {
	switch kind {
	case "userMessage", "agentMessage", "reasoning", "dynamicToolCall", "contextCompaction":
		return false
	}
	return true
}

func (a *cloudSessionActor) startMessage(id string) error {
	if id == "" || a.requestID == "" {
		return errors.New("Cloud output item identity is invalid")
	}
	if _, exists := a.hostedStates[id]; exists {
		return errors.New("Cloud reused a hosted output identity")
	}
	if index, exists := a.items[id]; exists {
		if a.output[index]["type"] != "message" {
			return errors.New("Cloud reused an output identity")
		}
		return nil
	}
	index := len(a.output)
	if index >= 1024 {
		return errors.New("Cloud output item count exceeded its limit")
	}
	item := map[string]any{"id": "msg_cloud_" + uuid.NewString(), "type": "message", "role": "assistant", "status": "in_progress", "content": []any{map[string]any{"type": "output_text", "text": "", "annotations": []any{}}}}
	a.items[id] = index
	a.output = append(a.output, item)
	added := map[string]any{"id": item["id"], "type": "message", "role": "assistant", "status": "in_progress", "content": []any{}}
	if err := a.emit("response.output_item.added", map[string]any{"output_index": index, "item": added}); err != nil {
		return err
	}
	return a.emit("response.content_part.added", map[string]any{"output_index": index, "item_id": item["id"], "content_index": 0, "part": item["content"].([]any)[0]})
}

func (a *cloudSessionActor) settlement(status string, unknown bool) *OpenAICloudSettlement {
	code := ""
	if status != "completed" {
		code, _ = a.publicFailure["code"].(string)
	}
	unknown = unknown || a.record.Usage == nil
	return &OpenAICloudSettlement{OpenAICloudBinding: a.record.OpenAICloudBinding, BillingSnapshot: a.record.BillingSnapshot, Status: status, Usage: a.record.Usage, Telemetry: a.record.Telemetry, Sequence: a.record.Sequence + 1, ObservedAt: time.Now(), Failure: a.record.Failure, ErrorCode: code, Unknown: unknown,
		LowerBound: a.lowerBound && !unknown && status != "completed"}
}

func cloudBuffersDuringToolWait(event cloudRPCMessage) bool {
	switch event.Method {
	case "item/agentMessage/delta":
		return true
	case "item/started", "item/completed":
		return gjson.GetBytes(event.Params, "item.type").String() == "agentMessage"
	case "rawResponseItem/completed":
		switch gjson.GetBytes(event.Params, "item.type").String() {
		case "message", "reasoning", "compaction":
			return true
		}
	}
	return false
}

func (a *cloudSessionActor) flushBufferedEvents() error {
	events := a.bufferedEvents
	a.bufferedEvents, a.bufferedEventBytes = nil, 0
	a.drainingBuffered = true
	defer func() { a.drainingBuffered = false }()
	for _, event := range events {
		if err := a.handleEvent(event); err != nil {
			return err
		}
	}
	return nil
}
