package middleware

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/authz"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

const permissionStepUpKey = "admin_permission_step_up"

func bindManagementSubject(c *gin.Context, settings *service.SettingService, reader jwtUserReader, user *service.User, sessionID string, auth *service.AuthService, claims *service.JWTClaims) bool {
	management := strings.HasPrefix(c.FullPath(), "/api/v1/admin/") || c.FullPath() == "/api/v1/pages"
	actor := authz.Subject{UserID: user.ID, Role: user.Role, SessionGeneration: user.SessionGeneration}
	if management {
		var err error
		actor, err = settings.ManagementSubject(c.Request.Context(), user)
		if err != nil {
			AbortWithError(c, 503, "ADMIN_POLICY_UNAVAILABLE", "Administrator permissions are unavailable")
			return false
		}
		c.Header("X-Admin-Policy-Version", strconv.FormatInt(actor.PolicyVersion, 10))
		c.Header("Cache-Control", "no-store")
	}
	actor.ManagementRequest = management
	actor.AuthMethod = service.AuditAuthMethodJWT
	if claims == nil && management {
		actor.AuthMethod = service.AuditAuthMethodAdminAPIKey
	}
	actor.SessionID = sessionID
	method, path := c.Request.Method, c.FullPath()
	ctx := authz.WithSubject(c.Request.Context(), actor)
	ctx = authz.WithRevalidation(ctx, func(ctx context.Context) error {
		current, err := reader.GetByID(ctx, actor.UserID)
		if err != nil || current == nil || !current.IsActive() || current.Role != actor.Role || current.SessionGeneration != actor.SessionGeneration {
			return service.ErrTokenRevoked
		}
		if auth != nil && claims != nil {
			if claims.ExpiresAt != nil && !time.Now().Before(claims.ExpiresAt.Time) {
				return service.ErrTokenRevoked
			}
			if err := auth.ValidateAccessSession(ctx, claims, current); err != nil {
				return err
			}
		}
		if actor.AuthMethod == service.AuditAuthMethodAdminAPIKey {
			key, err := settings.GetAdminAPIKey(ctx)
			if err != nil || key == "" || fmt.Sprintf("machine:%x", sha256.Sum256([]byte(key))) != actor.SessionID {
				return service.ErrTokenRevoked
			}
		}
		if management {
			latest, err := settings.ManagementSubject(ctx, current)
			if err != nil {
				return err
			}
			rule, declared := authz.RuleFor(method, path)
			if !declared || !rule.Allows(latest) {
				return service.ErrAdminPermissionDenied
			}
		}
		return nil
	})
	c.Request = c.Request.WithContext(ctx)
	return true
}

// Every authentication form (JWT, websocket JWT and the root machine key)
// converges here. Endpoint existence alone never grants a capability.
func continueAdminRequest(c *gin.Context, users *service.UserService, audit *service.AuditLogService) {
	actor, ok := authz.FromContext(c.Request.Context())
	rule, declared := authz.RuleFor(c.Request.Method, c.FullPath())
	if !ok || !declared || !rule.Allows(actor) {
		denyManagement(c, audit, "ADMIN_PERMISSION_DENIED", "This operation is not permitted")
		return
	}
	if rule.RequestSchema != "" {
		body, err := parseAdminBody(c)
		if err != nil {
			AbortWithError(c, 400, "INVALID_REQUEST", err.Error())
			return
		}
		sensitive, err := authz.ValidateRequestFields(rule.RequestSchema, body, actor)
		if err != nil {
			denyManagement(c, audit, "ADMIN_PERMISSION_DENIED", err.Error())
			return
		}
		c.Set(permissionStepUpKey, sensitive)
	}
	if actor.Role == service.RoleAdmin {
		if err := checkAdminRequest(c, users, actor, rule); err != nil {
			denyManagement(c, audit, "ADMIN_PERMISSION_DENIED", err.Error())
			return
		}
	} else if err := markSuperAdminStepUp(c, users, rule); err != nil {
		AbortWithError(c, 400, "INVALID_REQUEST", err.Error())
		return
	}
	if c.FullPath() == "/api/v1/admin/usage" && c.Query("export") == "true" {
		c.Set(permissionStepUpKey, true)
	}
	c.Set(permissionStepUpKey, c.GetBool(permissionStepUpKey) || authz.IsSensitive(rule.All))
	original := c.Writer
	ctx, cancel := context.WithCancel(c.Request.Context())
	c.Request = c.Request.WithContext(ctx)
	defer cancel()
	writer := &managementStreamWriter{ResponseWriter: original, context: ctx, cancel: cancel}
	c.Writer = writer
	defer func() { c.Writer = original }()

	c.Next()
}

// Root bypasses permission restrictions, not the shared action-level step-up
// switch. Inspect only existing JSON personnel/price actions, preserving native
// upload and system-management request bodies.
func markSuperAdminStepUp(c *gin.Context, users *service.UserService, rule authz.RouteRule) error {
	if !rule.Mutates {
		return nil
	}
	path := c.FullPath()
	personnel := strings.HasPrefix(path, "/api/v1/admin/users")
	if !personnel {
		return nil
	}
	body, err := parseAdminBody(c)
	if err != nil {
		return err
	}
	for key, value := range body {
		field := normalizedPermissionField(key)
		if personnel && ((field == "role" && value == service.RoleAdmin) || field == "balance" || field == "grouprates") {
			c.Set(permissionStepUpKey, true)
		}
	}
	if !personnel || users == nil {
		return nil
	}
	ids := []int64{}
	if id := c.Param("id"); id != "" {
		n, err := strconv.ParseInt(id, 10, 64)
		if err == nil {
			ids = append(ids, n)
		}
	}
	for key, value := range body {
		if normalizedPermissionField(key) != "userids" {
			continue
		}
		if values, ok := value.([]any); ok {
			for _, v := range values {
				n, err := strconv.ParseInt(fmt.Sprint(v), 10, 64)
				if err == nil {
					ids = append(ids, n)
				}
			}
		}
	}
	if len(ids) > 1000 {
		return fmt.Errorf("Too many target users")
	}
	for _, id := range ids {
		target, err := users.GetByID(c.Request.Context(), id)
		if err != nil {
			return err
		}
		if target.IsStaff() {
			c.Set(permissionStepUpKey, true)
		}
	}
	return nil
}

func denyManagement(c *gin.Context, audit *service.AuditLogService, code, message string) {
	actor, _ := authz.FromContext(c.Request.Context())
	uid := actor.UserID
	entry := &service.AuditLog{CreatedAt: time.Now().UTC(), ActorUserID: &uid, ActorRole: actor.Role, ActorEmail: c.GetString(ContextKeyAuthEmail), AuthMethod: c.GetString("auth_method"), Action: "authorization.denied", Method: c.Request.Method, Path: c.FullPath(), ClientIP: SecurityClientIP(c), StatusCode: 403}
	if c.GetBool("admin_protected_target") {
		entry.Visibility = service.RoleSuperAdmin
	}
	if audit != nil {
		audit.Record(entry)
	}
	AbortWithError(c, 403, code, message)
}

// Reuse the existing switch-controlled step-up middleware and factor stores.
func AdminPermissionStepUp(stepUp StepUpAuthMiddleware) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.GetBool(permissionStepUpKey) {
			if stepUp == nil {
				AbortWithError(c, 503, "STEP_UP_UNAVAILABLE", "Verification is unavailable")
				return
			}
			gin.HandlerFunc(stepUp)(c)
			return
		}
		c.Next()
	}
}

func normalizedPermissionField(key string) string { return authz.FieldName(key) }

func parseAdminBody(c *gin.Context) (map[string]any, error) {
	if c.Request.Body == nil || c.Request.ContentLength == 0 {
		return map[string]any{}, nil
	}
	if !strings.Contains(c.GetHeader("Content-Type"), "application/json") {
		return nil, fmt.Errorf("A JSON request body is required")
	}
	const maxBody = 8 << 20
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, maxBody+1))
	if err != nil {
		return nil, fmt.Errorf("Cannot read request body")
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(body))
	if len(body) > maxBody {
		return nil, fmt.Errorf("Request body is too large")
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return map[string]any{}, nil
	}
	result := map[string]any{}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return nil, fmt.Errorf("A JSON object is required")
	}
	seen := map[string]bool{}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil, fmt.Errorf("Invalid JSON request body")
		}
		key, ok := token.(string)
		if !ok {
			return nil, fmt.Errorf("Invalid JSON field")
		}
		normalized := authz.FieldName(key)
		if seen[normalized] {
			return nil, fmt.Errorf("Duplicate request field: %s", key)
		}
		seen[normalized] = true
		value, err := decodeAdminValue(decoder, 0)
		if err != nil {
			return nil, fmt.Errorf("Invalid JSON request body")
		}
		result[key] = value
	}
	if token, err = decoder.Token(); err != nil || token != json.Delim('}') {
		return nil, fmt.Errorf("Invalid JSON request body")
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return nil, fmt.Errorf("Invalid JSON request body")
	}
	return result, nil
}

// Match Go JSON binding's case folding at every nesting level. Otherwise a
// repeated credential/target field could be checked under a different value.
func decodeAdminValue(decoder *json.Decoder, depth int) (any, error) {
	if depth > 64 {
		return nil, fmt.Errorf("JSON nesting is too deep")
	}
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	delim, container := token.(json.Delim)
	if !container {
		return token, nil
	}
	switch delim {
	case '{':
		value := map[string]any{}
		seen := map[string]bool{}
		for decoder.More() {
			name, err := decoder.Token()
			if err != nil {
				return nil, err
			}
			key, ok := name.(string)
			if !ok {
				return nil, fmt.Errorf("Invalid field")
			}
			normalized := authz.FieldName(key)
			if seen[normalized] {
				return nil, fmt.Errorf("Duplicate field")
			}
			seen[normalized] = true
			item, err := decodeAdminValue(decoder, depth+1)
			if err != nil {
				return nil, err
			}
			value[key] = item
		}
		end, err := decoder.Token()
		if err != nil || end != json.Delim('}') {
			return nil, fmt.Errorf("Invalid object")
		}
		return value, nil
	case '[':
		value := []any{}
		for decoder.More() {
			item, err := decodeAdminValue(decoder, depth+1)
			if err != nil {
				return nil, err
			}
			value = append(value, item)
		}
		end, err := decoder.Token()
		if err != nil || end != json.Delim(']') {
			return nil, fmt.Errorf("Invalid array")
		}
		return value, nil
	}
	return nil, fmt.Errorf("Invalid JSON")
}

func checkAdminRequest(c *gin.Context, users *service.UserService, actor authz.Subject, rule authz.RouteRule) error {
	path := c.FullPath()
	if !actor.Can("billing.cost.read") {
		for _, key := range []string{"sort_by", "sort", "order_by"} {
			field := c.Query(key)
			if authz.IsAccountCostField(field, strings.HasPrefix(path, "/api/v1/admin/accounts")) {
				return fmt.Errorf("Sorting by cost is not permitted")
			}
		}
	}

	if path == "/api/v1/admin/usage" && c.Query("export") == "true" {
		if !actor.Can("usage.export") {
			return fmt.Errorf("Usage export is not permitted")
		}
		c.Set(permissionStepUpKey, true)
	}
	if path == "/api/v1/admin/accounts/:id/usage" && c.Query("force") == "true" && !actor.Can("accounts.diagnostics") {
		return fmt.Errorf("Active probing requires accounts.diagnostics")
	}
	if users == nil {
		return fmt.Errorf("Authorization is unavailable")
	}
	var body map[string]any
	var err error
	if rule.Mutates || path == "/api/v1/admin/user-attributes/batch" {
		body, err = parseAdminBody(c)
		if err != nil {
			return err
		}
	}
	if strings.HasSuffix(path, "/settings") && c.Request.Method == http.MethodPut {
		for key := range body {
			permission := authz.SettingFieldPermission(key)
			if permission == "" || !actor.Can(permission+".manage") {
				return fmt.Errorf("Setting field %s is not permitted", key)
			}
		}
	}
	if strings.HasPrefix(path, "/api/v1/admin/users") && rule.Mutates {
		for key, value := range body {
			switch normalizedPermissionField(key) {
			case "permissions", "adminpermissions", "authorization", "adminrolepolicy":
				return fmt.Errorf("Individual permission overrides are not supported")
			case "role":
				role, _ := value.(string)
				if role != "" && role != service.RoleUser && role != service.RoleAdmin {
					c.Set("admin_protected_target", true)
					return fmt.Errorf("This role cannot be assigned")
				}
				if path == "/api/v1/admin/users" && role == service.RoleAdmin && !actor.Can("staff.manage") {
					return fmt.Errorf("Creating an administrator is not permitted")
				}
				if path == "/api/v1/admin/users" && role == service.RoleAdmin {
					c.Set(permissionStepUpKey, true)
				}
			case "balance", "grouprates":
				permission := "billing.balance.adjust"
				if normalizedPermissionField(key) == "grouprates" {
					permission = "billing.rates.update"
				}
				if !actor.Can(permission) {
					return fmt.Errorf("Changing financial field %s is not permitted", key)
				}
				c.Set(permissionStepUpKey, true)
			}
		}
	}

	if rule.Mutates && strings.HasPrefix(path, "/api/v1/admin/accounts") {
		for key, value := range body {
			if normalizedPermissionField(key) == "credentials" && authz.HasCredentialSecret(value) && !actor.Can("accounts.authorize") {
				return fmt.Errorf("Account authorization is required to change credentials")
			}
			if normalizedPermissionField(key) == "credentials" && authz.HasCredentialSecret(value) {
				c.Set(permissionStepUpKey, true)
			}
		}
		if err := users.CheckAdminAccountTransport(c.Request.Context(), path, c.Param("id"), body); err != nil {
			return err
		}
	}
	if rule.Mutates && strings.HasPrefix(path, "/api/v1/admin/proxies") && (c.Request.Method == http.MethodPut || c.Request.Method == http.MethodDelete || strings.HasSuffix(path, "/batch-delete")) {
		if c.Request.Method == http.MethodDelete || strings.HasSuffix(path, "/batch-delete") {
			body["__deleting_proxy"] = true
		}
		if err := users.CheckAdminProxyTransport(c.Request.Context(), c.Param("id"), body); err != nil {
			return err
		}
	}
	if strings.HasPrefix(path, "/api/v1/admin/users/:id") {
		id, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil || id <= 0 {
			return fmt.Errorf("Invalid user ID")
		}
		user, err := users.GetByID(c.Request.Context(), id)
		if err != nil {
			return fmt.Errorf("The requested user is unavailable")
		}
		if user.Role == service.RoleSuperAdmin {
			c.Set("admin_protected_target", true)
			return fmt.Errorf("The requested user is unavailable")
		}
		if rule.Mutates {
			selfChange := c.Request.Method == http.MethodDelete || strings.HasSuffix(path, "/balance")
			for key, value := range body {
				switch normalizedPermissionField(key) {
				case "role":
					selfChange = selfChange || (value != "" && value != user.Role)
				case "status":
					selfChange = selfChange || (value != "" && value != user.Status)
				case "balance":
					selfChange = selfChange || value != nil
				}
			}
			if actor.UserID == id && selfChange {
				return fmt.Errorf("Use another administrator for this action")
			}
			securityChange := false
			for key, value := range body {
				switch normalizedPermissionField(key) {
				case "resettotp":
					securityChange = securityChange || value == true
				case "password":
					securityChange = securityChange || !emptyPermissionValue(value)
				case "email":
					email, _ := value.(string)
					securityChange = securityChange || (email != "" && email != user.Email)
				case "role":
					role, _ := value.(string)
					if role != "" && role != user.Role {
						if role == service.RoleAdmin && !actor.Can("staff.manage") {
							return fmt.Errorf("Creating an administrator is not permitted")
						}
						c.Set(permissionStepUpKey, true)
					}
				}
			}
			if securityChange {
				if !actor.Can(authz.PersonnelPermission("security", user.Role, "")) {
					return fmt.Errorf("User security permission is required")
				}
				c.Set(permissionStepUpKey, true)
			}
			if user.Role == service.RoleAdmin {
				c.Set(permissionStepUpKey, true)
			}
		}
	}
	if rule.Mutates || path == "/api/v1/admin/user-attributes/batch" {
		action := ""
		if strings.HasPrefix(path, "/api/v1/admin/users") {
			switch {
			case path == "/api/v1/admin/users":
				action = "create"
			case strings.HasSuffix(path, "/auth-identities"):
				action = "security"
			case c.Request.Method == http.MethodDelete || strings.HasSuffix(path, "/batch-delete"):
				action = "delete"
			case path == "/api/v1/admin/users/:id" || strings.Contains(path, "/batch-") || strings.HasSuffix(path, "/replace-group") || strings.Contains(path, "/platform-quotas") || strings.HasSuffix(path, "/attributes"):
				action = "update"
			}
		}
		actor.PersonnelAction = action
		nextRole := ""
		for key, value := range body {
			if normalizedPermissionField(key) == "role" {
				nextRole, _ = value.(string)
			}
		}
		if action == "create" {
			role := nextRole
			if role == "" {
				role = service.RoleUser
			}
			if !actor.Can(authz.PersonnelPermission(action, role, "")) {
				return fmt.Errorf("Creating this role is not permitted")
			}
		}
		if action != "" && nextRole == service.RoleAdmin {
			c.Set(permissionStepUpKey, true)
		}
		targets, err := users.CheckAdminResourceTargets(c.Request.Context(), path, c.Param("id"), c.Param("user_id"), body)
		if err != nil {
			c.Set("admin_protected_target", true)
			return err
		}
		for _, id := range targets {
			if action == "" {
				break
			}
			target, err := users.GetByID(c.Request.Context(), id)
			if err != nil || target == nil || !actor.Can(authz.PersonnelPermission(action, target.Role, nextRole)) {
				return fmt.Errorf("Managing this role is not permitted")
			}
			if target.Role == service.RoleAdmin || (nextRole != "" && nextRole != target.Role) {
				c.Set(permissionStepUpKey, true)
			}
		}
		actor.TargetUserIDs = targets
		c.Request = c.Request.WithContext(authz.WithSubject(c.Request.Context(), actor))
	}
	return nil
}

func hasPermissionField(body map[string]any, key string) bool {
	for k := range body {
		if normalizedPermissionField(k) == normalizedPermissionField(key) {
			return true
		}
	}
	return false
}
func emptyPermissionValue(value any) bool {
	switch v := value.(type) {
	case nil:
		return true
	case string:
		return v == ""
	case json.Number:
		return v == "0" || v == "0.0"
	case map[string]any:
		return len(v) == 0
	case []any:
		return len(v) == 0
	}
	return false
}
