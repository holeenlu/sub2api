package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/tidwall/gjson"
)

// Codex remote compaction on Cloud.
//
// Codex 0.164 compacts an "OpenAI" provider's conversation with remote
// compaction v2: its history plus a final compaction_trigger item, answered by
// exactly one compaction output item whose encrypted_content it replays
// verbatim in place of the summarized turns. App Server's thread/compact/start
// keeps its result inside the thread, which Cloud releases after 15 idle
// minutes, so it cannot back a client history that lives for days. Cloud runs
// Codex's local compaction instead: a turn on the Cloud thread that asks for a
// handoff summary with Codex's prompt. The compaction item carries the summary,
// and a later request injects it as the message Codex installs after its own
// local compaction.

// Codex's compaction texts: openai/codex rust-v0.164.0-alpha.11 (Apache-2.0),
// codex-rs/prompts/templates/compact/prompt.md and summary_prefix.md.
const (
	cloudCompactionPrompt        = "You are performing a CONTEXT CHECKPOINT COMPACTION. Create a handoff summary for another LLM that will resume the task.\n\nInclude:\n- Current progress and key decisions made\n- Important context, constraints, or user preferences\n- What remains to be done (clear next steps)\n- Any critical data, examples, or references needed to continue\n\nBe concise, structured, and focused on helping the next LLM seamlessly continue the work.\n"
	cloudCompactionSummaryPrefix = "Another language model started to solve this problem and produced a summary of its thinking process. You also have access to the state of the tools that were used by that language model. Use this to build on the work that has already been done and avoid duplicating work. Here is the summary produced by the other language model, use the information in this summary to assist with your own analysis:"
)

// cloudCompactionEnvelope starts the encrypted_content of a compaction item
// this gateway issued; the base64url summary follows. The summary is injected
// with user authority only, so forging an envelope is no more than sending a
// user message and it needs no secrecy or signature.
const cloudCompactionEnvelope = "sub2api.cloud.summary.v1."

// A summary turn asks only for text. Codex's local compaction offers no tools;
// the Cloud thread registered the client's at thread start, so their calls
// are declined, and a model that insists ends the request.
const (
	cloudCompactionToolDeclined = "Tools are unavailable while summarizing the conversation for a context checkpoint. Reply with the summary only."
	cloudCompactionToolLimit    = 3
)

// cloudCompactionToolResults introduces the results a mid-turn compaction
// carries in place of tool replies, in the summary turn's input.
const cloudCompactionToolResults = "The task was paused for a context checkpoint while these tool calls ran. Their results:"

func cloudCompactionItem(summary string) map[string]any {
	return map[string]any{"type": "compaction", "id": "cmp_cloud_" + uuid.NewString(), "status": "completed",
		"encrypted_content": cloudCompactionEnvelope + base64.RawURLEncoding.EncodeToString([]byte(summary))}
}

// cloudCompactionSummary decodes a compaction item this gateway issued. Other
// compaction items (native, BPS) remain opaque upstream state.
func cloudCompactionSummary(item map[string]any) (string, bool, error) {
	content, _ := item["encrypted_content"].(string)
	encoded, ours := strings.CutPrefix(content, cloudCompactionEnvelope)
	if !ours {
		return "", false, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || len(raw) == 0 || !utf8.Valid(raw) {
		return "", true, cloudBadRequest("compaction encrypted_content")
	}
	return string(raw), true, nil
}

// cloudCompactionText is the user message Codex installs after its local
// compaction (compact.rs: the summary prefix, a newline, the summary).
func cloudCompactionText(summary string) string { return cloudCompactionSummaryPrefix + "\n" + summary }

func cloudCompactionMessage(summary string) map[string]any {
	return map[string]any{"type": "message", "role": "user", "content": []any{map[string]any{"type": "input_text", "text": cloudCompactionText(summary)}}}
}

// declineCompactionTool answers a client tool call made during a summary turn.
func (a *cloudSessionActor) declineCompactionTool(id json.RawMessage) error {
	a.compactionDeclines++
	if a.compactionDeclines > cloudCompactionToolLimit {
		return errors.New("Cloud summary turn kept calling tools")
	}
	return a.rpc.Reply(a.ctx, id, map[string]any{"contentItems": []any{map[string]any{"type": "inputText", "text": cloudCompactionToolDeclined}}, "success": false})
}

// finishCompaction publishes a completed summary turn as the one compaction
// item Codex installs. Without summary text there is nothing to install: the
// request fails and the turn's measured usage is still settled.
func (a *cloudSessionActor) finishCompaction() error {
	summary := ""
	for _, item := range a.output[a.compactionFrom:] {
		if item["type"] != "message" || item["status"] != "completed" {
			continue
		}
		if text, _ := item["content"].([]any)[0].(map[string]any)["text"].(string); strings.TrimSpace(text) != "" {
			summary = text
		}
	}
	if summary == "" {
		a.errorStatus = http.StatusBadGateway
		a.publicFailure = map[string]any{"code": "cloud_compaction_failed", "message": "Cloud compaction produced no summary; retry the compaction.", "status_code": http.StatusBadGateway}
		a.terminate("failed", "Cloud compaction produced no summary", false)
		return nil
	}
	item := cloudCompactionItem(summary)
	index := len(a.output)
	a.output = append(a.output, item)
	if err := a.emit("response.output_item.added", map[string]any{"output_index": index, "item": map[string]any{"id": item["id"], "type": "compaction", "status": "in_progress"}}); err != nil {
		return err
	}
	if err := a.emit("response.output_item.done", map[string]any{"output_index": index, "item": item}); err != nil {
		return err
	}
	return a.finish("completed", false, true, a.record.Usage, nil)
}

// compactionToolHistory selects only results for the live pending calls, in
// client order. Admission and retention use the same list, including image data.
func (a *cloudSessionActor) compactionToolHistory(p *cloudProtocolRequest) []any {
	var history []any
	seen := make(map[string]bool)
	for _, raw := range p.history {
		item, _ := raw.(map[string]any)
		id, _ := item["call_id"].(string)
		if item["type"] != "function_call_output" || a.tools[id] == nil || seen[id] {
			continue
		}
		seen[id] = true
		history = append(history, map[string]any{"type": "function_call_output", "call_id": id, "output": p.outputHistory[id]})
	}
	return history
}

// compactActiveTurn summarizes a turn that waits for client tools. Codex sends
// the tool results with the trigger rather than as replies. Compaction must
// end the active task before starting the summary, so its turn is interrupted,
// its pending calls die with it, and the summary turn on the same
// thread receives the results as input. The billing root continues: usage keeps
// accumulating from the root's base, so one settlement covers both turns.
func (a *cloudSessionActor) compactActiveTurn(command OpenAICloudCommand, p *cloudProtocolRequest) error {
	// Text that arrived while the tools ran belongs to the summarized turn.
	if err := a.flushBufferedEvents(); err != nil {
		return err
	}
	if err := a.checkRequest(command); err != nil {
		return err
	}
	history := a.compactionToolHistory(p)
	size, err := cloudHistoryRetainedSize(append(history, p.inputMessage))
	if err != nil {
		return err
	}
	if size > OpenAICloudMaxRetainedBytes-a.retainedBytes {
		return errors.New("Cloud session retained data reached its limit")
	}
	if err := a.retainHistory(history...); err != nil {
		return err
	}
	if err := a.checkRequest(command); err != nil {
		return err
	}
	if !a.interruptForContinuation("compaction") {
		return nil
	}
	if err := a.checkRequest(command); err != nil {
		if _, _, _, known := cloudPublicError(err); known {
			a.setPublicFailure(err)
		}
		a.lowerBound = a.record.Usage != nil && !a.usageTainted && !a.uncertain
		a.terminate("interrupted", "Cloud compaction request ended before summary inference", !a.lowerBound)
		return nil
	}
	// Interruption drains late output into history. Recheck the prompt against
	// that new retained size before allowing the summary inference to start.
	size, err = cloudHistoryRetainedSize([]any{p.inputMessage})
	if err != nil || size > OpenAICloudMaxRetainedBytes-a.retainedBytes {
		// The terminal interruption was already consumed. Preserve its measured
		// lower bound instead of asking stop() to interrupt the ended turn again.
		a.lowerBound = a.record.Usage != nil && !a.usageTainted && !a.uncertain
		a.terminate("interrupted", "Cloud retained history reached its limit before summary inference", !a.lowerBound)
		return nil
	}
	a.compactionFrom = len(a.output)
	turn := a.compactionTurnParams(p)
	a.tools = make(map[string]*cloudPendingTool)
	a.record.Status, a.record.TurnID = "running", ""
	rpcCtx, cancel := a.historyRPCContext()
	result, err := a.rpc.Call(rpcCtx, "turn/start", turn)
	cancel()
	if err != nil {
		a.terminateError(err, "Cloud compaction turn acceptance is unknown; it will not be replayed", true)
		return nil
	}
	var started struct {
		Turn struct {
			ID string `json:"id"`
		} `json:"turn"`
	}
	if json.Unmarshal(result, &started) != nil || started.Turn.ID == "" {
		a.terminate("unknown", "Cloud compaction turn identity was not confirmed", true)
		return nil
	}
	a.record.TurnID = started.Turn.ID
	if err := a.retainHistory(p.inputMessage); err != nil {
		return err
	}
	if err := a.save(nil, nil); err != nil {
		a.stop("Cloud compaction turn binding could not be persisted")
	}
	return nil
}

// interruptForContinuation ends the turn waiting for client tools. Only its
// confirmed end lets the next turn/start begin a new turn instead of steering
// this one; without it the request ends with an unknown outcome, as stop does.
func (a *cloudSessionActor) interruptForContinuation(reason string) bool {
	// Drain late content as part of the existing response, without allowing a
	// completed message to publish the blocked tools again during the handoff.
	wasDraining := a.drainingBuffered
	a.drainingBuffered = true
	defer func() { a.drainingBuffered = wasDraining }()
	ctx, cancel := context.WithTimeout(context.WithoutCancel(a.ctx), 10*time.Second)
	defer cancel()
	_, err := a.rpc.Call(ctx, "turn/interrupt", map[string]any{"threadId": a.record.ThreadID, "turnId": a.record.TurnID})
	for err == nil {
		select {
		case delivery, ok := <-a.upstream:
			if !ok || delivery.err != nil {
				err = delivery.err
				if err == nil {
					err = errCloudRPCClosed
				}
				continue
			}
			if err = a.rpc.eventError(); err != nil {
				continue
			}
			event := delivery.event
			thread := gjson.GetBytes(event.Params, "threadId")
			switch {
			case !thread.Exists() || thread.String() != a.record.ThreadID:
				// handleEvent declines this connection's thread-less server
				// requests and ignores other threads' events.
				err = a.handleEvent(event)
			case event.Method == "turn/completed" && gjson.GetBytes(event.Params, "turn.id").String() == a.record.TurnID:
				if gjson.GetBytes(event.Params, "turn.status").String() != "interrupted" {
					a.terminate("unknown", "Cloud did not confirm the requested interruption", true)
					return false
				}
				if err := a.flushPendingMessages(); err != nil {
					a.terminate("unknown", "Cloud interrupted history could not be retained", true)
					return false
				}
				return true
			case event.Method == "thread/tokenUsage/updated":
				err = a.handleEvent(event)
			case len(event.ID) > 0:
				// The interrupted turn may still ask; it will never be answered.
				_ = a.rpc.send(a.ctx, map[string]any{"id": event.ID, "error": map[string]any{"code": -32601, "message": "Sub2API does not authorize this server request"}})
			default:
				// Model content already produced before interruption remains part
				// of the conversation, including raw reasoning and late messages.
				err = a.handleEvent(event)
				if a.record.Status != "running" && a.record.Status != "awaiting_tools" {
					return false
				}
			}
		case <-ctx.Done():
			err = ctx.Err()
		}
	}
	a.terminate("unknown", "Cloud did not confirm the end of the turn it interrupted for "+reason+"; execution will not be replayed", true)
	return false
}

func (a *cloudSessionActor) compactionTurnParams(p *cloudProtocolRequest) map[string]any {
	history := a.compactionToolHistory(p)
	var results []any
	for _, raw := range history {
		id := raw.(map[string]any)["call_id"].(string)
		tool := a.tools[id]
		if results == nil {
			results = []any{map[string]any{"type": "text", "text": cloudCompactionToolResults, "text_elements": []any{}}}
		}
		results = append(results, map[string]any{"type": "text", "text": tool.Name + " " + tool.Arguments + ":", "text_elements": []any{}})
		for _, rawPart := range p.outputItems[id] {
			part := rawPart.(map[string]any)
			if part["type"] == "inputImage" {
				image := map[string]any{"type": "image", "url": part["imageUrl"]}
				if detail, supplied := part["detail"]; supplied {
					image["detail"] = detail
				}
				results = append(results, image)
			} else {
				results = append(results, map[string]any{"type": "text", "text": part["text"], "text_elements": []any{}})
			}
		}
	}
	turn := map[string]any{"threadId": a.record.ThreadID, "input": append(results, p.input...), "environments": []any{}, "clientUserMessageId": uuid.NewString()}
	if p.effort != "" {
		turn["effort"] = p.effort
	}
	if p.summary != "" {
		turn["summary"] = p.summary
	}

	return turn
}
