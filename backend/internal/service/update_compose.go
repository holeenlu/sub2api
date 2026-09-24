package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"time"
)

// ContainerUpdateStatus survives app container replacement in the host updater.
type ContainerUpdateStatus struct {
	Status  string `json:"status"`
	Version string `json:"version,omitempty"`
	Message string `json:"message,omitempty"`
}

func inContainer() bool { _, err := os.Stat("/.dockerenv"); return err == nil }

func (s *UpdateService) RequiresRestart() bool { return s.composeSocket == "" }

func (s *UpdateService) composeRequest(ctx context.Context, method, path string, body []byte) (*ContainerUpdateStatus, error) {
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, "unix", s.composeSocket)
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	req, err := http.NewRequestWithContext(ctx, method, "http://localhost"+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("compose updater unavailable: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		return nil, fmt.Errorf("compose updater returned HTTP %d", resp.StatusCode)
	}
	var status ContainerUpdateStatus
	err = json.NewDecoder(io.LimitReader(resp.Body, 8192)).Decode(&status)
	return &status, err
}

func (s *UpdateService) queueContainerUpdate(ctx context.Context, version string) error {
	if s.composeSocket == "" {
		return fmt.Errorf("docker online updates require the host Compose updater (UPDATE_COMPOSE_SOCKET)")
	}
	body, _ := json.Marshal(map[string]string{"version": version})
	_, err := s.composeRequest(ctx, http.MethodPost, "/update", body)
	return err
}

func (s *UpdateService) decorateUpdateMethod(ctx context.Context, info *UpdateInfo) {
	if info == nil {
		return
	}
	info.UpdateMethod = "binary"
	if s.container || s.composeSocket != "" {
		info.UpdateMethod = "manual"
		if s.composeSocket != "" {
			status, err := s.composeRequest(ctx, http.MethodGet, "/status", nil)
			if err == nil {
				info.UpdateMethod = "compose"
				info.ContainerUpdate = status
			}
		}
	}
	info.ReleaseChannel = s.releaseChannel
}
