//go:build integration

package repository

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestObserverRepositoryScopeAndSharedBindings(t *testing.T) {
	tx := testEntTx(t)
	client := tx.Client()
	repo := newAccountRepositoryWithSQL(client, tx, nil)
	ctx := context.Background()
	granted := mustCreateGroup(t, client, &service.Group{Name: "observer-granted", Platform: service.PlatformOpenAI})
	other := mustCreateGroup(t, client, &service.Group{Name: "observer-other", Platform: service.PlatformOpenAI})
	shared := mustCreateAccount(t, client, newExcelBPSAutoDisableAccount())
	hidden := mustCreateAccount(t, client, newExcelBPSAutoDisableAccount())
	require.NoError(t, repo.BindGroups(ctx, shared.ID, []int64{granted.ID, other.ID}))
	require.NoError(t, repo.BindGroups(ctx, hidden.ID, []int64{other.ID}))
	scoped := service.WithObserverScope(ctx, []int64{granted.ID})
	rows, page, err := repo.List(scoped, pagination.DefaultPagination())
	require.NoError(t, err)
	require.EqualValues(t, 1, page.Total)
	require.Len(t, rows, 1)
	require.Equal(t, shared.ID, rows[0].ID)
	_, err = repo.GetByID(scoped, hidden.ID)
	require.ErrorIs(t, err, service.ErrAccountNotFound)
	exported, err := repo.ListAllWithFilters(scoped, "", "", "", "", 0, "")
	require.NoError(t, err)
	require.Len(t, exported, 1)
	require.NoError(t, repo.BindGroups(scoped, shared.ID, []int64{granted.ID}))
	after, err := repo.GetByID(ctx, shared.ID)
	require.NoError(t, err)
	require.ElementsMatch(t, []int64{granted.ID, other.ID}, after.GroupIDs, "editing granted groups must preserve other owners' bindings")
	require.ErrorIs(t, repo.BindGroups(scoped, shared.ID, []int64{other.ID}), service.ErrObserverScope)
	empty := service.WithObserverScope(ctx, []int64{})
	rows, page, err = repo.List(empty, pagination.DefaultPagination())
	require.NoError(t, err)
	require.Empty(t, rows)
	require.Zero(t, page.Total)
}
