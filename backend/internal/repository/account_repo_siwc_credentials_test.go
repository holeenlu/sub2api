package repository

import (
	"context"
	"encoding/json"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/authz"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestSIWCRepositoryDoubleCASAndParentIdentityProtection(t *testing.T) {
	for _, tc := range []struct {
		name, registration string
		primary            bool
		affected           int64
	}{{"initial registration", "", true, 1}, {"refresh", "reg-old", true, 1}, {"concurrent revoke", "reg-old", true, 0}, {"changed parent", "reg-old", false, 0}} {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer db.Close()
			client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
			defer client.Close()
			repo := newAccountRepositoryWithSQL(client, db, nil)
			a := &service.Account{ID: 44, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, Credentials: map[string]any{"access_token": "native-secret", "chatgpt_account_id": "native-workspace"}}
			primary, _ := json.Marshal(a.Credentials)
			mock.ExpectBegin()
			rows := sqlmock.NewRows([]string{"id"})
			if tc.primary {
				rows.AddRow(a.ID)
			}
			mock.ExpectQuery("SELECT a.id FROM accounts a WHERE.*vendor_id IS NULL.*parent_account_id IS NULL.*chatgpt_account_id.*chatgpt_user_id.*email.*client_id.*auth_mode.*openai_auth_mode.*FOR UPDATE").WithArgs(a.ID, string(primary)).WillReturnRows(rows)
			if tc.primary {
				pattern := "UPDATE openai_siwc_grants SET.*registration_ciphertext=.*credentials_ciphertext=.*WHERE account_id=.*registration_ciphertext=.*credentials_ciphertext="
				if tc.registration == "" {
					pattern = "INSERT INTO openai_siwc_grants.*ON CONFLICT.*registration_ciphertext=.*credentials_ciphertext="
				}
				mock.ExpectExec(pattern).WithArgs(a.ID, tc.registration, "grant-old", "reg-new", "grant-new").WillReturnResult(sqlmock.NewResult(0, tc.affected))
				mock.ExpectCommit()
			} else {
				mock.ExpectRollback()
			}
			saved, err := repo.StoreSIWCRecord(context.Background(), a, tc.registration, "grant-old", "reg-new", "grant-new")
			require.NoError(t, err)
			require.Equal(t, tc.affected == 1, saved)
			require.Equal(t, "native-secret", a.Credentials["access_token"])
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
func TestSIWCRepositoryScopesReadsAndSharesHostIdentity(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	defer client.Close()
	repo := newAccountRepositoryWithSQL(client, db, nil)
	a := &service.Account{ID: 44, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, Credentials: map[string]any{"chatgpt_account_id": "native-workspace"}}
	primary, _ := json.Marshal(a.Credentials)
	mock.ExpectQuery("SELECT g.registration_ciphertext,g.credentials_ciphertext.*a.vendor_id IS NULL.*a.parent_account_id IS NULL.*chatgpt_account_id.*chatgpt_user_id").WithArgs(a.ID, string(primary)).WillReturnRows(sqlmock.NewRows([]string{"registration_ciphertext", "credentials_ciphertext"}).AddRow("reg", "grant"))
	reg, grant, err := repo.GetSIWCRecord(context.Background(), a)
	require.NoError(t, err)
	require.Equal(t, "reg", reg)
	require.Equal(t, "grant", grant)
	for _, proposed := range []string{"urn:uuid:first", "urn:uuid:second"} {
		mock.ExpectQuery("INSERT INTO openai_siwc_installation.*ON CONFLICT.*RETURNING host_id").WithArgs(proposed).WillReturnRows(sqlmock.NewRows([]string{"host_id"}).AddRow("urn:uuid:persisted"))
		host, err := repo.SIWCInstallationID(context.Background(), proposed)
		require.NoError(t, err)
		require.Equal(t, "urn:uuid:persisted", host)
	}
	vendor := int64(8)
	supplier := *a
	supplier.VendorID = &vendor
	_, _, err = repo.GetSIWCRecord(context.Background(), &supplier)
	require.Error(t, err)
	scoped := authz.WithVendorScope(context.Background(), authz.VendorScope{VendorID: 8})
	_, _, err = repo.GetSIWCRecord(scoped, a)
	require.Error(t, err)
	_, err = repo.StoreSIWCRecord(scoped, a, "", "", "new", "grant")
	require.Error(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}
