package server

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRequestBodyDeadlineBoundsUploadWithoutTimingOutResponse(t *testing.T) {
	server := httptest.NewServer(requestBodyReadDeadline(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "body timeout", http.StatusRequestTimeout)
			return
		}
		time.Sleep(100 * time.Millisecond)
		fmt.Fprint(w, "stream remains writable")
	}), 40*time.Millisecond))
	defer server.Close()
	conn, err := net.Dial("tcp", strings.TrimPrefix(server.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	_, err = fmt.Fprint(conn, "POST / HTTP/1.1\r\nHost: localhost\r\nContent-Length: 10\r\n\r\nx")
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusRequestTimeout {
		t.Fatalf("status=%d", response.StatusCode)
	}
	response.Body.Close()
	response, err = http.Post(server.URL, "text/plain", strings.NewReader("complete"))
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "stream remains writable" {
		t.Fatalf("response=%q", body)
	}
}
