package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/gateway/execution"
	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
)

// NativeDispatchCredential is prepared from the actual credential source. Its
// secret stays in this boundary, never in model.PreparedIdentity or logs. The
// provider seals GrantDigest along with the final wire request; it still needs
// an execution-issued permit to send. Copying this value grants no send right.
type NativeDispatchCredential struct {
	Owner            model.ResourceOwner
	GrantDigest      [32]byte
	ValidUntil       time.Time
	token, tokenType string
}

// Bearer returns the exact value/type obtained for request preparation. Do not
// persist or log it. Type is the existing oauth/apikey transport classification.
func (c NativeDispatchCredential) Bearer() (string, string) { return c.token, c.tokenType }
func (NativeDispatchCredential) String() string             { return "native dispatch credential (redacted)" }
func (NativeDispatchCredential) GoString() string           { return "native dispatch credential (redacted)" }

// ResolveCredential is called by MAIN's operation root before provider Prepare.
// It checks the authenticated principal and current resource before obtaining a
// token, so a revoked operation cannot trigger refresh work. It never grants a
// dispatch permit or consumes an attempt, and never freezes authorization.
func (a *NativeDispatchAuthority) ResolveCredential(ctx context.Context, resourceID int64) (NativeDispatchCredential, error) {
	if !a.ready() || ctx == nil {
		return NativeDispatchCredential{}, execution.ErrDispatchUnavailable
	}
	if err := ctx.Err(); err != nil {
		return NativeDispatchCredential{}, context.Cause(ctx)
	}
	if _, _, _, err := a.loadKey(ctx); err != nil {
		return NativeDispatchCredential{}, err
	}
	_, owner, err := a.loadResource(ctx, resourceID)
	if err != nil {
		return NativeDispatchCredential{}, err
	}
	return a.resolveCredential(ctx, owner, true)
}

func (a *NativeDispatchAuthority) resolveCredential(ctx context.Context, owner *Account, prepare bool) (NativeDispatchCredential, error) {
	var empty NativeDispatchCredential
	if owner == nil || owner.Platform != PlatformOpenAI || owner.IsShadow() || owner.IsOpenAIAgentIdentity() || owner.IsExcelOAuth() || owner.IsOpenAICloudConfigured() {
		return empty, nativeDispatchError("unsupported native credential", ErrNativeDispatchGrantUnproven)
	}
	var token, tokenType string
	var expiry time.Time
	switch owner.Type {
	case AccountTypeAPIKey:
		token, tokenType = strings.TrimSpace(owner.GetOpenAIProtocolAPIKey()), "apikey"
	case AccountTypeSetupToken:
		if !owner.IsOpenAIOAuthLike() {
			return empty, nativeDispatchError("setup credential", ErrNativeDispatchGrantUnproven)
		}
		token, tokenType = owner.GetOpenAIAccessToken(), "oauth"
		if at := owner.GetCredentialAsTime("expires_at"); at != nil {
			expiry = *at
		}
	case AccountTypeOAuth:
		if nativeDispatchNil(a.tokens) {
			return empty, nativeDispatchError("token provider", execution.ErrDispatchUnavailable)
		}
		fresh := owner
		token = owner.GetOpenAIAccessToken()
		if prepare {
			var err error
			token, err = a.tokens.GetAccessToken(ctx, owner)
			if err != nil {
				return empty, nativeDispatchError("token provider", err)
			}
			// The provider can refresh or return a cache entry during preparation.
			// Revalidation never calls it: the final wire bearer is already sealed.
			fresh, err = a.accounts.GetByID(ctx, owner.ID)
			if err != nil {
				return empty, nativeDispatchError("refreshed credential lookup", err)
			}
			if fresh == nil || fresh.ID != owner.ID || fresh.IsShadow() || fresh.Type != owner.Type || fresh.Platform != owner.Platform || !SameProxyOwner(fresh.VendorID, owner.VendorID) || !fresh.IsActive() || fresh.IsVendorArchived() || fresh.VendorPausedAt != nil || fresh.IsOpenAIAgentIdentity() || fresh.IsExcelOAuth() || fresh.IsOpenAICloudConfigured() {
				return empty, nativeDispatchError("refreshed owner", ErrNativeDispatchGrantUnproven)
			}
			if sha256.Sum256([]byte(token)) != sha256.Sum256([]byte(fresh.GetOpenAIAccessToken())) {
				return empty, nativeDispatchError("token generation", ErrNativeDispatchGrantUnproven)
			}
		}

		at := fresh.GetCredentialAsTime("expires_at")
		if at == nil || at.IsZero() {
			// Only the durable PAT classification carries the existing no-refresh,
			// optional-expiry contract. Read it after any provider-side refresh; an
			// old classification must not authorize the newly loaded owner. A saved
			// malformed/zero expiry is not equivalent to an omitted expiry.
			if !fresh.IsOpenAIPersonalAccessToken() || strings.TrimSpace(fresh.GetCredential("expires_at")) != "" {
				return empty, nativeDispatchError("token expiry unknown", ErrNativeDispatchGrantUnproven)
			}
		} else {
			expiry = *at
		}
		if err := a.checks.ValidateVendorSupply(ctx, fresh, nil); err != nil {
			return empty, nativeDispatchError("refreshed owner supply", err)
		}
		if err := a.checks.ValidateVendorAuthorizationMode(ctx, fresh, nil, VendorOpenAIModeNative); err != nil {
			return empty, nativeDispatchError("refreshed owner mode", err)
		}
		owner, tokenType = fresh, "oauth"
	default:
		return empty, nativeDispatchError("unsupported native credential", ErrNativeDispatchGrantUnproven)
	}
	if strings.TrimSpace(token) == "" || (!expiry.IsZero() && !a.now().Before(expiry)) {
		return empty, nativeDispatchError("credential unavailable", ErrNativeDispatchGrantUnproven)
	}
	if err := ctx.Err(); err != nil {
		return empty, context.Cause(ctx)
	}
	// This existing helper covers the FULL persisted credential map. A stable
	// account/organization identity alone cannot detect credential replacement.
	credentialDigest, err := vendorSupplyCredentialDigest(owner.Credentials)
	if err != nil {
		return empty, nativeDispatchError("credential identity", err)
	}
	raw, err := json.Marshal([]any{"native-grant-v1", recoveryOwner(owner), owner.VendorID, owner.Platform, owner.Type, codexAccountIdentityNamespace(owner), credentialDigest, tokenType, sha256.Sum256([]byte(token))})
	if err != nil {
		return empty, nativeDispatchError("credential fingerprint", err)
	}
	return NativeDispatchCredential{Owner: recoveryOwner(owner), GrantDigest: sha256.Sum256(raw), ValidUntil: expiry, token: token, tokenType: tokenType}, nil
}
