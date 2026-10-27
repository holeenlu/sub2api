//go:build unit

package service

import (
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestClaudeAccountProbeIncludesSignedBilling(t *testing.T) {
	withCLIVersionResolverForTest(t, func() string { return "2.1.302" })
	response := newJSONResponse(http.StatusOK, "data: {\"type\":\"message_stop\"}\n\n")
	upstream := &queuedHTTPUpstream{responses: []*http.Response{response}}
	svc := &AccountTestService{httpUpstream: upstream}
	c, _ := newTestContext()
	account := &Account{ID: 1, Platform: PlatformAnthropic, Type: AccountTypeOAuth,
		Credentials: map[string]any{"access_token": "synthetic-test-token"}}
	require.NoError(t, svc.testClaudeAccountConnection(c, account, "claude-haiku-4-5"))
	require.Len(t, upstream.requests, 1)
	req := upstream.requests[0]
	body, err := io.ReadAll(req.Body)
	require.NoError(t, err)
	billing := gjson.GetBytes(body, "system.0.text").String()
	require.Contains(t, billing, "x-anthropic-billing-header: cc_version="+claudeCCHVersion+".")
	require.Regexp(t, `; cch=[0-9a-f]{5};`, billing)
	require.Equal(t, body, finalizeClaudeCCH(body, req.URL.String()))
	require.Equal(t, claudeCodeSystemPrompt, gjson.GetBytes(body, "system.1.text").String())
	require.Equal(t, claudeCodeMimicUserAgent(), getHeaderRaw(req.Header, "User-Agent"))
	require.EqualValues(t, len(body), req.ContentLength)
	replay, err := req.GetBody()
	require.NoError(t, err)
	defer replay.Close()
	replayed, err := io.ReadAll(replay)
	require.NoError(t, err)
	require.Equal(t, body, replayed)
	metadata := ParseMetadataUserID(gjson.GetBytes(body, "metadata.user_id").String())
	require.NotNil(t, metadata)
	require.Equal(t, metadata.SessionID, getHeaderRaw(req.Header, "X-Claude-Code-Session-Id"))
}
