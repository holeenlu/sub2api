package service

import (
	"context"
	"errors"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
)

// A same-account history reduction is not account failover. RPC errors also
// check events received before the reply: the actor may not have consumed them.
func (a *cloudSessionActor) encryptedHistoryRejectedBeforeInference(err error, stage string) bool {
	// A catalog handoff preserves the already ended segment's output and cost.
	// Only its fresh, still unstarted segment may repair rejected history; the
	// whole root is never reset or offered to account/protocol failover.
	unstartedHandoff := a.handoffBeforeInference && a.previousThreadUsage != nil && a.threadTotal == nil
	if (!unstartedHandoff && (a.turnProducedOutput || a.record.Usage != nil || a.threadCompacted || a.threadCompactionStarted || len(a.output) != 0)) ||
		a.uncertain || a.lowerBound || a.usageTainted || a.hasPendingTools() || a.hasPendingHosted() ||
		len(a.bufferedEvents) != 0 || a.requestID == "" {
		return false
	}
	var rpc *cloudRPCError
	if errors.As(err, &rpc) && rpc.EventsObserved {
		return false
	}
	if stage == "thread/inject_items" && (cloudThreadDiscarded(err) || cloudInjectRejected(err, stage)) {
		return true
	}
	var upstream *cloudUpstreamError
	if errors.As(err, &rpc) {
		upstream = rpc.Upstream
	} else {
		_ = errors.As(err, &upstream)
	}
	if upstream == nil {
		return false
	}
	switch upstream.Code {
	case "invalid_encrypted_content", "invalid_compaction", "compaction_decode_error":
		return true
	}
	return false
}

func (a *cloudSessionActor) checkHistoryRecoveryDispatch() bool {
	err := a.checkRequest(a.historyRecoveryCommand)
	if err == nil {
		err = a.service.validate(a.ctx, a.record.OpenAICloudOwner)
	}
	if err != nil {
		a.terminateError(err, "Cloud history recovery stopped before inference", false)
		return false
	}
	return true
}

// All rejection sites consume the same two-stage budget and native reducer.
// The snapshot holds complete history before this turn's input, including
// actor-owned history for previous_response_id continuations.
func (a *cloudSessionActor) recoverEncryptedHistory(rejection error, stage string, checkDispatch func() bool) bool {
	code := "invalid_encrypted_content"
	var rpc *cloudRPCError
	var upstream *cloudUpstreamError
	if errors.As(rejection, &rpc) {
		upstream = rpc.Upstream
	} else {
		_ = errors.As(rejection, &upstream)
	}
	upstreamCode := ""
	if upstream != nil {
		upstreamCode = upstream.Code
	}
	switch {
	case upstreamCode == "invalid_encrypted_content", upstreamCode == "invalid_compaction", upstreamCode == "compaction_decode_error":
		code = upstreamCode
	case upstreamCode == "cloud_thread_unavailable":
		if stage != "thread/inject_items" {
			return false
		}
	case cloudInjectRejected(rejection, stage):
		code = "cloud_inject_rejected"
	default:
		return false
	}
	reason, rejectedItem := "", ""
	if rpc != nil && rpc.Reason != "" {
		reason = rpc.Reason
		rejectedItem = cloudRejectedItemShape(a.historyRecoveryItems, reason)
	}
	log := func(outcome string) {
		logger.LegacyPrintfLevel(logger.LevelWarn, "service.openai_cloud",
			"Cloud encrypted history recovery stage=%s code=%s attempts=%d outcome=%s root=%s account=%d (%s) reason=%q rejected_item=%q",
			stage, code, a.historyRecoveries, outcome, a.record.RootRequestID, a.record.AccountID, openAIHistoryShape(a.historyRecoveryItems), reason, rejectedItem)
	}
	if !a.encryptedHistoryRejectedBeforeInference(rejection, stage) {
		log("execution_not_proven_safe")
		return false
	}
	if a.historyRecoveryCommand.RequestID != a.requestID {
		log("request_snapshot_unavailable")
		return false
	}
	if a.historyRecoveries >= len(cloudHistoryRecoveryStages) {
		log("exhausted")
		return false
	}
	reduced, reduction, changed := cloudReduceRejectedHistory(a.historyRecoveryItems, code)
	if !changed {
		log("no_removable_history")
		return false
	}
	if !checkDispatch() || !a.checkHistoryRecoveryDispatch() {
		log("dispatch_stopped")
		return false
	}
	a.historyRecoveries++
	log("retry_same_account")
	oldThread := a.record.ThreadID
	// Explicit rejection leaves a thread behind; unknown-thread already
	// discarded it. Archive before replacing its persisted binding.
	if !cloudThreadDiscarded(rejection) {
		rpcCtx, cancel := a.historyRPCContext()
		_, err := a.cleanupCloudCall(rpcCtx, "thread/archive", map[string]any{"threadId": oldThread})
		cancel()
		if err != nil {
			log("archive_failed")
			return false
		}
	}
	if !a.checkHistoryRecoveryDispatch() {
		return false
	}
	a.historyRecoveryItems = reduced
	cloudDecision(&a.protocol.decisions, "history.inject."+reduction, "map", "CLOUD-RECOVERY")
	a.record.TurnID = ""
	a.threadTotal, a.turnBase, a.lastStep = nil, nil, nil
	a.publicFailure, a.errorStatus = nil, 0
	if !a.startThread(a.protocol) {
		return false
	}
	if a.record.ThreadID == oldThread {
		a.terminateError(errCloudRPCProtocol, "Cloud recovery did not create a fresh thread", false)
		return false
	}
	if !a.checkHistoryRecoveryDispatch() {
		return false
	}
	return a.injectHistory(a.protocol, checkDispatch)
}

// Every RPC remains inside the currently accepted request deadline; retries do not reset it.
func (a *cloudSessionActor) historyRPCContext() (context.Context, context.CancelFunc) {
	if !a.historyRecoveryCommand.Deadline.IsZero() {
		return context.WithDeadline(a.ctx, a.historyRecoveryCommand.Deadline)
	}
	return context.WithCancel(a.ctx)
}

// Only the two approved reductions are executable. No third-stage switch or
// fallback exists. The actor retains all existing proof, deadline and budget guards.
var cloudHistoryRecoveryStages = [...]struct {
	name   string
	reduce func([]any) ([]any, bool)
}{
	{"reasoning", openAIHistoryWithoutReasoningCiphertext},
	{"compaction", openAIHistoryWithoutCompactionCiphertext},
}

func cloudReduceRejectedHistory(items []any, code string) ([]any, string, bool) {
	start := 0
	if code == "invalid_compaction" || code == "compaction_decode_error" {
		start = 1
	}
	for _, stage := range cloudHistoryRecoveryStages[start:] {
		if reduced, changed := stage.reduce(items); changed {
			return reduced, stage.name, true
		}
	}
	return nil, "", false
}

func cloudInjectRejected(err error, stage string) bool {
	var rpc *cloudRPCError
	return stage == "thread/inject_items" && errors.As(err, &rpc) &&
		(rpc.Upstream == nil || rpc.Upstream.Code != "cloud_message_too_large") &&
		(rpc.Code == -32602 || rpc.Code == -32600)
}

func (a *cloudSessionActor) logInjectRejection(err error) {
	var rpc *cloudRPCError
	if !errors.As(err, &rpc) || rpc.Reason == "" {
		return
	}
	logger.LegacyPrintfLevel(logger.LevelWarn, "service.openai_cloud",
		"Cloud rejected the injected history before inference rpc_code=%d attempts=%d root=%s request=%s account=%d (%s) reason=%q rejected_item=%q",
		rpc.Code, a.historyRecoveries, a.record.RootRequestID, a.requestID, a.record.AccountID, openAIHistoryShape(a.historyRecoveryItems), rpc.Reason, cloudRejectedItemShape(a.historyRecoveryItems, rpc.Reason))
}
