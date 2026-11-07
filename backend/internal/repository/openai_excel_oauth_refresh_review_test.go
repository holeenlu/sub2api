package repository

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestExcelOAuthRefreshReviewActualHTTPFailureProvenance(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		status               int
		body, clientID, code string
	}{
		{"invalid grant", 400, "{\"error\":\"invalid_grant\",\"error_description\":\"synthetic-secret-marker\"}", openai.ExcelClientID, "invalid_grant"},
		{"revoked token", 401, "{\"error\":{\"code\":\"token_revoked\",\"message\":\"synthetic-secret-marker\"}}", openai.ExcelClientID, "token_revoked"},
		{"revoked grant", 400, "{\"error\":\"grant_revoked\"}", openai.ExcelClientID, "grant_revoked"},
		{"provider 503 is not revocation", 503, "{\"error\":\"invalid_grant\"}", openai.ExcelClientID, ""},
		{"rate limit is not revocation", 429, "{\"error\":\"invalid_grant\"}", openai.ExcelClientID, ""},
		{"client configuration", 400, "{\"error\":\"invalid_client\"}", openai.ExcelClientID, ""},
		{"prose is not evidence", 401, "{\"error\":{\"message\":\"invalid_grant synthetic-secret-marker\"}}", openai.ExcelClientID, ""},
		{"non JSON", 401, "<html>invalid_grant synthetic-secret-marker</html>", openai.ExcelClientID, ""},
		{"invalid JSON", 401, "{\"error\":\"invalid_grant\"", openai.ExcelClientID, ""},
		{"oversized diagnostic", 400, "{\"error\":\"invalid_grant\",\"description\":\"" + strings.Repeat("x", 65<<10) + "\"}", openai.ExcelClientID, ""},
		{"native grant stays independent", 400, "{\"error\":\"invalid_grant\"}", openai.ClientID, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			srv := newLocalTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Method != http.MethodPost || r.ParseForm() != nil || r.PostForm.Get("client_id") != tc.clientID || r.PostForm.Get("grant_type") != "refresh_token" {
					t.Error("refresh client sent unexpected request")
					w.WriteHeader(500)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer srv.Close()
			client := &openaiOAuthService{tokenURL: srv.URL}
			value, err := client.RefreshTokenWithClientID(t.Context(), "synthetic-refresh-token", "", tc.clientID)
			require.Nil(t, value)
			require.Error(t, err)
			require.EqualValues(t, 1, calls.Load())
			require.Equal(t, "OPENAI_OAUTH_TOKEN_REFRESH_FAILED", infraerrors.Reason(err))
			var cause *service.ExcelOAuthRefreshError
			if tc.code == "" {
				require.False(t, errors.As(err, &cause))
			} else {
				require.ErrorAs(t, err, &cause)
				require.Equal(t, tc.code, cause.Code)
				require.Equal(t, tc.status, cause.Status)
			}
			if tc.clientID == openai.ExcelClientID {
				require.NotContains(t, err.Error(), "synthetic-secret-marker")
			}
		})
	}
}

func TestExcelOAuthRefreshReviewCanceledRequestCannotBecomeRevocation(t *testing.T) {
	var calls atomic.Int32
	srv := newLocalTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(401) }))
	defer srv.Close()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := (&openaiOAuthService{tokenURL: srv.URL}).RefreshTokenWithClientID(ctx, "synthetic", "", openai.ExcelClientID)
	require.Error(t, err)
	var cause *service.ExcelOAuthRefreshError
	require.False(t, errors.As(err, &cause))
	require.Zero(t, calls.Load())
}
