package basispoints

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func distinctPNGURLs(t *testing.T, count int) []string {
	t.Helper()
	urls := make([]string, count)
	for i := range urls {
		var img bytes.Buffer
		if err := png.Encode(&img, image.NewRGBA(image.Rect(0, 0, i+2, 3))); err != nil {
			t.Fatal(err)
		}
		urls[i] = "data:image/png;base64," + base64.StdEncoding.EncodeToString(img.Bytes())
	}
	return urls
}

func parallelImagePlan(t *testing.T, urls []string, duplicateFirst bool) *NativeImages {
	t.Helper()
	var content []any
	for _, url := range urls {
		content = append(content, object{"type": "input_image", "image_url": url})
	}
	if duplicateFirst {
		content = append(content, object{"type": "input_image", "image_url": urls[0]})
	}
	source := testSource()
	source["input"] = []any{object{"role": "user", "content": content}}
	raw, _ := json.Marshal(source)
	plan, err := PrepareNativeImagesWithLimit(raw, 64)
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func TestNativeAttachmentsUploadDistinctImagesInParallelWithinLimit(t *testing.T) {
	plan := parallelImagePlan(t, distinctPNGURLs(t, 9), true)
	var active, peak, calls atomic.Int64
	var mu sync.Mutex
	seen := map[[32]byte]int{}
	out, err := plan.Upload(context.Background(), new(AttachmentUploader), func(ctx context.Context, img InlineAttachment) (string, error) {
		n := calls.Add(1)
		now := active.Add(1)
		for {
			old := peak.Load()
			if now <= old || peak.CompareAndSwap(old, now) {
				break
			}
		}
		mu.Lock()
		seen[img.digest]++
		mu.Unlock()
		time.Sleep(30 * time.Millisecond)
		active.Add(-1)
		return fmt.Sprintf("file-par%d", n), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 9 {
		t.Fatalf("each distinct image uploads once, got %d uploads", calls.Load())
	}
	for digest, count := range seen {
		if count != 1 {
			t.Fatalf("image %x uploaded %d times", digest[:4], count)
		}
	}
	if peak.Load() > MaxParallelAttachmentUploads || peak.Load() < 2 {
		t.Fatalf("parallel uploads peaked at %d, want 2..%d", peak.Load(), MaxParallelAttachmentUploads)
	}
	var got object
	_ = decode(out, &got)
	items := mustTestValue[[]any](t, got["input"])
	content := mustTestValue[[]any](t, mustTestValue[object](t, items[0])["content"])
	if len(content) != 10 {
		t.Fatalf("content parts = %d", len(content))
	}
	ids := map[any]bool{}
	for _, raw := range content {
		part := mustTestValue[object](t, raw)
		if part["file_id"] == nil || part["image_url"] != nil {
			t.Fatal("every part must reference its uploaded file")
		}
		ids[part["file_id"]] = true
	}
	first := mustTestValue[object](t, content[0])["file_id"]
	if last := mustTestValue[object](t, content[9])["file_id"]; last != first || len(ids) != 9 {
		t.Fatal("a duplicate image must share the first upload")
	}
}

func TestNativeAttachmentsParallelFailureCancelsSiblingsAndKeepsCause(t *testing.T) {
	plan := parallelImagePlan(t, distinctPNGURLs(t, 6), false)
	rejected := errors.New("upload rejected")
	var calls atomic.Int64
	var failed atomic.Bool
	succeeded := make(chan struct{})
	_, err := plan.Upload(context.Background(), new(AttachmentUploader), func(ctx context.Context, img InlineAttachment) (string, error) {
		n := calls.Add(1)
		if n == 1 {
			close(succeeded)
			return "file-ok", nil
		}
		if failed.CompareAndSwap(false, true) {
			<-succeeded
			return "", rejected
		}
		<-ctx.Done()
		return "", ctx.Err()
	})
	if !errors.Is(err, rejected) {
		t.Fatalf("want the failure that stopped the batch, got %v", err)
	}
	if len(plan.uploaded) != 1 {
		t.Fatalf("only the completed upload is retained, got %d", len(plan.uploaded))
	}
	// Continue reuses the completed upload instead of sending it again.
	next, err := plan.Continue(parallelPlanBody(t, plan), 64)
	if err != nil {
		t.Fatal(err)
	}
	if len(next.uploaded) != 1 {
		t.Fatal("Continue must keep request-local deduplication")
	}
}

func TestNativeAttachmentsParallelHonorsCallerCancellation(t *testing.T) {
	plan := parallelImagePlan(t, distinctPNGURLs(t, 6), false)
	ctx, cancel := context.WithCancel(context.Background())
	var calls atomic.Int64
	_, err := plan.Upload(ctx, new(AttachmentUploader), func(ctx context.Context, img InlineAttachment) (string, error) {
		if calls.Add(1) == 1 {
			cancel()
		}
		<-ctx.Done()
		return "", ctx.Err()
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want caller cancellation, got %v", err)
	}
	if calls.Load() > MaxParallelAttachmentUploads {
		t.Fatalf("no upload may start after cancellation, got %d", calls.Load())
	}
}

func parallelPlanBody(t *testing.T, plan *NativeImages) []byte {
	t.Helper()
	raw, err := json.Marshal(plan.source)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
