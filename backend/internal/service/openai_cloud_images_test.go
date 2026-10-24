package service

import (
	"encoding/base64"
	"encoding/json"
	"strings"
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

func TestCloudImageEnvelopeBoundsRemainEnforced(t *testing.T) {
	request := func(url string, count int) []byte {
		var parts []any
		for range count {
			parts = append(parts, map[string]any{"type": "input_image", "image_url": url})
		}
		raw, err := json.Marshal(map[string]any{"model": "m", "input": []any{map[string]any{"role": "user", "content": parts}}})
		require.NoError(t, err)
		return raw
	}
	url := "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString([]byte("<svg/>"))
	_, err := parseCloudProtocolRequest(request(url, 8))
	require.NoError(t, err)
	_, err = parseCloudProtocolRequest(request(url, 9))
	require.Error(t, err)
	for _, invalid := range []string{
		"data:text/plain;base64,PHN2Zy8+",
		"data:image/png;base64,",
		"data:image/png;base64,!!==",
		"data:image/png;base64,PHN2Zy8+\n",
		"data:image/png,PHN2Zy8+",
		"data:image;base64,PHN2Zy8+",
	} {
		_, err := parseCloudProtocolRequest(request(invalid, 1))
		require.Error(t, err, invalid)
	}
	// Leave space for the JSON envelope below the independent 4 MiB request
	// limit; image bytes are bounded without decoding or allocating a bitmap.
	within := "data:image/tiff;base64," + base64.StdEncoding.EncodeToString([]byte(strings.Repeat("x", (3<<20)-256)))
	_, err = parseCloudProtocolRequest(request(within, 1))
	require.NoError(t, err)
	tooLarge := "data:image/tiff;base64," + base64.StdEncoding.EncodeToString([]byte(strings.Repeat("x", (3<<20)+1)))
	_, err = parseCloudProtocolRequest(request(tooLarge, 1))
	require.Error(t, err)
}
