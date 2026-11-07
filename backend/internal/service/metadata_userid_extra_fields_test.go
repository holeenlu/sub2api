//go:build unit

package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

const (
	extraFieldsDeviceID  = "d61f76d0730d2b920763648949bad5c79742155c27037fc77ac3f9805cb90169"
	extraFieldsSessionID = "7578cf37-aaca-46e4-a45c-71285d9dbb83"
	extraFieldsParentID  = "0b4f7a61-1c2d-4e3f-8a9b-0c1d2e3f4a5b"
	extraFieldsUA        = "claude-cli/2.1.78 (external, cli)"
)

// 带未知扩展字段的新格式 user_id，键顺序刻意不是 device/account/session。
func extraFieldsUserID(t *testing.T) string {
	t.Helper()
	return `{"session_id":"` + extraFieldsSessionID + `",` +
		`"parent_session_id":"` + extraFieldsParentID + `",` +
		`"device_id":"` + extraFieldsDeviceID + `",` +
		`"account_uuid":"client-account",` +
		`"tk":"opaque-token",` +
		`"future_field":{"nested":[1,2,3]}}`
}

func extraFieldsBody(t *testing.T, userID string) []byte {
	t.Helper()
	return []byte(`{"messages":[],"metadata":{"user_id":` + strconvQuote(userID) + `},"stream":true}`)
}

func rewrittenUserID(t *testing.T, body []byte) gjson.Result {
	t.Helper()
	raw := gjson.GetBytes(body, "metadata.user_id").String()
	require.True(t, strings.HasPrefix(raw, "{"), "重写后仍应是 JSON 格式")
	return gjson.Parse(raw)
}

func TestParseMetadataUserID_CapturesExtraFields(t *testing.T) {
	parsed := ParseMetadataUserID(extraFieldsUserID(t))
	require.NotNil(t, parsed)
	require.True(t, parsed.IsNewFormat)
	require.Equal(t, extraFieldsDeviceID, parsed.DeviceID)
	require.Equal(t, extraFieldsSessionID, parsed.SessionID)
	require.Equal(t, "client-account", parsed.AccountUUID)

	require.Len(t, parsed.ExtraFields, 3)
	require.Equal(t, extraFieldsParentID, parsed.metadataUserIDParentSession())
	require.JSONEq(t, `{"nested":[1,2,3]}`, string(parsed.ExtraFields["future_field"]))
	require.Equal(t, `"opaque-token"`, string(parsed.ExtraFields["tk"]))
}

func TestParseMetadataUserID_LegacyFormatHasNoExtraFields(t *testing.T) {
	parsed := ParseMetadataUserID("user_" + extraFieldsDeviceID + "_account__session_" + extraFieldsSessionID)
	require.NotNil(t, parsed)
	require.False(t, parsed.IsNewFormat)
	require.Empty(t, parsed.RawJSON)
	require.Nil(t, parsed.ExtraFields)
	require.Empty(t, parsed.metadataUserIDParentSession())
}

// 核心回归：未知字段以前会被按三个已知键重建的 user_id 整段丢掉。
func TestRewriteUserID_PreservesUnknownMetadataFields(t *testing.T) {
	svc := NewIdentityService(&identityCacheStub{})
	original := extraFieldsUserID(t)

	result, err := svc.RewriteUserID(extraFieldsBody(t, original), 123, "acc-uuid", "client-xyz", extraFieldsUA)
	require.NoError(t, err)

	uid := rewrittenUserID(t, result)
	require.Equal(t, "opaque-token", uid.Get("tk").String(), "tk 必须原样保留")
	require.JSONEq(t, `{"nested":[1,2,3]}`, uid.Get("future_field").Raw, "未知扩展字段必须原样保留")

	// 身份字段照常改写
	require.Equal(t, "client-xyz", uid.Get("device_id").String())
	require.Equal(t, "acc-uuid", uid.Get("account_uuid").String())
	require.NotEqual(t, extraFieldsSessionID, uid.Get("session_id").String())
}

// parent_session_id 是客户端真实会话标识，必须改写而不是原样透传。
func TestRewriteUserID_RemapsParentSessionInSameNamespace(t *testing.T) {
	svc := NewIdentityService(&identityCacheStub{})

	result, err := svc.RewriteUserID(extraFieldsBody(t, extraFieldsUserID(t)), 123, "acc-uuid", "client-xyz", extraFieldsUA)
	require.NoError(t, err)
	uid := rewrittenUserID(t, result)

	parent := uid.Get("parent_session_id").String()
	require.NotEmpty(t, parent)
	require.NotEqual(t, extraFieldsParentID, parent, "不得把客户端原始父会话 ID 泄露给上游")

	// 同一账号下，父会话 ID 出现在 session 位时必须映射到同一个值——
	// 父子关联在改写后依然成立。
	asSession := `{"device_id":"` + extraFieldsDeviceID + `","account_uuid":"a","session_id":"` + extraFieldsParentID + `"}`
	sessionResult, err := svc.RewriteUserID(extraFieldsBody(t, asSession), 123, "acc-uuid", "client-xyz", extraFieldsUA)
	require.NoError(t, err)
	require.Equal(t, parent, rewrittenUserID(t, sessionResult).Get("session_id").String())
}

func TestRewriteUserID_ParentSessionIsAccountScoped(t *testing.T) {
	svc := NewIdentityService(&identityCacheStub{})
	body := extraFieldsBody(t, extraFieldsUserID(t))

	first, err := svc.RewriteUserID(body, 123, "acc-uuid", "client-xyz", extraFieldsUA)
	require.NoError(t, err)
	second, err := svc.RewriteUserID(body, 456, "acc-uuid", "client-xyz", extraFieldsUA)
	require.NoError(t, err)

	require.NotEqual(t,
		rewrittenUserID(t, first).Get("parent_session_id").String(),
		rewrittenUserID(t, second).Get("parent_session_id").String(),
		"不同账号不得共享父会话映射")
}

// 非字符串类型的 parent_session_id 不得被改坏。
func TestRewriteUserID_LeavesNonStringParentSessionUntouched(t *testing.T) {
	svc := NewIdentityService(&identityCacheStub{})
	original := `{"device_id":"` + extraFieldsDeviceID + `","account_uuid":"a","session_id":"` + extraFieldsSessionID + `","parent_session_id":{"unexpected":true}}`

	result, err := svc.RewriteUserID(extraFieldsBody(t, original), 123, "acc-uuid", "client-xyz", extraFieldsUA)
	require.NoError(t, err)
	require.JSONEq(t, `{"unexpected":true}`, rewrittenUserID(t, result).Get("parent_session_id").Raw)
}

// 原地改写保留客户端的键顺序，不重排成 device/account/session 规范序。
func TestRewriteUserID_PreservesClientKeyOrder(t *testing.T) {
	svc := NewIdentityService(&identityCacheStub{})

	result, err := svc.RewriteUserID(extraFieldsBody(t, extraFieldsUserID(t)), 123, "acc-uuid", "client-xyz", extraFieldsUA)
	require.NoError(t, err)

	raw := gjson.GetBytes(result, "metadata.user_id").String()
	assertJSONTokenOrder(t, raw,
		`"session_id"`, `"parent_session_id"`, `"device_id"`, `"account_uuid"`, `"tk"`, `"future_field"`)
}

// Session and parent must share the same original-ID namespace, with or without
// masking. Validate the relationship through actual requests, not a fixed UUID.
func TestRewriteUserIDWithMasking_PreservesUnknownMetadataFields(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		name := "disabled"
		if enabled {
			name = "enabled"
		}
		t.Run(name, func(t *testing.T) {
			cache := &identityCacheStub{maskedSessionID: "11111111-2222-4333-8444-555555555555"}
			svc := NewIdentityService(cache)
			account := &Account{ID: 123, Platform: PlatformAnthropic, Type: AccountTypeOAuth, Extra: map[string]any{"session_id_masking_enabled": enabled}}
			original := extraFieldsUserID(t)
			body := extraFieldsBody(t, original)
			rewrite := func(raw string) []byte {
				input := extraFieldsBody(t, raw)
				before := string(input)
				out, err := svc.RewriteUserIDWithMasking(context.Background(), input, account, "acc-uuid", "client-xyz", extraFieldsUA)
				require.NoError(t, err)
				require.Equal(t, before, string(input), "shared input is not mutated")
				return out
			}
			result := rewrite(original)
			uid := rewrittenUserID(t, result)
			require.Equal(t, "client-xyz", uid.Get("device_id").String())
			require.Equal(t, "acc-uuid", uid.Get("account_uuid").String())
			for _, field := range []string{"tk", "future_field"} {
				require.Equal(t, gjson.Get(original, field).Raw, uid.Get(field).Raw, "unknown fields retain their original encoding")
			}
			assertJSONTokenOrder(t, uid.Raw, `"session_id"`, `"parent_session_id"`, `"device_id"`, `"account_uuid"`, `"tk"`, `"future_field"`)
			assertJSONTokenOrder(t, string(result), `"messages"`, `"metadata"`, `"stream"`)
			require.Equal(t, result, rewrite(original), "repeated raw IDs have stable mappings")

			// Send the parent's raw ID in the session position, then make the
			// original child a parent of another request. Both edges must join.
			parentRequest := strings.Replace(original, extraFieldsSessionID, extraFieldsParentID, 1)
			parentRequest = strings.Replace(parentRequest, `"parent_session_id":"`+extraFieldsParentID+`",`, "", 1)
			parentUID := rewrittenUserID(t, rewrite(parentRequest))
			require.Equal(t, parentUID.Get("session_id").String(), uid.Get("parent_session_id").String())
			grandchildRequest := strings.Replace(original, extraFieldsParentID, extraFieldsSessionID, 1)
			grandchildRequest = strings.Replace(grandchildRequest, `"session_id":"`+extraFieldsSessionID+`"`, `"session_id":"grandchild-session"`, 1)
			grandchildUID := rewrittenUserID(t, rewrite(grandchildRequest))
			require.Equal(t, uid.Get("session_id").String(), grandchildUID.Get("parent_session_id").String())
			require.NotEqual(t, uid.Get("session_id").String(), grandchildUID.Get("session_id").String())
			require.NotEqual(t, uid.Get("session_id").String(), uid.Get("parent_session_id").String(), "distinct original sessions cannot collapse")
			require.NotContains(t, uid.Raw, extraFieldsSessionID)
			require.NotContains(t, uid.Raw, extraFieldsParentID)
			require.NotEqual(t, cache.maskedSessionID, uid.Get("session_id").String(), "seed itself is not an outgoing session")
			ordinary, err := svc.RewriteUserID(body, account.ID, "acc-uuid", "client-xyz", extraFieldsUA)
			require.NoError(t, err)
			if enabled {
				require.NotEqual(t, ordinary, result)
			} else {
				require.Equal(t, ordinary, result, "disabled masking preserves the established mapping")
			}
			// JSON IDs are opaque, unlike legacy UUIDs: matching raw IDs must
			// still link when the accepted string contains leading/trailing spaces.
			t.Run("opaque raw ID", func(t *testing.T) {
				paddedParent := strings.Replace(parentRequest, extraFieldsParentID, " "+extraFieldsParentID+" ", 1)
				paddedChild := strings.Replace(original, extraFieldsParentID, " "+extraFieldsParentID+" ", 1)
				parentUID := rewrittenUserID(t, rewrite(paddedParent))
				childUID := rewrittenUserID(t, rewrite(paddedChild))
				require.Equal(t, parentUID.Get("session_id").String(), childUID.Get("parent_session_id").String())
			})
			account.ID++
			other := rewrittenUserID(t, rewrite(original))
			require.NotEqual(t, uid.Get("session_id").String(), other.Get("session_id").String())
			require.NotEqual(t, uid.Get("parent_session_id").String(), other.Get("parent_session_id").String())
		})
	}
}

// legacy 原文没有可保留的扩展字段，仍走规范构造，行为不变。
func TestRewriteUserID_LegacyInputStillCanonical(t *testing.T) {
	svc := NewIdentityService(&identityCacheStub{})
	original := "user_" + extraFieldsDeviceID + "_account__session_" + extraFieldsSessionID

	result, err := svc.RewriteUserID(extraFieldsBody(t, original), 123, "acc-uuid", "client-xyz", extraFieldsUA)
	require.NoError(t, err)

	var keys []string
	rewrittenUserID(t, result).ForEach(func(key, _ gjson.Result) bool {
		keys = append(keys, key.String())
		return true
	})
	require.Equal(t, []string{"device_id", "account_uuid", "session_id"}, keys)
}

// 目标格式是 legacy 时无处安放扩展字段，必须回到拼接格式而不是半 JSON。
func TestRewriteUserID_LegacyOutputDropsExtraFields(t *testing.T) {
	svc := NewIdentityService(&identityCacheStub{})

	result, err := svc.RewriteUserID(
		extraFieldsBody(t, extraFieldsUserID(t)), 123, "acc-uuid", "client-xyz", "claude-cli/2.1.77 (external, cli)",
	)
	require.NoError(t, err)

	raw := gjson.GetBytes(result, "metadata.user_id").String()
	require.True(t, strings.HasPrefix(raw, "user_client-xyz_account_acc-uuid_session_"), "got %q", raw)
	require.NotContains(t, raw, "opaque-token")
}

// 重复键构造不得绕过会话隔离：改写回读不过时整体退回规范构造。
func TestRewriteUserID_DuplicateKeysCannotLeakOriginalSession(t *testing.T) {
	svc := NewIdentityService(&identityCacheStub{})
	original := `{"device_id":"` + extraFieldsDeviceID + `","account_uuid":"a",` +
		`"session_id":"decoy","session_id":"` + extraFieldsSessionID + `"}`

	result, err := svc.RewriteUserID(extraFieldsBody(t, original), 123, "acc-uuid", "client-xyz", extraFieldsUA)
	require.NoError(t, err)

	raw := gjson.GetBytes(result, "metadata.user_id").String()
	require.NotContains(t, raw, extraFieldsSessionID, "原始会话 ID 不得出现在出站 user_id 中")
	require.NotContains(t, raw, "decoy")

	var fields map[string]any
	require.NoError(t, json.Unmarshal([]byte(raw), &fields))
	require.Equal(t, "client-xyz", fields["device_id"])
	require.Equal(t, "acc-uuid", fields["account_uuid"])
}
