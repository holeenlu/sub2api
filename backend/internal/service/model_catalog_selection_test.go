//go:build unit

package service

import "context"

// Existing accounts do not need a new save or a successful discovery request
// before the simplified group selection can route to their declared supply.

type selectionCatalogRepo struct{ *catalogMemoryRepo }

func (r *selectionCatalogRepo) ListPlatform(_ context.Context, platform string) ([]ModelCatalogSnapshot, error) {
	var rows []ModelCatalogSnapshot
	for _, snapshot := range r.snapshots {
		if platform == "" || snapshot.Platform == platform {
			rows = append(rows, *snapshot)
		}
	}
	return rows, nil
}
