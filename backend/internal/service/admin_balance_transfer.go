package service

import (
	"context"
	"fmt"
	"math"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/authz"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/shopspring/decimal"
)

// transferUserBalance moves existing funds atomically with both ledger entries.
func (s *adminServiceImpl) transferUserBalance(ctx context.Context, userID int64, amount float64, operation, notes string) (*User, error) {
	actor, ok := authz.FromContext(ctx)
	if !ok || !actor.ManagementRequest || actor.Role != RoleAdmin || !actor.Can("billing.balance.adjust") || actor.UserID == userID {
		return nil, ErrAdminPermissionDenied
	}
	if math.IsNaN(amount) || math.IsInf(amount, 0) || amount <= 0 || (operation != "add" && operation != "subtract") {
		return nil, infraerrors.BadRequest("INVALID_BALANCE_TRANSFER", "Use a positive amount and add or subtract")
	}
	money := decimal.NewFromFloat(amount).Round(8)
	if !money.IsPositive() {
		return nil, infraerrors.BadRequest("INVALID_BALANCE_TRANSFER", "Amount is below balance precision")
	}
	if s.entClient == nil || s.redeemCodeRepo == nil {
		return nil, ErrAdminPolicyUnavailable
	}
	return WithManagementWrite(ctx, s.entClient, []int64{userID}, func(ctx context.Context) (*User, error) {
		tx := dbent.TxFromContext(ctx)
		if tx == nil {
			return nil, ErrAdminPolicyUnavailable
		}
		client := tx.Client()
		// The management guard locks actor and target in ID order and checks ownership.
		target, err := client.User.Get(ctx, userID)
		if err != nil || target.Role != RoleUser || target.CreatedBy == nil || *target.CreatedBy != actor.UserID {
			return nil, ErrAdminPermissionDenied
		}
		sourceID, destinationID := actor.UserID, userID
		if operation == "subtract" {
			sourceID, destinationID = userID, actor.UserID
		}
		result, err := client.ExecContext(ctx, `UPDATE users SET balance=balance-$1::numeric,updated_at=NOW() WHERE id=$2 AND deleted_at IS NULL AND balance >= $1::numeric`, money.String(), sourceID)
		if err != nil {
			return nil, err
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return nil, err
		}
		if affected != 1 {
			return nil, infraerrors.BadRequest("INSUFFICIENT_TRANSFER_BALANCE", "The funding account has insufficient balance")
		}
		result, err = client.ExecContext(ctx, `UPDATE users SET balance=balance+$1::numeric,updated_at=NOW() WHERE id=$2 AND deleted_at IS NULL`, money.String(), destinationID)
		if err != nil {
			return nil, err
		}
		affected, err = result.RowsAffected()
		if err != nil {
			return nil, err
		}
		if affected != 1 {
			return nil, ErrUserNotFound
		}
		reference, err := GenerateRedeemCode()
		if err != nil {
			return nil, err
		}
		value, _ := money.Float64()
		now := time.Now()
		for i, uid := range []int64{sourceID, destinationID} {
			delta := -value
			if i == 1 {
				delta = value
			}
			record := &RedeemCode{Code: fmt.Sprintf("%s-%d", reference[:30], i), Type: AdjustmentTypeAdminBalance, Value: delta, Status: StatusUsed, UsedBy: &uid, UsedAt: &now, Notes: fmt.Sprintf("Transfer #%d → #%d; %s", sourceID, destinationID, notes)}
			if err := s.redeemCodeRepo.Create(ctx, record); err != nil {
				return nil, err
			}
		}
		if err := RecordManagementUserChange(ctx, client, userID, "admin.users.balance.transfer", target.Role, target.Role); err != nil {
			return nil, err
		}
		invalidate := func(committedCtx context.Context) {
			for _, id := range []int64{actor.UserID, userID} {
				if s.authCacheInvalidator != nil {
					s.authCacheInvalidator.InvalidateAuthCacheByUserID(committedCtx, id)
				}
				if s.billingCacheService != nil {
					_ = s.billingCacheService.InvalidateUserBalance(committedCtx, id)
				}
			}
		}
		if !DeferManagementCommit(ctx, invalidate) {
			return nil, ErrAdminPolicyUnavailable
		}
		return s.userRepo.GetByID(ctx, userID)
	})
}
