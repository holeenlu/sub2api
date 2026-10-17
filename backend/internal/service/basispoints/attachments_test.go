package basispoints

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"image"
	"image/png"
	"io"
	"mime"
	"mime/multipart"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func nativeTestURL(t *testing.T) (string, []byte) {
	t.Helper()
	var data bytes.Buffer
	require.NoError(t, png.Encode(&data, image.NewRGBA(image.Rect(0, 0, 2, 3))))
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(data.Bytes()), data.Bytes()
}
func nativeTestRequest(t *testing.T, urls ...string) []byte {
	t.Helper()
	var parts []any
	for _, url := range urls {
		parts = append(parts, object{"type": "input_image", "image_url": url, "detail": "original"})
	}
	raw, err := json.Marshal(object{"model": "gpt-6-astra", "input": []any{object{"role": "user", "content": parts}}, "metadata": object{"large": json.Number("9007199254740993")}})
	require.NoError(t, err)
	return raw
}

func TestNativeImagesMultipartAndRequestIsolation(t *testing.T) {
	url, data := nativeTestURL(t)
	raw := nativeTestRequest(t, url, url)
	cache := &AttachmentUploader{}
	calls := 0
	upload := func(_ context.Context, img InlineAttachment) (string, error) {
		calls++
		reader, contentType, size, err := img.Multipart()
		require.NoError(t, err)
		body, err := io.ReadAll(reader)
		require.NoError(t, err)
		require.Equal(t, int64(len(body)), size)
		_, params, err := mime.ParseMediaType(contentType)
		require.NoError(t, err)
		mr := multipart.NewReader(bytes.NewReader(body), params["boundary"])
		part, err := mr.NextPart()
		require.NoError(t, err)
		require.Equal(t, "file", part.FormName())
		require.Equal(t, "image.png", part.FileName())
		require.Equal(t, "image/png", part.Header.Get("Content-Type"))
		decoded, err := io.ReadAll(part)
		require.NoError(t, err)
		require.Equal(t, data, decoded)
		_, err = mr.NextPart()
		require.ErrorIs(t, err, io.EOF)
		return fmt.Sprintf("file-upload%d", calls), nil
	}
	for _, scope := range []string{"account1/key1/thread1/token1", "account1/key1/thread1/token1", "account2/key1/thread1/token1", "account1/key2/thread1/token1", "account1/key1/thread2/token1", "account1/key1/thread1/token2", "", ""} {
		plan, err := PrepareNativeImages(raw)
		require.NoError(t, err)
		preflight, err := plan.Body()
		require.NoError(t, err)
		_, _, err = Prepare(preflight, scope, nil)
		require.NoError(t, err)
		out, err := plan.Upload(context.Background(), cache, upload)
		require.NoError(t, err)
		require.NotContains(t, string(out), "data:image")
		require.Contains(t, string(out), "9007199254740993")
		require.Contains(t, string(out), "original")
		require.NotContains(t, string(out), "file-preflight")
		_, _, err = Prepare(out, scope, nil)
		require.NoError(t, err)
	}
	require.Equal(t, 8, calls, "duplicates upload once per request; separate requests never share uploaded IDs")
}

func TestNativeImagesValidateAllBeforeUpload(t *testing.T) {
	url, _ := nativeTestURL(t)
	for name, urls := range map[string][]string{
		"bad second image": {url, "data:image/png;base64,PRIVATE_INVALID"},
		"mime mismatch":    {strings.Replace(url, "image/png", "image/jpeg", 1)},
		"bad image bytes":  {"data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte("not an image"))},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := PrepareNativeImages(nativeTestRequest(t, urls...))
			require.Error(t, err)
			require.NotContains(t, err.Error(), "PRIVATE_INVALID")
		})
	}
	// 20 occurrences remain legal; invalid detail and mixed references fail locally.
	_, err := PrepareNativeImages(nativeTestRequest(t, strings.Fields(strings.Repeat(url+" ", 20))...))
	require.NoError(t, err)
	_, err = PrepareNativeImages(nativeTestRequest(t, strings.Fields(strings.Repeat(url+" ", 21))...))
	require.EqualError(t, err, "local gateway is configured for at most 20 inline images per request")
	_, err = PrepareNativeImagesWithLimit(nativeTestRequest(t, url, url), 1)
	require.EqualError(t, err, "local gateway is configured for at most 1 inline images per request")
	raw := nativeTestRequest(t, url)
	_, err = PrepareNativeImages(bytes.ReplaceAll(raw, []byte("original"), []byte("invalid")))
	require.Error(t, err)
	_, err = PrepareNativeImages(append(raw, []byte(" {}")...))
	require.Error(t, err)
}

func TestNativeImagesOnlyTraverseTypedContent(t *testing.T) {
	url, _ := nativeTestURL(t)
	source := object{"model": "gpt-6-astra", "input": []any{
		object{"type": "function_call", "arguments": object{"content": []any{object{"type": "input_image", "image_url": url}}}, "content": []any{object{"type": "input_image", "image_url": url}}},
		object{"type": "function_call_output", "output": []any{object{"type": "input_image", "image_url": url}}},
		object{"type": "custom_tool_call_output", "output": []any{object{"type": "input_image", "image_url": url}}},
	}}
	raw, err := json.Marshal(source)
	require.NoError(t, err)
	plan, err := PrepareNativeImages(raw)
	require.NoError(t, err)
	require.Empty(t, plan.parts)
	out, err := plan.Upload(context.Background(), &AttachmentUploader{}, func(context.Context, InlineAttachment) (string, error) {
		t.Fatal("tool screenshots must not upload")
		return "", nil
	})
	require.NoError(t, err)
	var decoded object
	require.NoError(t, json.Unmarshal(out, &decoded))
	items := mustTestValue[[]any](t, decoded["input"])
	require.Equal(t, mustTestValue[[]any](t, source["input"])[0], items[0])
}

func TestAttachmentUploaderFailureAndCancellationReleaseCapacity(t *testing.T) {
	uploads := new(AttachmentUploader)
	for _, failure := range []error{errors.New("failed"), context.Canceled, context.DeadlineExceeded} {
		_, err := uploads.upload(t.Context(), InlineAttachment{}, func(context.Context, InlineAttachment) (string, error) { return "", failure })
		require.ErrorIs(t, err, failure)
		require.Zero(t, uploads.active)
	}
	_, err := uploads.upload(t.Context(), InlineAttachment{}, func(context.Context, InlineAttachment) (string, error) { return "https://invalid", nil })
	require.Error(t, err)
	require.Zero(t, uploads.active)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = uploads.upload(ctx, InlineAttachment{}, func(context.Context, InlineAttachment) (string, error) {
		t.Fatal("canceled requests must not upload")
		return "", nil
	})
	require.ErrorIs(t, err, context.Canceled)
	require.Zero(t, uploads.active)
}

func TestNativeImagesByteAndPixelLimits(t *testing.T) {
	_, data := nativeTestURL(t)
	// A valid PNG header with excessive dimensions must fail without allocating a bitmap.
	large := append([]byte(nil), data...)
	binary.BigEndian.PutUint32(large[16:20], 1<<20)
	binary.BigEndian.PutUint32(large[20:24], 128)
	binary.BigEndian.PutUint32(large[29:33], crc32.ChecksumIEEE(large[12:29]))
	_, err := PrepareNativeImages(nativeTestRequest(t, "data:image/png;base64,"+base64.StdEncoding.EncodeToString(large)))
	require.ErrorContains(t, err, "64 megapixel")
	t.Run("single image", func(t *testing.T) {
		url := "data:image/png;base64," + strings.Repeat("A", base64.StdEncoding.EncodedLen(imageRelayMaxImageBytes)+4)
		_, err := PrepareNativeImages(nativeTestRequest(t, url))
		require.ErrorContains(t, err, "20 MiB")
	})
	t.Run("duplicate occurrences count toward request bytes", func(t *testing.T) {
		padded := make([]byte, 17<<20)
		copy(padded, data)
		url := "data:image/png;base64," + base64.StdEncoding.EncodeToString(padded)
		_, err := PrepareNativeImages(nativeTestRequest(t, url, url))
		require.ErrorContains(t, err, "32 MiB")
	})
}

func TestNativeImagesCompactionPartitions(t *testing.T) {
	_, data := nativeTestURL(t)
	padded := make([]byte, 17<<20)
	copy(padded, data)
	url := "data:image/png;base64," + base64.StdEncoding.EncodeToString(padded)
	item := object{"role": "user", "content": []any{object{"type": "input_image", "image_url": url}}}
	source := object{"model": "gpt-6-astra", "input": []any{item, message("assistant", "seen"), item}}
	raw, err := json.Marshal(source)
	require.NoError(t, err)
	_, err = PrepareNativeImagesWithLimit(raw, 20)
	require.ErrorContains(t, err, "32 MiB")
	plan, err := PrepareNativeImagesForCompaction(raw, 20, 2)
	require.NoError(t, err)
	uploads := 0
	upload := func(context.Context, InlineAttachment) (string, error) {
		uploads++
		return "file-current-request", nil
	}
	prefix, err := plan.Upload(t.Context(), new(AttachmentUploader), upload)
	require.NoError(t, err)
	require.Equal(t, 1, uploads)
	require.Contains(t, string(prefix), "file-preflight", "the new image has not been uploaded")
	_, err = plan.Continue(raw, 20)
	require.ErrorContains(t, err, "32 MiB", "an insufficient compact window still fails locally")
	source["input"] = []any{object{"type": "compaction", "encrypted_content": "opaque"}, item}
	raw, err = json.Marshal(source)
	require.NoError(t, err)
	next, err := plan.Continue(raw, 20)
	require.NoError(t, err)
	continued, err := next.Upload(t.Context(), new(AttachmentUploader), upload)
	require.NoError(t, err)
	require.Equal(t, 1, uploads, "same-request duplicates still share the first successful upload")
	require.NotContains(t, string(continued), "file-preflight")
	require.Equal(t, 1, next.FileReferences(continued).FreshUpload)
}

func TestAttachmentUploaderBoundsActualUploads(t *testing.T) {
	cache := &AttachmentUploader{}
	started := make(chan struct{}, 32)
	release := make(chan struct{})
	upload := func(context.Context, InlineAttachment) (string, error) {
		started <- struct{}{}
		<-release
		return "file-bounded", nil
	}
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := cache.upload(context.Background(), InlineAttachment{}, upload)
			require.NoError(t, err)
		}()
	}
	for i := 0; i < 32; i++ {
		<-started
	}
	_, err := cache.upload(context.Background(), InlineAttachment{}, upload)
	require.ErrorIs(t, err, ErrAttachmentBusy)
	close(release)
	wg.Wait()
	require.Zero(t, cache.active)
}

func TestNativeToolImageValidationAndScope(t *testing.T) {
	url, _ := nativeTestURL(t)
	source := object{"model": "gpt-6-astra", "input": []any{
		object{"type": "function_call", "name": "view_image", "call_id": "call_image", "arguments": "{}"},
		object{"type": "function_call_output", "call_id": "call_image", "output": []any{object{"type": "input_image", "image_url": url}}},
	}}
	raw, err := json.Marshal(source)
	require.NoError(t, err)
	plan, err := PrepareNativeImages(raw)
	require.NoError(t, err)
	require.False(t, plan.HasImages())
	wire, bridge, err := plan.PrepareWithCatalog("scope", nil, new(CatalogCache))
	require.NoError(t, err)
	require.Contains(t, string(wire), url)
	require.Contains(t, string(wire), `"detail":"auto"`)
	_, _, err = Prepare(raw, "scope", nil)
	require.ErrorContains(t, err, "image support is disabled")
	for _, change := range []func(object){
		func(p object) { p["image_url"] = "data:image/png;base64,PRIVATE_INVALID" },
		func(p object) { p["detail"] = "invalid" },
		func(p object) { p["file_id"] = "file-mixed" },
	} {
		var modified object
		require.NoError(t, json.Unmarshal(raw, &modified))
		items := mustTestValue[[]any](t, modified["input"])
		item := mustTestValue[object](t, items[1])
		parts := mustTestValue[[]any](t, item["output"])
		part := mustTestValue[object](t, parts[0])
		change(part)
		invalid, err := json.Marshal(modified)
		require.NoError(t, err)
		_, err = PrepareNativeImages(invalid)
		require.Error(t, err)
		require.NotContains(t, err.Error(), "PRIVATE_INVALID")
		_, _, err = bridge.Reprepare(invalid)
		require.Error(t, err, "reprepare must not admit new/unvalidated screenshots or invalid metadata")
	}
	// The native grant is restricted to tool output, even for an identical URL.
	messageRaw := nativeTestRequest(t, url)
	_, _, err = bridge.Reprepare(messageRaw)
	require.Error(t, err)
	source["input"] = append(mustTestValue[[]any](t, source["input"]), object{"role": "user", "content": []any{object{"type": "input_image", "image_url": url}}})
	mixed, err := json.Marshal(source)
	require.NoError(t, err)
	_, err = PrepareNativeImagesWithLimit(mixed, 1)
	require.EqualError(t, err, "local gateway is configured for at most 1 inline images per request")
}

func TestNativeFileReferencesInspectOnlyFailedWireImages(t *testing.T) {
	url, _ := nativeTestURL(t)
	plan, err := PrepareNativeImages(nativeTestRequest(t, url, url))
	require.NoError(t, err)
	_, err = plan.Upload(t.Context(), &AttachmentUploader{}, func(context.Context, InlineAttachment) (string, error) {
		return "file-managed", nil
	})
	require.NoError(t, err)
	// Only one of the original images survives compaction. Tool arguments and
	// text resembling image references must not contribute to these counts.
	wire := []byte(`{"input":[
		{"type":"message","content":[{"type":"input_image","file_id":"file-managed"},{"type":"input_image","file_id":"file-client"},{"type":"input_text","file_id":"file-business"}]},
		{"type":"function_call_output","output":[{"type":"input_image","image_url":"data:image/png;base64,PRIVATE"},{"type":"input_image","image_url":"https://private.example/image"}]},
		{"type":"custom_tool_call_output","output":[{"type":"input_image","file_id":"file-other"}]},
		{"type":"function_call","content":[{"type":"input_image","file_id":"file-business"}],"arguments":{"file_id":"file-business"}},
		{"type":"agent_message","content":[{"type":"input_image","file_id":"file-agent"}]}
	]}`)
	require.Equal(t, FileReferenceCounts{FreshUpload: 1, Unmanaged: 3, Inline: 1, Remote: 1}, plan.FileReferences(wire))
	require.Equal(t, FileReferenceCounts{Unmanaged: 4, Inline: 1, Remote: 1}, (*NativeImages)(nil).FileReferences(wire))
}
