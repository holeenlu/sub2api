package service

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
)

// Keep the client's declarations for inheritance, and use the same reversible
// tool-search/history lowering as the other function-only Responses upstreams.
// Cloud's RPC mapping does not define a separate discovery wire format.
func cloudAdaptSearchTools(p *cloudProtocolRequest) (map[string]any, error) {
	ownedHistory := false
	if input, ok := p.body["input"].([]any); ok {
		for _, raw := range input {
			item, _ := raw.(map[string]any)
			id, _ := item["call_id"].(string)
			if (item["type"] == "tool_search_call" || item["type"] == "tool_search_output") && strings.HasPrefix(id, "call_cloud_") {
				ownedHistory = true
				break
			}
		}
	}
	if !p.toolSearch && !ownedHistory {
		return p.body, nil
	}
	body := cloneCloudValue(p.body).(map[string]any)
	var err error
	if !p.toolSearch {
		// An incremental result can omit tools. Lower its owned references using
		// the shared inherited mapping, without inventing a declaration. The
		// actor inherits its real catalog after ownership has been checked.
		history := map[string]any{"input": body["input"]}
		_, _, err := apicompat.AdaptResponsesClientToolsWithInheritedMapping(history, apicompat.ResponsesClientToolMapping{ToolSearch: true})
		body["input"] = history["input"]
		return body, err
	}
	// The shared adapter preserves custom input text. Retain Cloud's existing
	// grammar guidance as well, including for tools returned by discovery.
	var describe func([]any)
	describe = func(tools []any) {
		for _, raw := range tools {
			tool, _ := raw.(map[string]any)
			if tool["type"] == "namespace" {
				children, _ := tool["tools"].([]any)
				describe(children)
			}
			if tool["type"] == "custom" {
				description, _ := tool["description"].(string)
				format, _ := tool["format"].(map[string]any)
				tool["description"] = cloudCustomToolDescription(description, format)
			}
		}
	}
	tools, _ := body["tools"].([]any)
	// Adapt first, then add custom guidance to the resulting function. Changing
	// a declaration before discovery deduplication would create false conflicts.
	mapping, _, err := apicompat.AdaptResponsesClientTools(body)
	if err != nil {
		return nil, err
	}
	p.names, p.customTools = mapping.NamespaceTools, mapping.CustomTools
	// Preserve original grammar descriptions keyed by the shared namespace name.
	if len(mapping.CustomTools) > 0 {
		describe(tools)
		descriptions := map[string]string{}
		var collect func([]any, string)
		collect = func(list []any, namespace string) {
			for _, raw := range list {
				tool, _ := raw.(map[string]any)
				name, _ := tool["name"].(string)
				if tool["type"] == "namespace" {
					children, _ := tool["tools"].([]any)
					collect(children, name)
				} else if tool["type"] == "custom" {
					for flat, identity := range mapping.NamespaceTools {
						if identity.Namespace == namespace && identity.Name == name {
							name = flat
							break
						}
					}
					descriptions[name], _ = tool["description"].(string)
				}
			}
		}
		collect(tools, "")
		for _, raw := range body["tools"].([]any) {
			tool := raw.(map[string]any)
			name, _ := tool["name"].(string)
			if description := descriptions[name]; mapping.CustomTools[name] && description != "" {
				tool["description"] = description
			}
		}
	}
	// Shared schemas use json.RawMessage; the RPC parser uses decoded objects.
	// Decode those schemas only; do not serialize the whole history a second
	// time, especially when it contains images or opaque encrypted context.
	for _, raw := range body["tools"].([]any) {
		tool := raw.(map[string]any)
		if schema, ok := tool["parameters"].(json.RawMessage); ok {
			var decoded any
			decoder := json.NewDecoder(bytes.NewReader(schema))
			decoder.UseNumber()
			if err := decoder.Decode(&decoded); err != nil {
				return nil, err
			}
			tool["parameters"] = decoded
		}
	}
	return body, nil
}

// Cloud registers tools at thread/start and dynamic replies cannot retain
// original image detail. Both cases reuse one confirmed history handoff: end
// the blocked inference, retain measured cost, then import the conversation
// and exact tool results into a fresh thread. Never re-execute a client tool.
func (a *cloudSessionActor) restartPendingTurn(command OpenAICloudCommand, p *cloudProtocolRequest, continuation []any) error {
	if err := a.checkRequest(command); err != nil {
		return err
	}
	// Bind before interruption/archive, including direct handoff callers.
	a.historyRecoveryCommand = command
	a.historyRecoveryCommand.Request.Body = nil
	// The client supplied these results before receiving this response's late
	// events. Keep that same order in retained history and the replacement thread.
	a.history = append(a.history, continuation...)
	encoded, _ := json.Marshal(continuation)
	a.retainedBytes += len(encoded)
	if err := a.flushBufferedEvents(); err != nil {
		return err
	}
	if !a.interruptForContinuation("tool history handoff") {
		return nil
	}
	if a.record.Usage == nil || a.usageTainted || a.uncertain {
		a.terminate("unknown", "Cloud tool history handoff lacks confirmed usage; execution will not be replayed", true)
		return nil
	}
	a.previousThreadUsage = a.record.Usage
	checkDispatch := func() bool {
		err := a.checkRequest(command)
		if err == nil {
			err = a.service.validate(a.ctx, a.record.OpenAICloudOwner)
		}
		if err != nil {
			a.terminateError(err, "Cloud tool history handoff ended before inference", false)
		}
		return err == nil
	}
	if !checkDispatch() {
		return nil
	}
	oldThread := a.record.ThreadID
	rpcCtx, cancel := a.historyRPCContext()
	_, err := a.rpc.Call(rpcCtx, "thread/archive", map[string]any{"threadId": oldThread})
	cancel()
	if err != nil {
		a.terminateError(err, "Cloud could not close the previous tool thread", false)
		return nil
	}
	a.tools = make(map[string]*cloudPendingTool)
	a.protocol = p
	a.record.TurnID = ""
	a.record.Status = "running"
	a.threadTotal, a.turnBase = nil, nil
	if !checkDispatch() || !a.startThread(p) {
		return nil
	}
	if a.record.ThreadID == oldThread {
		a.terminateError(errors.New("Cloud reused an archived thread"), "Cloud tool history identity is invalid", false)
		return nil
	}
	snapshot := *p
	snapshot.history = a.history
	a.historyRecoveryItems = cloudInjectionHistory(&snapshot)
	a.historyRecoveries = 0
	a.handoffBeforeInference = true
	if !checkDispatch() || !a.injectHistory(p, checkDispatch) {
		return nil
	}
	if checkDispatch() {
		a.startTurn(p, checkDispatch)
	}
	return nil
}

func cloudSumTokenUsage(previous, current *OpenAICloudUsage) *OpenAICloudUsage {
	if previous == nil {
		return current
	}
	if current == nil {
		return previous
	}
	sum := *current
	sum.InputTokens += previous.InputTokens
	sum.OutputTokens += previous.OutputTokens
	sum.TotalTokens += previous.TotalTokens
	sum.CachedInputTokens += previous.CachedInputTokens
	sum.CacheWriteInputTokens += previous.CacheWriteInputTokens
	sum.ReasoningOutputTokens += previous.ReasoningOutputTokens
	return &sum
}
