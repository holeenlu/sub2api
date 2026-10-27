package service

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// These cases prove adapter passthrough only. SVG/AVIF/TIFF recognition and
// rendering remain the upstream's responsibility, just as on the native path.
func TestCloudImagePayloadsAreForwardedWithoutLocalCodecPolicy(t *testing.T) {
	for _, mime := range []string{"image/png", "image/jpeg", "image/webp", "image/gif", "image/svg+xml", "image/avif", "image/tiff", "image/x-client-format"} {
		t.Run(mime, func(t *testing.T) {
			// Deliberately opaque to local codecs, including possible trailing
			// bytes/multiple frames. The gateway must neither decode nor rewrite.
			data := []byte("<svg xmlns=\"http://www.w3.org/2000/svg\"></svg>\x00opaque-image-bytes")
			url := "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data)
			part := map[string]any{"type": "input_image", "image_url": url, "detail": "high"}
			for _, tool := range []bool{false, true} {
				item := map[string]any{"role": "user", "content": []any{part}}
				if tool {
					item = map[string]any{"type": "function_call_output", "call_id": "call-image", "output": []any{part}}
				}
				raw, err := json.Marshal(map[string]any{"model": "m", "input": []any{item}})
				require.NoError(t, err)
				parsed, err := parseCloudProtocolRequest(raw)
				require.NoError(t, err)
				if tool {
					require.Equal(t, []any{map[string]any{"type": "inputImage", "imageUrl": url}}, parsed.outputItems["call-image"])
					require.Equal(t, []any{part}, parsed.outputHistory["call-image"])
				} else {
					require.Equal(t, []any{map[string]any{"type": "image", "url": url, "detail": "high"}}, parsed.input)
				}
			}
		})
	}
}

func TestCloudImageEnvelopeKeepsProtocolChecksWithoutGatewaySizeOrCountCaps(t *testing.T) {
	request := func(url string, count int, tool bool) []byte {
		var parts []any
		for range count {
			parts = append(parts, map[string]any{"type": "input_image", "image_url": url})
		}
		item := map[string]any{"role": "user", "content": parts}
		if tool {
			item = map[string]any{"type": "function_call_output", "call_id": "call-images", "output": parts}
		}
		raw, err := json.Marshal(map[string]any{"model": "m", "input": []any{item}})
		require.NoError(t, err)
		return raw
	}
	url := "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString([]byte("<svg/>"))
	// Native /responses has no per-message image count cap; neither does Cloud.
	parsed, err := parseCloudProtocolRequest(request(url, 12, false))
	require.NoError(t, err)
	require.Len(t, parsed.input, 12)
	parsed, err = parseCloudProtocolRequest(request(url, 12, true))
	require.NoError(t, err)
	require.Len(t, parsed.outputItems["call-images"], 12)
	for _, invalid := range []string{
		"data:text/plain;base64,PHN2Zy8+",
		"data:image/png;base64,",
		"data:image/png;base64,!!==",
		"data:image/png;base64,PHN2Zy8+\n",
		"data:image/png;base64,PHN2Zy8+\r",
		"data:image/png;base64,PHN2Zy8",
		"data:image/png,PHN2Zy8+",
		"data:image;base64,PHN2Zy8+",
	} {
		_, err := parseCloudProtocolRequest(request(invalid, 1, false))
		require.Error(t, err, invalid)
	}
	// A 5 MiB decoded image (over the former 3 MiB cap) inside a request over
	// the former 4 MiB cap. A repeated pattern keeps the test cheap; the
	// gateway validates encoding only and never decodes the image itself.
	large := "data:image/png;base64," + base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0x89, 'P', 'N', 'G'}, (5<<20)/4))
	raw := request(large, 1, false)
	require.Greater(t, len(raw), 5<<20)
	parsed, err = parseCloudProtocolRequest(raw)
	require.NoError(t, err)
	require.Len(t, parsed.input, 1)
	image := parsed.input[0].(map[string]any)
	require.Equal(t, "image", image["type"])
	require.True(t, image["url"] == large, "the inline image is forwarded unchanged")
}
