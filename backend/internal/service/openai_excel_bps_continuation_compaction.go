package service

import (
	"encoding/json"

	"github.com/Wei-Shaw/sub2api/internal/service/basispoints"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// excelBPSCompactedContinuationSources replaces the old input with the actual
// compacted window and the original, unuploaded tail. WithCompactedWindow also
// prepends that same window to response.output; verify and remove precisely that
// prefix before prepareExcelBPSContinuation appends the generation output.
// Callers pass compactOutput and imagePolicy.history.Input[imagePolicy.split:],
// never the attachment-rewritten body. Both collections retain json.Number.
// Original request fields stay intact, including the opaque-context fields that
// the snapshot publisher rejects and tools that a later turn may inherit.
func excelBPSCompactedContinuationSources(originalBody, completedResponse []byte, window, originalTail []any) ([]byte, []byte, error) {
	if len(window) == 0 {
		return originalBody, completedResponse, nil
	}
	if len(originalTail) == 0 || !json.Valid(originalBody) || !json.Valid(completedResponse) {
		return nil, nil, errExcelBPSContinuationInvalid
	}
	if len(window) > excelBPSContinuationMaxItems || len(originalTail) > excelBPSContinuationMaxItems-len(window) {
		return nil, nil, basispoints.ErrStateCapacity
	}
	output := gjson.GetBytes(completedResponse, "output")
	if !output.IsArray() {
		return nil, nil, errExcelBPSContinuationInvalid
	}
	if !excelBPSContinuationArrayLimit(output, excelBPSContinuationMaxItems+excelBPSContinuationMaxOutputItems) {
		return nil, nil, basispoints.ErrStateCapacity
	}
	windowJSON, err := json.Marshal(window)
	if err != nil {
		return nil, nil, errExcelBPSContinuationInvalid
	}
	var windowItems, outputItems []json.RawMessage
	if json.Unmarshal(windowJSON, &windowItems) != nil || json.Unmarshal([]byte(output.Raw), &outputItems) != nil || !openAIWSRawItemsHasPrefix(outputItems, windowItems) {
		return nil, nil, errExcelBPSContinuationInvalid
	}
	tailJSON, err := json.Marshal(originalTail)
	if err != nil {
		return nil, nil, errExcelBPSContinuationInvalid
	}
	var tailItems []json.RawMessage
	if json.Unmarshal(tailJSON, &tailItems) != nil {
		return nil, nil, errExcelBPSContinuationInvalid
	}
	// Use the exact emitted window, rather than reserializing its payload.
	input := combineOpenAIWSReplayItems(outputItems[:len(windowItems)], tailItems)
	snapshotBody, err := setOpenAIWSPayloadInputSequence(originalBody, input, true)
	if err != nil {
		return nil, nil, errExcelBPSContinuationInvalid
	}
	generation, err := json.Marshal(outputItems[len(windowItems):])
	if err != nil {
		return nil, nil, errExcelBPSContinuationInvalid
	}
	snapshotResponse, err := sjson.SetRawBytes(completedResponse, "output", generation)
	if err != nil {
		return nil, nil, errExcelBPSContinuationInvalid
	}
	return snapshotBody, snapshotResponse, nil
}
