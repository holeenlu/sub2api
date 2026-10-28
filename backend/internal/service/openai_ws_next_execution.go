package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/gateway/session"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// OpenAIWSNextExecution transfers an already-read independent create to the
// stream handler before operation admission. It is neither a failure nor a
// retry. The immutable payload retains the existing bounded reader allocation;
// no second frame, host admission, reader or writer is created for the handoff.
type OpenAIWSNextExecution struct {
	downstream    *OpenAIWSDownstream
	turn          uint64
	payload       []byte
	originalModel string
	accepted      bool
}

func (*OpenAIWSNextExecution) Error() string { return "websocket next independent execution" }

// Accept is called once, after the previous coordinator has joined and saved
// its history. It prepares only the new turn's budget and envelope; the handler
// remains the sole owner of authorization, pricing, capacity and mode selection.
// The returned body is immutable and remains charged to the existing lane.
func (e *OpenAIWSNextExecution) Accept(ctx context.Context, cfg *config.Config) (context.Context, []byte, string, error) {
	if e == nil || e.accepted || e.downstream == nil || e.turn != e.downstream.turn.Load() {
		return ctx, nil, "", errors.New("websocket next execution handoff is no longer current")
	}
	s := e.downstream
	if err := ctx.Err(); err != nil {
		return ctx, nil, "", err
	}
	if s.ClientClosed() {
		return ctx, nil, "", context.Canceled
	}
	if err := s.requireTurnAdmission(); err != nil {
		return ctx, nil, "", err
	}
	if err := s.bindRequestEnvelope(e.payload); err != nil {
		return ctx, nil, "", err
	}
	e.accepted = true
	s.budget.Store(NewOpenAITurnBudget(cfg))
	s.stagingDeadline, s.responseIDs = time.Time{}, nil
	s.publishedResponseID, s.publishedTurnEvents = "", nil
	// Keep controls queued until fresh selection chooses their consumer. Neither
	// response aliases nor committed predecessor history belongs to this reset.
	s.controlsEnabled.Store(true)
	s.modeSelected.Store(false)
	return s.TurnBudgetContext(ctx, cfg, e.payload), e.payload, e.originalModel, nil
}

// nextOpenAIWSExecution must see original wire bytes, before multiplex replay,
// implicit tool binding, account mapping or mode-specific normalization. A
// missing predecessor after any such transformation is not independence proof.
func (s *OpenAIGatewayService) nextOpenAIWSExecution(ctx context.Context, c *gin.Context, downstream *OpenAIWSDownstream, account *Account, payload []byte, fallbackModel string) error {
	if downstream == nil || account == nil || !account.IsOpenAI() || !openAIWSIndependentInput(payload) {
		return nil
	}
	if cloudHasRawMarker(payload) {
		protocol, err := parseCloudProtocolRequest(payload)
		if err != nil {
			return nil
		}
		hasCloudCall := false
		for _, id := range protocol.continuation.CallIDs {
			hasCloudCall = hasCloudCall || strings.HasPrefix(id, "call_cloud_")
		}
		if hasCloudCall {
			key := getAPIKeyFromContext(c)
			if s.cloudSessions == nil || key == nil {
				return nil // No authority to reclassify possible live Cloud tool replies.
			}
			binding, err := s.cloudSessions.resolveProtocol(ctx, key.UserID, key.ID, getOpenAIGroupIDFromContext(c), protocol)
			if err != nil || binding != nil {
				return nil // Preserve the original continuation/error handling and owner.
			}
		}
	}
	if downstream.openAIWSOwnsToolReply(ctx, c, account, payload) {
		return nil
	}
	model := strings.TrimSpace(gjson.GetBytes(payload, "model").String())
	if model == "" {
		model = strings.TrimSpace(fallbackModel)
	}
	return &OpenAIWSNextExecution{downstream: downstream, turn: downstream.turn.Load(), payload: payload, originalModel: model}
}

// A paired call/output can still answer this lane's outstanding call. Consult
// the existing committed owner history, without stripping IDs or inventing a
// call registry. Unknown/incomplete owner history cannot prove independence.
func (s *OpenAIWSDownstream) openAIWSOwnsToolReply(ctx context.Context, c *gin.Context, account *Account, payload []byte) bool {
	hasLocalOutput := false
	gjson.GetBytes(payload, "input").ForEach(func(_, item gjson.Result) bool {
		hasLocalOutput = isCodexToolCallOutputItemType(item.Get("type").String()) && !strings.HasPrefix(item.Get("call_id").String(), "call_cloud_")
		return !hasLocalOutput
	})
	if !hasLocalOutput {
		return false
	}
	lane, ok := s.conn.(*openAIWSStreamConn)
	id := s.lastTerminalID.Load()
	if !ok || id == nil || *id == "" {
		return true
	}
	snapshot, err := lane.mux.history.Resolve(ctx, lane.done, *id, lane.id, session.ContinuationIdentity{
		AccountID: account.ID, APIKeyID: getAPIKeyIDFromContext(c), GroupID: getOpenAIGroupIDFromContext(c), Credentials: openAIWSAccountGeneration(account),
	})
	if err != nil || !snapshot.Known {
		return true
	}
	pending := make(map[string]bool)
	for _, raw := range snapshot.Input {
		item := gjson.ParseBytes(raw)
		kind, callID := item.Get("type").String(), item.Get("call_id").String()
		if callID == "" {
			continue
		}
		if isCodexToolCallContextItemType(kind) {
			pending[callID] = true
		} else if isCodexToolCallOutputItemType(kind) {
			delete(pending, callID)
		}
	}
	owned := false
	gjson.GetBytes(payload, "input").ForEach(func(_, item gjson.Result) bool {
		owned = isCodexToolCallOutputItemType(item.Get("type").String()) && pending[item.Get("call_id").String()]
		return !owned
	})
	return owned
}

func openAIWSIndependentInput(payload []byte) bool {
	if !gjson.ValidBytes(payload) || !gjson.ParseBytes(payload).IsObject() {
		return false
	}
	kind := gjson.GetBytes(payload, "type").String()
	if kind != "" && kind != "response.create" {
		return false
	}
	// This is a new submission, not permission to replay an uncertain send.
	// Reuse the structural resource checks with the existing direct-input cipher
	// exception, then require each item to prove it is self-contained. Native,
	// BPS and Cloud retain their own validation of supplied reasoning/compaction;
	// this does not decrypt, strip, or make encrypted tasks/file references portable.
	if openAIResponsesReplayBlocker(payload, true) != "" {
		return false
	}
	input := gjson.GetBytes(payload, "input")
	if input.Type == gjson.String {
		return strings.TrimSpace(input.String()) != ""
	}
	if !input.IsArray() || len(input.Array()) == 0 {
		return false
	}
	complete := true
	input.ForEach(func(_, item gjson.Result) bool {
		known, selfContained := openAIResponsesSelfContainedInputItem(item)
		complete = known && selfContained && item.Get("role").String() != "tool"
		return complete
	})
	if !complete {
		return false
	}
	coverage := AnalyzeToolCallOutputContextCoverageBytes(payload)
	return !coverage.HasFunctionCallOutput || coverage.ContextCoversAllCallIDs
}
