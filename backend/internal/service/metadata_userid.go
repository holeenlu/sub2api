package service

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/tidwall/sjson"
)

// NewMetadataFormatMinVersion is the minimum Claude Code version that uses
// JSON-formatted metadata.user_id instead of the legacy concatenated string.
const NewMetadataFormatMinVersion = "2.1.78"

// ParsedUserID represents the components extracted from a metadata.user_id value.
type ParsedUserID struct {
	DeviceID    string // 64-char hex (or arbitrary client id)
	AccountUUID string // may be empty
	SessionID   string // UUID
	IsNewFormat bool   // true if the original was JSON format
	// RawJSON 是新格式下的原始 JSON 文本。重写时在它上面做原地改写，
	// 以保留客户端的键顺序和本网关不认识的扩展字段（例如
	// parent_session_id、tk，以及更新版 Claude Code 新增的字段）：
	// 按三个已知键重新序列化会把它们整段丢掉，既丢语义也是可判别特征。
	RawJSON string
	// ExtraFields 是三个已知键之外的字段，值保留原始字节。
	ExtraFields map[string]json.RawMessage
}

// MetadataUserIDParentSessionKey 是 Claude Code 在 metadata.user_id 里携带的
// 父会话字段。它和 session_id 一样是客户端真实会话标识，必须和 session_id
// 用同一套派生规则改写——只保留不改写等于把原始会话 ID 泄露给上游，
// 且会让上游看到一个它从未见过的父会话。
const MetadataUserIDParentSessionKey = "parent_session_id"

// legacyUserIDRegex matches the legacy user_id format:
//
//	user_{64hex}_account_{optional_uuid}_session_{uuid}
var legacyUserIDRegex = regexp.MustCompile(`^user_([a-fA-F0-9]{64})_account_([a-fA-F0-9-]*)_session_([a-fA-F0-9-]{36})$`)

// jsonUserID is the JSON structure for the new metadata.user_id format.
type jsonUserID struct {
	DeviceID    string `json:"device_id"`
	AccountUUID string `json:"account_uuid"`
	SessionID   string `json:"session_id"`
}

// ParseMetadataUserID parses a metadata.user_id string in either format.
// Returns nil if the input cannot be parsed.
func ParseMetadataUserID(raw string) *ParsedUserID {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}

	// Try JSON format first (starts with '{')
	if raw[0] == '{' {
		var j jsonUserID
		if err := json.Unmarshal([]byte(raw), &j); err != nil {
			return nil
		}
		if j.DeviceID == "" || j.SessionID == "" {
			return nil
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal([]byte(raw), &fields); err != nil {
			return nil
		}
		var extra map[string]json.RawMessage
		for key, value := range fields {
			switch key {
			case "device_id", "account_uuid", "session_id":
				continue
			}
			if extra == nil {
				extra = make(map[string]json.RawMessage, len(fields))
			}
			extra[key] = value
		}
		return &ParsedUserID{
			DeviceID:    j.DeviceID,
			AccountUUID: j.AccountUUID,
			SessionID:   j.SessionID,
			IsNewFormat: true,
			RawJSON:     raw,
			ExtraFields: extra,
		}
	}

	// Try legacy format
	matches := legacyUserIDRegex.FindStringSubmatch(raw)
	if matches == nil {
		return nil
	}
	return &ParsedUserID{
		DeviceID:    matches[1],
		AccountUUID: matches[2],
		SessionID:   matches[3],
		IsNewFormat: false,
	}
}

// FormatMetadataUserID builds a metadata.user_id string in the format
// appropriate for the given CLI version. Components are the rewritten values
// (not necessarily the originals).
func FormatMetadataUserID(deviceID, accountUUID, sessionID, uaVersion string) string {
	if IsNewMetadataFormatVersion(uaVersion) {
		b, _ := json.Marshal(jsonUserID{
			DeviceID:    deviceID,
			AccountUUID: accountUUID,
			SessionID:   sessionID,
		})
		return string(b)
	}
	// Legacy format
	return "user_" + deviceID + "_account_" + accountUUID + "_session_" + sessionID
}

// rewriteMetadataUserIDFields 用给定的身份值改写一个已解析的 metadata.user_id。
//
// 原文是新格式、且按 uaVersion 输出的也是新格式时，在原始 JSON 上做原地改写：
// 客户端的键顺序和所有未知扩展字段原样保留。其它组合（legacy 原文、或要输出
// legacy 格式）没有可保留的原文，退回 FormatMetadataUserID 的规范构造。
//
// parentSessionID 为空表示不改写 parent_session_id（调用方已在上一轮改写过，
// 或本次没有可用的派生值）；非空时只在该字段确实是 JSON 字符串时才写入，
// 避免把客户端的非预期类型改坏。
func rewriteMetadataUserIDFields(parsed *ParsedUserID, deviceID, accountUUID, sessionID, parentSessionID, uaVersion string) string {
	if parsed == nil || !parsed.IsNewFormat || parsed.RawJSON == "" || !IsNewMetadataFormatVersion(uaVersion) {
		return FormatMetadataUserID(deviceID, accountUUID, sessionID, uaVersion)
	}
	out := parsed.RawJSON
	for _, field := range [...]struct {
		path  string
		value string
	}{
		{"device_id", deviceID},
		{"account_uuid", accountUUID},
		{"session_id", sessionID},
	} {
		next, err := sjson.Set(out, field.path, field.value)
		if err != nil {
			// 原地改写失败就整体退回规范构造：宁可丢扩展字段，
			// 也不能把半改写的 user_id 发出去。
			return FormatMetadataUserID(deviceID, accountUUID, sessionID, uaVersion)
		}
		out = next
	}
	if parentSessionID != "" {
		next, err := sjson.Set(out, MetadataUserIDParentSessionKey, parentSessionID)
		if err != nil {
			return FormatMetadataUserID(deviceID, accountUUID, sessionID, uaVersion)
		}
		out = next
	}
	// 回读校验，按「重复键取最后一个」的解析语义确认改写真的生效。
	// 客户端可以构造 {"session_id":"x","session_id":"<真实会话>"}：sjson 只改
	// 其中一处，上游解析时仍会读到真实会话 ID，隔离被绕过。任何一项对不上就
	// 整体退回规范构造——宁可丢掉扩展字段，也不能漏出客户端的原始标识。
	if !metadataUserIDRewriteApplied(out, deviceID, accountUUID, sessionID, parentSessionID) {
		return FormatMetadataUserID(deviceID, accountUUID, sessionID, uaVersion)
	}
	return out
}

func metadataUserIDRewriteApplied(raw, deviceID, accountUUID, sessionID, parentSessionID string) bool {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &fields); err != nil {
		return false
	}
	expected := map[string]string{
		"device_id":    deviceID,
		"account_uuid": accountUUID,
		"session_id":   sessionID,
	}
	if parentSessionID != "" {
		expected[MetadataUserIDParentSessionKey] = parentSessionID
	}
	for key, want := range expected {
		var got string
		if err := json.Unmarshal(fields[key], &got); err != nil || got != want {
			return false
		}
	}
	return true
}

// metadataUserIDParentSession 取出 parent_session_id 的字符串值。
// 字段缺失、为空或不是 JSON 字符串时返回空串，调用方据此跳过改写。
func (p *ParsedUserID) metadataUserIDParentSession() string {
	if p == nil || len(p.ExtraFields) == 0 {
		return ""
	}
	raw, ok := p.ExtraFields[MetadataUserIDParentSessionKey]
	if !ok {
		return ""
	}
	var parent string
	if err := json.Unmarshal(raw, &parent); err != nil {
		return ""
	}
	return strings.TrimSpace(parent)
}

// IsNewMetadataFormatVersion returns true if the given CLI version uses the
// new JSON metadata.user_id format (>= 2.1.78).
func IsNewMetadataFormatVersion(version string) bool {
	if version == "" {
		return false
	}
	return CompareVersions(version, NewMetadataFormatMinVersion) >= 0
}

// ExtractCLIVersion extracts the Claude Code version from a User-Agent string.
// Returns "" if the UA doesn't match the expected pattern.
func ExtractCLIVersion(ua string) string {
	matches := claudeCodeUAVersionPattern.FindStringSubmatch(ua)
	if len(matches) >= 2 {
		return matches[1]
	}
	return ""
}
