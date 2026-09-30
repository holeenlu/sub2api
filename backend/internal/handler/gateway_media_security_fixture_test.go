//go:build unit

package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type securityHandlerMediaRepo struct {
	service.GatewayMediaRepository
	mu   sync.Mutex
	jobs map[string][]byte
}

func newSecurityHandlerMediaRepo() *securityHandlerMediaRepo {
	return &securityHandlerMediaRepo{jobs: map[string][]byte{}}
}
func (r *securityHandlerMediaRepo) CreateJob(_ context.Context, j *service.GatewayMediaJob) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, b := range r.jobs {
		var old service.GatewayMediaJob
		_ = json.Unmarshal(b, &old)
		if old.UserID == j.UserID && old.State != "settled" && old.State != "failed" {
			return service.ErrMediaPendingCapacity
		}
	}
	b, err := json.Marshal(j)
	r.jobs[j.ID] = b
	return err
}
func (r *securityHandlerMediaRepo) SaveJob(_ context.Context, j *service.GatewayMediaJob) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	var old service.GatewayMediaJob
	_ = json.Unmarshal(r.jobs[j.ID], &old)
	if old.Version != j.Version {
		return fmt.Errorf("stale version")
	}
	j.Version++
	b, err := json.Marshal(j)
	r.jobs[j.ID] = b
	return err
}
func (r *securityHandlerMediaRepo) GetJob(_ context.Context, g, u int64, id string) (*service.GatewayMediaJob, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, b := range r.jobs {
		var j service.GatewayMediaJob
		_ = json.Unmarshal(b, &j)
		if j.GroupID == g && j.UserID == u && j.TaskID == id && id != "" {
			return &j, nil
		}
	}
	return nil, service.ErrMediaNotOwned
}
