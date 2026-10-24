package repository

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestSchedulerPreferenceMigrationEffectiveValues(t *testing.T) {
	for _, tc := range []struct {
		name, enabled, subscription, cost, legacy string
		yaml                                      float64
		wantCost, wantSubscription                string
	}{
		{name: "default disabled does not activate stored subscription", subscription: "true", legacy: "true", wantSubscription: "false"},
		{name: "disabled gate preserves legacy cost", enabled: "false", legacy: "true", cost: "2", yaml: 3, wantSubscription: "false"},
		{name: "enabled uses stored weight", enabled: "true", cost: ".5", wantCost: "true"},
		{name: "enabled explicit zero overrides YAML", enabled: "true", cost: "0", yaml: 3, wantCost: "false"},
		{name: "enabled missing override uses YAML", enabled: "true", yaml: 3, wantCost: "true"},
		{name: "invalid override uses YAML", enabled: "true", cost: "invalid", yaml: 3, wantCost: "true"},
		{name: "NaN override uses YAML", enabled: "true", cost: "NaN", yaml: 3, wantCost: "true"},
		{name: "inactive legacy toggle cannot turn on cost", enabled: "true", cost: "0", legacy: "true", wantCost: "false"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			old := map[string]string{"openai_advanced_scheduler_enabled": tc.enabled, "openai_advanced_scheduler_subscription_priority_enabled": tc.subscription, "openai_advanced_scheduler_weight_upstream_cost": tc.cost, "openai_low_upstream_rate_priority_enabled": tc.legacy}
			updates := schedulerPreferenceMigrationValues(old, map[string]float64{"upstream_cost": tc.yaml})
			require.Equal(t, tc.wantCost, updates["openai_low_upstream_rate_priority_enabled"])
			require.Equal(t, tc.wantSubscription, updates["openai_advanced_scheduler_subscription_priority_enabled"])
			require.Equal(t, tc.legacy, old["openai_low_upstream_rate_priority_enabled"], "backup remains unchanged")
		})
	}
	require.Empty(t, schedulerPreferenceMigrationValues(map[string]string{}, nil), "new installation has no old intent")
}

func TestSchedulerPreferenceMigrationCompletionAndRollback(t *testing.T) {
	for _, tc := range []struct {
		name           string
		complete, fail bool
	}{{name: "new migration"}, {name: "already completed", complete: true}, {name: "failed update rolls back", fail: true}} {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer func() { _ = db.Close() }()
			old := map[string]string{"openai_advanced_scheduler_enabled": "true", "openai_advanced_scheduler_weight_upstream_cost": "0.5"}
			if tc.complete {
				old["_migration_completed"] = "true"
			}
			raw, err := json.Marshal(old)
			require.NoError(t, err)
			mock.ExpectBegin()
			mock.ExpectQuery("SELECT value FROM settings WHERE key=.+ FOR UPDATE").WithArgs(schedulerPreferencesBackupKey).WillReturnRows(sqlmock.NewRows([]string{"value"}).AddRow(string(raw)))
			if tc.complete {
				mock.ExpectCommit()
			} else {
				write := mock.ExpectExec("INSERT INTO settings").WithArgs("openai_low_upstream_rate_priority_enabled", "true")
				if tc.fail {
					write.WillReturnError(errors.New("db unavailable"))
					mock.ExpectRollback()
				} else {
					write.WillReturnResult(sqlmock.NewResult(0, 1))
					mock.ExpectExec("UPDATE settings SET value").WithArgs(sqlmock.AnyArg(), schedulerPreferencesBackupKey).WillReturnResult(sqlmock.NewResult(0, 1))
					mock.ExpectCommit()
				}
			}
			err = migrateSchedulerBusinessPreferences(context.Background(), db, nil)
			if tc.fail {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestSchedulerMigrationInvalidWholeVectorFallsBackAndMissingBackupFailsClosed(t *testing.T) {
	old := map[string]string{"openai_advanced_scheduler_enabled": "true"}
	for _, key := range []string{"priority", "load", "queue", "error_rate", "ttft", "reset", "quota_headroom", "upstream_cost"} {
		old["openai_advanced_scheduler_weight_"+key] = "0"
	}
	values := schedulerPreferenceMigrationValues(old, map[string]float64{"upstream_cost": 0.8})
	require.Equal(t, "true", values["openai_low_upstream_rate_priority_enabled"], "zero whole base vector must fall back to full YAML")
	old["openai_advanced_scheduler_weight_priority"] = "1e308"
	old["openai_advanced_scheduler_weight_load"] = "1e308"
	require.Equal(t, "true", schedulerPreferenceMigrationValues(old, map[string]float64{"upstream_cost": 0.8})["openai_low_upstream_rate_priority_enabled"], "overflow rejects the entire override vector")
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT value FROM settings WHERE key=.+ FOR UPDATE").WillReturnRows(sqlmock.NewRows([]string{"value"}))
	mock.ExpectRollback()
	err = migrateSchedulerBusinessPreferences(context.Background(), db, nil)
	require.ErrorContains(t, err, "backup is missing")
	require.NoError(t, mock.ExpectationsWereMet())
}
