package openai

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeStandaloneToolOutputContent(t *testing.T) {
	image := map[string]any{"type": "input_image", "image_url": "data:image/svg+xml;base64,PHN2Zy8+", "detail": "original"}
	unknown := map[string]any{"type": "input_file", "file_id": "private-file"}
	input := []any{
		map[string]any{"type": "text", "text": "plain", "arguments": map[string]any{"must_be_validated": true}},
		map[string]any{"type": "output_text", "text": "output", "annotations": []any{}},
		image,
		map[string]any{"type": "refusal", "refusal": "declined"},
		map[string]any{"type": "input_text", "text": "input"},
		unknown,
	}
	before, err := json.Marshal(input)
	require.NoError(t, err)
	got, err := NormalizeStandaloneToolOutputContent(input)
	require.NoError(t, err)
	require.Len(t, got, len(input))
	for index, text := range map[int]string{0: "plain", 1: "output", 3: "declined", 4: "input"} {
		part := got[index].(map[string]any)
		require.Equal(t, "input_text", part["type"])
		require.Equal(t, text, part["text"])
	}
	require.NotContains(t, got[3], "refusal")
	require.Contains(t, got[0], "arguments", "normalization must not bypass a target's executable-field validation")
	require.Equal(t, image, got[2], "media policy belongs to the protocol, not this text helper")
	require.Equal(t, unknown, got[5], "unknown content remains visible to protocol validation")
	got[0].(map[string]any)["text"] = "changed normalized copy"
	got[2].(map[string]any)["detail"] = "high"
	after, err := json.Marshal(input)
	require.NoError(t, err)
	require.Equal(t, before, after)
	plain, err := NormalizeStandaloneToolOutputContent("exact\ntext")
	require.NoError(t, err)
	require.Equal(t, []any{map[string]any{"type": "input_text", "text": "exact\ntext"}}, plain)
}

func TestNormalizeStandaloneToolOutputContentInvalidText(t *testing.T) {
	for _, kind := range []string{"input_text", "text", "output_text", "refusal"} {
		t.Run(kind, func(t *testing.T) {
			field := "text"
			if kind == "refusal" {
				field = "refusal"
			}
			_, err := NormalizeStandaloneToolOutputContent([]any{map[string]any{"type": kind, field: map[string]any{"private": "never log this"}}})
			var invalid *StandaloneToolTextError
			require.True(t, errors.As(err, &invalid))
			require.Zero(t, invalid.Index)
			require.Equal(t, field, invalid.Field)
			require.NotContains(t, err.Error(), "private")
		})
	}
	_, err := NormalizeStandaloneToolOutputContent(map[string]any{"private": "not a content array"})
	var invalid *StandaloneToolTextError
	require.True(t, errors.As(err, &invalid))
	require.Equal(t, -1, invalid.Index)
}
