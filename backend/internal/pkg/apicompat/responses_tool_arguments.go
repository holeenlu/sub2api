package apicompat

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ValidateResponsesToolArguments checks function calls before a buffered
// response is delivered. Empty arguments denote a no-argument call; custom
// tools carry free text and are deliberately not subjected to JSON validation.
func ValidateResponsesToolArguments(resp *ResponsesResponse) error {
	if resp == nil {
		return nil
	}
	for _, item := range resp.Output {
		if item.Type == "function_call" {
			if err := validateFunctionArguments(item.Arguments); err != nil {
				return fmt.Errorf("tool call %q (%s): %w", item.CallID, item.Name, err)
			}
		}
	}
	return nil
}

func validateFunctionArguments(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	if raw[0] != '{' || !json.Valid([]byte(raw)) {
		return fmt.Errorf("arguments must be a complete JSON object")
	}
	return nil
}

// Each tool owns its arguments and downstream index. The two Responses
// converters and the buffered collector share this bookkeeping so an
// interleaved call cannot close or overwrite another call's arguments.
type responsesToolCall struct {
	item        ResponsesOutput
	outputIndex int
	index       int
	arguments   string
	emitted     string
	started     bool
	done        bool
	closed      bool
}

type responsesToolCalls struct {
	byOutput map[int]*responsesToolCall
	ordered  []*responsesToolCall
}

func (s *responsesToolCalls) ingest(evt *ResponsesStreamEvent) (*responsesToolCall, error) {
	item := ResponsesOutput{Type: "function_call", ID: evt.ItemID, CallID: evt.CallID, Name: evt.Name}
	if strings.Contains(evt.Type, "custom_tool_call") {
		item.Type = "custom_tool_call"
	}
	if evt.Item != nil {
		item = *evt.Item
	}
	if item.Type != "function_call" && item.Type != "custom_tool_call" {
		return nil, nil
	}
	if s.byOutput == nil {
		s.byOutput = make(map[int]*responsesToolCall)
	}
	call := s.byOutput[evt.OutputIndex]
	// A terminal output array may omit other items, changing array positions.
	// Resolve stable identity first; never attach its parameters to a neighbour.
	for _, known := range s.ordered {
		if (item.ID != "" && known.item.ID == item.ID) || (item.CallID != "" && known.item.CallID == item.CallID) {
			call = known
			break
		}
	}
	if call == nil {
		call = &responsesToolCall{item: item, outputIndex: evt.OutputIndex, index: len(s.ordered)}
		s.ordered = append(s.ordered, call)
		s.byOutput[evt.OutputIndex] = call
	} else {
		for _, pair := range [][2]string{{call.item.ID, item.ID}, {call.item.CallID, item.CallID}, {call.item.Name, item.Name}, {call.item.Type, item.Type}} {
			if pair[0] != "" && pair[1] != "" && pair[0] != pair[1] {
				return nil, fmt.Errorf("tool identity changed at output index %d", evt.OutputIndex)
			}
		}
		if item.ID != "" {
			call.item.ID = item.ID
		}
		if item.CallID != "" {
			call.item.CallID = item.CallID
		}
		if item.Name != "" {
			call.item.Name = item.Name
		}
		if item.Namespace != "" {
			call.item.Namespace = item.Namespace
		}
		if item.Status != "" {
			call.item.Status = item.Status
		}
	}
	if strings.HasSuffix(evt.Type, ".delta") {
		if !call.done {
			call.arguments += evt.Delta
		}
		return call, nil
	}
	raw := evt.Arguments
	if item.Type == "custom_tool_call" {
		raw = evt.Input
	}
	if evt.Item != nil {
		raw = item.Arguments
		if item.Type == "custom_tool_call" {
			raw = item.Input
		}
	}
	if raw != "" {
		if !strings.HasPrefix(raw, call.arguments) {
			return nil, fmt.Errorf("tool call %q arguments disagree with the streamed prefix", call.item.CallID)
		}
		call.arguments = raw
	}
	if strings.HasSuffix(evt.Type, ".done") {
		if err := call.finish(); err != nil {
			return nil, err
		}
	}
	return call, nil
}

func (c *responsesToolCall) finish() error {
	if c.item.Type == "function_call" {
		if err := validateFunctionArguments(c.arguments); err != nil {
			return fmt.Errorf("tool call %q (%s): %w", c.item.CallID, c.item.Name, err)
		}
		if strings.TrimSpace(c.arguments) == "" {
			c.arguments = "{}"
		}
	}
	c.done = true
	return nil
}

func (c *responsesToolCall) output() ResponsesOutput {
	item := c.item
	if item.Type == "custom_tool_call" {
		item.Input = c.arguments
	} else {
		item.Arguments = c.arguments
	}
	return item
}
