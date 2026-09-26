package service

import (
	"context"
	"encoding/json"
)

// CodexTicketStoreRepository conditionally persists a verified ticket against
// the account snapshot that started the probe. A false result means credentials,
// participation, account eligibility or the previous ticket generation changed;
// callers must discard the probe result instead of updating their local cache.
type CodexTicketStoreRepository interface {
	StoreCodexTicketIfUnchanged(context.Context, *Account, string, json.RawMessage) (bool, error)
}
