package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

const schedulerPreferencesBackupKey = "migration_275_scheduler_preferences_backup"

// SQL establishes the durable backup. Finalize it before serving traffic: only
// this startup boundary can combine the old DB overrides with deployment YAML.
// The row lock serializes replicas; the completion marker prevents subsequent
// starts from overriding an administrator's new business preferences.
func migrateSchedulerBusinessPreferences(ctx context.Context, db *sql.DB, yamlWeights map[string]float64) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var raw string
	if err := tx.QueryRowContext(ctx, "SELECT value FROM settings WHERE key=$1 FOR UPDATE", schedulerPreferencesBackupKey).Scan(&raw); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("scheduler migration 275 backup is missing; restore its verified backup before startup: %w", err)
		}
		return err
	}
	old := map[string]string{}
	if err := json.Unmarshal([]byte(raw), &old); err != nil {
		return fmt.Errorf("decode scheduler preference backup: %w", err)
	}
	if old["_migration_completed"] == "true" {
		return tx.Commit()
	}
	for key, value := range schedulerPreferenceMigrationValues(old, yamlWeights) {
		if _, err := tx.ExecContext(ctx, "INSERT INTO settings (key,value) VALUES ($1,$2) ON CONFLICT (key) DO UPDATE SET value=EXCLUDED.value,updated_at=NOW()", key, value); err != nil {
			return err
		}
	}
	old["_migration_completed"] = "true"
	old["_yaml_upstream_cost_weight"] = strconv.FormatFloat(yamlWeights["upstream_cost"], 'g', -1, 64)
	backup, err := json.Marshal(old)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE settings SET value=$1,updated_at=NOW() WHERE key=$2", string(backup), schedulerPreferencesBackupKey); err != nil {
		return err
	}
	return tx.Commit()
}

func schedulerPreferenceMigrationValues(old map[string]string, yamlWeights map[string]float64) map[string]string {
	updates := map[string]string{}
	enabled := strings.EqualFold(strings.TrimSpace(old["openai_advanced_scheduler_enabled"]), "true")
	subscription := "openai_advanced_scheduler_subscription_priority_enabled"
	if !enabled {
		if _, exists := old[subscription]; exists {
			updates[subscription] = "false"
		}
		// Legacy mode already consumed the low-cost preference directly.
		return updates
	}
	weights := config.SchedulerMigrationDefaultWeights()
	for key, value := range yamlWeights {
		weights[key] = value
	}
	fallback := weights["upstream_cost"]
	for key := range weights {
		if parsed, err := strconv.ParseFloat(strings.TrimSpace(old["openai_advanced_scheduler_weight_"+key]), 64); err == nil && parsed >= 0 && !math.IsNaN(parsed) && !math.IsInf(parsed, 0) {
			weights[key] = parsed
		}
	}
	base, total := 0.0, 0.0
	valid := true
	for key, value := range weights {
		if value < 0 || math.IsNaN(value) || math.IsInf(value, 0) {
			valid = false
		}
		total += value
		if key != "previous_response" && key != "session_sticky" {
			base += value
		}
	}
	valid = valid && base > 0 && !math.IsInf(base, 0) && !math.IsNaN(base) && !math.IsInf(total, 0) && !math.IsNaN(total)
	weight := weights["upstream_cost"]
	if !valid {
		weight = fallback
	}
	updates["openai_low_upstream_rate_priority_enabled"] = strconv.FormatBool(weight > 0 && !math.IsNaN(weight) && !math.IsInf(weight, 0))
	return updates
}
