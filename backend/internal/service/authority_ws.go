package service

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/gateway/execution"
	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	native "github.com/Wei-Shaw/sub2api/internal/providers/openai"
)

// nativeWSHandshake is an immutable observation of one successful handshake.
// It grants nothing on its own. Only the connection owner can publish it after
// Dial succeeds; every frame also needs current principal/resource/lease proof.
// No bearer, private key, random epoch or guessed lifetime is retained here.
type nativeWSHandshake struct {
	resourceID                            int64
	owner                                 model.ResourceOwner
	selectedCredentials, ownerCredentials [32]byte
	selectedPolicy, ownerPolicy           [32]byte
	grant                                 [32]byte
	target                                native.WSTarget
	succeeded                             bool
}

func (nativeWSHandshake) String() string   { return "native WS handshake (redacted)" }
func (nativeWSHandshake) GoString() string { return "native WS handshake (redacted)" }

// snapshotNativeWSHandshake runs after per-dial signing and before Dial. It is
// also used by the existing pool's background prewarm; it authorizes no frame.
func (s *OpenAIGatewayService) snapshotNativeWSHandshake(ctx context.Context, selected *Account, endpoint, proxy string, headers http.Header) (_ nativeWSHandshake, returnErr error) {
	defer func() {
		if returnErr != nil {
			returnErr = nativeDispatchError("WS handshake", returnErr)
		}
	}()
	var empty nativeWSHandshake
	if s == nil || ctx == nil || selected == nil || nativeDispatchNil(s.accountRepo) {
		return empty, execution.ErrDispatchUnavailable
	}
	resource, err := s.accountRepo.GetByID(ctx, selected.ID)
	if err != nil {
		return empty, err
	}
	if resource == nil || resource.ID != selected.ID || !resource.IsSchedulable() || nativeDispatchWirePolicy(resource) != nativeDispatchWirePolicy(selected) {
		return empty, ErrNativeDispatchScopeChanged
	}
	owner, err := resolveCredentialAccount(ctx, s.accountRepo, resource)
	if err != nil {
		return empty, err
	}
	if owner == nil || !owner.IsActive() || owner.IsVendorArchived() || owner.VendorPausedAt != nil || !SameProxyOwner(owner.VendorID, resource.VendorID) {
		return empty, ErrNativeDispatchGrantUnproven
	}
	expected, err := s.buildOpenAIResponsesWSURL(resource)
	if err != nil {
		return empty, err
	}
	if expected != endpoint || resolveAccountProxyURL(resource) != proxy {
		return empty, ErrNativeDispatchScopeChanged
	}
	if err := validateNativeWSHandshakeAuthentication(owner, headers, time.Now()); err != nil {
		return empty, err
	}
	return nativeWSHandshakeSnapshot(resource, owner, endpoint, proxy, headers)
}

// nativeWSReuseScope partitions reusable connections by persisted credential
// generations and account-owned routing policy, not by expiring dial assertions.
// It never grants a send: the real held-handshake authority still revalidates.
func nativeWSReuseScope(resource, owner *Account) ([32]byte, error) {
	if resource == nil || owner == nil {
		return [32]byte{}, ErrNativeDispatchGrantUnproven
	}
	selected, err := vendorSupplyCredentialDigest(resource.Credentials)
	if err != nil {
		return [32]byte{}, err
	}
	credential, err := vendorSupplyCredentialDigest(owner.Credentials)
	if err != nil {
		return [32]byte{}, err
	}
	value, err := json.Marshal([]any{"native-ws-reuse-v1", resource.ID, recoveryOwner(owner), selected, credential,
		nativeDispatchWirePolicy(resource), nativeDispatchWirePolicy(owner)})
	if err != nil {
		return [32]byte{}, err
	}
	return sha256.Sum256(value), nil
}

func nativeWSHandshakeSnapshot(resource, owner *Account, endpoint, proxy string, headers http.Header) (nativeWSHandshake, error) {
	var empty nativeWSHandshake
	selectedDigest, err := vendorSupplyCredentialDigest(resource.Credentials)
	if err != nil {
		return empty, err
	}
	ownerDigest, err := vendorSupplyCredentialDigest(owner.Credentials)
	if err != nil {
		return empty, err
	}
	selectedHash := sha256.Sum256([]byte(selectedDigest))
	ownerHash := sha256.Sum256([]byte(ownerDigest))
	headerBytes, err := json.Marshal(headers)
	if err != nil {
		return empty, err
	}
	fact := nativeWSHandshake{resourceID: resource.ID, owner: recoveryOwner(owner), selectedCredentials: selectedHash, ownerCredentials: ownerHash, selectedPolicy: nativeDispatchWirePolicy(resource), ownerPolicy: nativeDispatchWirePolicy(owner), target: native.WSTarget{URL: endpoint, ProxyURL: proxy, HandshakeDigest: sha256.Sum256(headerBytes)}}
	return fact.seal()
}

func (fact nativeWSHandshake) seal() (nativeWSHandshake, error) {
	raw, err := json.Marshal([]any{"native-ws-held-grant-v1", fact.resourceID, fact.owner, fact.selectedCredentials, fact.ownerCredentials, fact.selectedPolicy, fact.ownerPolicy, fact.target})
	if err != nil {
		return nativeWSHandshake{}, err
	}
	fact.grant = sha256.Sum256(raw)
	return fact, nil
}

func validateNativeWSHandshakeAuthentication(owner *Account, headers http.Header, now time.Time) error {
	// Cloud configuration selects a Responses executor; it is not a different
	// credential kind and cannot invalidate an independently selected native
	// handshake. Actual native mode/resource/lease authorization is revalidated
	// by the registered attempt, not inferred here from the account flag.
	if owner == nil || owner.IsExcelOAuth() {
		return ErrNativeDispatchGrantUnproven
	}
	var auth string
	count := 0
	for name, values := range headers {
		if strings.EqualFold(name, "Authorization") {
			count++
			if len(values) != 1 {
				return ErrNativeDispatchGrantUnproven
			}
			auth = values[0]
		}
	}
	if count != 1 || auth == "" {
		return ErrNativeDispatchGrantUnproven
	}
	if owner.IsOpenAIOAuthLike() {
		// Header construction and the final durable read can race a workspace or
		// FedRAMP replacement even when the bearer itself is unchanged. Bind the
		// actual handshake to the same current credential owner, not just its token.
		expected := make(http.Header)
		setOpenAIChatGPTAccountHeaders(expected, owner)
		for _, field := range []string{"chatgpt-account-id", "x-openai-fedramp"} {
			actual, count := "", 0
			for name, values := range headers {
				if strings.EqualFold(name, field) {
					count++
					if len(values) != 1 {
						return ErrNativeDispatchGrantUnproven
					}
					actual = values[0]
				}
			}
			if count > 1 || actual != expected.Get(field) {
				return ErrNativeDispatchGrantUnproven
			}
		}
	}
	if owner.IsOpenAIAgentIdentity() {
		key, err := agentIdentityKeyFromAccount(owner)
		if err != nil {
			return err
		}
		if !strings.HasPrefix(auth, "AgentAssertion ") {
			return ErrNativeDispatchGrantUnproven
		}
		encoded, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(auth, "AgentAssertion "))
		if err != nil {
			return ErrNativeDispatchGrantUnproven
		}
		var envelope struct {
			Runtime   string `json:"agent_runtime_id"`
			Task      string `json:"task_id"`
			Timestamp string `json:"timestamp"`
			Signature string `json:"signature"`
		}
		if json.Unmarshal(encoded, &envelope) != nil || envelope.Runtime != key.runtimeID || envelope.Task == "" || envelope.Task != key.taskID {
			return ErrNativeDispatchGrantUnproven
		}
		if _, err := time.Parse(time.RFC3339, envelope.Timestamp); err != nil {
			return ErrNativeDispatchGrantUnproven
		}
		signature, err := base64.StdEncoding.DecodeString(envelope.Signature)
		if err != nil || !ed25519.Verify(key.privateKey.Public().(ed25519.PublicKey), []byte(envelope.Runtime+":"+envelope.Task+":"+envelope.Timestamp), signature) {
			return ErrNativeDispatchGrantUnproven
		}
		return nil
	}
	token := owner.GetOpenAIAccessToken()
	if owner.Type == AccountTypeAPIKey {
		token = strings.TrimSpace(owner.GetOpenAIProtocolAPIKey())
	} else if owner.Type != AccountTypeOAuth && owner.Type != AccountTypeSetupToken {
		return ErrNativeDispatchGrantUnproven
	}
	if token == "" || sha256.Sum256([]byte(auth)) != sha256.Sum256([]byte("Bearer "+token)) {
		return ErrNativeDispatchGrantUnproven
	}
	expiry := owner.GetCredentialAsTime("expires_at")
	if owner.Type != AccountTypeAPIKey && expiry != nil && !expiry.IsZero() && !now.Before(*expiry) {
		return ErrNativeDispatchGrantUnproven
	}
	if owner.Type == AccountTypeOAuth && (expiry == nil || expiry.IsZero()) && (!owner.IsOpenAIPersonalAccessToken() || strings.TrimSpace(owner.GetCredential("expires_at")) != "") {
		return ErrNativeDispatchGrantUnproven
	}
	return nil
}

func (h nativeWSHandshake) matches(resource, owner *Account) bool {
	if !h.succeeded || resource == nil || owner == nil || h.resourceID != resource.ID || h.owner != recoveryOwner(resource) || h.owner != recoveryOwner(owner) || h.selectedPolicy != nativeDispatchWirePolicy(resource) || h.ownerPolicy != nativeDispatchWirePolicy(owner) || h.target.ProxyURL != resolveAccountProxyURL(resource) {
		return false
	}
	selected, err := vendorSupplyCredentialDigest(resource.Credentials)
	if err != nil {
		return false
	}
	credential, err := vendorSupplyCredentialDigest(owner.Credentials)
	if err != nil {
		return false
	}
	// Full durable credentials detect existing rotation/replacement rules. The
	// clock passing bearer expiry alone is intentionally NOT a held-socket revoke.
	return h.selectedCredentials == sha256.Sum256([]byte(selected)) && h.ownerCredentials == sha256.Sum256([]byte(credential))
}

func (a *NativeDispatchAuthority) revalidateWS(ctx context.Context, id model.PreparedIdentity, h nativeWSHandshake, live func() bool, check nativeDispatchResourceCheck) (execution.DispatchValidation, error) {
	var denied execution.DispatchValidation
	if !a.ready() || ctx == nil || live == nil || !live() {
		return denied, ErrNativeDispatchGrantUnproven
	}
	r := a.request
	if !id.Valid() || id.OperationID != r.OperationID || id.UserID != r.UserID || id.APIKeyID != r.APIKeyID || id.GroupID != r.GroupID || id.GroupBound != r.GroupBound || id.Mode != VendorOpenAIModeNative || id.Transport != "ws" || id.GrantDigest != h.grant {
		return denied, execution.ErrInvalidPreparedIdentity
	}
	key, sub, deadline, err := a.loadKey(ctx)
	if err != nil {
		return denied, err
	}
	resource, owner, err := a.loadResource(ctx, id.ResourceID)
	if err != nil {
		return denied, err
	}
	if check != nil {
		if err := check(ctx, resource); err != nil {
			return denied, err
		}
	}
	if !h.matches(resource, owner) || id.Owner != h.owner {
		return denied, ErrNativeDispatchGrantUnproven
	}
	lease, err := a.leases.RevalidateNativeDispatchLeases(ctx, id)
	if err != nil {
		return denied, err
	}
	if lease.Fingerprint == ([32]byte{}) || (!lease.ValidUntil.IsZero() && !a.now().Before(lease.ValidUntil)) {
		return denied, ErrNativeDispatchLeaseUnproven
	}
	deadline = nativeDispatchEarlier(deadline, lease.ValidUntil)
	if resource.AutoPauseOnExpired && resource.ExpiresAt != nil {
		deadline = nativeDispatchEarlier(deadline, *resource.ExpiresAt)
	}
	if owner.AutoPauseOnExpired && owner.ExpiresAt != nil {
		deadline = nativeDispatchEarlier(deadline, *owner.ExpiresAt)
	}
	if !deadline.IsZero() && !a.now().Before(deadline) {
		return denied, execution.ErrDispatchExpired
	}
	proof, err := a.authorizationFingerprint(key, sub, resource, NativeDispatchCredential{Owner: h.owner, GrantDigest: h.grant})
	if err != nil {
		return denied, err
	}
	if err := ctx.Err(); err != nil {
		return denied, context.Cause(ctx)
	}
	if !live() {
		return denied, ErrNativeDispatchGrantUnproven
	}
	return execution.DispatchValidation{Authorization: proof, Leases: lease.Fingerprint, ValidUntil: deadline}, nil
}

// completeNativeWSHandshake records the successful upgrade, not the requested
// URL before the controlled client's existing redirect handling. Redirects may
// not turn a credential-owned socket into an anonymous or changed-tenant grant.
func completeNativeWSHandshake(fact nativeWSHandshake, connection openAIWSClientConn, sent http.Header) (nativeWSHandshake, error) {
	observation, ok := connection.(interface{ takeNativeHandshakeRequest() (string, http.Header) })
	if !ok {
		return nativeWSHandshake{}, nativeDispatchError("WS handshake observation", ErrNativeDispatchGrantUnproven)
	}
	endpoint, actual := observation.takeNativeHandshakeRequest()
	if endpoint == "" {
		return nativeWSHandshake{}, nativeDispatchError("WS handshake target", ErrNativeDispatchGrantUnproven)
	}
	for _, field := range []string{"Authorization", "chatgpt-account-id", "x-openai-fedramp"} {
		value, count := "", 0
		for name, values := range actual {
			if strings.EqualFold(name, field) {
				count++
				if len(values) != 1 {
					return nativeWSHandshake{}, ErrNativeDispatchGrantUnproven
				}
				value = values[0]
			}
		}
		if count > 1 || value != sent.Get(field) {
			return nativeWSHandshake{}, nativeDispatchError("WS handshake credential changed in transport", ErrNativeDispatchGrantUnproven)
		}
	}
	encoded, err := json.Marshal(actual)
	if err != nil {
		return nativeWSHandshake{}, err
	}
	fact.target.URL = endpoint
	fact.target.HandshakeDigest = sha256.Sum256(encoded)
	fact.succeeded = true
	return fact.seal()
}

// nativeWSHTTPBridgeRegistration authorizes only the selected bridge's HTTP
// preparation under its original client-WS operation. It neither changes the
// root transport nor grants a credential, lease or second attempt budget.
type nativeWSHTTPBridgeRegistration struct {
	operation  *nativeDispatchOperation
	resourceID int64
}
type nativeWSHTTPBridgeKey struct{}

func withNativeWSHTTPBridge(ctx context.Context, account *Account) (context.Context, error) {
	op := nativeDispatchFromContext(ctx)
	if op == nil || account == nil {
		return nil, execution.ErrDispatchUnavailable
	}
	if op.request.Transport == "http" {
		return ctx, nil
	}
	if op.request.Transport != "ws" {
		return nil, execution.ErrInvalidPreparedIdentity
	}
	return context.WithValue(ctx, nativeWSHTTPBridgeKey{}, nativeWSHTTPBridgeRegistration{operation: op, resourceID: account.ID}), nil
}

func (o *nativeDispatchOperation) revalidateWSHTTPBridge(ctx context.Context, id model.PreparedIdentity) (execution.DispatchValidation, error) {
	if ctx == nil {
		return execution.DispatchValidation{}, execution.ErrDispatchUnavailable
	}
	registration, ok := ctx.Value(nativeWSHTTPBridgeKey{}).(nativeWSHTTPBridgeRegistration)
	if !ok || o == nil || o.authority == nil || registration.operation != o || registration.resourceID != id.ResourceID || o.request.Transport != "ws" || id.Transport != "http" || id.Purpose != model.DispatchGeneration {
		return execution.DispatchValidation{}, nativeDispatchError("WS HTTP bridge registration", execution.ErrInvalidPreparedIdentity)
	}
	// The original HTTP prepare path records the complete sealed identity only
	// after validating the actual resource, headers, credential and target. The
	// marker alone cannot authorize an unprepared or substituted request.
	o.mu.Lock()
	_, sealed := o.policies[id]
	o.mu.Unlock()
	if !sealed || nativeDispatchFromContext(ctx) != o {
		return execution.DispatchValidation{}, nativeDispatchError("WS HTTP bridge preparation", execution.ErrInvalidPreparedIdentity)
	}
	// Keep the same authority and root fingerprint. Its normal fresh principal,
	// resource, HTTP credential and capacity checks still run at every gate.
	return o.authority.revalidatePreparedTransport(ctx, id, "http", o.preparedResourceCheck(id))
}
