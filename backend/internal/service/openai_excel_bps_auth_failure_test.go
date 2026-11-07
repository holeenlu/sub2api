package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type excelAttachmentAdmissionRepo struct {
	AccountRepository
	failure *UpstreamFailoverError
}

func (r *excelAttachmentAdmissionRepo) ValidateExcelBPSDispatch(context.Context, *Account, *int64, string) error {
	return r.failure
}
func (*excelAttachmentAdmissionRepo) ValidateVendorSupply(context.Context, *Account, *int64) error {
	return nil
}

func TestExcelBPSAttachmentPreparationPreservesTrustedAdmissionFailure(t *testing.T) {
	for _, failure := range []*UpstreamFailoverError{
		newExcelBPSStateUnavailable(errors.New("state store unavailable"), true),
		newExcelBPSModelPermissionFailoverError(),
		newExcelBPSRateLimitedFailoverError("90"),
	} {
		t.Run(string(failure.Reason), func(t *testing.T) {
			failure.RequestNotSent = true
			svc := excelBPSGatewayTestService(nil)
			enableNativeAttachments(svc)
			svc.accountRepo = &excelAttachmentAdmissionRepo{failure: failure}
			svc.httpUpstream = &nativeAttachmentUpstream{do: func(*http.Request, string, int64, int) (*http.Response, error) {
				t.Fatal("local admission must fail before upstream I/O")
				return nil, nil
			}}
			body, _ := nativeGatewayBody(t)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			_, err := svc.Forward(t.Context(), c, excelAccount(), body)
			var actual *UpstreamFailoverError
			require.ErrorAs(t, err, &actual)
			require.Same(t, failure, actual)
			require.False(t, c.Writer.Written(), "do not turn admission into a written 502 attachment response")
		})
	}
}

type excelFailureTestRepo struct {
	*excelGrantTestRepo
	readErr error
	facts   []ExcelBPSFailure
}

func (r *excelFailureTestRepo) GetExcelCredentials(ctx context.Context, id int64) (string, error) {
	if r.readErr != nil {
		return "", r.readErr
	}
	return r.excelGrantTestRepo.GetExcelCredentials(ctx, id)
}

func (r *excelFailureTestRepo) DisableExcelBPSForFailure(_ context.Context, _ *Account, fact ExcelBPSFailure) (bool, error) {
	r.facts = append(r.facts, fact)
	return true, nil
}

func TestExcelAuthorizationFailureRequiresAuthoritativeEvidence(t *testing.T) {
	for _, tc := range []struct {
		name, reason string
		readErr      error
		refreshErr   error
		corrupt      bool
		missing      bool
	}{
		{name: "confirmed missing", missing: true, reason: "grant_missing"},
		{name: "database failure", readErr: errors.New("database unavailable")},
		{name: "decrypt failure", corrupt: true},
		{name: "network failure", refreshErr: context.DeadlineExceeded},
		{name: "untrusted invalid grant prose", refreshErr: errors.New("invalid_grant: proxy failed")},
		{name: "refresh 503 even with grant code", refreshErr: &ExcelOAuthRefreshError{Status: 503, Code: "invalid_grant"}},
		{name: "refresh client configuration", refreshErr: &ExcelOAuthRefreshError{Status: 400, Code: "invalid_client"}},
		{name: "confirmed invalid grant", refreshErr: &ExcelOAuthRefreshError{Status: 400, Code: "invalid_grant"}, reason: "invalid_grant"},
		{name: "confirmed revoked grant", refreshErr: &ExcelOAuthRefreshError{Status: 401, Code: "grant_revoked"}, reason: "grant_revoked"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			oauth, original, account := excelGrantService(t)
			grant := excelGrantFixture(t, "test-account", "user", time.Now().Add(time.Minute), "first")
			require.NoError(t, oauth.ImportExcelCredentials(t.Context(), account, grant))
			repo := &excelFailureTestRepo{excelGrantTestRepo: original, readErr: tc.readErr}
			oauth.excelAccountRepo = repo
			oauth.oauthClient = &excelRefreshTestClient{refresh: func(context.Context, string, string, string) (*openai.TokenResponse, error) {
				return nil, tc.refreshErr
			}}
			if tc.missing {
				repo.ciphertext = ""
			}
			if tc.corrupt {
				repo.ciphertext = "bad ciphertext"
			}
			svc := &OpenAIGatewayService{accountRepo: repo, openAITokenProvider: &OpenAITokenProvider{openAIOAuthService: oauth}}
			_, err := svc.getExcelBPSAccessToken(t.Context(), account)
			require.Error(t, err)
			if tc.reason == "" {
				require.Empty(t, repo.facts, "a dependency failure must not disable a grant")
			} else {
				require.Len(t, repo.facts, 1)
				require.Equal(t, tc.reason, repo.facts[0].Reason)
				require.True(t, repo.facts[0].ReauthRequired)
			}
			require.Equal(t, "test-token", account.GetCredential("access_token"), "independent Excel failures preserve native credentials")
		})
	}
}

func TestExcel401RefreshAndReauthorizationHaveDifferentIdentities(t *testing.T) {
	oauth, _, a := excelGrantService(t)
	svc := &OpenAIGatewayService{openAITokenProvider: &OpenAITokenProvider{openAIOAuthService: oauth}}
	old := excelGrantFixture(t, "test-account", "user", time.Now().Add(time.Hour), "old")
	next := excelGrantFixture(t, "test-account", "user", time.Now().Add(time.Hour), "rotated")
	require.NoError(t, oauth.ImportExcelCredentials(t.Context(), a, old))
	identity, err := svc.ExcelBPSGrantIdentity(t.Context(), a)
	require.NoError(t, err)
	require.NoError(t, oauth.rejectExcelAccessToken(t.Context(), a, old["access_token"].(string), nil))
	marked, err := svc.ExcelBPSGrantIdentity(t.Context(), a)
	require.NoError(t, err)
	require.Equal(t, identity, marked)
	calls := 0
	oauth.oauthClient = &excelRefreshTestClient{refresh: func(context.Context, string, string, string) (*openai.TokenResponse, error) {
		calls++
		return &openai.TokenResponse{AccessToken: next["access_token"].(string), RefreshToken: next["refresh_token"].(string), IDToken: next["id_token"].(string)}, nil
	}}
	token, err := oauth.ExcelAccessToken(t.Context(), a, nil)
	require.NoError(t, err)
	require.Equal(t, 1, calls)
	rotated, err := svc.ExcelBPSGrantIdentity(t.Context(), a)
	require.NoError(t, err)
	require.Equal(t, identity, rotated)
	err = oauth.rejectExcelAccessToken(t.Context(), a, token, nil)
	var rejected *excelAuthorizationFailure
	require.ErrorAs(t, err, &rejected)
	require.Equal(t, "refreshed_access_token_rejected", rejected.fact.Reason)
	require.NoError(t, oauth.ImportExcelCredentials(t.Context(), a, next))
	replaced, err := svc.ExcelBPSGrantIdentity(t.Context(), a)
	require.NoError(t, err)
	require.NotEqual(t, identity, replaced, "explicit import is a new authorization even for the same tokens and workspace")
}

func TestExcelBPS403ClassificationDoesNotInferAccountBanFromStatus(t *testing.T) {
	for _, code := range []string{"permission_denied", "basispoints_model_access_changed", "file_not_owned", "content_policy_violation", "unsupported_tool", "invalid_request_error", ""} {
		require.False(t, excelBPSAccountForbidden(403, []byte(`{"error":{"code":"`+code+`"}}`)), code)
	}
	require.False(t, excelBPSAccountForbidden(503, []byte(`{"error":{"code":"account_deactivated"}}`)))
	require.True(t, excelBPSAccountForbidden(403, []byte(`{"error":{"code":"account_deactivated"}}`)))
	require.True(t, excelBPSAccountForbidden(403, []byte(`{"error":{"code":"bps_account_blocked"}}`)))
}

func TestExcelBPSReauthorizationNeverClaimsOrProbes(t *testing.T) {
	now := time.Now()
	account := recoveryTestAccount(now)
	account.Extra[ExcelBPSFailureKey] = map[string]any{
		"id": "system-action", "source": "system", "state": "disabled",
		"reason": "invalid_grant", "reauth_required": true, "next_probe_at": nil,
	}
	repo := &recoveryTestRepo{claim: true}
	upstream := &bpsProbeUpstream{}
	svc := &OpenAIGatewayService{accountRepo: repo, httpUpstream: upstream}
	svc.recoverExcelBPS403Account(t.Context(), account, now)
	require.Zero(t, repo.claims)
	require.Zero(t, repo.restores)
	require.Empty(t, upstream.bodies)
}

func TestExcelBPSDispatchGrantIsAccountBoundAndDetectsReplacement(t *testing.T) {
	oauth, _, account := excelGrantService(t)
	grant := excelGrantFixture(t, "test-account", "user", time.Now().Add(time.Hour), "dispatch")
	require.NoError(t, oauth.ImportExcelCredentials(t.Context(), account, grant))
	svc := &OpenAIGatewayService{openAITokenProvider: &OpenAITokenProvider{openAIOAuthService: oauth}}
	ctx, err := svc.ExcelBPSDispatchContext(t.Context(), account, grant["access_token"].(string))
	require.NoError(t, err)
	before, ok := ExcelBPSDispatchFingerprint(ctx, account.ID)
	require.True(t, ok)
	beforeIdentity, err := svc.ExcelBPSGrantIdentity(ctx, account)
	require.NoError(t, err)
	_, ok = ExcelBPSDispatchFingerprint(ctx, account.ID+1)
	require.False(t, ok)
	require.NoError(t, oauth.ImportExcelCredentials(t.Context(), account, grant))
	next, err := svc.ExcelBPSDispatchContext(t.Context(), account, grant["access_token"].(string))
	require.NoError(t, err)
	after, ok := ExcelBPSDispatchFingerprint(next, account.ID)
	require.True(t, ok)
	require.NotEqual(t, before, after)
	boundIdentity, err := svc.ExcelBPSGrantIdentity(ctx, account)
	require.NoError(t, err)
	require.Equal(t, beforeIdentity, boundIdentity, "completion metadata keeps the actual sending grant")
	newIdentity, err := svc.ExcelBPSGrantIdentity(t.Context(), account)
	require.NoError(t, err)
	require.NotEqual(t, beforeIdentity, newIdentity, "an independent new request reads the new authorization")
	unchanged, _ := ExcelBPSDispatchFingerprint(ctx, account.ID)
	require.Equal(t, before, unchanged, "a prepared request retains its original grant binding")
}

func TestExcelBPSRateLimitObservationPrioritizesCyberPolicy(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		raw    string
		write  bool
	}{
		{"HTTP cyber with retry header", 429, `{"error":{"code":"cyber_policy","scope":"account"}}`, false},
		{"terminal cyber with rate status", 0, `{"type":"response.failed","response":{"error":{"code":"cyber_policy","status":429,"scope":"account"}}}`, false},
		{"native cyber spelling", 0, `{"type":"response.failed","response":{"error":{"code":" CYBER_POLICY ","status":429,"scope":"account"}}}`, false},
		{"rate limit merely mentions cyber in prose", 429, `{"error":{"code":"rate_limit_exceeded","message":"cyber_policy","scope":"account"}}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &excelBPSCooldownRepoStub{}
			svc := &OpenAIGatewayService{accountRepo: repo}
			account := excelAccount()
			err := svc.observeExcelBPSRateLimit(t.Context(), account, tc.status, http.Header{"Retry-After": {"60"}}, []byte(tc.raw), "gpt-6-astra", "/basispoints/api/responses")
			require.NoError(t, err)
			if tc.write {
				require.Len(t, repo.deadlines, 1)
				require.False(t, repo.writeDeadline.IsZero())
				require.False(t, svc.excelBPSCooldownDeadline(account.ID).IsZero())
			} else {
				require.Empty(t, repo.deadlines)
				require.True(t, repo.writeDeadline.IsZero(), "no persistent cooldown write for content-policy rejection")
				require.True(t, svc.excelBPSCooldownDeadline(account.ID).IsZero(), "no local cooldown before security classification")
			}
		})
	}
}

type excelBPSEarlyCooldownRepo struct {
	*excelBPSCooldownRepoStub
	get func(context.Context, int64, []string) (time.Time, error)
}

func (r *excelBPSEarlyCooldownRepo) GetExcelBPSCooldown(ctx context.Context, id int64, scopes []string) (time.Time, error) {
	return r.get(ctx, id, scopes)
}

func TestExcelBPSEarlyCooldownFailureProvenance(t *testing.T) {
	for _, tc := range []struct {
		name          string
		images, probe bool
		dependency    bool
		status        int
	}{
		{"text model read failure", false, false, true, http.StatusServiceUnavailable},
		{"images rate read failure", true, false, true, http.StatusServiceUnavailable},
		{"images probe rate read failure", true, true, true, http.StatusServiceUnavailable},
		{"text model deadline", false, false, false, http.StatusForbidden},
		{"images rate deadline", true, false, false, http.StatusTooManyRequests},
		{"images probe rate deadline", true, true, false, http.StatusTooManyRequests},
	} {
		t.Run(tc.name, func(t *testing.T) {
			type requestContextKey struct{}
			ctx := context.WithValue(t.Context(), requestContextKey{}, "original-request")
			readFailure := errors.New("cooldown store unavailable")
			repo := &excelBPSEarlyCooldownRepo{excelBPSCooldownRepoStub: &excelBPSCooldownRepoStub{}}
			reads := 0
			repo.get = func(readCtx context.Context, _ int64, scopes []string) (time.Time, error) {
				reads++
				require.Equal(t, "original-request", readCtx.Value(requestContextKey{}))
				if !tc.images && !(len(scopes) == 1 && scopes[0] == excelBPSModelPermissionScope("gpt-6-astra")) {
					return time.Time{}, nil // The independent BPS 429 lookup succeeds.
				}
				if tc.dependency {
					return time.Time{}, readFailure
				}
				return time.Now().Add(time.Minute), nil
			}
			svc := excelBPSGatewayTestService(nil)
			svc.accountRepo = repo
			svc.httpUpstream = &nativeAttachmentUpstream{do: func(*http.Request, string, int64, int) (*http.Response, error) {
				t.Fatal("early cooldown rejection must not send upstream")
				return nil, nil
			}}
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			c.Set(bpsAccountProbeRequiredContextKey, tc.probe)
			var err error
			if tc.images {
				request := &OpenAIImagesRequest{Endpoint: openAIImagesGenerationsEndpoint, Model: "gpt-image-2", Prompt: "draw"}
				var fallback bool
				_, fallback, err = svc.forwardExcelBPSImages(ctx, c, excelBPSImagesAccount(), request, "gpt-image-2", "gpt-image-2", time.Now())
				require.False(t, fallback)
			} else {
				_, err = svc.forwardExcelBPS(ctx, c, excelAccount(), []byte(`{"model":"gpt-6-astra","input":"test"}`), time.Now())
			}
			var failure *UpstreamFailoverError
			require.ErrorAs(t, err, &failure)
			require.Positive(t, reads)
			require.Equal(t, tc.status, failure.StatusCode)
			require.True(t, failure.RequestNotSent)
			require.False(t, c.Writer.Written())
			if tc.dependency {
				require.ErrorIs(t, err, readFailure)
				require.EqualValues(t, "selection", failure.Stage)
				require.Equal(t, GatewayFailureScopeProvider, failure.Scope)
				require.Equal(t, NextAccountStop, failure.NextAccountAction)
			}
			require.Empty(t, repo.deadlines, "a read or cached deadline does not create a new penalty")
			require.True(t, repo.writeDeadline.IsZero())
		})
	}
}
