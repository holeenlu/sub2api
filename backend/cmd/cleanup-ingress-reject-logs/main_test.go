package main

import "testing"

func TestHistoricalIngressRejectReason(t *testing.T) {
	tests := []struct {
		name   string
		item   candidate
		reason string
		match  bool
	}{
		{name: "standard invalid key", item: candidate{body: `{"code":"INVALID_API_KEY","message":"Invalid API key"}`}, reason: "invalid_key", match: true},
		{name: "google missing key", item: candidate{body: `{"error":{"code":401,"message":"API key is required","status":"UNAUTHENTICATED"}}`}, reason: "missing_key", match: true},
		{name: "google group deleted", item: candidate{body: `{"error":{"code":403,"message":"API Key 所属分组已删除","status":"PERMISSION_DENIED"}}`}, reason: "group_deleted", match: true},
		// 簡體是資料庫裡的歷史日誌，繁體是產生端轉繁後的新日誌，兩者都必須被歸類。
		{name: "google group deleted traditional", item: candidate{body: `{"error":{"code":403,"message":"API Key 所屬分組已刪除","status":"PERMISSION_DENIED"}}`}, reason: "group_deleted", match: true},
		{name: "group disabled simplified", item: candidate{body: `{"error":{"code":403,"message":"API Key 所属分组已停用","status":"PERMISSION_DENIED"}}`}, reason: "group_disabled", match: true},
		{name: "group disabled traditional", item: candidate{body: `{"error":{"code":403,"message":"API Key 所屬分組已停用","status":"PERMISSION_DENIED"}}`}, reason: "group_disabled", match: true},
		{name: "group forbidden simplified", item: candidate{body: `{"error":{"code":403,"message":"API Key 所属专属分组不再允许当前用户使用","status":"PERMISSION_DENIED"}}`}, reason: "group_forbidden", match: true},
		{name: "group forbidden traditional", item: candidate{body: `{"error":{"code":403,"message":"API Key 所屬專屬分組不再允許當前使用者使用","status":"PERMISSION_DENIED"}}`}, reason: "group_forbidden", match: true},
		{name: "ip acl", item: candidate{body: `{"code":"ACCESS_DENIED","message":"Access denied. Your IP is 192.0.2.1"}`}, reason: "ip_acl_denied", match: true},
		{name: "user not found remains", item: candidate{body: `{"code":"USER_NOT_FOUND","message":"User associated with API key not found"}`}, match: false},
		{name: "quota remains", item: candidate{body: `{"code":"API_KEY_QUOTA_EXHAUSTED","message":"quota"}`}, match: false},
		{name: "database failure remains", item: candidate{statusCode: 500, message: "Failed to validate API key", body: `{"code":"INTERNAL_ERROR","message":"Failed to validate API key"}`}, match: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reason, ok := historicalIngressRejectReason(tt.item)
			if ok != tt.match || reason != tt.reason {
				t.Fatalf("got (%q, %v), want (%q, %v)", reason, ok, tt.reason, tt.match)
			}
		})
	}
}
