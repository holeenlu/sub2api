// Package authz owns the fixed roles and the shared administrator permission
// catalogue. HTTP, services and the UI consume the same decisions.
package authz

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
)

const (
	SuperAdmin = domain.RoleSuperAdmin
	Admin      = domain.RoleAdmin
	User       = domain.RoleUser
	PolicyKey  = "admin_role_policy"
)

type Permission struct {
	Key          string   `json:"key"`
	Module       string   `json:"module"`
	Label        string   `json:"label"`
	Dependencies []string `json:"dependencies"`
	Sensitive    bool     `json:"sensitive"`
	Aliases      []string `json:"aliases"`
	Group        string   `json:"group"`
}

// Catalogue is the complete set of delegable capabilities. Super-administrator
// operations deliberately have no delegable permission.
func Catalogue() []Permission {
	var out []Permission
	add := func(module, action, label string, sensitive bool, dependencies ...string) {
		if dependencies == nil {
			dependencies = []string{}
		}
		group := "resources"
		switch module {
		case "staff", "users", "api_keys":
			group = "personnel"
		case "billing", "subscriptions", "orders", "plans", "redeem_codes", "promo_codes", "affiliates", "usage", "dashboard":
			group = "finance"
		case "announcements", "ops", "risk", "prompt_audit", "audit":
			group = "operations"
		case "settings.general", "settings.content":
			group = "settings"
		}
		out = append(out, Permission{Key: module + "." + action, Module: module, Label: label, Dependencies: dependencies, Sensitive: sensitive, Aliases: []string{}, Group: group})
	}
	add("staff", "manage", "Administrator membership", true, "users.read")
	add("dashboard", "read", "Dashboard", false)
	for _, module := range []string{"users", "api_keys", "groups", "channels", "channel_monitor", "accounts", "proxies", "usage", "subscriptions", "orders", "plans", "redeem_codes", "promo_codes", "affiliates", "announcements", "ops", "risk", "prompt_audit", "audit", "settings.general", "settings.content"} {
		add(module, "read", module+" read", false)
	}
	for _, module := range []string{"api_keys", "groups", "channels", "channel_monitor", "accounts", "proxies", "subscriptions", "orders", "plans", "redeem_codes", "promo_codes", "affiliates", "announcements", "ops", "risk", "settings.general", "settings.content"} {
		add(module, "manage", module+" manage", module == "redeem_codes", module+".read")
	}
	for _, action := range []string{"create", "update", "delete", "security"} {
		if action == "security" {
			add("users", action, "users "+action, true, "users.read", "users.update")
		} else {
			add("users", action, "users "+action, false, "users.read")
		}
	}
	add("accounts", "authorize", "Account authorization", true, "accounts.read", "accounts.manage")
	add("accounts", "diagnostics", "Account diagnostics", false, "accounts.read")
	add("usage", "export", "Usage export", true, "usage.read")
	add("redeem_codes", "export", "Redeem code export", true, "redeem_codes.read")
	add("billing", "cost.read", "Upstream cost", false)
	add("billing", "balance.adjust", "Adjust balance", true, "users.read")
	add("billing", "rates.update", "Edit prices", true)
	add("orders", "refund", "Refund orders", true, "orders.read")
	add("affiliates", "adjust", "Adjust affiliate credits", true, "affiliates.read")
	add("prompt_audit", "content.read", "Read audited content", true, "prompt_audit.read")
	return out
}

type Policy struct {
	LegacyAuditMaxID int64     `json:"legacy_audit_max_id,omitempty"`
	Version          int64     `json:"version"`
	Permissions      []string  `json:"permissions"`
	UpdatedBy        int64     `json:"updated_by"`
	UpdatedAt        time.Time `json:"updated_at"`
}

func NormalizePermissions(values []string) ([]string, error) {
	known := make(map[string]Permission)
	for _, p := range Catalogue() {
		known[p.Key] = p
	}
	selected := make(map[string]bool)
	for _, key := range values {
		if _, ok := known[key]; !ok {
			return nil, fmt.Errorf("unknown or non-delegable permission: %q", key)
		}
		selected[key] = true
	}
	for key := range selected {
		for _, dependency := range known[key].Dependencies {
			if !selected[dependency] {
				return nil, fmt.Errorf("permission %s requires %s", key, dependency)
			}
		}
	}
	out := make([]string, 0, len(selected))
	for key := range selected {
		out = append(out, key)
	}
	sort.Strings(out)
	return out, nil
}

func (p Policy) Validate() error {
	if p.Version < 1 {
		return fmt.Errorf("invalid administrator policy version")
	}
	if p.Permissions == nil {
		return fmt.Errorf("missing administrator permissions")
	}
	return nil
}

// Stored policies survive catalogue evolution. Unknown keys never grant access;
// incomplete dependency chains are removed rather than implicitly granting more.
func ReadPermissions(values []string) (permissions, ignored []string) {
	known := map[string]Permission{}
	aliases := map[string]string{}
	for _, p := range Catalogue() {
		known[p.Key] = p
		for _, a := range p.Aliases {
			aliases[a] = p.Key
		}
	}
	selected := map[string]bool{}
	for _, key := range values {
		original := key
		if canonical, ok := aliases[key]; ok {
			key = canonical
		}
		if _, ok := known[key]; !ok {
			ignored = append(ignored, original)
			continue
		}
		selected[key] = true
	}
	for changed := true; changed; {
		changed = false
		for key := range selected {
			for _, dep := range known[key].Dependencies {
				if !selected[dep] {
					delete(selected, key)
					ignored = append(ignored, key)
					changed = true
					break
				}
			}
		}
	}
	permissions = []string{}
	for key := range selected {
		permissions = append(permissions, key)
	}
	sort.Strings(permissions)
	sort.Strings(ignored)
	return
}

// PersonnelPermission is shared by request preflight and identity transactions.
// Financial and API-key operations have their own permissions, not staff.manage.
func PersonnelPermission(action, targetRole, nextRole string) string {
	if targetRole == Admin || (nextRole != "" && nextRole != targetRole) {
		return "staff.manage"
	}
	return "users." + action
}

func IsManagementRole(role string) bool { return role == SuperAdmin || role == Admin }
func ValidRole(role string) bool        { return role == User || IsManagementRole(role) }

type Subject struct {
	PersonnelAction   string
	AuthMethod        string
	TargetUserIDs     []int64
	SessionID         string
	ManagementRequest bool
	UserID            int64
	Role              string
	SessionGeneration int64
	PolicyVersion     int64
	Permissions       []string
}

func (s Subject) Can(permission string) bool {
	if s.Role == SuperAdmin {
		return true
	}
	if s.Role != Admin || s.PolicyVersion < 1 {
		return false
	}
	for _, key := range s.Permissions {
		if key == permission {
			return true
		}
	}
	return false
}

func (s Subject) CanAll(permissions ...string) bool {
	for _, key := range permissions {
		if !s.Can(key) {
			return false
		}
	}
	return true
}

func (s Subject) CanAny(permissions ...string) bool {
	for _, key := range permissions {
		if s.Can(key) {
			return true
		}
	}
	return false
}

type contextKey struct{}

func WithSubject(ctx context.Context, subject Subject) context.Context {
	return context.WithValue(ctx, contextKey{}, subject)
}
func FromContext(ctx context.Context) (Subject, bool) {
	s, ok := ctx.Value(contextKey{}).(Subject)
	return s, ok
}

func IsManagementRequest(ctx context.Context) bool {
	actor, ok := FromContext(ctx)
	return ok && actor.ManagementRequest && IsManagementRole(actor.Role)
}

// Snapshot is safe for /auth/me and never contains a credential or another
// user's grant. Administrators share PolicyVersion and Permissions.
type Snapshot struct {
	WriteFields map[string][]string `json:"admin_write_fields,omitempty"`
	Version     int64               `json:"policy_version"`
	Permissions []string            `json:"permissions"`
	Pages       []string            `json:"admin_pages"`
	Features    map[string]bool     `json:"admin_features,omitempty"`
}

func (s Subject) Snapshot() Snapshot {
	permissions := append([]string{}, s.Permissions...)
	if s.Role == SuperAdmin {
		for _, p := range Catalogue() {
			permissions = append(permissions, p.Key)
		}
	}
	pages := []string{}
	for _, entry := range PagePermissions() {
		page, permission := entry[0], entry[1]
		if s.Role == SuperAdmin || (permission != "super_admin" && s.Can(permission)) {
			pages = append(pages, page)
		}
	}
	if s.Role == Admin && !s.Can("settings.general.read") && s.Can("settings.content.read") {
		pages = append(pages, "/admin/settings")
	}
	return Snapshot{Version: s.PolicyVersion, Permissions: permissions, Pages: pages, WriteFields: EditableRequestFields(s)}
}

// Page order follows the existing navigation, and is also the landing priority.
func PagePermissions() [][2]string {
	return [][2]string{
		{"/admin/dashboard", "dashboard.read"}, {"/admin/ops", "ops.read"}, {"/admin/users", "users.read"}, {"/admin/groups", "groups.read"},
		{"/admin/channels/pricing", "channels.read"}, {"/admin/channels/monitor", "channel_monitor.read"}, {"/admin/subscriptions", "subscriptions.read"},
		{"/admin/accounts", "accounts.read"}, {"/admin/announcements", "announcements.read"}, {"/admin/proxies", "proxies.read"},
		{"/admin/redeem", "redeem_codes.read"}, {"/admin/promo-codes", "promo_codes.read"}, {"/admin/usage", "usage.read"},
		{"/admin/orders", "orders.read"}, {"/admin/orders/dashboard", "orders.read"}, {"/admin/orders/plans", "plans.read"},
		{"/admin/affiliates/invites", "affiliates.read"}, {"/admin/affiliates/rebates", "affiliates.read"}, {"/admin/affiliates/transfers", "affiliates.read"},
		{"/admin/risk-control", "risk.read"}, {"/admin/prompt-audit", "prompt_audit.read"}, {"/admin/audit-logs", "audit.read"},
		{"/admin/plugins", "super_admin"}, {"/admin/settings", "settings.general.read"},
	}
}

func IsSensitive(permissions []string) bool {
	for _, permission := range Catalogue() {
		if permission.Sensitive {
			for _, key := range permissions {
				if key == permission.Key {
					return true
				}
			}
		}
	}
	return false
}

func HasPrefixPermission(s Subject, prefix string) bool {
	if s.Role == SuperAdmin {
		return true
	}
	for _, p := range s.Permissions {
		if strings.HasPrefix(p, prefix) {
			return true
		}
	}
	return false
}
