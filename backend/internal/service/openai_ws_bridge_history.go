package service

import (
	"encoding/json"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// An authenticated bridge may own complete history that the current incremental
// frame omits. Bind that verified predecessor before generic orphan validation;
// the ordinary continuation path still validates mode, authority and replay.
// Unknown call IDs stay unchanged and are rejected by the existing validator.
func bindOpenAIWSImplicitToolContinuation(body []byte, history []json.RawMessage, responseID string) ([]byte, error) {
	if responseID == "" || len(history) == 0 || gjson.GetBytes(body, "previous_response_id").String() != "" {
		return body, nil
	}
	coverage := AnalyzeToolCallOutputContextCoverageBytes(body)
	if !coverage.HasFunctionCallOutput || coverage.ContextCoversAllCallIDs {
		return body, nil
	}
	combined, exists, err := buildOpenAIWSReplayInputSequence(history, true, body, true)
	if err != nil || !exists {
		return body, err
	}
	proof, err := setOpenAIWSPayloadInputSequence(body, combined, true)
	if err != nil {
		return body, err
	}
	if !AnalyzeToolCallOutputContextCoverageBytes(proof).ContextCoversAllCallIDs {
		return body, nil
	}
	return sjson.SetBytes(body, "previous_response_id", responseID)
}
