package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestImageOffloadRejectsPrivateURLBeforeNetwork(t *testing.T) {
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		_, _ = io.WriteString(w, "private credential")
	}))
	defer server.Close()
	storage := &fakeImageStorage{}
	uploader := NewImageResultUploader(storage, "", 0, nil)
	for _, rawURL := range []string{server.URL, "http://[::ffff:127.0.0.1]/secret", "http://169.254.169.254/latest/meta-data/", "file:///etc/passwd"} {
		body, err := json.Marshal(map[string]any{"data": []map[string]string{{"url": rawURL}}})
		require.NoError(t, err)
		_, err = uploader.Rewrite(context.Background(), "task", body)
		require.Error(t, err)
	}
	require.Zero(t, requests.Load())
	require.Empty(t, storage.saved)
}

func TestImageOffloadRejectsNonImageBytesForAllRepresentations(t *testing.T) {
	for _, contentType := range []string{"text/plain", "image/png"} {
		storage := &fakeImageStorage{}
		client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {contentType}}, Body: io.NopCloser(bytes.NewReader([]byte("private credential")))}, nil
		})}
		uploader := NewImageResultUploader(storage, "", 0, client)
		b64 := base64.StdEncoding.EncodeToString([]byte("private credential"))
		for _, item := range []map[string]string{{"url": "https://public.example/image"}, {"b64_json": b64}, {"url": "data:image/png;base64," + b64}} {
			body, err := json.Marshal(map[string]any{"data": []map[string]string{item}})
			require.NoError(t, err)
			_, err = uploader.Rewrite(context.Background(), "task", body)
			require.ErrorContains(t, err, "not an allowed image")
		}
		require.Empty(t, storage.saved)
	}
}

func TestImageOffloadBoundsB64JSONBeforeStorage(t *testing.T) {
	storage := &fakeImageStorage{}
	uploader := NewImageResultUploader(storage, "", 3, nil)
	body := json.RawMessage(`{"data":[{"b64_json":"` + base64.StdEncoding.EncodeToString(pngBytes) + `"}]}`)
	_, err := uploader.Rewrite(context.Background(), "task", body)
	require.ErrorContains(t, err, "exceeds 3 bytes")
	require.Empty(t, storage.saved)
}
