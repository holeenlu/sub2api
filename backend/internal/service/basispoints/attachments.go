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
	"time"

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
	// Cross-request attachment ID cache (2026-10-24): optional, scoped by the
	// caller. cachedIDs marks IDs this request reused instead of uploading.
	cache      *AttachmentCache
	cacheScope string
	cachedIDs  map[string]bool
}

// Attachment ID cache bounds, matching the source implementation.
const (
	AttachmentCacheMaxEntries = 512
	AttachmentCacheTTL        = 30 * time.Minute
)

// AttachmentCache keeps uploaded attachment IDs (never image bytes) for
// AttachmentCacheTTL, at most AttachmentCacheMaxEntries entries, keyed by the
// caller's scope and the image digest. A cached ID is not proof that upstream
// still accepts it: callers must invalidate the IDs of a request that fails
// file ownership validation, and may then upload the originals again.
type AttachmentCache struct {
	mu      sync.Mutex
	entries map[[32]byte]attachmentCacheEntry
	now     func() time.Time
}

type attachmentCacheEntry struct {
	id, scope     string
	expires, used time.Time
}

func (c *AttachmentCache) clock() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
}

func attachmentCacheKey(scope string, img InlineAttachment) [32]byte {
	return sha256.Sum256([]byte(scope + "\x00" + img.MIME + "\x00" + string(img.digest[:])))
}

func (c *AttachmentCache) lookup(scope string, img InlineAttachment) (string, bool) {
	if c == nil || scope == "" {
		return "", false
	}
	key := attachmentCacheKey(scope, img)
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok {
		return "", false
	}
	now := c.clock()
	if !now.Before(entry.expires) {
		delete(c.entries, key)
		return "", false
	}
	entry.used = now
	c.entries[key] = entry
	return entry.id, true
}

func (c *AttachmentCache) store(scope string, img InlineAttachment, id string) {
	if c == nil || scope == "" || !ValidAttachmentID(id) {
		return
	}
	key := attachmentCacheKey(scope, img)
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.clock()
	if c.entries == nil {
		c.entries = make(map[[32]byte]attachmentCacheEntry)
	}
	if _, exists := c.entries[key]; !exists && len(c.entries) >= AttachmentCacheMaxEntries {
		for k, entry := range c.entries {
			if !now.Before(entry.expires) {
				delete(c.entries, k)
			}
		}
		if len(c.entries) >= AttachmentCacheMaxEntries {
			var oldest [32]byte
			var used time.Time
			for k, entry := range c.entries {
				if used.IsZero() || entry.used.Before(used) {
					oldest, used = k, entry.used
				}
			}
			delete(c.entries, oldest)
		}
	}
	c.entries[key] = attachmentCacheEntry{id: id, scope: scope, expires: now.Add(AttachmentCacheTTL), used: now}
}

// invalidate removes this scope's entries holding any of ids.
func (c *AttachmentCache) invalidate(scope string, ids map[string]bool) int {
	if c == nil || scope == "" || len(ids) == 0 {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	removed := 0
	for k, entry := range c.entries {
		if entry.scope == scope && ids[entry.id] {
			delete(c.entries, k)
			removed++
		}
	}
	return removed
}

// Len reports the number of entries, including expired ones not yet removed.
func (c *AttachmentCache) Len() int {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.entries)
}

// UseCache lets Upload reuse and record attachment IDs under scope. An empty
// scope (no trusted conversation identity) disables the cache.
func (p *NativeImages) UseCache(cache *AttachmentCache, scope string) {
	if p == nil {
		return
	}
	p.cache, p.cacheScope = cache, scope
}

// UsedCachedAttachments reports whether this request referenced a cached ID.
func (p *NativeImages) UsedCachedAttachments() bool { return p != nil && len(p.cachedIDs) > 0 }

// InvalidateAttachmentCache removes every cached entry for the IDs this request
// used (reused or freshly uploaded) and forgets them for this request, so a
// retry uploads the original images again. It returns the entries removed.
func (p *NativeImages) InvalidateAttachmentCache() int {
	if p == nil {
		return 0
	}
	ids := make(map[string]bool, len(p.uploaded))
	for _, id := range p.uploaded {
		ids[id] = true
	}
	removed := p.cache.invalidate(p.cacheScope, ids)
	p.uploaded = make(map[[32]byte]string)
	p.cachedIDs = nil
	return removed
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

// AttachmentUploader bounds concurrent uploads across requests. Reuse of IDs
// across requests is the separate, scoped AttachmentCache.
type AttachmentUploader struct {
	mu     sync.Mutex
	active int
}

// MaxParallelAttachmentUploads bounds concurrent uploads of one request plan.
// The process-wide limit in AttachmentUploader still applies across requests.
const MaxParallelAttachmentUploads = 4

// Upload sends each distinct image of this plan once, at most
// MaxParallelAttachmentUploads at a time. The first failure cancels uploads
// still in flight and is returned; IDs of images that did upload are kept for
// deduplication by Continue. upload may be called concurrently.
func (p *NativeImages) Upload(ctx context.Context, uploads *AttachmentUploader, upload AttachmentUpload) ([]byte, error) {
	if p.uploaded == nil {
		p.uploaded = make(map[[32]byte]string)
	}
	var pending []InlineAttachment
	queued := make(map[[32]byte]bool)
	for i := range p.parts {
		part := &p.parts[i]
		if p.compactSplit > 0 && part.inputIndex >= p.compactSplit {
			continue
		}
		digest := part.image.digest
		if p.uploaded[digest] != "" || queued[digest] {
			continue
		}
		if id, ok := p.cache.lookup(p.cacheScope, part.image); ok {
			p.uploaded[digest] = id
			if p.cachedIDs == nil {
				p.cachedIDs = make(map[string]bool)
			}
			p.cachedIDs[id] = true
			continue
		}
		queued[digest] = true
		pending = append(pending, part.image)
	}
	ids, err := uploadAttachmentsParallel(ctx, uploads, upload, pending)
	for i, id := range ids {
		if id != "" {
			p.uploaded[pending[i].digest] = id
			p.cache.store(p.cacheScope, pending[i], id)
		}
	}
	if err != nil {
		return nil, err
	}
	for i := range p.parts {
		part := &p.parts[i]
		if p.compactSplit > 0 && part.inputIndex >= p.compactSplit {
			continue
		}
		part.part["file_id"] = p.uploaded[part.image.digest]
	}
	return p.Body()
}

func uploadAttachmentsParallel(ctx context.Context, uploads *AttachmentUploader, send AttachmentUpload, images []InlineAttachment) ([]string, error) {
	ids := make([]string, len(images))
	if len(images) == 0 {
		return ids, nil
	}
	if len(images) == 1 {
		id, err := uploads.upload(ctx, images[0], send)
		if err == nil {
			ids[0] = id
		}
		return ids, err
	}
	groupCtx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	var (
		mu       sync.Mutex
		firstErr error
		wg       sync.WaitGroup
	)
	slots := make(chan struct{}, MaxParallelAttachmentUploads)
	for i := range images {
		select {
		case slots <- struct{}{}:
		case <-groupCtx.Done():
		}
		if groupCtx.Err() != nil {
			break
		}
		wg.Add(1)
		go func(i int) {
			defer func() { <-slots; wg.Done() }()
			id, err := uploads.upload(groupCtx, images[i], send)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				// Keep the failure that stopped the batch, not the cancellations
				// it caused in sibling uploads.
				if firstErr == nil {
					firstErr = err
					cancel(err)
				}
				return
			}
			ids[i] = id
		}(i)
	}
	wg.Wait()
	if firstErr == nil {
		// The caller's context ended before every image was started.
		firstErr = ctx.Err()
	}
	return ids, firstErr
}

// Continue keeps this request's uploaded IDs and its cache scope.
// Revalidate all images, including any retained by the upstream compact window.
func (p *NativeImages) Continue(raw []byte, maxImages int) (*NativeImages, error) {
	next, err := PrepareNativeImagesWithLimit(raw, maxImages)
	if err == nil {
		next.uploaded = p.uploaded
		next.cache, next.cacheScope, next.cachedIDs = p.cache, p.cacheScope, p.cachedIDs
	}
	return next, err
}

// FileReferenceCounts describes typed image references and opaque input states
// in the failed wire request. Reference counts are occurrences; FreshUploadFiles
// counts distinct freshly uploaded IDs actually referenced. Opaque state counts
// indicate uninspectable context, not proof of file dependencies or ownership.
// ReusedUpload counts references to IDs taken from the attachment cache;
// unmanaged references may come from the client or compaction.
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
				if p != nil && p.cachedIDs[id] {
					counts.ReusedUpload++
				} else if origins[id] {
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
