package basispoints

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"io"
	"mime/multipart"
	"net/textproto"
	"strings"
	"sync"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/tidwall/gjson"
)

const AttachmentsURL = "https://bps.openai.com/basispoints/api/attachments"

// Native limits are shared by preflight and automatic history-compaction policy.
const (
	NativeImageMaxMiB       = imageRelayMaxImageBytes >> 20
	NativeImageRequestBytes = imageRelayMaxRequestBytes
)

var ErrAttachmentBusy = errors.New("basispoints attachment upload capacity exhausted")

// InlineAttachment retains only the existing encoded input, never a second
// decoded image buffer. Both validation and multipart upload stream its bytes.
type InlineAttachment struct {
	MIME    string
	payload string
	Size    int64
	digest  [32]byte
}

func (a InlineAttachment) Multipart() (io.Reader, string, int64, error) {
	var framing bytes.Buffer
	writer := multipart.NewWriter(&framing)
	extension := map[string]string{"image/png": "png", "image/jpeg": "jpg", "image/gif": "gif", "image/webp": "webp"}[a.MIME]
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", "form-data; name=\"file\"; filename=\"image."+extension+"\"")
	header.Set("Content-Type", a.MIME)
	if _, err := writer.CreatePart(header); err != nil {
		return nil, "", 0, err
	}
	prefixLen := framing.Len()
	if err := writer.Close(); err != nil {
		return nil, "", 0, err
	}
	data := framing.Bytes()
	return io.MultiReader(bytes.NewReader(data[:prefixLen]), base64.NewDecoder(base64.StdEncoding, strings.NewReader(a.payload)), bytes.NewReader(data[prefixLen:])), writer.FormDataContentType(), int64(len(data)) + a.Size, nil
}

type nativeImagePart struct {
	part       object
	image      InlineAttachment
	inputIndex int
}
type NativeImages struct {
	source       object
	parts        []nativeImagePart
	raw          []byte
	toolImages   map[string]bool
	inlineCount  int
	compactSplit int
	uploaded     map[[32]byte]string
}

// PrepareNativeImages checks every inline image before any upload. Only typed
// message/tool-output content is traversed; arbitrary tool arguments are opaque.
func PrepareNativeImages(raw []byte) (*NativeImages, error) {
	return PrepareNativeImagesWithLimit(raw, imageRelayMaxRequestImages)
}

// PrepareNativeImagesWithLimit is PrepareNativeImages with an administrator-
// configurable per-request inline image limit.
func PrepareNativeImagesWithLimit(raw []byte, maxImages int) (*NativeImages, error) {
	return prepareNativeImages(raw, maxImages, 0)
}

// PrepareNativeImagesForCompaction validates both partitions before any upload.
// Upload sends only the history; Continue validates the actual compacted window
// and remaining input before uploading the continuation in the same request.
func PrepareNativeImagesForCompaction(raw []byte, maxImages, split int) (*NativeImages, error) {
	if split <= 0 {
		return nil, fmt.Errorf("invalid native image compaction boundary")
	}
	return prepareNativeImages(raw, maxImages, split)
}

func prepareNativeImages(raw []byte, maxImages, split int) (*NativeImages, error) {
	if maxImages < 1 {
		maxImages = imageRelayMaxRequestImages
	}
	var source object
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&source); err != nil {
		return nil, fmt.Errorf("invalid basispoints image request")
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, fmt.Errorf("invalid basispoints image request")
	}
	plan := &NativeImages{source: source, raw: raw, compactSplit: split}
	var total int64
	var count int
	input, _ := source["input"].([]any)
	if split > 0 && split >= len(input) {
		return nil, fmt.Errorf("invalid native image compaction boundary")
	}
	for inputIndex, entry := range input {
		if inputIndex == split && split > 0 {
			count, total = 0, 0
		}
		item, _ := entry.(map[string]any)
		field := ""
		switch text(item["type"]) {
		case "", "message", "agent_message":
			field = "content"
		case "function_call_output", "custom_tool_call_output":
			field = "output"
		default:
			continue
		}
		parts, _ := item[field].([]any)
		for _, value := range parts {
			part, _ := value.(map[string]any)
			if text(part["type"]) != "input_image" {
				continue
			}
			rawURL, _ := part["image_url"].(string)
			if !strings.HasPrefix(strings.ToLower(rawURL), "data:") {
				continue
			}
			if _, exists := part["file_id"]; exists {
				return nil, fmt.Errorf("basispoints input_image requires exactly one image reference")
			}
			if count >= maxImages {
				return nil, fmt.Errorf("local gateway is configured for at most %d inline images per request", maxImages)
			}
			plan.inlineCount++
			count++
			// Apply the same administrator-configured image size and count
			// safeguards as the temporary HTTPS relay before uploading natively.
			mimeType, payload, err := relayImagePayload(rawURL, imageRelayMaxImageBytes>>20)
			if err != nil {
				return nil, err
			}
			hash := sha256.New()
			reader := base64.NewDecoder(base64.StdEncoding, strings.NewReader(payload))
			size, err := io.Copy(hash, io.LimitReader(reader, imageRelayMaxImageBytes+1))
			if err != nil || size == 0 {
				return nil, fmt.Errorf("basispoints inline image contains invalid base64 data")
			}
			if size > imageRelayMaxImageBytes {
				return nil, fmt.Errorf("basispoints inline image exceeds the 20 MiB limit")
			}
			config, format, err := image.DecodeConfig(base64.NewDecoder(base64.StdEncoding, strings.NewReader(payload)))
			if err != nil || "image/"+format != mimeType {
				return nil, fmt.Errorf("basispoints inline image content does not match its media type")
			}
			if config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > imageRelayMaxPixels {
				return nil, fmt.Errorf("basispoints inline image dimensions exceed the 64 megapixel limit")
			}
			total += size
			if total > imageRelayMaxRequestBytes {
				return nil, fmt.Errorf("basispoints inline images exceed the 32 MiB request limit")
			}
			attachment := InlineAttachment{MIME: mimeType, payload: payload, Size: size}
			copy(attachment.digest[:], hash.Sum(nil))
			if part["detail"] == nil {
				part["detail"] = "auto"
			}
			delete(part, "image_url")
			part["file_id"] = "file-preflight"
			if err := validateImage(part); err != nil {
				return nil, err
			}
			if field == "output" && !openai.IsNamedStandaloneToolOutput(item) {
				// BPS tool screenshots use inline image_url, not attachment IDs.
				delete(part, "file_id")
				part["image_url"] = rawURL
				if plan.toolImages == nil {
					plan.toolImages = make(map[string]bool)
				}
				plan.toolImages[rawURL] = true
			} else {
				plan.parts = append(plan.parts, nativeImagePart{part: part, image: attachment, inputIndex: inputIndex})
			}
		}
	}
	return plan, nil
}

// HasImages reports whether any message images require attachment upload.
// Validated inline tool screenshots do not require an upload or attachment lease.
func (p *NativeImages) HasImages() bool { return len(p.parts) != 0 }

func (p *NativeImages) Body() ([]byte, error) {
	if p.inlineCount == 0 {
		return p.raw, nil
	}
	return json.Marshal(p.source)
}

func ValidAttachmentID(id string) bool {
	if !strings.HasPrefix(id, "file-") || len(id) < 6 || len(id) > 256 {
		return false
	}
	for _, ch := range id[5:] {
		switch {
		case ch >= 'a' && ch <= 'z', ch >= 'A' && ch <= 'Z', ch >= '0' && ch <= '9', ch == '-', ch == '_':
		default:
			return false
		}
	}
	return true
}

type AttachmentUpload func(context.Context, InlineAttachment) (string, error)

// AttachmentUploader bounds uploads across requests without retaining file IDs.
// The upload response carries no ownership-expiry proof for cross-request reuse.
// Each request uploads its own originals; only duplicates in that request share IDs.
type AttachmentUploader struct {
	mu     sync.Mutex
	active int
}

func (p *NativeImages) Upload(ctx context.Context, uploads *AttachmentUploader, upload AttachmentUpload) ([]byte, error) {
	if p.uploaded == nil {
		p.uploaded = make(map[[32]byte]string)
	}
	for i := range p.parts {
		part := &p.parts[i]
		if p.compactSplit > 0 && part.inputIndex >= p.compactSplit {
			continue
		}
		id := p.uploaded[part.image.digest]
		if id == "" {
			var err error
			id, err = uploads.upload(ctx, part.image, upload)
			if err != nil {
				return nil, err
			}
			p.uploaded[part.image.digest] = id
		}
		part.part["file_id"] = id
	}
	return p.Body()
}

// Continue retains upload deduplication only within this client request.
// Revalidate all images, including any retained by the upstream compact window.
func (p *NativeImages) Continue(raw []byte, maxImages int) (*NativeImages, error) {
	next, err := PrepareNativeImagesWithLimit(raw, maxImages)
	if err == nil {
		next.uploaded = p.uploaded
	}
	return next, err
}

// FileReferenceCounts describes typed image references and opaque input states
// in the failed wire request. Reference counts are occurrences; FreshUploadFiles
// counts distinct freshly uploaded IDs actually referenced. Opaque state counts
// indicate uninspectable context, not proof of file dependencies or ownership.
// ReusedUpload remains zero
// for diagnostic compatibility; unmanaged references may come from the client or compaction.
// No identifiers, URLs, credentials or image contents leave the request plan.
type FileReferenceCounts struct {
	FreshUploadFiles        int `json:"fresh_upload_files"`
	OpaqueCompactionItems   int `json:"opaque_compaction_items"`
	EncryptedReasoningItems int `json:"encrypted_reasoning_items"`
	FreshUpload             int `json:"fresh_upload_references"`
	ReusedUpload            int `json:"reused_upload_references"`
	Unmanaged               int `json:"unmanaged_file_references"`
	Inline                  int `json:"inline_image_references"`
	Remote                  int `json:"remote_image_references"`
}

// FileReferences is nil-safe for relay/disabled modes. Inspect only on failure,
// using the actual failed request rather than the pre-compaction image plan.
func (p *NativeImages) FileReferences(raw []byte) (counts FileReferenceCounts) {
	freshFiles := make(map[string]bool)
	origins := make(map[string]bool)
	if p != nil {
		for _, id := range p.uploaded {
			origins[id] = true
		}
	}
	gjson.GetBytes(raw, "input").ForEach(func(_, item gjson.Result) bool {
		field := ""
		switch item.Get("type").String() {
		case "compaction":
			if item.Get("encrypted_content").String() != "" {
				counts.OpaqueCompactionItems++
			}
			return true
		case "reasoning":
			if item.Get("encrypted_content").String() != "" {
				counts.EncryptedReasoningItems++
			}
			return true
		case "", "message", "agent_message":
			field = "content"
		case "function_call_output", "custom_tool_call_output":
			field = "output"
		default:
			return true
		}
		item.Get(field).ForEach(func(_, part gjson.Result) bool {
			if part.Get("type").String() != "input_image" {
				return true
			}
			if id := part.Get("file_id").String(); id != "" {
				if origins[id] {
					counts.FreshUpload++
					freshFiles[id] = true
				} else {
					counts.Unmanaged++
				}
			} else if url := part.Get("image_url").String(); url != "" {
				if strings.HasPrefix(strings.ToLower(url), "data:") {
					counts.Inline++
				} else {
					counts.Remote++
				}
			}
			return true
		})
		return true
	})
	counts.FreshUploadFiles = len(freshFiles)
	return counts
}

func (u *AttachmentUploader) upload(ctx context.Context, img InlineAttachment, send AttachmentUpload) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	u.mu.Lock()
	if u.active >= 32 {
		u.mu.Unlock()
		return "", ErrAttachmentBusy
	}
	u.active++
	u.mu.Unlock()
	defer func() {
		u.mu.Lock()
		u.active--
		u.mu.Unlock()
	}()
	id, err := send(ctx, img)
	if err == nil && !ValidAttachmentID(id) {
		err = fmt.Errorf("basispoints returned an invalid attachment ID")
	}
	return id, err
}

// PrepareWithCatalog permits only the tool screenshots validated by this native
// request plan. Ordinary Prepare calls keep rejecting inline images.
func (p *NativeImages) PrepareWithCatalog(scope string, replay *ReplayCache, cache *CatalogCache) ([]byte, *Bridge, error) {
	raw, err := p.Body()
	if err != nil {
		return nil, nil, err
	}
	return prepareWithCatalog(raw, scope, replay, cache, p.toolImages)
}
