package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/authz"
	"github.com/lib/pq"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/authidentity"
	"github.com/Wei-Shaw/sub2api/ent/authidentitychannel"
	"github.com/Wei-Shaw/sub2api/ent/pendingauthsession"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
)

// User management implementations
func (s *adminServiceImpl) ListUsers(ctx context.Context, page, pageSize int, filters UserListFilters, sortBy, sortOrder string) ([]User, int64, error) {
	params := pagination.PaginationParams{Page: page, PageSize: pageSize, SortBy: sortBy, SortOrder: sortOrder}
	users, result, err := s.userRepo.ListWithFilters(ctx, params, filters)
	if err != nil {
		return nil, 0, err
	}
	if len(users) > 0 {
		userIDs := make([]int64, 0, len(users))
		for i := range users {
			userIDs = append(userIDs, users[i].ID)
		}
		lastUsedByUserID, latestErr := s.userRepo.GetLatestUsedAtByUserIDs(ctx, userIDs)
		if latestErr != nil {
			logger.LegacyPrintf("service.admin", "failed to load user last_used_at in batch: err=%v", latestErr)
		} else {
			for i := range users {
				users[i].LastUsedAt = lastUsedByUserID[users[i].ID]
			}
		}
	}
	// 批量加载用户专属分组倍率
	if s.userGroupRateRepo != nil && len(users) > 0 {
		if batchRepo, ok := s.userGroupRateRepo.(userGroupRateBatchReader); ok {
			userIDs := make([]int64, 0, len(users))
			for i := range users {
				userIDs = append(userIDs, users[i].ID)
			}
			ratesByUser, err := batchRepo.GetByUserIDs(ctx, userIDs)
			if err != nil {
				logger.LegacyPrintf("service.admin", "failed to load user group rates in batch: err=%v", err)
				s.loadUserGroupRatesOneByOne(ctx, users)
			} else {
				for i := range users {
					if rates, ok := ratesByUser[users[i].ID]; ok {
						users[i].GroupRates = rates
					}
				}
			}
		} else {
			s.loadUserGroupRatesOneByOne(ctx, users)
		}
	}
	s.loadUserCreators(ctx, users)
	return users, result.Total, nil
}

// Load creator display names in one query. Ownership is persisted when the user is created.
func (s *adminServiceImpl) loadUserCreators(ctx context.Context, users []User) {
	if s.entClient == nil || len(users) == 0 {
		return
	}
	ids := make([]string, len(users))
	for i := range users {
		ids[i] = strconv.FormatInt(users[i].ID, 10)
	}
	rows, err := s.entClient.QueryContext(ctx, `SELECT target.id::text,target.created_by,COALESCE(creator.username,'') FROM users target LEFT JOIN users creator ON creator.id=target.created_by WHERE target.id::text=ANY($1) AND target.created_by IS NOT NULL`, pq.Array(ids))
	if err != nil {
		logger.LegacyPrintf("service.admin", "creator provenance lookup failed: %v", err)
		return
	}
	defer rows.Close()
	creators := map[int64]struct {
		id   int64
		name string
	}{}
	for rows.Next() {
		var id string
		var creator int64
		var name string
		if rows.Scan(&id, &creator, &name) != nil {
			return
		}
		uid, _ := strconv.ParseInt(id, 10, 64)
		creators[uid] = struct {
			id   int64
			name string
		}{creator, strings.TrimSpace(name)}
	}
	if rows.Err() != nil {
		return
	}
	for i := range users {
		if creator, ok := creators[users[i].ID]; ok {
			users[i].CreatedBy = &creator.id
			users[i].CreatedByName = creator.name
		}
	}
}

func (s *adminServiceImpl) loadUserGroupRatesOneByOne(ctx context.Context, users []User) {
	if s.userGroupRateRepo == nil {
		return
	}
	for i := range users {
		rates, err := s.userGroupRateRepo.GetByUserID(ctx, users[i].ID)
		if err != nil {
			logger.LegacyPrintf("service.admin", "failed to load user group rates: user_id=%d err=%v", users[i].ID, err)
			continue
		}
		users[i].GroupRates = rates
	}
}

func (s *adminServiceImpl) GetUser(ctx context.Context, id int64) (*User, error) {
	user, err := s.userRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := guardManagedUser(ctx, user, "", false); err != nil {
		return nil, err
	}
	lastUsedAt, latestErr := s.userRepo.GetLatestUsedAtByUserID(ctx, id)
	if latestErr != nil {
		logger.LegacyPrintf("service.admin", "failed to load user last_used_at: user_id=%d err=%v", id, latestErr)
	} else {
		user.LastUsedAt = lastUsedAt
	}
	// 加载用户专属分组倍率
	if s.userGroupRateRepo != nil {
		rates, err := s.userGroupRateRepo.GetByUserID(ctx, id)
		if err != nil {
			logger.LegacyPrintf("service.admin", "failed to load user group rates: user_id=%d err=%v", id, err)
		} else {
			user.GroupRates = rates
		}
	}
	return user, nil
}

func (s *adminServiceImpl) GetUserIncludeDeleted(ctx context.Context, id int64) (*User, error) {
	user, err := s.userRepo.GetByIDIncludeDeleted(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := guardManagedUser(ctx, user, "", false); err != nil {
		return nil, err
	}
	return user, nil
}

// normalizeUserRole 校验并归一化角色输入。
// 空字符串返回 fallback(未提供时的默认角色);供应商只能保留原角色。
func normalizeUserRole(role, fallback string) (string, error) {
	if role == "" {
		return fallback, nil
	}
	if !authz.ValidRole(role) || (role != fallback && (authz.IsVendorRole(role) || authz.IsVendorRole(fallback))) {
		return "", infraerrors.BadRequest("INVALID_USER_ROLE", fmt.Sprintf("invalid role: %q", role))
	}
	return role, nil
}

func (s *adminServiceImpl) CreateUser(ctx context.Context, input *CreateUserInput) (*User, error) {
	ctx = withPersonnelAction(ctx, "create")
	return WithManagementWrite(ctx, s.entClient, nil, func(ctx context.Context) (*User, error) {
		balance := 0.0
		if input.Balance != nil {
			balance = *input.Balance
		} else if s.settingService != nil {
			if actor, ok := authz.FromContext(ctx); !ok || actor.Role != authz.Admin {
				balance = s.settingService.GetDefaultBalance(ctx)
			}
		}

		// 角色可由管理员在创建时指定(admin/user);未提供时默认 user。
		role, err := normalizeUserRole(input.Role, RoleUser)
		if err != nil {
			return nil, err
		}
		if actor, ok := authz.FromContext(ctx); ok && actor.Role == RoleAdmin && (role != RoleUser || !actor.Can("users.create")) {
			return nil, ErrAdminPermissionDenied
		}

		user := &User{
			Email:         input.Email,
			Username:      input.Username,
			Notes:         input.Notes,
			Role:          role,
			Balance:       balance,
			Concurrency:   input.Concurrency,
			RPMLimit:      input.RPMLimit,
			Status:        StatusActive,
			AllowedGroups: input.AllowedGroups,

			RestrictPublicGroups: input.RestrictPublicGroups,
		}
		initialBalance := balance
		if actor, ok := authz.FromContext(ctx); ok {
			creator := actor.UserID
			user.CreatedBy = &creator
			if actor.Role == RoleAdmin {
				user.Balance = 0
			}
		}
		if role == RoleSuperAdmin {
			user.Balance = 0
		}
		if err := user.SetPassword(input.Password); err != nil {
			return nil, err
		}
		if err := s.userRepo.Create(ctx, user); err != nil {
			return nil, err
		}
		if actor, ok := authz.FromContext(ctx); ok && actor.Role == RoleAdmin && initialBalance != 0 {
			return s.transferUserBalance(ctx, user.ID, initialBalance, "add", "Initial user funding")
		}
		// 创建管理员属权限敏感操作，落审计日志（含操作者），便于事后追溯。
		if user.Role == RoleAdmin {
			logger.LegacyPrintf("service.admin", "audit: admin user created actor_admin_id=%d target_user_id=%d",
				input.ActorAdminID, user.ID)
		}
		if actor, ok := authz.FromContext(ctx); !ok || actor.Role != authz.Admin {
			s.assignDefaultSubscriptions(ctx, user.ID)
		}
		return user, nil
	})
}

func (s *adminServiceImpl) assignDefaultSubscriptions(ctx context.Context, userID int64) {
	if s.settingService == nil || s.defaultSubAssigner == nil || userID <= 0 {
		return
	}
	items := s.settingService.GetDefaultSubscriptions(ctx)
	for _, item := range items {
		if _, _, err := s.defaultSubAssigner.AssignOrExtendSubscription(ctx, &AssignSubscriptionInput{
			UserID:       userID,
			GroupID:      item.GroupID,
			ValidityDays: item.ValidityDays,
			Notes:        "auto assigned by default user subscriptions setting",
		}); err != nil {
			logger.LegacyPrintf("service.admin", "failed to assign default subscription: user_id=%d group_id=%d err=%v", userID, item.GroupID, err)
		}
	}
}

func (s *adminServiceImpl) UpdateUser(ctx context.Context, id int64, input *UpdateUserInput) (*User, error) {
	if input.Balance != nil {
		return nil, infraerrors.BadRequest("BALANCE_ENDPOINT_REQUIRED", "Use the balance adjustment action")
	}
	ctx = withPersonnelAction(ctx, "update")
	var previousEmail string
	var notifySecurity bool
	updatedUser, updateErr := WithManagementWrite(ctx, s.entClient, []int64{id}, func(ctx context.Context) (*User, error) {
		// 校验用户专属分组倍率：必须 > 0（nil 合法，表示清除专属倍率）
		if input.GroupRates != nil {
			for groupID, rate := range input.GroupRates {
				if rate != nil && *rate <= 0 {
					return nil, fmt.Errorf("rate_multiplier must be > 0 (group_id=%d)", groupID)
				}
			}
		}

		user, err := s.userRepo.GetByID(ctx, id)
		if err != nil {
			return nil, err
		}

		if err := guardManagedUser(ctx, user, input.Role, input.Status == StatusDisabled); err != nil {
			return nil, err
		}

		if actor, ok := authz.FromContext(ctx); ok && actor.UserID == id && (input.ResetTOTP || input.Password != "" || (input.Email != "" && input.Email != user.Email)) {
			return nil, ErrAdminPermissionDenied
		}
		if actor, ok := authz.FromContext(ctx); ok && actor.Role == authz.Admin && (input.ResetTOTP || input.Password != "" || (input.Email != "" && input.Email != user.Email)) && !actor.Can(authz.PersonnelPermission("security", user.Role, "")) {
			return nil, ErrAdminPermissionDenied
		}
		notifySecurity = input.ResetTOTP || input.Password != "" || (input.Email != "" && input.Email != user.Email) || (input.Role != "" && input.Role != user.Role) || (input.Status != "" && input.Status != user.Status)
		if notifySecurity {
			previousEmail = s.verifiedManagementEmail(ctx, user.ID, user.Email)
		}
		oldConcurrency := user.Concurrency
		oldStatus := user.Status
		oldRole := user.Role
		oldRPMLimit := user.RPMLimit
		oldAllowedGroups := append([]int64(nil), user.AllowedGroups...)

		// fields 与下面的 input.X 判空条件一一对应：管理员没提交的列不写回，
		// 避免这份快照回滚并发的扣费、状态变更或批量限额调整。
		var fields UserUpdateFields

		if input.Email != "" {
			user.Email = input.Email
			fields.Email = true
		}
		if input.Password != "" {
			if err := user.SetPassword(input.Password); err != nil {
				return nil, err
			}
			fields.PasswordHash = true
		}

		if input.Username != nil {
			user.Username = *input.Username
			fields.Username = true
		}
		if input.Notes != nil {
			user.Notes = *input.Notes
			fields.Notes = true
		}

		if input.Status != "" {
			user.Status = input.Status
			fields.Status = true
		}

		// 空字符串表示不修改；供应商角色允许原值回传，但不能通过用户管理转换身份或层级。
		if input.Role != "" {
			role, err := normalizeUserRole(input.Role, user.Role)
			if err != nil {
				return nil, err
			}

			user.Role = role
			fields.Role = true
		}

		if input.Concurrency != nil {
			user.Concurrency = *input.Concurrency
			fields.Concurrency = true
		}

		if input.RPMLimit != nil {
			user.RPMLimit = *input.RPMLimit
			fields.RPMLimit = true
		}

		if input.AllowedGroups != nil {
			user.AllowedGroups = *input.AllowedGroups
			fields.AllowedGroups = true
		}

		oldRestrictPublicGroups := user.RestrictPublicGroups
		if input.RestrictPublicGroups != nil {
			user.RestrictPublicGroups = *input.RestrictPublicGroups
			fields.RestrictPublicGroups = true
		}

		if err := s.userRepo.Update(ctx, user, fields); err != nil {
			return nil, err
		}

		if input.ResetTOTP {
			if err := s.userRepo.DisableTotp(ctx, id); err != nil {
				return nil, err
			}
			if s.entClient != nil {
				tx := dbent.TxFromContext(ctx)
				if tx == nil {
					return nil, ErrAdminPolicyUnavailable
				}
				if err := tx.PendingAuthSession.Update().Where(pendingauthsession.TargetUserIDEQ(id), pendingauthsession.ConsumedAtIsNil()).SetConsumedAt(time.Now()).Exec(ctx); err != nil {
					return nil, err
				}
				if err := RecordManagementUserChange(ctx, tx.Client(), id, "admin.user.mfa.reset", oldRole, user.Role); err != nil {
					return nil, err
				}
			}
			user.SessionGeneration++
			user.TokenVersion = CredentialVersion(user)
			user.TotpEnabled = false
			user.TotpEnabledAt = nil
			user.TotpSecretEncrypted = nil
		}
		// 角色变更属权限敏感操作，落审计日志（含操作者），便于事后追溯。
		if user.Role != oldRole {
			logger.LegacyPrintf("service.admin", "audit: user role changed actor_admin_id=%d target_user_id=%d old_role=%s new_role=%s",
				input.ActorAdminID, user.ID, oldRole, user.Role)
		}

		// 同步用户专属分组倍率
		if input.GroupRates != nil && s.userGroupRateRepo != nil {
			if err := s.userGroupRateRepo.SyncUserGroupRates(ctx, user.ID, input.GroupRates); err != nil {
				return nil, fmt.Errorf("sync user group rates: %w", err)
			}
		}

		if s.authCacheInvalidator != nil {
			// RPMLimit 直接参与 billing_cache_service.checkRPM 的三级级联，
			// allowed_groups 参与 API Key 专属分组授权判断；不失效缓存会让修改在一个 L2 TTL 内失去效果。
			if user.Concurrency != oldConcurrency || user.Status != oldStatus || user.Role != oldRole || user.RPMLimit != oldRPMLimit || user.RestrictPublicGroups != oldRestrictPublicGroups || !sameInt64Set(user.AllowedGroups, oldAllowedGroups) {
				s.authCacheInvalidator.InvalidateAuthCacheByUserID(ctx, user.ID)
			}
		}

		concurrencyDiff := user.Concurrency - oldConcurrency
		if concurrencyDiff != 0 {
			code, err := GenerateRedeemCode()
			if err != nil {
				logger.LegacyPrintf("service.admin", "failed to generate adjustment redeem code: %v", err)
				return user, nil
			}
			adjustmentRecord := &RedeemCode{
				Code:   code,
				Type:   AdjustmentTypeAdminConcurrency,
				Value:  float64(concurrencyDiff),
				Status: StatusUsed,
				UsedBy: &user.ID,
			}
			now := time.Now()
			adjustmentRecord.UsedAt = &now
			if err := s.redeemCodeRepo.Create(ctx, adjustmentRecord); err != nil {
				logger.LegacyPrintf("service.admin", "failed to create concurrency adjustment redeem code: %v", err)
			}
		}

		return user, nil
	})
	if updateErr == nil && notifySecurity {
		s.notifyManagementSecurityChange(ctx, id, previousEmail)
	}
	return updatedUser, updateErr

}

func sameInt64Set(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	if len(a) == 0 {
		return true
	}
	counts := make(map[int64]int, len(a))
	for _, v := range a {
		counts[v]++
	}
	for _, v := range b {
		if counts[v] == 0 {
			return false
		}
		counts[v]--
	}
	return true
}

func (s *adminServiceImpl) DeleteUser(ctx context.Context, id int64) error {
	ctx = withPersonnelAction(ctx, "delete")
	// Peer administrators are manageable; the super-admin and self guards still apply.
	user, err := s.userRepo.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if err := guardManagedUser(ctx, user, "", true); err != nil {
		return err
	}

	apiKeys, err := s.listUserAPIKeysForDeletion(ctx, id)
	if err != nil {
		return err
	}

	if s.entClient != nil {
		tx, err := s.entClient.Tx(ctx)
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback() }()

		opCtx := dbent.NewTxContext(ctx, tx)
		if err := authz.LockManagementWrite(opCtx, tx.Client(), []int64{id}, "", "", true); err != nil {
			return err
		}
		if err := s.deleteUserWithAPIKeys(opCtx, id, apiKeys); err != nil {
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	} else {
		if err := s.deleteUserWithAPIKeys(ctx, id, apiKeys); err != nil {
			return err
		}
	}

	if s.authCacheInvalidator != nil {
		for _, key := range apiKeys {
			if keyValue := strings.TrimSpace(key.Key); keyValue != "" {
				s.authCacheInvalidator.InvalidateAuthCacheByKey(ctx, keyValue)
			}
		}
		s.authCacheInvalidator.InvalidateAuthCacheByUserID(ctx, id)
	}
	return nil
}

// DeleteUsers receives the complete selection; separate DELETE requests cannot
// atomically reject a protected member mixed into a batch.
func (s *adminServiceImpl) DeleteUsers(ctx context.Context, ids []int64) error {
	ctx = withPersonnelAction(ctx, "delete")
	actor, ok := authz.FromContext(ctx)
	if !ok || !actor.ManagementRequest || !actor.Can("users.delete") {
		return ErrAdminPermissionDenied
	}
	if len(ids) == 0 || len(ids) > 1000 {
		return infraerrors.BadRequest("INVALID_USER_IDS", "user_ids must contain between 1 and 1000 IDs")
	}
	seen := map[int64]bool{}
	targets := []int64{}
	for _, id := range ids {
		if id <= 0 {
			return infraerrors.BadRequest("INVALID_USER_IDS", "user IDs must be positive")
		}
		if !seen[id] {
			seen[id] = true
			targets = append(targets, id)
		}
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i] < targets[j] })
	return RunManagementWrite(ctx, s.entClient, targets, func(ctx context.Context) error {
		tx := dbent.TxFromContext(ctx)
		if tx == nil {
			return ErrAdminPolicyUnavailable
		}
		if err := authz.LockManagementWrite(ctx, tx.Client(), targets, "", "", true); err != nil {
			return err
		}
		for _, id := range targets {
			keys, err := s.listUserAPIKeysForDeletion(ctx, id)
			if err != nil {
				return err
			}
			if err = s.deleteUserWithAPIKeys(ctx, id, keys); err != nil {
				return err
			}
			if s.authCacheInvalidator != nil {
				for _, key := range keys {
					s.authCacheInvalidator.InvalidateAuthCacheByKey(ctx, key.Key)
				}
				s.authCacheInvalidator.InvalidateAuthCacheByUserID(ctx, id)
			}
		}
		return nil
	})
}

func (s *adminServiceImpl) listUserAPIKeysForDeletion(ctx context.Context, userID int64) ([]APIKey, error) {
	if s.apiKeyRepo == nil {
		return nil, nil
	}

	const pageSize = 1000
	keys := make([]APIKey, 0)
	for page := 1; ; page++ {
		batch, result, err := s.apiKeyRepo.ListByUserID(ctx, userID, pagination.PaginationParams{
			Page:      page,
			PageSize:  pageSize,
			SortBy:    "id",
			SortOrder: pagination.SortOrderAsc,
		}, APIKeyListFilters{})
		if err != nil {
			return nil, fmt.Errorf("list user api keys: %w", err)
		}
		keys = append(keys, batch...)
		if len(batch) == 0 || len(batch) < pageSize || result == nil || int64(len(keys)) >= result.Total {
			break
		}
	}
	return keys, nil
}

func (s *adminServiceImpl) deleteUserWithAPIKeys(ctx context.Context, userID int64, apiKeys []APIKey) error {
	if s.apiKeyRepo != nil {
		for _, key := range apiKeys {
			if key.ID <= 0 {
				continue
			}
			if err := s.apiKeyRepo.DeleteWithAudit(ctx, key.ID); err != nil {
				logger.LegacyPrintf("service.admin", "delete user api key failed: user_id=%d api_key_id=%d err=%v", userID, key.ID, err)
				return fmt.Errorf("delete user api key %d: %w", key.ID, err)
			}
		}
	}

	if err := s.userRepo.Delete(ctx, userID); err != nil {
		logger.LegacyPrintf("service.admin", "delete user failed: user_id=%d err=%v", userID, err)
		return err
	}
	return nil
}

func (s *adminServiceImpl) BatchUpdateConcurrency(ctx context.Context, userIDs []int64, value int, mode string) (int, error) {
	cleaned := make([]int64, 0, len(userIDs))
	for _, uid := range userIDs {
		if uid > 0 {
			cleaned = append(cleaned, uid)
		}
	}
	if len(cleaned) == 0 {
		return 0, nil
	}

	var affected int
	var err error
	switch mode {
	case "set":
		affected, err = s.userRepo.BatchSetConcurrency(ctx, cleaned, value)
	case "add":
		affected, err = s.userRepo.BatchAddConcurrency(ctx, cleaned, value)
	default:
		return 0, errors.New("invalid mode: must be 'set' or 'add'")
	}
	if err != nil {
		return 0, err
	}

	if s.authCacheInvalidator != nil {
		for _, uid := range cleaned {
			s.authCacheInvalidator.InvalidateAuthCacheByUserID(ctx, uid)
		}
	}
	return affected, nil
}

func (s *adminServiceImpl) BatchUpdateLimits(ctx context.Context, userIDs []int64, concurrency, rpmLimit *int) (int, error) {
	if concurrency == nil && rpmLimit == nil {
		return 0, fmt.Errorf("at least one of concurrency or rpm_limit is required")
	}

	cleaned := make([]int64, 0, len(userIDs))
	seen := make(map[int64]struct{}, len(userIDs))
	for _, userID := range userIDs {
		if userID <= 0 {
			continue
		}
		if _, ok := seen[userID]; ok {
			continue
		}
		seen[userID] = struct{}{}
		cleaned = append(cleaned, userID)
	}
	if len(cleaned) == 0 {
		return 0, nil
	}

	affected, err := s.userRepo.BatchUpdateLimits(ctx, cleaned, concurrency, rpmLimit)
	if err != nil {
		return 0, err
	}
	if s.authCacheInvalidator != nil {
		for _, userID := range cleaned {
			s.authCacheInvalidator.InvalidateAuthCacheByUserID(ctx, userID)
		}
	}
	return affected, nil
}

func (s *adminServiceImpl) UpdateUserBalance(ctx context.Context, userID int64, balance float64, operation string, notes string) (*User, error) {
	if actor, ok := authz.FromContext(ctx); ok && actor.Role == RoleAdmin {
		return s.transferUserBalance(ctx, userID, balance, operation, notes)
	}

	if target, err := s.userRepo.GetByID(ctx, userID); err != nil {
		return nil, err
	} else if target.Role == RoleSuperAdmin {
		return nil, infraerrors.BadRequest("SUPER_ADMIN_BALANCE_EXEMPT", "Super administrators do not require a balance")
	}
	// 余额调整必须走原子接口：先读后整行写回会把并发的计费扣款覆盖掉。
	var (
		change BalanceChange
		err    error
	)
	switch operation {
	case "set":
		change, err = s.userRepo.SetBalance(ctx, userID, balance)
	case "add":
		change, err = s.userRepo.AdjustBalance(ctx, userID, balance)
	case "subtract":
		change, err = s.userRepo.AdjustBalance(ctx, userID, -balance)
	default:
		return nil, fmt.Errorf("unsupported balance operation: %q", operation)
	}
	if errors.Is(err, ErrBalanceNegative) {
		return nil, fmt.Errorf("balance cannot be negative, current balance: %.2f, requested operation would result in: %.2f", change.Old, change.New)
	}
	if err != nil {
		return nil, err
	}

	if s.billingCacheService != nil {
		// Register cleanup as soon as the mutation commits, including when the
		// following read or bookkeeping fails. Finish within the request owner,
		// with a bounded budget independent of request cancellation.
		defer func() {
			cacheCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			if err := s.billingCacheService.InvalidateUserBalance(cacheCtx, userID); err != nil {
				logger.LegacyPrintf("service.admin", "invalidate user balance cache failed: user_id=%d err=%v", userID, err)
			}
		}()
	}

	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return nil, err
	}

	balanceDiff := change.New - change.Old
	if s.authCacheInvalidator != nil && balanceDiff != 0 {
		s.authCacheInvalidator.InvalidateAuthCacheByUserID(ctx, userID)
	}

	if balanceDiff != 0 {
		code, err := GenerateRedeemCode()
		if err != nil {
			logger.LegacyPrintf("service.admin", "failed to generate adjustment redeem code: %v", err)
			return user, nil
		}

		adjustmentRecord := &RedeemCode{
			Code:   code,
			Type:   AdjustmentTypeAdminBalance,
			Value:  balanceDiff,
			Status: StatusUsed,
			UsedBy: &user.ID,
			Notes:  notes,
		}
		now := time.Now()
		adjustmentRecord.UsedAt = &now

		if err := s.redeemCodeRepo.Create(ctx, adjustmentRecord); err != nil {
			logger.LegacyPrintf("service.admin", "failed to create balance adjustment redeem code: %v", err)
		}
	}

	return user, nil
}

func (s *adminServiceImpl) GetUserAPIKeys(ctx context.Context, userID int64, page, pageSize int, sortBy, sortOrder string) ([]APIKey, int64, error) {
	params := pagination.PaginationParams{Page: page, PageSize: pageSize, SortBy: sortBy, SortOrder: sortOrder}
	keys, result, err := s.apiKeyRepo.ListByUserID(ctx, userID, params, APIKeyListFilters{})
	if err != nil {
		return nil, 0, err
	}
	return keys, result.Total, nil
}

func (s *adminServiceImpl) GetUserRPMStatus(ctx context.Context, userID int64) (*UserRPMStatus, error) {
	if s.userRPMCache == nil {
		return nil, ErrRPMStatusUnavailable
	}

	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return nil, err
	}

	userRPMUsed, err := s.userRPMCache.GetUserRPM(ctx, userID)
	if err != nil {
		logger.LegacyPrintf("service.admin", "failed to get user rpm: user_id=%d err=%v", userID, err)
	}

	keys, _, err := s.GetUserAPIKeys(ctx, userID, 1, 1000, "", "")
	if err != nil {
		return nil, err
	}

	groupIDSet := make(map[int64]struct{})
	for _, key := range keys {
		if key.GroupID != nil && *key.GroupID > 0 {
			groupIDSet[*key.GroupID] = struct{}{}
		}
	}

	groupIDs := make([]int64, 0, len(groupIDSet))
	for groupID := range groupIDSet {
		groupIDs = append(groupIDs, groupID)
	}
	sort.Slice(groupIDs, func(i, j int) bool { return groupIDs[i] < groupIDs[j] })

	var perGroup []UserGroupRPMStatus
	for _, groupID := range groupIDs {
		used, getErr := s.userRPMCache.GetUserGroupRPM(ctx, userID, groupID)
		if getErr != nil {
			logger.LegacyPrintf("service.admin", "failed to get user group rpm: user_id=%d group_id=%d err=%v", userID, groupID, getErr)
		}

		entry := UserGroupRPMStatus{
			GroupID: groupID,
			Used:    used,
		}

		if s.groupRepo != nil {
			if group, groupErr := s.groupRepo.GetByIDLite(ctx, groupID); groupErr == nil && group != nil {
				entry.GroupName = group.Name
				entry.Limit = group.RPMLimit
				entry.Source = "group"
			} else if groupErr != nil {
				logger.LegacyPrintf("service.admin", "failed to get group rpm status metadata: group_id=%d err=%v", groupID, groupErr)
			}
		}

		if s.userGroupRateRepo != nil {
			override, overrideErr := s.userGroupRateRepo.GetRPMOverrideByUserAndGroup(ctx, userID, groupID)
			if overrideErr != nil {
				logger.LegacyPrintf("service.admin", "failed to get rpm override: user_id=%d group_id=%d err=%v", userID, groupID, overrideErr)
			} else if override != nil {
				entry.Limit = *override
				entry.Source = "override"
			}
		}

		perGroup = append(perGroup, entry)
	}

	return &UserRPMStatus{
		UserRPMUsed:  userRPMUsed,
		UserRPMLimit: user.RPMLimit,
		PerGroup:     perGroup,
	}, nil
}

func (s *adminServiceImpl) GetUserUsageStats(ctx context.Context, userID int64, period string) (any, error) {
	// Return mock data for now
	return map[string]any{
		"period":          period,
		"total_requests":  0,
		"total_cost":      0.0,
		"total_tokens":    0,
		"avg_duration_ms": 0,
	}, nil
}

// GetUserBalanceHistory returns paginated balance/concurrency change records for a user.
func (s *adminServiceImpl) GetUserBalanceHistory(ctx context.Context, userID int64, page, pageSize int, codeType string) ([]RedeemCode, int64, float64, error) {
	if actor, ok := authz.FromContext(ctx); ok && actor.Role == RoleAdmin {
		user, err := s.userRepo.GetByID(ctx, userID)
		if err != nil {
			return nil, 0, 0, err
		}
		if err := guardManagedUser(ctx, user, "", false); err != nil {
			return nil, 0, 0, err
		}
	}
	params := pagination.PaginationParams{Page: page, PageSize: pageSize}
	if codeType == RedeemTypeAffiliateBalance {
		codes, total, err := s.listAffiliateBalanceHistory(ctx, userID, params)
		if err != nil {
			return nil, 0, 0, err
		}
		totalRecharged, err := s.redeemCodeRepo.SumPositiveBalanceByUser(ctx, userID)
		if err != nil {
			return nil, 0, 0, err
		}
		return codes, total, totalRecharged, nil
	}

	if codeType == "" {
		return s.getAllUserBalanceHistory(ctx, userID, params)
	}

	codes, result, err := s.redeemCodeRepo.ListByUserPaginated(ctx, userID, params, codeType)
	if err != nil {
		return nil, 0, 0, err
	}
	total := result.Total
	// Aggregate total recharged amount (only once, regardless of type filter)
	totalRecharged, err := s.redeemCodeRepo.SumPositiveBalanceByUser(ctx, userID)
	if err != nil {
		return nil, 0, 0, err
	}
	return codes, total, totalRecharged, nil
}

func (s *adminServiceImpl) getAllUserBalanceHistory(ctx context.Context, userID int64, params pagination.PaginationParams) ([]RedeemCode, int64, float64, error) {
	needed := params.Offset() + params.Limit()
	if needed < params.Limit() {
		needed = params.Limit()
	}

	redeemCodes, redeemTotal, err := s.listRedeemBalanceHistoryForMerge(ctx, userID, needed)
	if err != nil {
		return nil, 0, 0, err
	}
	affiliateCodes, affiliateTotal, err := s.listAffiliateBalanceHistoryForMerge(ctx, userID, needed)
	if err != nil {
		return nil, 0, 0, err
	}
	codes := mergeBalanceHistoryCodes(redeemCodes, affiliateCodes, params)

	totalRecharged, err := s.redeemCodeRepo.SumPositiveBalanceByUser(ctx, userID)
	if err != nil {
		return nil, 0, 0, err
	}
	return codes, redeemTotal + affiliateTotal, totalRecharged, nil
}

func (s *adminServiceImpl) listRedeemBalanceHistoryForMerge(ctx context.Context, userID int64, needed int) ([]RedeemCode, int64, error) {
	if needed <= 0 {
		return nil, 0, nil
	}

	var (
		out   []RedeemCode
		total int64
	)
	for page := 1; len(out) < needed; page++ {
		params := pagination.PaginationParams{Page: page, PageSize: 1000}
		codes, result, err := s.redeemCodeRepo.ListByUserPaginated(ctx, userID, params, "")
		if err != nil {
			return nil, 0, err
		}
		if result != nil {
			total = result.Total
		}
		out = append(out, codes...)
		if len(codes) < params.Limit() || int64(len(out)) >= total {
			break
		}
	}
	if len(out) > needed {
		out = out[:needed]
	}
	return out, total, nil
}

func (s *adminServiceImpl) listAffiliateBalanceHistoryForMerge(ctx context.Context, userID int64, needed int) ([]RedeemCode, int64, error) {
	if needed <= 0 {
		return nil, 0, nil
	}

	var (
		out   []RedeemCode
		total int64
	)
	for page := 1; len(out) < needed; page++ {
		params := pagination.PaginationParams{Page: page, PageSize: 1000}
		codes, currentTotal, err := s.listAffiliateBalanceHistory(ctx, userID, params)
		if err != nil {
			return nil, 0, err
		}
		total = currentTotal
		out = append(out, codes...)
		if len(codes) < params.Limit() || int64(len(out)) >= total {
			break
		}
	}
	if len(out) > needed {
		out = out[:needed]
	}
	return out, total, nil
}

func (s *adminServiceImpl) listAffiliateBalanceHistory(ctx context.Context, userID int64, params pagination.PaginationParams) ([]RedeemCode, int64, error) {
	if s == nil || s.entClient == nil || userID <= 0 {
		return nil, 0, nil
	}

	rows, err := s.entClient.QueryContext(ctx, `
SELECT id,
       amount::double precision,
       created_at
FROM user_affiliate_ledger
WHERE user_id = $1
  AND action = 'transfer'
ORDER BY created_at DESC, id DESC
OFFSET $2
LIMIT $3`, userID, params.Offset(), params.Limit())
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()

	codes := make([]RedeemCode, 0, params.Limit())
	for rows.Next() {
		var id int64
		var amount float64
		var createdAt time.Time
		if err := rows.Scan(&id, &amount, &createdAt); err != nil {
			return nil, 0, err
		}
		usedBy := userID
		usedAt := createdAt
		codes = append(codes, RedeemCode{
			ID:        -id,
			Code:      fmt.Sprintf("AFF-%d", id),
			Type:      RedeemTypeAffiliateBalance,
			Value:     amount,
			Status:    StatusUsed,
			UsedBy:    &usedBy,
			UsedAt:    &usedAt,
			CreatedAt: createdAt,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	total, err := countAffiliateBalanceHistory(ctx, s.entClient, userID)
	if err != nil {
		return nil, 0, err
	}
	return codes, total, nil
}

func countAffiliateBalanceHistory(ctx context.Context, client *dbent.Client, userID int64) (int64, error) {
	rows, err := client.QueryContext(ctx, `
SELECT COUNT(*)
FROM user_affiliate_ledger
WHERE user_id = $1
  AND action = 'transfer'`, userID)
	if err != nil {
		return 0, err
	}
	defer func() { _ = rows.Close() }()

	var total sql.NullInt64
	if rows.Next() {
		if err := rows.Scan(&total); err != nil {
			return 0, err
		}
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if !total.Valid {
		return 0, nil
	}
	return total.Int64, nil
}

func mergeBalanceHistoryCodes(redeemCodes, affiliateCodes []RedeemCode, params pagination.PaginationParams) []RedeemCode {
	combined := append(append([]RedeemCode{}, redeemCodes...), affiliateCodes...)
	sort.SliceStable(combined, func(i, j int) bool {
		return redeemCodeHistoryTime(combined[i]).After(redeemCodeHistoryTime(combined[j]))
	})
	offset := params.Offset()
	if offset >= len(combined) {
		return []RedeemCode{}
	}
	end := offset + params.Limit()
	if end > len(combined) {
		end = len(combined)
	}
	return combined[offset:end]
}

func redeemCodeHistoryTime(code RedeemCode) time.Time {
	if code.UsedAt != nil {
		return *code.UsedAt
	}
	return code.CreatedAt
}

func (s *adminServiceImpl) BindUserAuthIdentity(ctx context.Context, userID int64, input AdminBindAuthIdentityInput) (*AdminBoundAuthIdentity, error) {
	ctx = withPersonnelAction(ctx, "security")
	if userID <= 0 {
		return nil, infraerrors.BadRequest("INVALID_INPUT", "user_id must be greater than 0")
	}
	if s == nil || s.entClient == nil || s.userRepo == nil {
		return nil, infraerrors.InternalServer("ADMIN_AUTH_IDENTITY_BIND_UNAVAILABLE", "auth identity binding service is unavailable")
	}
	if _, err := s.userRepo.GetByID(ctx, userID); err != nil {
		return nil, err
	}

	if actor, ok := authz.FromContext(ctx); ok && actor.UserID == userID {
		return nil, ErrAdminPermissionDenied
	}

	providerType := normalizeAdminAuthIdentityProviderType(input.ProviderType)
	providerKey := strings.TrimSpace(input.ProviderKey)
	providerSubject := strings.TrimSpace(input.ProviderSubject)
	if providerType == "" {
		return nil, infraerrors.BadRequest("INVALID_INPUT", "provider_type must be one of email, linuxdo, oidc, wechat, or dingtalk")
	}
	if providerKey == "" || providerSubject == "" {
		return nil, infraerrors.BadRequest("INVALID_INPUT", "provider_type, provider_key, and provider_subject are required")
	}
	canonicalProviderKey := canonicalAdminAuthIdentityProviderKey(providerType, "", providerKey)
	compatibleProviderKeys := compatibleAdminAuthIdentityProviderKeys(providerType, providerKey)

	var issuer *string
	if input.Issuer != nil {
		trimmed := strings.TrimSpace(*input.Issuer)
		if trimmed != "" {
			issuer = &trimmed
		}
	}

	channelInput := normalizeAdminBindChannelInput(input.Channel)
	if input.Channel != nil && channelInput == nil {
		return nil, infraerrors.BadRequest("INVALID_INPUT", "channel, channel_app_id, and channel_subject are required when channel binding is provided")
	}

	verifiedAt := time.Now().UTC()
	tx, err := s.entClient.Tx(ctx)
	if err != nil {
		return nil, infraerrors.InternalServer("ADMIN_AUTH_IDENTITY_BIND_TX_FAILED", "failed to start auth identity bind transaction").WithCause(err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := authz.LockManagementWrite(ctx, tx.Client(), []int64{userID}, "", "", false); err != nil {
		return nil, err
	}
	target, err := LockAuthLifecycleUser(ctx, tx.Client(), userID)
	if err != nil {
		return nil, err
	}

	previousEmail := s.verifiedManagementEmail(ctx, userID, target.Email)
	identityRecords, err := tx.AuthIdentity.Query().
		Where(
			authidentity.ProviderTypeEQ(providerType),
			authidentity.ProviderKeyIn(compatibleProviderKeys...),
			authidentity.ProviderSubjectEQ(providerSubject),
		).
		All(ctx)
	if err != nil {
		return nil, infraerrors.InternalServer("ADMIN_AUTH_IDENTITY_BIND_LOOKUP_FAILED", "failed to inspect auth identity ownership").WithCause(err)
	}
	if hasAdminAuthIdentityOwnershipConflict(identityRecords, userID) {
		return nil, infraerrors.Conflict("AUTH_IDENTITY_OWNERSHIP_CONFLICT", "auth identity already belongs to another user")
	}
	identity := selectOwnedAdminAuthIdentity(identityRecords, userID)

	if identity == nil {
		create := tx.AuthIdentity.Create().
			SetUserID(userID).
			SetProviderType(providerType).
			SetProviderKey(canonicalProviderKey).
			SetProviderSubject(providerSubject).
			SetVerifiedAt(verifiedAt)
		if issuer != nil {
			create = create.SetIssuer(*issuer)
		}
		if input.Metadata != nil {
			create = create.SetMetadata(cloneAdminAuthIdentityMetadata(input.Metadata))
		}
		identity, err = create.Save(ctx)
		if err != nil {
			return nil, infraerrors.InternalServer("ADMIN_AUTH_IDENTITY_BIND_SAVE_FAILED", "failed to save auth identity").WithCause(err)
		}
	} else {
		update := tx.AuthIdentity.UpdateOneID(identity.ID).
			SetVerifiedAt(verifiedAt).
			SetProviderKey(canonicalProviderKey)
		if issuer != nil {
			update = update.SetIssuer(*issuer)
		}
		if input.Metadata != nil {
			update = update.SetMetadata(cloneAdminAuthIdentityMetadata(input.Metadata))
		}
		identity, err = update.Save(ctx)
		if err != nil {
			return nil, infraerrors.InternalServer("ADMIN_AUTH_IDENTITY_BIND_SAVE_FAILED", "failed to save auth identity").WithCause(err)
		}
	}

	var channel *dbent.AuthIdentityChannel
	if channelInput != nil {
		channelRecords, err := tx.AuthIdentityChannel.Query().
			Where(
				authidentitychannel.ProviderTypeEQ(providerType),
				authidentitychannel.ProviderKeyIn(compatibleProviderKeys...),
				authidentitychannel.ChannelEQ(channelInput.Channel),
				authidentitychannel.ChannelAppIDEQ(channelInput.ChannelAppID),
				authidentitychannel.ChannelSubjectEQ(channelInput.ChannelSubject),
			).
			WithIdentity().
			All(ctx)
		if err != nil {
			return nil, infraerrors.InternalServer("ADMIN_AUTH_IDENTITY_CHANNEL_LOOKUP_FAILED", "failed to inspect auth identity channel ownership").WithCause(err)
		}
		if hasAdminAuthIdentityChannelOwnershipConflict(channelRecords, userID) {
			return nil, infraerrors.Conflict("AUTH_IDENTITY_CHANNEL_OWNERSHIP_CONFLICT", "auth identity channel already belongs to another user")
		}
		channel = selectOwnedAdminAuthIdentityChannel(channelRecords, userID)
		if channel == nil {
			create := tx.AuthIdentityChannel.Create().
				SetIdentityID(identity.ID).
				SetProviderType(providerType).
				SetProviderKey(canonicalProviderKey).
				SetChannel(channelInput.Channel).
				SetChannelAppID(channelInput.ChannelAppID).
				SetChannelSubject(channelInput.ChannelSubject)
			if channelInput.Metadata != nil {
				create = create.SetMetadata(cloneAdminAuthIdentityMetadata(channelInput.Metadata))
			}
			channel, err = create.Save(ctx)
			if err != nil {
				return nil, infraerrors.InternalServer("ADMIN_AUTH_IDENTITY_CHANNEL_SAVE_FAILED", "failed to save auth identity channel").WithCause(err)
			}
		} else {
			update := tx.AuthIdentityChannel.UpdateOneID(channel.ID).
				SetIdentityID(identity.ID).
				SetProviderKey(canonicalProviderKey)
			if channelInput.Metadata != nil {
				update = update.SetMetadata(cloneAdminAuthIdentityMetadata(channelInput.Metadata))
			}
			channel, err = update.Save(ctx)
			if err != nil {
				return nil, infraerrors.InternalServer("ADMIN_AUTH_IDENTITY_CHANNEL_SAVE_FAILED", "failed to save auth identity channel").WithCause(err)
			}
		}
	}

	if err := tx.User.UpdateOneID(userID).AddSessionGeneration(1).Exec(ctx); err != nil {
		return nil, err
	}
	if err := tx.PendingAuthSession.Update().Where(pendingauthsession.TargetUserIDEQ(userID), pendingauthsession.ConsumedAtIsNil()).SetConsumedAt(time.Now()).Exec(ctx); err != nil {
		return nil, err
	}
	if err := RecordManagementUserChange(ctx, tx.Client(), userID, "admin.user.identity.bind", target.Role, target.Role); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, infraerrors.InternalServer("ADMIN_AUTH_IDENTITY_BIND_COMMIT_FAILED", "failed to commit auth identity bind").WithCause(err)
	}
	s.notifyManagementSecurityChange(ctx, userID, previousEmail)
	return buildAdminBoundAuthIdentity(identity, channel), nil
}

func compatibleAdminAuthIdentityProviderKeys(providerType, providerKey string) []string {
	providerType = strings.TrimSpace(strings.ToLower(providerType))
	providerKey = strings.TrimSpace(providerKey)
	if providerKey == "" {
		return []string{providerKey}
	}
	if providerType != "wechat" {
		return []string{providerKey}
	}

	keys := []string{providerKey}
	if !strings.EqualFold(providerKey, "wechat-main") {
		keys = append(keys, "wechat-main")
	}
	if !strings.EqualFold(providerKey, "wechat") {
		keys = append(keys, "wechat")
	}
	return keys
}

func canonicalAdminAuthIdentityProviderKey(providerType, existingKey, requestedKey string) string {
	providerType = strings.TrimSpace(strings.ToLower(providerType))
	existingKey = strings.TrimSpace(existingKey)
	requestedKey = strings.TrimSpace(requestedKey)
	if providerType != "wechat" {
		if requestedKey != "" {
			return requestedKey
		}
		return existingKey
	}
	if strings.EqualFold(existingKey, "wechat") || strings.EqualFold(existingKey, "wechat-main") || strings.EqualFold(requestedKey, "wechat-main") {
		return "wechat-main"
	}
	if requestedKey != "" {
		return requestedKey
	}
	return existingKey
}

func adminAuthIdentityProviderKeyRank(providerType, providerKey string) int {
	providerType = strings.TrimSpace(strings.ToLower(providerType))
	providerKey = strings.TrimSpace(providerKey)
	if providerType != "wechat" {
		return 0
	}
	switch {
	case strings.EqualFold(providerKey, "wechat-main"):
		return 0
	case strings.EqualFold(providerKey, "wechat"):
		return 2
	default:
		return 1
	}
}

func selectOwnedAdminAuthIdentity(records []*dbent.AuthIdentity, userID int64) *dbent.AuthIdentity {
	var selected *dbent.AuthIdentity
	for _, record := range records {
		if record.UserID != userID {
			continue
		}
		if selected == nil || adminAuthIdentityProviderKeyRank(record.ProviderType, record.ProviderKey) < adminAuthIdentityProviderKeyRank(selected.ProviderType, selected.ProviderKey) {
			selected = record
		}
	}
	return selected
}

func hasAdminAuthIdentityOwnershipConflict(records []*dbent.AuthIdentity, userID int64) bool {
	for _, record := range records {
		if record.UserID != userID {
			return true
		}
	}
	return false
}

func selectOwnedAdminAuthIdentityChannel(records []*dbent.AuthIdentityChannel, userID int64) *dbent.AuthIdentityChannel {
	var selected *dbent.AuthIdentityChannel
	for _, record := range records {
		if record.Edges.Identity == nil || record.Edges.Identity.UserID != userID {
			continue
		}
		if selected == nil || adminAuthIdentityProviderKeyRank(record.ProviderType, record.ProviderKey) < adminAuthIdentityProviderKeyRank(selected.ProviderType, selected.ProviderKey) {
			selected = record
		}
	}
	return selected
}

func hasAdminAuthIdentityChannelOwnershipConflict(records []*dbent.AuthIdentityChannel, userID int64) bool {
	for _, record := range records {
		if record.Edges.Identity != nil && record.Edges.Identity.UserID != userID {
			return true
		}
	}
	return false
}

func normalizeAdminBindChannelInput(input *AdminBindAuthIdentityChannelInput) *AdminBindAuthIdentityChannelInput {
	if input == nil {
		return nil
	}
	channel := &AdminBindAuthIdentityChannelInput{
		Channel:        strings.TrimSpace(input.Channel),
		ChannelAppID:   strings.TrimSpace(input.ChannelAppID),
		ChannelSubject: strings.TrimSpace(input.ChannelSubject),
		Metadata:       cloneAdminAuthIdentityMetadata(input.Metadata),
	}
	if channel.Channel == "" || channel.ChannelAppID == "" || channel.ChannelSubject == "" {
		return nil
	}
	return channel
}

func normalizeAdminAuthIdentityProviderType(input string) string {
	switch strings.ToLower(strings.TrimSpace(input)) {
	case "email":
		return "email"
	case "linuxdo":
		return "linuxdo"
	case "oidc":
		return "oidc"
	case "wechat":
		return "wechat"
	case "dingtalk":
		return "dingtalk"
	default:
		return ""
	}
}

func buildAdminBoundAuthIdentity(identity *dbent.AuthIdentity, channel *dbent.AuthIdentityChannel) *AdminBoundAuthIdentity {
	if identity == nil {
		return nil
	}
	result := &AdminBoundAuthIdentity{
		UserID:          identity.UserID,
		ProviderType:    strings.TrimSpace(identity.ProviderType),
		ProviderKey:     strings.TrimSpace(identity.ProviderKey),
		ProviderSubject: strings.TrimSpace(identity.ProviderSubject),
		VerifiedAt:      identity.VerifiedAt,
		Issuer:          identity.Issuer,
		Metadata:        cloneAdminAuthIdentityMetadata(identity.Metadata),
		CreatedAt:       identity.CreatedAt,
		UpdatedAt:       identity.UpdatedAt,
	}
	if channel != nil {
		result.Channel = &AdminBoundAuthIdentityChannel{
			Channel:        strings.TrimSpace(channel.Channel),
			ChannelAppID:   strings.TrimSpace(channel.ChannelAppID),
			ChannelSubject: strings.TrimSpace(channel.ChannelSubject),
			Metadata:       cloneAdminAuthIdentityMetadata(channel.Metadata),
			CreatedAt:      channel.CreatedAt,
			UpdatedAt:      channel.UpdatedAt,
		}
	}
	return result
}

func cloneAdminAuthIdentityMetadata(input map[string]any) map[string]any {
	if input == nil {
		return nil
	}
	if len(input) == 0 {
		return map[string]any{}
	}
	data, err := json.Marshal(input)
	if err != nil {
		out := make(map[string]any, len(input))
		for key, value := range input {
			out[key] = value
		}
		return out
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		out = make(map[string]any, len(input))
		for key, value := range input {
			out[key] = value
		}
	}
	return out
}

// guardManagedUser distinguishes peer membership management from root authority.
func guardManagedUser(ctx context.Context, user *User, nextRole string, removing bool) error {
	actor, ok := authz.FromContext(ctx)
	if !ok {
		return nil
	}
	if !authz.IsManagementRole(actor.Role) {
		return ErrAdminPermissionDenied
	}
	if actor.Role == RoleAdmin && (user.Role != RoleUser || (nextRole != "" && nextRole != RoleUser) || user.CreatedBy == nil || *user.CreatedBy != actor.UserID) {
		return ErrAdminPermissionDenied
	}
	if actor.Role == authz.Admin && actor.PersonnelAction != "" && !actor.Can(authz.PersonnelPermission(actor.PersonnelAction, user.Role, nextRole)) {
		return ErrAdminPermissionDenied
	}
	if actor.UserID == user.ID && (removing || (nextRole != "" && nextRole != user.Role)) {
		return ErrAdminPermissionDenied
	}
	return nil
}
