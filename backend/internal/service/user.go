package service

import (
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/Wei-Shaw/sub2api/internal/access"
)

type User struct {
	CreatedBy      *int64 // Immutable creator identity; also scopes delegated user management.
	CreatedByName  string // Current creator username, admin-only display metadata.
	ID             int64
	Email          string
	Username       string
	Notes          string
	AvatarURL      string
	AvatarSource   string
	AvatarMIME     string
	AvatarByteSize int
	AvatarSHA256   string
	PasswordHash   string
	Role           string
	Balance        float64
	FrozenBalance  float64
	Concurrency    int
	Status         string
	AllowedGroups  []int64
	// RestrictPublicGroups narrows the public groups this user may bind to the
	// ones listed in AllowedGroups. False keeps the default, where every public
	// group is bindable.
	RestrictPublicGroups bool
	SessionGeneration    int64 // Durable security lifecycle generation; zero preserves legacy tokens.
	TokenVersion         int64 // Incremented on password change to invalidate existing tokens
	// TokenVersionResolved indicates TokenVersion already contains the fingerprint-derived
	// value expected in JWT claims and refresh-token state.
	TokenVersionResolved bool
	SignupSource         string
	LastLoginAt          *time.Time
	LastActiveAt         *time.Time
	LastUsedAt           *time.Time
	CreatedAt            time.Time
	UpdatedAt            time.Time
	DeletedAt            *time.Time // 非 nil 表示用户已软删除

	// GroupRates 用户专属分组倍率配置
	// map[groupID]rateMultiplier
	GroupRates map[int64]float64

	// TOTP 双因素认证字段
	TotpSecretEncrypted *string    // AES-256-GCM 加密的 TOTP 密钥
	TotpEnabled         bool       // 是否启用 TOTP
	TotpEnabledAt       *time.Time // TOTP 启用时间

	// 余额不足通知
	BalanceNotifyEnabled       bool
	BalanceNotifyThresholdType string // "fixed" (default) | "percentage"
	BalanceNotifyThreshold     *float64
	BalanceNotifyExtraEmails   []NotifyEmailEntry
	TotalRecharged             float64

	// RPMLimit 用户级每分钟请求数上限（0 = 不限制）。仅在所用分组未设置 rpm_limit
	// 且该 (用户, 分组) 无 rpm_override 时作为全局兜底生效，计数键 rpm:u:{userID}:{min}。
	RPMLimit int

	// UserGroupRPMOverride 来自 auth cache snapshot 的 (user, group) RPM 覆盖值。
	// nil = 该 API Key 对应的 (user, group) 无 override；非 nil 时 checkRPM 直接使用，
	// 避免每请求查 DB。字段不持久化到数据库。
	UserGroupRPMOverride *int

	APIKeys       []APIKey
	Subscriptions []UserSubscription
}

func (u *User) IsStaff() bool {
	return u != nil && (u.Role == RoleAdmin || u.Role == RoleSuperAdmin)
}

func (u *User) IsVendor() bool {
	return u != nil && (u.Role == RoleVendorLevel1 || u.Role == RoleVendorLevel2)
}

func (u *User) IsSuperAdmin() bool { return u != nil && u.Role == RoleSuperAdmin }

func (u *User) IsActive() bool {
	return u.Status == StatusActive
}

// CanBindGroup converts the user entity for API-key creation, update, and group
// listing. Policy belongs to access. Remove this adapter when those consumers
// receive GroupBinding values directly.
func (u *User) CanBindGroup(groupID int64, isExclusive bool) bool {
	return access.CanBindGroup(access.GroupBinding{
		Vendor:               u.IsVendor(),
		RestrictPublicGroups: u.RestrictPublicGroups,
		AllowedGroups:        u.AllowedGroups,
	}, groupID, isExclusive)
}

func (u *User) SetPassword(password string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	u.PasswordHash = string(hash)
	return nil
}

func (u *User) CheckPassword(password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) == nil
}
