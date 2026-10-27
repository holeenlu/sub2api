//go:build unit

package repository

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestCloudSettlementArchivePreservesRawFactsAndDetectsConflict(t *testing.T) {
	for _, scenario := range []string{"stored", "database_error", "payload_conflict"} {
		t.Run(scenario, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close() })
			payload := []byte("{malformed-but-must-be-retained")
			digest := sha256.Sum256(payload)
			query := mock.ExpectQuery("INSERT INTO openai_cloud_settlement_archive").WithArgs("root", hex.EncodeToString(digest[:]), "invalid_record", payload)
			switch scenario {
			case "stored":
				query.WillReturnRows(sqlmock.NewRows([]string{"payload"}).AddRow(payload))
			case "database_error":
				query.WillReturnError(errors.New("database unavailable"))
			case "payload_conflict":
				query.WillReturnRows(sqlmock.NewRows([]string{"payload"}).AddRow([]byte("different")))
			}
			err = (&usageBillingRepository{db: db}).StoreCloudSettlementArchive(t.Context(), "root", "invalid_record", payload)
			if scenario == "stored" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
