package service

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"runtime"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"

	bodyutil "github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
)

// Opt-in resource probe. Run each configuration in a fresh process under time(1)
// to obtain peak RSS, using the same probe on the fixed base and working tree.
// local_first_frame measures in-process read/replay/conversion latency, excluding
// network/provider generation; it must not be described as production TTFT.
func BenchmarkGatewayBodyReplay(b *testing.B) {
	if os.Getenv("SUB2API_RESOURCE_BENCH") != "1" {
		b.Skip("opt-in resource benchmark")
	}
	size, _ := strconv.Atoi(os.Getenv("BODY_MIB"))
	if size == 0 {
		size = 1
	}
	workers, _ := strconv.Atoi(os.Getenv("BODY_CONCURRENCY"))
	if workers == 0 {
		workers = 1
	}
	retries, _ := strconv.Atoi(os.Getenv("BODY_RETRIES"))
	prefix, suffix := `{"model":"gemini-test","messages":[{"role":"user","content":"`, `"}]}`
	if os.Getenv("BODY_KIND") == "image" {
		prefix, suffix = `{"model":"gemini-test","messages":[{"role":"user","content":[{"type":"image","source":{"type":"base64","media_type":"image/png","data":"`, `"}}]}]}`
	}
	n := size*(1<<20) - len(prefix) - len(suffix)
	if os.Getenv("BODY_KIND") == "image" {
		n -= n % 4
	}
	payload := append([]byte(prefix), bytes.Repeat([]byte("A"), n)...)
	payload = append(payload, suffix...)
	convert := os.Getenv("BODY_PATH") == "conversion"
	fdCount := func() int {
		entries, err := os.ReadDir("/dev/fd")
		if err != nil {
			entries, _ = os.ReadDir("/proc/self/fd")
		}
		return len(entries)
	}
	runtime.GC()
	goroutines, descriptors := runtime.NumGoroutine(), fdCount()
	var latency []int64
	b.SetBytes(int64(len(payload) * workers))
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		errors := make(chan error, workers)
		elapsed := make(chan int64, workers)
		var wait sync.WaitGroup
		wait.Add(workers)
		for worker := 0; worker < workers; worker++ {
			go func() {
				defer wait.Done()
				start := time.Now()
				request := &http.Request{Body: io.NopCloser(bytes.NewReader(payload)), ContentLength: int64(len(payload)), Header: make(http.Header)}
				body, err := bodyutil.ReadRequestBodyWithPrealloc(request)
				if err != nil {
					errors <- err
					return
				}
				request.Body = bodyutil.NewPrereadBody(body)
				for attempt := 0; attempt <= retries; attempt++ {
					out, err := bodyutil.ReadRequestBodyWithPrealloc(request)
					if err == nil && convert {
						out, err = convertClaudeMessagesToGeminiGenerateContent(out)
					}
					if err != nil {
						errors <- err
						return
					}
					if _, err := io.Copy(io.Discard, bytes.NewReader(out)); err != nil {
						errors <- err
						return
					}
				}
				_ = request.Body.Close()
				// Equivalent to the first local SSE write once the final attempt is ready.
				elapsed <- time.Since(start).Nanoseconds()
			}()
		}
		wait.Wait()
		close(errors)
		close(elapsed)
		for err := range errors {
			b.Fatal(err)
		}
		for duration := range elapsed {
			latency = append(latency, duration)
		}
	}
	b.StopTimer()
	slices.Sort(latency)
	if len(latency) > 0 {
		b.ReportMetric(float64(latency[len(latency)/2])/1e6, "local_first_frame_p50_ms")
		b.ReportMetric(float64(latency[(len(latency)-1)*95/100])/1e6, "local_first_frame_p95_ms")
	}
	runtime.GC()
	b.ReportMetric(float64(runtime.NumGoroutine()-goroutines), "goroutines_delta")
	b.ReportMetric(float64(fdCount()-descriptors), "fd_delta")
	b.ReportMetric(float64(workers), "requests/op")
}
