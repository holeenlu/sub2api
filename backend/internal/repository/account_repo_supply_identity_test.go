package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/authz"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type nativeIdentityGuardStub struct {
	verify func(context.Context, *service.Account) (*service.VerifiedSupplyIdentity, error)
	claim  func(context.Context, service.VendorProxySQL, *service.Account, *service.VerifiedSupplyIdentity) error
}

func (g nativeIdentityGuardStub) Verify(ctx context.Context, a *service.Account) (*service.VerifiedSupplyIdentity, error) {
	return g.verify(ctx, a)
}
func (g nativeIdentityGuardStub) Claim(ctx context.Context, tx service.VendorProxySQL, a *service.Account, p *service.VerifiedSupplyIdentity) error {
	return g.claim(ctx, tx, a, p)
}

func TestNativeSupplyIdentityCreateRequiresVerifiedAtomicClaim(t *testing.T) {
	for _, grouped := range []bool{false, true} {
		t.Run(map[bool]string{false: "create", true: "create with groups"}[grouped], func(t *testing.T) {
			repo, mock := supplyRepoMock(t)
			a := &service.Account{Name: "native", Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, Credentials: map[string]any{"access_token": "verified-token"}, Status: service.StatusActive, Schedulable: true, Concurrency: 1}
			require.ErrorIs(t, repo.Create(context.Background(), a), service.ErrVendorIdentityUnverified)
			verified := false
			proof := new(service.VerifiedSupplyIdentity)
			repo.SetSupplyIdentityGuard(nativeIdentityGuardStub{
				verify: func(_ context.Context, input *service.Account) (*service.VerifiedSupplyIdentity, error) {
					verified = true
					require.Zero(t, input.ID)
					return proof, nil
				},
				claim: func(ctx context.Context, tx service.VendorProxySQL, input *service.Account, p *service.VerifiedSupplyIdentity) error {
					require.True(t, verified)
					require.Equal(t, int64(99), input.ID)
					require.Same(t, proof, p)
					_, err := tx.ExecContext(ctx, "SELECT 'claim after insert'")
					return err
				},
			})
			mock.ExpectBegin()
			mock.ExpectQuery("INSERT INTO.*accounts").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(99))
			mock.ExpectExec("SELECT 'claim after insert'").WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectExec("INSERT INTO scheduler_outbox").WillReturnResult(sqlmock.NewResult(1, 1))
			mock.ExpectCommit()
			if grouped {
				require.NoError(t, repo.CreateWithAccountGroups(context.Background(), a, nil))
			} else {
				require.NoError(t, repo.Create(context.Background(), a))
			}
		})
	}
}

func TestNativeSupplyIdentityTokenReplacementVerifiesEvenWithUnchangedIdentityFields(t *testing.T) {
	repo, mock := supplyRepoMock(t)
	mock.ExpectQuery("SELECT platform,type,vendor_id,parent_account_id,proxy_id,credentials=").WillReturnRows(sqlmock.NewRows([]string{"platform", "type", "owner", "parent", "proxy", "unchanged"}).AddRow(service.PlatformOpenAI, service.AccountTypeOAuth, nil, nil, nil, false))
	repo.SetSupplyIdentityGuard(nativeIdentityGuardStub{verify: func(_ context.Context, a *service.Account) (*service.VerifiedSupplyIdentity, error) {
		require.Equal(t, "other-owner-token", a.GetCredential("access_token"))
		return nil, service.ErrVendorIdentityBound
	}})
	err := repo.UpdateCredentials(context.Background(), 1, map[string]any{"access_token": "other-owner-token", "chatgpt_account_id": "unchanged-declared-id"})
	require.ErrorIs(t, err, service.ErrVendorIdentityBound, "verification failure must precede any transaction/write")
}

func TestNativeSupplyIdentityRefreshClaimFailureRollsBackCredentialWrite(t *testing.T) {
	repo, mock := supplyRepoMock(t)
	mock.ExpectQuery("SELECT platform,type,vendor_id,parent_account_id,proxy_id,credentials=").WillReturnRows(sqlmock.NewRows([]string{"platform", "type", "owner", "parent", "proxy", "unchanged"}).AddRow(service.PlatformOpenAI, service.AccountTypeOAuth, nil, nil, nil, false))
	repo.SetSupplyIdentityGuard(nativeIdentityGuardStub{
		verify: func(context.Context, *service.Account) (*service.VerifiedSupplyIdentity, error) {
			return new(service.VerifiedSupplyIdentity), nil
		},
		claim: func(ctx context.Context, tx service.VendorProxySQL, _ *service.Account, _ *service.VerifiedSupplyIdentity) error {
			_, err := tx.ExecContext(ctx, "SELECT 'claim after credentials'")
			return err
		},
	})
	mock.ExpectBegin()
	mock.ExpectExec("UPDATE accounts").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("SELECT 'claim after credentials'").WillReturnError(service.ErrVendorIdentityBound)
	mock.ExpectRollback()
	require.ErrorIs(t, repo.UpdateCredentials(context.Background(), 1, map[string]any{"access_token": "new-token"}), service.ErrVendorIdentityBound)
}

func TestNativeSupplyIdentityMetadataEditsDoNotRequireNetwork(t *testing.T) {
	repo, mock := supplyRepoMock(t)
	mock.ExpectQuery("SELECT platform=.*credentials=").WillReturnRows(sqlmock.NewRows([]string{"unchanged"}).AddRow(true))
	identity, err := repo.prepareNativeSupplyUpdate(context.Background(), &service.Account{ID: 1, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, Credentials: map[string]any{"access_token": "same"}, Name: "metadata-only"})
	require.NoError(t, err)
	require.Nil(t, identity)
}

func TestNativeSupplyIdentityBulkProofFailurePreventsBatchMutation(t *testing.T) {
	repo, mock := supplyRepoMock(t)
	mock.ExpectQuery("native_supply_credential_preflight").WillReturnRows(sqlmock.NewRows([]string{"id", "platform", "type", "owner", "parent", "proxy", "credentials"}).AddRow(1, service.PlatformOpenAI, service.AccountTypeOAuth, nil, nil, nil, []byte("{}")))
	mock.ExpectQuery("SELECT platform=.*credentials=").WillReturnRows(sqlmock.NewRows([]string{"unchanged"}).AddRow(false))
	want := errors.New("upstream unavailable")
	repo.SetSupplyIdentityGuard(nativeIdentityGuardStub{verify: func(context.Context, *service.Account) (*service.VerifiedSupplyIdentity, error) { return nil, want }})
	_, err := repo.BulkUpdate(context.Background(), []int64{1}, service.AccountBulkUpdate{Credentials: map[string]any{"access_token": "new"}})
	require.ErrorIs(t, err, want)
}

func TestNativeSupplierRefreshRequiresProofAndAtomicClaim(t *testing.T) {
	for _, scoped := range []bool{false, true} {
		t.Run(map[bool]string{false: "background", true: "workbench scope"}[scoped], func(t *testing.T) {
			repo, mock := supplyRepoMock(t)
			ctx := context.Background()
			if scoped {
				ctx = authz.WithVendorScope(ctx, authz.VendorScope{VendorID: 7, ActorUserID: 9, PolicyVersion: 3})
				mock.ExpectQuery("SELECT COUNT").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
			}
			mock.ExpectQuery("SELECT platform,type,vendor_id,parent_account_id,proxy_id,credentials=").WillReturnRows(sqlmock.NewRows([]string{"platform", "type", "owner", "parent", "proxy", "unchanged"}).AddRow(service.PlatformOpenAI, service.AccountTypeOAuth, 7, nil, nil, false))
			mock.ExpectQuery("SELECT EXISTS.*FROM accounts a").WillReturnRows(sqlmock.NewRows([]string{"allowed"}).AddRow(true))
			proof := new(service.VerifiedSupplyIdentity)
			repo.SetSupplyIdentityGuard(nativeIdentityGuardStub{
				verify: func(_ context.Context, a *service.Account) (*service.VerifiedSupplyIdentity, error) {
					require.Equal(t, int64(7), *a.VendorID)
					require.Equal(t, "new-token", a.GetCredential("access_token"))
					return proof, nil
				},
				claim: func(ctx context.Context, tx service.VendorProxySQL, a *service.Account, p *service.VerifiedSupplyIdentity) error {
					require.Equal(t, int64(7), *a.VendorID)
					require.Same(t, proof, p)
					_, err := tx.ExecContext(ctx, "SELECT 'supplier claim after credentials'")
					return err
				},
			})
			mock.ExpectBegin()
			mock.ExpectExec("UPDATE accounts").WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectExec("SELECT 'supplier claim after credentials'").WillReturnError(service.ErrVendorIdentityBound)
			mock.ExpectRollback()
			require.ErrorIs(t, repo.UpdateCredentials(ctx, 1, map[string]any{"access_token": "new-token", "chatgpt_account_id": "unchanged-id"}), service.ErrVendorIdentityBound)
		})
	}
}

func TestNativeSupplierRefreshCannotReplaceTokenWithUnverifiedIdentity(t *testing.T) {
	repo, mock := supplyRepoMock(t)
	mock.ExpectQuery("SELECT platform,type,vendor_id,parent_account_id,proxy_id,credentials=").WillReturnRows(sqlmock.NewRows([]string{"platform", "type", "owner", "parent", "proxy", "unchanged"}).AddRow(service.PlatformOpenAI, service.AccountTypeOAuth, 7, nil, nil, false))
	mock.ExpectQuery("SELECT EXISTS.*FROM accounts a").WillReturnRows(sqlmock.NewRows([]string{"allowed"}).AddRow(true))
	repo.SetSupplyIdentityGuard(nativeIdentityGuardStub{verify: func(_ context.Context, a *service.Account) (*service.VerifiedSupplyIdentity, error) {
		require.Equal(t, "different-owner-token", a.GetCredential("access_token"))
		return nil, service.ErrVendorIdentityBound
	}})
	require.ErrorIs(t, repo.UpdateCredentials(context.Background(), 1, map[string]any{"access_token": "different-owner-token", "chatgpt_account_id": "same-declared-id"}), service.ErrVendorIdentityBound)
}

func TestNativeSupplierShadowCannotStoreIndependentCredentials(t *testing.T) {
	repo, mock := supplyRepoMock(t)
	mock.ExpectQuery("SELECT platform,type,vendor_id,parent_account_id,proxy_id,credentials=").WillReturnRows(sqlmock.NewRows([]string{"platform", "type", "owner", "parent", "proxy", "unchanged"}).AddRow(service.PlatformOpenAI, service.AccountTypeOAuth, 7, 2, nil, false))
	require.ErrorIs(t, repo.UpdateCredentials(context.Background(), 1, map[string]any{"access_token": "shadow-token"}), authz.ErrPermissionDenied)
}
