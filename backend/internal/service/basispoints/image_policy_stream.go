package basispoints

import (
	"encoding/json"
	"fmt"
	"io"
)

// SetCompactedWindow projects the real compacted history into this conversion's
// outgoing lifecycle. Call before Convert; window and usage are read-only. The
// projection never changes the converter's generation/repair usage facts.
func (c *StreamConversion) SetCompactedWindow(window []any, usage map[string]any) {
	if len(window) > 0 {
		c.window = &compactedStreamWindow{items: window, usage: usage}
	}
}

type compactedStreamWindow struct {
	items       []any
	usage       object
	inserted    bool
	progressive object
}

func (w *compactedStreamWindow) emit(kind string, payload object, emit func(string, object) error) error {
	if !w.inserted && kind != "response.created" && kind != "response.in_progress" {
		w.inserted = true
		for i, item := range w.items {
			if err := emit("response.output_item.added", object{"output_index": i, "item": item}); err != nil {
				return err
			}
			if err := emit("response.output_item.done", object{"output_index": i, "item": item}); err != nil {
				return err
			}
		}
	}
	// Own only the containers we change. Generation/repair snapshots and the
	// caller's compacted window remain immutable across projection and Close.
	projected := make(object, len(payload))
	for key, value := range payload {
		projected[key] = value
	}
	if index, ok := projected["output_index"]; ok {
		switch n := index.(type) {
		case json.Number:
			i, err := n.Int64()
			if err != nil {
				return err
			}
			projected["output_index"] = i + int64(len(w.items))
		case int:
			projected["output_index"] = n + len(w.items)
		case float64:
			projected["output_index"] = n + float64(len(w.items))
		}
	}
	response, _ := payload["response"].(object)
	observed, _ := response["usage"].(object)
	if observed == nil {
		observed, _ = payload["usage"].(object)
	}
	switch kind {
	case "response.completed", "response.failed", "response.cancelled", "response.incomplete":
		if response != nil {
			copy := make(object, len(response))
			for key, value := range response {
				copy[key] = value
			}
			output, _ := response["output"].([]any)
			copy["output"] = append(append([]any{}, w.items...), output...)
			current, _ := response["usage"].(object)
			current = copyStreamUsage(current)
			if !hasImageUsage(maxReportedUsage(nil, current)) {
				current = maxReportedUsage(current, w.progressive)
			}
			mergeImageUsage(current, w.usage)
			if len(current) > 0 {
				copy["usage"] = current
			}
			projected["response"] = copy
		}
	default:
		w.progressive = maxReportedUsage(w.progressive, observed)
	}
	return emit(kind, projected)
}

func copyStreamUsage(usage object) object {
	copy := make(object, len(usage))
	for key, value := range usage {
		if nested, ok := value.(object); ok {
			value = copyStreamUsage(nested)
		}
		copy[key] = value
	}
	return copy
}

func mergeImageUsage(dst, src object) {
	for k, v := range src {
		if child, ok := v.(object); ok {
			target, _ := dst[k].(object)
			if target == nil {
				target = object{}
				dst[k] = target
			}
			mergeImageUsage(target, child)
			continue
		}
		var a, b json.Number
		switch n := v.(type) {
		case json.Number:
			b = n
		case float64:
			b = json.Number(fmt.Sprint(n))
		case int:
			b = json.Number(fmt.Sprint(n))
		default:
			continue
		}
		switch n := dst[k].(type) {
		case json.Number:
			a = n
		case float64:
			a = json.Number(fmt.Sprint(n))
		case int:
			a = json.Number(fmt.Sprint(n))
		}
		x, _ := a.Int64()
		y, _ := b.Int64()
		dst[k] = x + y
	}
}

// ReadImageCompaction observes reported usage even when no successful terminal
// arrives. The caller never sees intermediate text or executable tool events.
func ReadImageCompaction(reader io.Reader, observe func([]byte)) (map[string]any, error) {
	var response object
	completed := false
	progressiveUsage := object{}
	err := readEvents(io.LimitReader(reader, 32<<20), func(event string, raw []byte) error {
		if string(raw) == "[DONE]" {
			return nil
		}
		var p object
		if e := decode(raw, &p); e != nil {
			return e
		}
		if observe != nil {
			observe(raw)
		}
		r, _ := p["response"].(object)
		u, _ := r["usage"].(object)
		if u == nil {
			u, _ = p["usage"].(object)
		}
		copyNonzeroImageUsage(progressiveUsage, u)
		kind := text(p["type"])
		if kind == "" {
			kind = event
		}
		item, _ := p["item"].(object)
		if isTool(item) || isToolEvent(kind) {
			return fmt.Errorf("image compaction returned an unexpected tool event")
		}
		switch kind {
		case "response.completed", "response.failed", "response.cancelled", "response.incomplete", "error":
			response, _ = p["response"].(object)
			completed = kind == "response.completed"
			if failure := classifyUpstreamFailure(kind, p); failure != nil {
				return failure
			}
			return io.EOF
		}
		return nil
	})
	if response != nil {
		u, _ := response["usage"].(object)
		if !hasImageUsage(u) {
			response["usage"] = progressiveUsage
		}
	}
	if err != nil && err != io.EOF {
		return response, err
	}
	if !completed || response == nil {
		return response, fmt.Errorf("image history compaction did not complete")
	}
	return response, nil
}

// Positive progressive fields are snapshots; a nonzero terminal is authoritative.
func hasImageUsage(usage object) bool {
	for _, v := range usage {
		if nested, ok := v.(object); ok {
			if hasImageUsage(nested) {
				return true
			}
			continue
		}
		if n, ok := v.(json.Number); ok {
			f, _ := n.Float64()
			if f > 0 {
				return true
			}
		}
	}
	return false
}
func copyNonzeroImageUsage(dst, src object) {
	for k, v := range src {
		if nested, ok := v.(object); ok {
			target, _ := dst[k].(object)
			if target == nil {
				target = object{}
				dst[k] = target
			}
			copyNonzeroImageUsage(target, nested)
		} else if n, ok := v.(json.Number); ok {
			f, _ := n.Float64()
			if f > 0 {
				dst[k] = v
			}
		}
	}
}
