//go:build integration

package repository

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/billing"
	"github.com/Wei-Shaw/sub2api/internal/gateway/execution"
	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	"github.com/Wei-Shaw/sub2api/internal/service"
	dbmigrations "github.com/Wei-Shaw/sub2api/migrations"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func nativeIntentPGFixture(t *testing.T) (*usageBillingRepository, *service.NativeBillingIntentService, service.NativeUsageIntent, service.NativeUsageEvidenceInput) {
	t.Helper()
	repo, _, evidence := nativePGFixture(t)
	intent := nativeIntentRepositoryInput(t)
	intent.Key = billing.NativeIntentKey{OperationID: evidence.Evidence.OperationID, APIKeyID: evidence.Evidence.Identity.APIKeyID}
	intent.UserID, intent.OriginalWireDigest, intent.Attempt.Prepared = evidence.Evidence.UserID, evidence.Evidence.OriginalWireDigest, *evidence.Evidence.Prepared
	svc, err := service.ProvideNativeBillingIntentService(repo)
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, err := integrationDB.ExecContext(ctx, "DELETE FROM usage_billing_events WHERE api_key_id=$1", intent.Key.APIKeyID)
		require.NoError(t, err)
	})
	return repo, svc, intent, evidence
}

func TestNativeIntentPostgres299PreservesLegacyMoneyAndRejectsMixedShapes(t *testing.T) {
	tx := testTx(t)
	schema := "intent_migration_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	// All DDL and the pre-299 money fixture live in one rolled-back schema.
	_, err := tx.ExecContext(t.Context(), "CREATE SCHEMA "+schema+"; SET LOCAL search_path TO "+schema+"; CREATE TABLE usage_logs(id BIGINT)")
	require.NoError(t, err)
	for _, name := range []string{"281_usage_billing_events.sql", "298_usage_billing_native_evidence.sql"} {
		sql, err := dbmigrations.FS.ReadFile(name)
		require.NoError(t, err)
		_, err = tx.ExecContext(t.Context(), string(sql))
		require.NoError(t, err)
	}
	_, err = tx.ExecContext(t.Context(), "INSERT INTO usage_billing_events(request_id,api_key_id,request_fingerprint,payload,status) VALUES('original-money',2,'original-v1','{\"PayloadVersion\":1,\"BalanceCost\":1.25}','pending')")
	require.NoError(t, err)
	for _, name := range []string{"299_usage_billing_native_intent.sql", "300_usage_billing_native_intent_closure.sql"} {
		sql, err := dbmigrations.FS.ReadFile(name)
		require.NoError(t, err)
		_, err = tx.ExecContext(t.Context(), string(sql))
		require.NoError(t, err)
	}
	var fingerprint, payload string
	require.NoError(t, tx.QueryRowContext(t.Context(), "INSERT INTO usage_billing_events(request_id,api_key_id,request_fingerprint,payload,status) VALUES('original-money',2,'losing-v1','{}','pending') ON CONFLICT(request_id,api_key_id) DO UPDATE SET updated_at=usage_billing_events.updated_at RETURNING request_fingerprint,payload::text").Scan(&fingerprint, &payload))
	require.Equal(t, "original-v1", fingerprint)
	require.JSONEq(t, "{\"PayloadVersion\":1,\"BalanceCost\":1.25}", payload)
	for _, op := range []string{"original-money", "second-operation"} {
		_, err = tx.ExecContext(t.Context(), "INSERT INTO usage_billing_events(event_type,operation_id,api_key_id,request_id,request_fingerprint,payload,status,native_intent,native_intent_fingerprint) VALUES('native_intent',$1,2,NULL,'',NULL,'intent_preserved','{}','scope')", op)
		require.NoError(t, err, "NULL technical identity must not collide with real money")
	}
	for name, query := range map[string]string{
		"technical_payload":     "UPDATE usage_billing_events SET payload='{}' WHERE operation_id='second-operation'",
		"technical_money_id":    "UPDATE usage_billing_events SET request_id='fake-money' WHERE operation_id='second-operation'",
		"technical_money_state": "UPDATE usage_billing_events SET status='applied' WHERE operation_id='second-operation'",
		"money_null_id":         "UPDATE usage_billing_events SET request_id=NULL WHERE request_id='original-money'",
		"close_without_proof":   "UPDATE usage_billing_events SET status='intent_not_sent' WHERE operation_id='second-operation'",
		"missing_canonical":     "UPDATE usage_billing_events SET status='intent_bound',proposed_request_id='absent',canonical_request_id='absent',native_evidence='{}',native_evidence_fingerprint='e' WHERE operation_id='second-operation'",
	} {
		_, err = tx.ExecContext(t.Context(), "SAVEPOINT shape")
		require.NoError(t, err)
		_, err = tx.ExecContext(t.Context(), query)
		require.Error(t, err, name)
		_, err = tx.ExecContext(t.Context(), "ROLLBACK TO SAVEPOINT shape")
		require.NoError(t, err)
	}
}

func TestNativeIntentPostgresConcurrentPreserveAndOriginalRecoveryIsolation(t *testing.T) {
	repo, svc, intent, evidence := nativeIntentPGFixture(t)
	errs := make(chan error, 4)
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			receipt, err := svc.PreserveBeforeSend(t.Context(), intent)
			if err == nil && !receipt.Durable() {
				err = errors.New("missing durable preservation")
			}
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	loaded, err := svc.Load(t.Context(), intent.Key)
	require.NoError(t, err)
	require.Len(t, loaded.Attempts, 1)
	// The money ID may have exactly the same spelling as the technical op.
	command := nativeRepositoryFinal(t, evidence).Settlement
	command.RequestID = intent.Key.OperationID
	command.UsageLog.RequestID = command.RequestID
	command.RequestFingerprint = ""
	require.NoError(t, repo.persistUsageBillingEvent(t.Context(), command))
	n, err := repo.RecoverPendingUsageBilling(t.Context(), 64, nil)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	result, err := repo.Apply(t.Context(), command)
	require.NoError(t, err)
	require.False(t, result.Applied)
	var balance float64
	var status string
	var attempts int
	require.NoError(t, integrationDB.QueryRowContext(t.Context(), "SELECT balance FROM users WHERE id=$1", intent.UserID).Scan(&balance))
	require.Equal(t, 98.75, balance)
	require.NoError(t, integrationDB.QueryRowContext(t.Context(), "SELECT status,attempts FROM usage_billing_events WHERE operation_id=$1 AND api_key_id=$2", intent.Key.OperationID, intent.Key.APIKeyID).Scan(&status, &attempts))
	require.Equal(t, "intent_preserved", status)
	require.Zero(t, attempts)
	ledger := &usageBillingLedger{repo: repo}
	ledger.Retry(t.Context(), billing.Identity{RequestID: intent.Key.OperationID, APIKeyID: intent.Key.APIKeyID}, false, errors.New("late worker"))
	n, err = repo.RecoverPendingUsageBilling(t.Context(), 64, nil)
	require.NoError(t, err)
	require.Zero(t, n)
	_, err = integrationDB.ExecContext(t.Context(), "UPDATE usage_billing_events SET status='pending_usage',native_evidence='{}',native_evidence_fingerprint='actual-evidence' WHERE operation_id=$1 AND api_key_id=$2", intent.Key.OperationID, intent.Key.APIKeyID)
	require.NoError(t, err)
	claimed, err := ledger.Claim(t.Context(), 64)
	require.NoError(t, err)
	require.Empty(t, claimed)
	require.NoError(t, integrationDB.QueryRowContext(t.Context(), "SELECT attempts FROM usage_billing_events WHERE operation_id=$1 AND api_key_id=$2", intent.Key.OperationID, intent.Key.APIKeyID).Scan(&attempts))
	require.Zero(t, attempts)
}

type nativeIntentPGAuthority struct{ revoked bool }

func (a *nativeIntentPGAuthority) RevalidateDispatch(context.Context, model.PreparedIdentity) (execution.DispatchValidation, error) {
	if a.revoked {
		return execution.DispatchValidation{}, execution.ErrDispatchChanged
	}
	return execution.DispatchValidation{Authorization: [32]byte{1}, Leases: [32]byte{2}}, nil
}

type nativeIntentPGPrepared struct {
	intent       service.NativeUsageIntent
	service      *service.NativeBillingIntentService
	authority    *nativeIntentPGAuthority
	cancel       context.CancelFunc
	afterConsume bool
	receipt      billing.NativeIntentReceipt
}

func (p *nativeIntentPGPrepared) Identity() model.PreparedIdentity { return p.intent.Attempt.Prepared }
func (p *nativeIntentPGPrepared) Target() model.AttemptTarget {
	return model.AttemptTarget{Owner: p.Identity().Owner, Mode: "native", Limit: 1}
}
func (*nativeIntentPGPrepared) Close() error { return nil }
func (*nativeIntentPGPrepared) Reprepare(context.Context) (execution.PreparedAttempt, error) {
	return nil, errors.New("unexpected retry")
}
func (p *nativeIntentPGPrepared) Open(ctx context.Context, permit *execution.SendPermit) (model.SendState, execution.PreRequestFailure, error) {
	p.intent.Attempt.Sequence = permit.AttemptSequence()
	var err error
	p.receipt, err = p.service.PreserveBeforeSend(ctx, p.intent)
	if err != nil {
		return model.SendNotSent, execution.PreRequestFailure{}, err
	}
	if p.afterConsume {
		if err = permit.Consume(ctx, p.Identity()); err != nil {
			return model.SendNotSent, execution.PreRequestFailure{}, err
		}
	}
	if p.cancel != nil {
		p.cancel()
	} else {
		p.authority.revoked = true
	}
	if p.afterConsume {
		err = permit.RevalidateConsumed(ctx, p.Identity())
	} else {
		err = permit.Consume(ctx, p.Identity())
	}
	if err == nil {
		return model.SendUnknown, execution.PreRequestFailure{}, errors.New("expected real permit refusal")
	}
	return model.SendNotSent, execution.PreRequestFailure{}, err
}

func TestNativeIntentPostgresRealPermitNotSentClosureAndRetention(t *testing.T) {
	for _, stage := range []string{"revoked_before_consume", "canceled_before_consume", "revoked_before_write", "canceled_before_write"} {
		t.Run(stage, func(t *testing.T) {
			repo, svc, intent, _ := nativeIntentPGFixture(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			authority := &nativeIntentPGAuthority{}
			dispatch, err := execution.NewDispatcher(ctx, intent.Key.OperationID, execution.NewAttemptBudget(execution.AttemptLimits{Total: 2, PerOwner: 2, Preparation: 1, RetryWait: time.Second}), execution.NewFirstOutput(time.Now()), authority)
			require.NoError(t, err)
			op, err := execution.NewOperation(dispatch, model.OperationResult{OperationID: intent.Key.OperationID, APIKeyID: intent.Key.APIKeyID, UserID: intent.UserID, OriginalWireDigest: intent.OriginalWireDigest})
			require.NoError(t, err)
			prepared := &nativeIntentPGPrepared{intent: intent, service: svc, authority: authority, afterConsume: strings.HasSuffix(stage, "write")}
			if strings.HasPrefix(stage, "canceled") {
				prepared.cancel = cancel
			}
			err = op.Execute(ctx, func() execution.CycleResult {
				retries := 0
				_, _, err := op.OpenPrepared(ctx, prepared, &retries, func(context.Context) error { return errors.New("unexpected wait") })
				return execution.CycleResult{Err: err}
			})
			require.Error(t, err)
			require.True(t, prepared.receipt.Durable())
			snapshot := op.Snapshot()
			require.Len(t, snapshot.Attempts, 1)
			require.Equal(t, model.SendNotSent, snapshot.Attempts[0].Send)
			closeInput := service.NativeIntentNotSentInput{Key: intent.Key, IntentFingerprint: prepared.receipt.IntentFingerprint, Operation: snapshot}
			cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer stop()
			receipt, err := svc.CloseNotSent(cleanup, closeInput)
			require.NoError(t, err)
			require.True(t, receipt.Durable())
			again, err := svc.CloseNotSent(cleanup, closeInput)
			require.NoError(t, err)
			require.Equal(t, receipt, again)
			reopened, err := svc.PreserveBeforeSend(cleanup, intent)
			require.ErrorIs(t, err, billing.ErrNativeIntentClosed)
			require.False(t, reopened.Durable())
			var rows int
			require.NoError(t, integrationDB.QueryRowContext(cleanup, "SELECT COUNT(*) FROM usage_logs WHERE api_key_id=$1", intent.Key.APIKeyID).Scan(&rows))
			require.Zero(t, rows)
			n, err := repo.RecoverPendingUsageBilling(cleanup, 64, nil)
			require.NoError(t, err)
			require.Zero(t, n)
			_, err = integrationDB.ExecContext(cleanup, "UPDATE usage_billing_events SET updated_at=NOW()-INTERVAL '31 days' WHERE operation_id=$1 AND api_key_id=$2", intent.Key.OperationID, intent.Key.APIKeyID)
			require.NoError(t, err)
			n, err = repo.RecoverPendingUsageBilling(cleanup, 64, nil)
			require.NoError(t, err)
			require.Zero(t, n)
			require.NoError(t, integrationDB.QueryRowContext(cleanup, "SELECT COUNT(*) FROM usage_billing_events WHERE operation_id=$1 AND api_key_id=$2", intent.Key.OperationID, intent.Key.APIKeyID).Scan(&rows))
			require.Zero(t, rows)
		})
	}
}

func TestNativeIntentPostgresRetentionKeepsUnresolvedAndRecentBindings(t *testing.T) {
	repo, svc, intent, _ := nativeIntentPGFixture(t)
	_, err := svc.PreserveBeforeSend(t.Context(), intent)
	require.NoError(t, err)
	for _, status := range []string{"intent_preserved", "pending_usage", "pending_preparation", "intent_conflict"} {
		evidence, proposal := any(nil), any(nil)
		if status == "pending_preparation" {
			evidence, proposal = "{}", "not-yet-bound"
		}
		_, err = integrationDB.ExecContext(t.Context(), "UPDATE usage_billing_events SET status=$3,native_evidence=$4,native_evidence_fingerprint=CASE WHEN $4::jsonb IS NULL THEN NULL ELSE 'fact' END,proposed_request_id=$5,updated_at=NOW()-INTERVAL '31 days' WHERE operation_id=$1 AND api_key_id=$2", intent.Key.OperationID, intent.Key.APIKeyID, status, evidence, proposal)
		require.NoError(t, err)
		n, err := repo.RecoverPendingUsageBilling(t.Context(), 64, nil)
		require.NoError(t, err)
		require.Zero(t, n)
		var got string
		require.NoError(t, integrationDB.QueryRowContext(t.Context(), "SELECT status FROM usage_billing_events WHERE operation_id=$1 AND api_key_id=$2", intent.Key.OperationID, intent.Key.APIKeyID).Scan(&got))
		require.Equal(t, status, got)
	}
	// Old usage_recorded is ambiguous: no inferred log-only completion.
	_, err = integrationDB.ExecContext(t.Context(), "INSERT INTO usage_billing_events(request_id,api_key_id,request_fingerprint,status,native_evidence,native_evidence_fingerprint,updated_at) VALUES('ambiguous-final',$1,'','usage_recorded','{}','fact',NOW()-INTERVAL '31 days'),('old-money',$1,'v1','applied',NULL,NULL,NOW()-INTERVAL '31 days')", intent.Key.APIKeyID)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(t.Context(), "UPDATE usage_billing_events SET status='intent_bound',proposed_request_id='old-money',canonical_request_id='old-money',native_evidence='{}',native_evidence_fingerprint='fact',updated_at=NOW() WHERE operation_id=$1 AND api_key_id=$2", intent.Key.OperationID, intent.Key.APIKeyID)
	require.NoError(t, err)
	n, err := repo.RecoverPendingUsageBilling(t.Context(), 64, nil)
	require.NoError(t, err)
	require.Zero(t, n)
	var count int
	require.NoError(t, integrationDB.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM usage_billing_events WHERE api_key_id=$1", intent.Key.APIKeyID).Scan(&count))
	require.Equal(t, 3, count)
	_, err = integrationDB.ExecContext(t.Context(), "UPDATE usage_billing_events SET updated_at=NOW()-INTERVAL '31 days' WHERE operation_id=$1 AND api_key_id=$2", intent.Key.OperationID, intent.Key.APIKeyID)
	require.NoError(t, err)
	_, err = repo.RecoverPendingUsageBilling(t.Context(), 64, nil)
	require.NoError(t, err)
	require.NoError(t, integrationDB.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM usage_billing_events WHERE api_key_id=$1", intent.Key.APIKeyID).Scan(&count))
	require.Equal(t, 1, count)
}

func TestNativeIntentPostgresNotSentClosureIncludesUnpreservedAttempt(t *testing.T) {
	repo, svc, intent, _ := nativeIntentPGFixture(t)
	preserved, err := svc.PreserveBeforeSend(t.Context(), intent)
	require.NoError(t, err)
	unsaved := intent.Attempt.Prepared
	unsaved.TargetDigest = [32]byte{42}
	input := service.NativeIntentNotSentInput{Key: intent.Key, IntentFingerprint: preserved.IntentFingerprint,
		Operation: model.OperationResult{OperationID: intent.Key.OperationID, UserID: intent.UserID,
			APIKeyID: intent.Key.APIKeyID, OriginalWireDigest: intent.OriginalWireDigest, State: model.OperationFailed,
			Attempts: []model.AttemptFact{
				{Sequence: intent.Attempt.Sequence, Identity: intent.Attempt.Prepared, Send: model.SendNotSent},
				{Sequence: intent.Attempt.Sequence + 1, Identity: unsaved, Send: model.SendNotSent},
			}}}
	closed, err := svc.CloseNotSent(t.Context(), input)
	require.NoError(t, err)
	require.True(t, closed.Durable())
	var scopeCount, closureCount, logs int
	require.NoError(t, integrationDB.QueryRowContext(t.Context(),
		"SELECT jsonb_array_length(native_intent->'Attempts'), jsonb_array_length(native_intent_closure->'Attempts') FROM usage_billing_events WHERE operation_id=$1 AND api_key_id=$2",
		intent.Key.OperationID, intent.Key.APIKeyID).Scan(&scopeCount, &closureCount))
	require.Equal(t, 1, scopeCount)
	require.Equal(t, 2, closureCount)
	require.NoError(t, integrationDB.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM usage_logs WHERE api_key_id=$1", intent.Key.APIKeyID).Scan(&logs))
	require.Zero(t, logs)
	n, err := repo.RecoverPendingUsageBilling(t.Context(), 64, nil)
	require.NoError(t, err)
	require.Zero(t, n)
}
