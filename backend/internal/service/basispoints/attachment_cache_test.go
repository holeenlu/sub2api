package basispoints

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

func countingUpload(calls *atomic.Int64) AttachmentUpload {
	return func(ctx context.Context, img InlineAttachment) (string, error) {
		return fmt.Sprintf("file-cache%d", calls.Add(1)), nil
	}
}

func TestAttachmentCacheReusesIDsWithinScopeOnly(t *testing.T) {
	urls := distinctPNGURLs(t, 3)
	cache := new(AttachmentCache)
	var calls atomic.Int64

	first := parallelImagePlan(t, urls, true)
	first.UseCache(cache, "scope-a")
	if _, err := first.Upload(context.Background(), new(AttachmentUploader), countingUpload(&calls)); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 3 || first.UsedCachedAttachments() {
		t.Fatalf("first request uploads every distinct image: %d", calls.Load())
	}

	second := parallelImagePlan(t, urls, true)
	second.UseCache(cache, "scope-a")
	out, err := second.Upload(context.Background(), new(AttachmentUploader), countingUpload(&calls))
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 3 || !second.UsedCachedAttachments() {
		t.Fatal("the same scope reuses cached IDs without uploading")
	}
	counts := second.FileReferences(out)
	if counts.ReusedUpload != 4 || counts.FreshUpload != 0 {
		t.Fatalf("reused references = %+v", counts)
	}

	other := parallelImagePlan(t, urls, false)
	other.UseCache(cache, "scope-b")
	if _, err := other.Upload(context.Background(), new(AttachmentUploader), countingUpload(&calls)); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 6 || other.UsedCachedAttachments() {
		t.Fatal("another scope (account, key, conversation or credential) never reuses IDs")
	}

	unscoped := parallelImagePlan(t, urls, false)
	unscoped.UseCache(cache, "")
	if _, err := unscoped.Upload(context.Background(), new(AttachmentUploader), countingUpload(&calls)); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 9 {
		t.Fatal("an empty scope disables the cache")
	}
}

func TestAttachmentCacheExpiresAndEvictsLeastRecentlyUsed(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	cache := &AttachmentCache{now: func() time.Time { return now }}
	urls := distinctPNGURLs(t, 2)
	var calls atomic.Int64
	plan := parallelImagePlan(t, urls[:1], false)
	plan.UseCache(cache, "scope")
	if _, err := plan.Upload(context.Background(), new(AttachmentUploader), countingUpload(&calls)); err != nil {
		t.Fatal(err)
	}
	now = now.Add(AttachmentCacheTTL - time.Second)
	again := parallelImagePlan(t, urls[:1], false)
	again.UseCache(cache, "scope")
	_, _ = again.Upload(context.Background(), new(AttachmentUploader), countingUpload(&calls))
	if calls.Load() != 1 {
		t.Fatal("an unexpired entry is reused")
	}
	now = now.Add(2 * time.Second)
	expired := parallelImagePlan(t, urls[:1], false)
	expired.UseCache(cache, "scope")
	_, _ = expired.Upload(context.Background(), new(AttachmentUploader), countingUpload(&calls))
	if calls.Load() != 2 || expired.UsedCachedAttachments() {
		t.Fatal("an expired entry is uploaded again")
	}

	img := expired.parts[0].image
	full := &AttachmentCache{now: func() time.Time { return now }}
	for i := 0; i < AttachmentCacheMaxEntries; i++ {
		full.store(fmt.Sprintf("scope-%d", i), img, fmt.Sprintf("file-e%d", i))
		now = now.Add(time.Millisecond)
	}
	if _, ok := full.lookup("scope-0", img); !ok {
		t.Fatal("entry 0 should still be cached")
	}
	full.store("scope-new", img, "file-new")
	if full.Len() != AttachmentCacheMaxEntries {
		t.Fatalf("cache size = %d", full.Len())
	}
	if _, ok := full.lookup("scope-1", img); ok {
		t.Fatal("the least recently used entry is evicted")
	}
	if _, ok := full.lookup("scope-0", img); !ok {
		t.Fatal("a recently used entry survives eviction")
	}
}

func TestAttachmentCacheInvalidationForcesFreshUpload(t *testing.T) {
	urls := distinctPNGURLs(t, 2)
	cache := new(AttachmentCache)
	var calls atomic.Int64
	first := parallelImagePlan(t, urls, false)
	first.UseCache(cache, "scope")
	_, _ = first.Upload(context.Background(), new(AttachmentUploader), countingUpload(&calls))

	reused := parallelImagePlan(t, urls, false)
	reused.UseCache(cache, "scope")
	_, _ = reused.Upload(context.Background(), new(AttachmentUploader), countingUpload(&calls))
	if !reused.UsedCachedAttachments() {
		t.Fatal("expected cached IDs")
	}
	if removed := reused.InvalidateAttachmentCache(); removed != 2 {
		t.Fatalf("removed = %d", removed)
	}
	if reused.UsedCachedAttachments() || reused.InvalidateAttachmentCache() != 0 {
		t.Fatal("invalidation is idempotent and forgets this request's IDs")
	}
	if _, err := reused.Upload(context.Background(), new(AttachmentUploader), countingUpload(&calls)); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 4 || reused.UsedCachedAttachments() {
		t.Fatalf("a retry after invalidation uploads the originals again: %d", calls.Load())
	}
	if cache.Len() != 2 {
		t.Fatal("fresh uploads are cached again")
	}
}
