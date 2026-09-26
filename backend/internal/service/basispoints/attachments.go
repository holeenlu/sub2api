package basispoints

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"
)

const AttachmentsURL = "https://bps.openai.com/basispoints/api/attachments"

var attachmentSlots = make(chan struct{}, 16)

// UploadAttachment runs on the gateway's already selected account and proxy.
// payload is validated base64; callers must not log it or provider response bodies.
type UploadAttachment func(context.Context, string, string, int64) (string, error)
type inlineAttachment struct {
	part           object
	media, payload string
	size           int64
	digest         [32]byte
}

// RewriteAttachments validates the whole image batch before any upload. Images
// stream from base64 into multipart; there are no image-sized buffers or files.
// Repeated images share an upload within this request only. Provider file lifetime
// is unknown, so no cross-request file-ID cache is assumed.
func RewriteAttachments(ctx context.Context, raw []byte, upload UploadAttachment) ([]byte, error) {
	reserved := false
	defer func() {
		if reserved {
			<-attachmentSlots
		}
	}()
	var source object
	if decode(raw, &source) != nil || source == nil {
		return nil, fmt.Errorf("invalid Basispoints request JSON")
	}
	var staged []inlineAttachment
	input, _ := source["input"].([]any)
	var total int64
	for _, rawItem := range input {
		item, _ := rawItem.(object)
		for _, field := range []string{"content", "output"} {
			if field == "output" && text(item["type"]) != "function_call_output" && text(item["type"]) != "custom_tool_call_output" {
				continue
			}
			parts, _ := item[field].([]any)
			for _, rawPart := range parts {
				part, _ := rawPart.(object)
				if text(part["type"]) != "input_image" {
					continue
				}
				url := text(part["image_url"])
				if !strings.HasPrefix(strings.ToLower(url), "data:") {
					if err := validateImage(part); err != nil {
						return nil, err
					}
					continue
				}
				if !reserved {
					select {
					case attachmentSlots <- struct{}{}:
						reserved = true
					case <-ctx.Done():
						return nil, ctx.Err()
					}
				}
				if _, exists := part["file_id"]; exists {
					return nil, fmt.Errorf("basispoints image must have exactly one source")
				}
				if err := validateImageDetail(part); err != nil {
					return nil, err
				}
				if len(staged) >= imageRelayMaxRequestImages {
					return nil, fmt.Errorf("basispoints accepts at most 20 inline images per request")
				}
				media, payload, err := relayImagePayload(url)
				if err != nil {
					return nil, err
				}
				if err := ctx.Err(); err != nil {
					return nil, err
				}
				size, err := io.Copy(io.Discard, io.LimitReader(base64.NewDecoder(base64.StdEncoding, strings.NewReader(payload)), imageRelayMaxImageBytes+1))
				if err != nil || size == 0 {
					return nil, fmt.Errorf("basispoints inline image contains invalid base64 data")
				}
				if size > imageRelayMaxImageBytes {
					return nil, fmt.Errorf("basispoints inline image exceeds the 20 MiB limit")
				}
				total += size
				if total > imageRelayMaxRequestBytes {
					return nil, fmt.Errorf("basispoints inline images exceed the 32 MiB request limit")
				}
				dimensions, format, err := image.DecodeConfig(base64.NewDecoder(base64.StdEncoding, strings.NewReader(payload)))
				if err != nil || dimensions.Width <= 0 || dimensions.Height <= 0 || int64(dimensions.Width)*int64(dimensions.Height) > imageRelayMaxPixels {
					return nil, fmt.Errorf("basispoints inline image is invalid or exceeds 64 megapixels")
				}
				if "image/"+format != media {
					return nil, fmt.Errorf("basispoints inline image media type does not match its contents")
				}
				staged = append(staged, inlineAttachment{part: part, media: media, payload: payload, size: size, digest: sha256.Sum256([]byte(url))})
			}
		}
	}
	if len(staged) == 0 {
		return raw, nil
	}
	if upload == nil {
		return nil, fmt.Errorf("basispoints attachment transport is unavailable")
	}
	ids := make(map[[32]byte]string)
	for _, img := range staged {
		id := ids[img.digest]
		if id == "" {
			var err error
			id, err = upload(ctx, img.media, img.payload, img.size)
			if err != nil {
				return nil, err
			}
			if !validFileID(id) {
				return nil, fmt.Errorf("basispoints attachment response has no valid file ID")
			}
			ids[img.digest] = id
		}
	}
	for _, img := range staged {
		delete(img.part, "image_url")
		img.part["file_id"] = ids[img.digest]
	}
	return json.Marshal(source)
}

func NewAttachmentRequest(ctx context.Context, headers http.Header, media, payload string, size int64) (*http.Request, error) {
	var framing bytes.Buffer
	writer := multipart.NewWriter(&framing)
	partHeaders := make(textproto.MIMEHeader)
	partHeaders.Set("Content-Disposition", `form-data; name="file"; filename="image"`)
	partHeaders.Set("Content-Type", media)
	if _, err := writer.CreatePart(partHeaders); err != nil {
		return nil, err
	}
	split := framing.Len()
	if err := writer.Close(); err != nil {
		return nil, err
	}
	prefix, suffix := framing.Bytes()[:split], framing.Bytes()[split:]
	makeBody := func() io.ReadCloser {
		return io.NopCloser(io.MultiReader(bytes.NewReader(prefix), base64.NewDecoder(base64.StdEncoding, strings.NewReader(payload)), bytes.NewReader(suffix)))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, AttachmentsURL, makeBody())
	if err != nil {
		return nil, err
	}
	req.Header = headers.Clone()
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Accept", "application/json")
	req.Header.Del("Content-Length")
	req.Header.Del("Content-Encoding")
	req.ContentLength = int64(len(prefix)+len(suffix)) + size
	req.GetBody = func() (io.ReadCloser, error) { return makeBody(), nil }
	return req, nil
}

// ReadAttachmentID bounds diagnostics and never returns provider bodies or IDs
// through error strings. Error statuses are handled by the gateway before this.
func ReadAttachmentID(reader io.Reader) (string, error) {
	raw, err := io.ReadAll(io.LimitReader(reader, (64<<10)+1))
	if err != nil || len(raw) > 64<<10 {
		return "", fmt.Errorf("basispoints attachment response is unreadable or too large")
	}
	var result object
	if decode(raw, &result) != nil || !validFileID(text(result["openai_file_id"])) {
		return "", fmt.Errorf("basispoints attachment response has no valid file ID")
	}
	return text(result["openai_file_id"]), nil
}
