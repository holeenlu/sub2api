package repository

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

var _ service.CodexTicketStoreRepository = (*accountRepository)(nil)

// StoreCodexTicketIfUnchanged keeps a completed probe from replacing a ticket
// after reauthorization, deactivation, a participation change or a newer probe.
// The row update and scheduler outbox insert commit together. Updating only one
// JSONB key also preserves unrelated settings and concurrent other-model tickets.
func (r *accountRepository) StoreCodexTicketIfUnchanged(
	ctx context.Context,
	expectedAccount *service.Account,
	model string,
	ticket json.RawMessage,
) (bool, error) {
	if r == nil || r.sql == nil {
		return false, errors.New("account repository SQL executor is not configured")
	}
	model = strings.TrimSpace(model)
	if expectedAccount == nil || expectedAccount.ID <= 0 || model == "" {
		return false, errors.New("invalid account or model for Codex ticket storage")
	}
	if !expectedAccount.IsOpenAIOAuthLike() || expectedAccount.IsShadow() || expectedAccount.Status != service.StatusActive {
		return false, nil
	}
	var document map[string]json.RawMessage
	if err := json.Unmarshal(ticket, &document); err != nil || document == nil {
		return false, errors.New("codex ticket must be a JSON object")
	}
	var generation string
	if err := json.Unmarshal(document["generation_id"], &generation); err != nil || generation == "" {
		return false, errors.New("codex ticket generation is required")
	}
	expectedCredentials, err := json.Marshal(normalizeJSONMap(expectedAccount.Credentials))
	if err != nil {
		return false, err
	}
	key := "codex_turn_ticket:" + model
	expectedTicket, err := json.Marshal(expectedAccount.Extra[key])
	if err != nil {
		return false, err
	}
	result, err := r.sql.ExecContext(ctx, `
		WITH updated AS (
			UPDATE accounts AS a
			SET extra = COALESCE(a.extra, '{}'::jsonb) || jsonb_build_object($5::text, $6::jsonb),
				updated_at = NOW()
			WHERE a.id = $1
				AND a.deleted_at IS NULL
				AND a.parent_account_id IS NULL
				AND a.platform = $2
				AND a.type = $3
				AND a.status = $4
				AND a.credentials = $7::jsonb
				AND COALESCE(jsonb_typeof(a.extra), 'object') = 'object'
				AND (a.extra->'codex_ticket_harvest_enabled') IS DISTINCT FROM 'false'::jsonb
				AND (a.extra->'codex_ticket_harvest_models'->$8::text) IS DISTINCT FROM 'false'::jsonb
				AND COALESCE(a.extra->$5::text->>'generation_id', '') = COALESCE($9::jsonb->>'generation_id', '')
				AND (CASE
					WHEN jsonb_typeof(a.extra->'openai_passthrough') = 'boolean'
						THEN a.extra->'openai_passthrough' = 'true'::jsonb
					WHEN jsonb_typeof(a.extra->'openai_oauth_passthrough') = 'boolean'
						THEN a.extra->'openai_oauth_passthrough' = 'true'::jsonb
					ELSE FALSE
				END) = $10::boolean
			RETURNING a.id
		)
		INSERT INTO scheduler_outbox (event_type, account_id, group_id, payload)
		SELECT $11, updated.id, NULL, NULL FROM updated
	`,
		expectedAccount.ID,
		service.PlatformOpenAI,
		expectedAccount.Type,
		service.StatusActive,
		key,
		string(ticket),
		string(expectedCredentials),
		model,
		string(expectedTicket),
		expectedAccount.IsOpenAIPassthroughEnabled(),
		service.SchedulerOutboxEventAccountChanged,
	)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	if err != nil || affected == 0 {
		return false, err
	}
	r.syncSchedulerAccountSnapshotDetached(ctx, expectedAccount.ID)
	return true, nil
}
