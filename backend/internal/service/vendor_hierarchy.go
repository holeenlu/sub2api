package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"github.com/Wei-Shaw/sub2api/internal/authz"
	"github.com/lib/pq"
	"net/mail"
	"strings"
	"unicode/utf8"
)

// Login fields exist only on creation. Never serialize this input into audits.
type VendorCreateInput struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required,min=6"`
	Vendor
	passwordHash string
}

func NewVendorPolicy() Vendor {
	return Vendor{Tier: 1, Status: StatusActive, AccountLimit: 0, ProxyLimit: 0, DefaultConcurrency: 15, DefaultPriority: 1, DefaultRateMultiplier: 1, DefaultCoefficient: "1"}
}

func (s *VendorService) CreateVendor(ctx context.Context, in VendorCreateInput) (*Vendor, error) {
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	email, err := mail.ParseAddress(in.Email)
	if err != nil || email.Address != in.Email || len(in.Email) > 254 || isReservedEmail(in.Email) || utf8.RuneCountInString(in.Password) < 6 || len(in.Password) > 72 || in.UserID != 0 {
		return nil, ErrVendorInput
	}
	if _, err := s.vendorManager(ctx); err != nil {
		return nil, err
	}
	user := &User{}
	if err := user.SetPassword(in.Password); err != nil {
		return nil, ErrVendorInput
	}
	in.passwordHash = user.PasswordHash
	in.Password = ""
	return s.saveVendor(ctx, 0, in.Vendor, &in)
}

func sameVendorParent(a, b *int64) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && *a == *b)
}

// A nil manager denotes a live super administrator. Other roles fail closed.
func (s *VendorService) vendorManager(ctx context.Context) (*Vendor, error) {
	if scope, ok := authz.VendorScopeFromContext(ctx); ok && (scope.ReadOnly || scope.Children) {
		return nil, ErrVendorDenied
	}
	actor, ok := authz.FromContext(ctx)
	if !ok {
		return nil, ErrVendorDenied
	}
	if actor.Role == authz.SuperAdmin {
		return nil, s.RequireSuperAdmin(ctx)
	}
	if actor.Role != authz.VendorLevel1 {
		return nil, ErrVendorDenied
	}
	return s.CurrentVendor(ctx)
}

func (s *VendorService) ManagedVendor(ctx context.Context, id int64) (*Vendor, error) {
	manager, err := s.vendorManager(ctx)
	if err != nil {
		return nil, err
	}
	v, err := s.loadVendor(ctx, id)
	if err != nil {
		return nil, err
	}
	if manager != nil && (v.Tier != 2 || v.ParentVendorID == nil || *v.ParentVendorID != manager.ID) {
		return nil, ErrVendorDenied
	}
	return v, nil
}

type vendorIdentityReader interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func (s *VendorService) validateVendorActor(ctx context.Context, q vendorIdentityReader, actor authz.Subject, id int64) error {
	var valid bool
	err := q.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM vendors v JOIN users u ON u.id=v.user_id WHERE v.id=$1 AND u.id=$2 AND u.role=$3 AND u.session_generation=$4 AND "+authz.ActiveVendorSQL("v", "u")+")", id, actor.UserID, actor.Role, actor.SessionGeneration).Scan(&valid)
	if err != nil {
		return err
	}
	if !valid {
		return ErrVendorDenied
	}
	return nil
}

func (s *VendorService) lockVendorManager(ctx context.Context, tx *sql.Tx, actor authz.Subject, manager *Vendor) error {
	var role, status string
	var generation int64
	if err := tx.QueryRowContext(ctx, "SELECT role,status,session_generation FROM users WHERE id=$1 AND deleted_at IS NULL FOR SHARE", actor.UserID).Scan(&role, &status, &generation); err != nil {
		return ErrVendorDenied
	}
	if role != actor.Role || status != StatusActive || generation != actor.SessionGeneration {
		return ErrVendorDenied
	}
	if manager == nil {
		if role != RoleSuperAdmin {
			return ErrVendorDenied
		}
		return nil
	}
	return s.validateVendorActor(ctx, tx, actor, manager.ID)
}

func validateVendorParentGrant(ctx context.Context, tx *sql.Tx, v *Vendor) error {
	var parentUserID int64
	// Serialize revocation with a child creation or policy update.
	if err := tx.QueryRowContext(ctx, "SELECT u.id FROM users u JOIN vendors p ON p.user_id=u.id WHERE p.id=$1 FOR SHARE OF u", v.ParentVendorID).Scan(&parentUserID); err != nil {
		return ErrVendorDenied
	}
	var valid bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM vendors p JOIN users u ON u.id=p.user_id
 WHERE p.id=$1 AND p.tier=1 AND p.parent_vendor_id IS NULL AND p.status='active'
 AND u.role='vendor_level1' AND u.status='active' AND u.deleted_at IS NULL
 AND p.platforms @> $2::jsonb AND p.openai_authorization_modes @> $5::jsonb AND (NOT $3 OR p.allow_direct)
 AND NOT EXISTS(SELECT 1 FROM unnest($4::bigint[]) g(id) WHERE NOT EXISTS(SELECT 1 FROM vendor_groups pg WHERE pg.vendor_id=p.id AND pg.group_id=g.id)))`, v.ParentVendorID, mustVendorJSON(v.Platforms), v.AllowDirect, pq.Array(v.GroupIDs), mustVendorJSON(v.OpenAIAuthorizationModes)).Scan(&valid)
	if err != nil {
		return err
	}
	if !valid {
		return ErrVendorDenied
	}
	return nil
}

func applyVendorParentPolicy(v, p *Vendor) {
	platforms := []string{}
	for _, value := range v.Platforms {
		if stringsContain(p.Platforms, value) {
			platforms = append(platforms, value)
		}
	}
	v.Platforms = platforms
	modes := []string{}
	for _, mode := range v.OpenAIAuthorizationModes {
		if stringsContain(p.OpenAIAuthorizationModes, mode) && stringsContain(platforms, PlatformOpenAI) {
			modes = append(modes, mode)
		}
	}
	v.OpenAIAuthorizationModes = modes
	v.AllowDirect = v.AllowDirect && p.AllowDirect
	groups := []VendorGroup{}
	ids := []int64{}
	for _, g := range v.Groups {
		for _, parentGroup := range p.Groups {
			if parentGroup.ID == g.ID && stringsContain(platforms, g.Platform) {
				groups = append(groups, g)
				ids = append(ids, g.ID)
				break
			}
		}
	}
	v.Groups = groups
	v.GroupIDs = ids
}

// This is the only cross-owner capability, and is explicitly read-only.
func (s *VendorService) ReadVendorScope(ctx context.Context, id int64) (*Vendor, error) {
	own, err := s.CurrentVendor(ctx)
	if err != nil {
		return nil, err
	}
	if own.ID == id {
		return own, nil
	}
	actor, _ := authz.FromContext(ctx)
	if actor.Role != authz.VendorLevel1 {
		return nil, ErrVendorDenied
	}
	v, err := s.loadVendor(ctx, id)
	if err != nil {
		return nil, err
	}
	if v.Tier != 2 || v.ParentVendorID == nil || *v.ParentVendorID != own.ID {
		return nil, ErrVendorDenied
	}
	applyVendorParentPolicy(v, own)
	return v, nil
}

func requireVendorWriteScope(ctx context.Context) error {
	if scope, ok := authz.VendorScopeFromContext(ctx); ok && scope.ReadOnly {
		return ErrVendorDenied
	}
	return nil
}
func (s *VendorService) CurrentVendorForWrite(ctx context.Context) (*Vendor, error) {
	if err := requireVendorWriteScope(ctx); err != nil {
		return nil, err
	}
	return s.CurrentVendor(ctx)
}

func mustVendorJSON(v any) string { b, _ := json.Marshal(v); return string(b) }

// Keep login state stable through resource commits, including parent revocation.
func (s *VendorService) lockVendorResourceActor(ctx context.Context, tx *sql.Tx, id int64) error {
	actor, ok := authz.FromContext(ctx)
	if !ok {
		return ErrVendorDenied
	}
	rows, err := tx.QueryContext(ctx, "SELECT u.id FROM users u WHERE u.id=$2 OR u.id IN (SELECT user_id FROM vendors WHERE id=$1 OR id=(SELECT parent_vendor_id FROM vendors WHERE id=$1)) ORDER BY u.id FOR SHARE", id, actor.UserID)
	if err != nil {
		return err
	}
	for rows.Next() {
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if actor.Role == authz.SuperAdmin {
		return s.lockVendorManager(ctx, tx, actor, nil)
	}
	if err := requireVendorWriteScope(ctx); err != nil {
		return err
	}
	return s.validateVendorActor(ctx, tx, actor, id)
}
