package logger

import (
	"io"
	"log"
	"os"
	"strings"
	"testing"
)

func TestInferStdLogLevel(t *testing.T) {
	cases := []struct {
		msg  string
		want Level
	}{
		{msg: "Warning: queue full", want: LevelWarn},
		{msg: "Forward request failed: timeout", want: LevelError},
		{msg: "[ERROR] upstream unavailable", want: LevelError},
		{msg: "[OpenAI WS Mode] reconnect_retry account_id=22 retry=1 max_retries=5", want: LevelInfo},
		{msg: "service started", want: LevelInfo},
		{msg: "debug: cache miss", want: LevelDebug},
	}

	for _, tc := range cases {
		got := inferStdLogLevel(tc.msg)
		if got != tc.want {
			t.Fatalf("inferStdLogLevel(%q)=%v want=%v", tc.msg, got, tc.want)
		}
	}
}

func TestNormalizeStdLogMessage(t *testing.T) {
	raw := "  [TokenRefresh]  cycle complete \n total=1   failed=0 \n"
	got := normalizeStdLogMessage(raw)
	want := "[TokenRefresh] cycle complete total=1 failed=0"
	if got != want {
		t.Fatalf("normalizeStdLogMessage()=%q want=%q", got, want)
	}
}

func TestStdLogBridgeRoutesLevels(t *testing.T) {
	origStdout := os.Stdout
	origStderr := os.Stderr
	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stdout pipe: %v", err)
	}
	stderrR, stderrW, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stderr pipe: %v", err)
	}
	os.Stdout = stdoutW
	os.Stderr = stderrW
	t.Cleanup(func() {
		os.Stdout = origStdout
		os.Stderr = origStderr
		_ = stdoutR.Close()
		_ = stdoutW.Close()
		_ = stderrR.Close()
		_ = stderrW.Close()
	})

	if err := Init(InitOptions{
		Level:       "debug",
		Format:      "json",
		ServiceName: "sub2api",
		Environment: "test",
		Output: OutputOptions{
			ToStdout: true,
			ToFile:   false,
		},
		Sampling: SamplingOptions{Enabled: false},
	}); err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	log.Printf("service started")
	log.Printf("Warning: queue full")
	log.Printf("Forward request failed: timeout")
	// Skip Sync() — on Windows, fsync on pipes deadlocks (FlushFileBuffers).

	_ = stdoutW.Close()
	_ = stderrW.Close()
	stdoutBytes, _ := io.ReadAll(stdoutR)
	stderrBytes, _ := io.ReadAll(stderrR)
	stdoutText := string(stdoutBytes)
	stderrText := string(stderrBytes)

	if !strings.Contains(stdoutText, "service started") {
		t.Fatalf("stdout missing info log: %s", stdoutText)
	}
	if !strings.Contains(stderrText, "Warning: queue full") {
		t.Fatalf("stderr missing warn log: %s", stderrText)
	}
	if !strings.Contains(stderrText, "Forward request failed: timeout") {
		t.Fatalf("stderr missing error log: %s", stderrText)
	}
	if !strings.Contains(stderrText, "\"legacy_stdlog\":true") {
		t.Fatalf("stderr missing legacy_stdlog marker: %s", stderrText)
	}
}

func TestLegacyPrintfRoutesLevels(t *testing.T) {
	origStdout := os.Stdout
	origStderr := os.Stderr
	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stdout pipe: %v", err)
	}
	stderrR, stderrW, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stderr pipe: %v", err)
	}
	os.Stdout = stdoutW
	os.Stderr = stderrW
	t.Cleanup(func() {
		os.Stdout = origStdout
		os.Stderr = origStderr
		_ = stdoutR.Close()
		_ = stdoutW.Close()
		_ = stderrR.Close()
		_ = stderrW.Close()
	})

	if err := Init(InitOptions{
		Level:       "debug",
		Format:      "json",
		ServiceName: "sub2api",
		Environment: "test",
		Output: OutputOptions{
			ToStdout: true,
			ToFile:   false,
		},
		Sampling: SamplingOptions{Enabled: false},
	}); err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	LegacyPrintf("service.test", "request started")
	LegacyPrintf("service.test", "Warning: queue full")
	LegacyPrintf("service.test", "forward failed: timeout")
	// Skip Sync() — on Windows, fsync on pipes deadlocks (FlushFileBuffers).

	_ = stdoutW.Close()
	_ = stderrW.Close()
	stdoutBytes, _ := io.ReadAll(stdoutR)
	stderrBytes, _ := io.ReadAll(stderrR)
	stdoutText := string(stdoutBytes)
	stderrText := string(stderrBytes)

	if !strings.Contains(stdoutText, "request started") {
		t.Fatalf("stdout missing info log: %s", stdoutText)
	}
	if !strings.Contains(stderrText, "Warning: queue full") {
		t.Fatalf("stderr missing warn log: %s", stderrText)
	}
	if !strings.Contains(stderrText, "forward failed: timeout") {
		t.Fatalf("stderr missing error log: %s", stderrText)
	}
	if !strings.Contains(stderrText, "\"legacy_printf\":true") {
		t.Fatalf("stderr missing legacy_printf marker: %s", stderrText)
	}
	if !strings.Contains(stderrText, "\"component\":\"service.test\"") {
		t.Fatalf("stderr missing component field: %s", stderrText)
	}
}

func TestLegacyPrintfLevelBypassesKeywordInference(t *testing.T) {
	origStdout := os.Stdout
	origStderr := os.Stderr
	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stdout pipe: %v", err)
	}
	stderrR, stderrW, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stderr pipe: %v", err)
	}
	os.Stdout = stdoutW
	os.Stderr = stderrW
	t.Cleanup(func() {
		os.Stdout = origStdout
		os.Stderr = origStderr
		_ = stdoutR.Close()
		_ = stdoutW.Close()
		_ = stderrR.Close()
		_ = stderrW.Close()
	})

	if err := Init(InitOptions{
		Level:       "debug",
		Format:      "json",
		ServiceName: "sub2api",
		Environment: "test",
		Output: OutputOptions{
			ToStdout: true,
			ToFile:   false,
		},
		Sampling: SamplingOptions{Enabled: false},
	}); err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	// 信息日志的事件名/字段名含 error、fail、fallback 关键字，显式级别必须优先于推断。
	LegacyPrintfLevel(LevelInfo, "service.test", "ingress_ws_error_event account_id=%d fallback_reason=%s", 7, "upstream_err_event")
	LegacyPrintfLevel(LevelWarn, "service.test", "conn pool shrink")
	// Skip Sync() — on Windows, fsync on pipes deadlocks (FlushFileBuffers).

	_ = stdoutW.Close()
	_ = stderrW.Close()
	stdoutBytes, _ := io.ReadAll(stdoutR)
	stderrBytes, _ := io.ReadAll(stderrR)
	stdoutText := string(stdoutBytes)
	stderrText := string(stderrBytes)

	if !strings.Contains(stdoutText, "ingress_ws_error_event account_id=7 fallback_reason=upstream_err_event") {
		t.Fatalf("stdout missing explicit info log: %s", stdoutText)
	}
	if strings.Contains(stderrText, "ingress_ws_error_event") {
		t.Fatalf("explicit info log was promoted by keyword inference: %s", stderrText)
	}
	if !strings.Contains(stderrText, "conn pool shrink") {
		t.Fatalf("stderr missing explicit warn log: %s", stderrText)
	}
	if !strings.Contains(stdoutText, "\"legacy_printf\":true") || !strings.Contains(stdoutText, "\"component\":\"service.test\"") {
		t.Fatalf("stdout missing legacy_printf/component fields: %s", stdoutText)
	}
}
