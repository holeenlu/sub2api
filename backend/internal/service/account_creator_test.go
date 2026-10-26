package service

import (
	"context"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/authz"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestAccountCreatorUsesLiveActorSnapshot(t *testing.T) {
	for _, role := range []string{authz.SuperAdmin, authz.Admin, authz.VendorLevel1, authz.VendorLevel2} {
		t.Run(role, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer db.Close()
			ctx := authz.WithSubject(context.Background(), authz.Subject{UserID: 12, Role: role})
			mock.ExpectQuery("SELECT COALESCE").WithArgs(int64(12)).
				WillReturnRows(sqlmock.NewRows([]string{"name", "role"}).AddRow("creation-time-name", role))
			creator, err := ResolveAccountCreator(ctx, db)
			require.NoError(t, err)
			require.Equal(t, int64(12), *creator.ID)
			require.Equal(t, "creation-time-name", creator.Name)
			require.Equal(t, role, creator.Role)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestAccountCreatorRejectsChangedActorAndLabelsSystem(t *testing.T) {
	system, err := ResolveAccountCreator(context.Background(), nil)
	require.NoError(t, err)
	require.Nil(t, system.ID)
	require.Equal(t, "system", system.Role)
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	mock.ExpectQuery("SELECT COALESCE").WillReturnRows(sqlmock.NewRows([]string{"name", "role"}).AddRow("changed", authz.User))
	_, err = ResolveAccountCreator(authz.WithSubject(context.Background(), authz.Subject{UserID: 12, Role: authz.SuperAdmin}), db)
	require.ErrorIs(t, err, authz.ErrPermissionDenied)
}

func TestAccountCreationDefaultsAreSharedWithoutChangingExplicitValues(t *testing.T) {
	for _, platform := range []string{PlatformAnthropic, PlatformOpenAI} {
		a, err := buildAccountForCreate(&CreateAccountInput{Platform: platform, Type: AccountTypeOAuth, Priority: DefaultAccountPriority}, map[string]any{})
		require.NoError(t, err)
		require.Equal(t, NewAccountCreationDefaults().Concurrency, a.Concurrency)
		require.Equal(t, 1, a.Priority)
		require.Equal(t, 1.0, a.BillingRateMultiplier())
		require.True(t, a.AutoPauseOnExpired)
		a, err = buildAccountForCreate(&CreateAccountInput{Platform: platform, Type: AccountTypeOAuth, Concurrency: 27, Priority: 0}, map[string]any{})
		require.NoError(t, err)
		require.Equal(t, 27, a.Concurrency)
		require.Zero(t, a.Priority)
	}
}
