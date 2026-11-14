package service

import (
	"context"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/gateway/execution"
)

// CloudExecutionClaim binds one command to the actual socket owner. Tokens
// remain internal; neither this value nor a saved deadline is a live proof.
type CloudExecutionClaim struct {
	SessionID, OwnerToken, CommandID, OperationID string
	TransferDigest                                [32]byte
}

// OpenAICloudExecutionStore extends the existing Cloud Redis state. Every
// transition is atomic with owner/cancel checks. Returning facts is allowed
// after terminal Save retires the lease; reclaiming execution is not allowed
// after cancellation or an unconfirmed owner return.
type OpenAICloudExecutionStore interface {
	OfferCloudExecution(context.Context, string, string, string, [32]byte) error
	ClaimCloudExecution(context.Context, CloudExecutionClaim) (time.Duration, error)
	CheckCloudExecution(context.Context, CloudExecutionClaim) (time.Duration, error)
	SaveCloudExecutionReturn(context.Context, CloudExecutionClaim, execution.OperationReturn) error
	LoadCloudExecutionReturn(context.Context, string, string, [32]byte) (*execution.OperationReturn, error)
	ClaimCloudExecutionReturn(context.Context, string, string, [32]byte, [32]byte) error
}
