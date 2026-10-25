package service

import "github.com/Wei-Shaw/sub2api/internal/service/basispoints"

const ExcelBPSOmitUnsupportedToolsKey = "openai_excel_bps_omit_unsupported_tools"

// IsExcelBPSOmitUnsupportedToolsEnabled opts into the bridge's hosted-tool
// omission and capability notice. Forced hosted selections still fail validation.
func (a *Account) IsExcelBPSOmitUnsupportedToolsEnabled() bool {
	return a.IsExcelBPSEnabled() && a.Extra[ExcelBPSOmitUnsupportedToolsKey] == true
}

func (a *Account) excelBPSNativeFallbackReason(body []byte) string {
	if a.IsExcelBPSOmitUnsupportedToolsEnabled() {
		return ""
	}
	return basispoints.NativeFallbackReason(body)
}

// Inspect only the original request: a catalog inherited inside BPS is not
// available to native forwarding. Normalization runs on this decoded copy so a
// choice that native would downgrade cannot silently become an automatic one.
func excelBPSClientToolChoiceNativeFallbackAllowed(body []byte) bool {
	var source map[string]any
	if decodeJSONUseNumber(body, &source) != nil || !basispoints.NativeClientToolChoiceEligible(source) {
		return false
	}
	return !normalizeCodexToolChoice(source)
}
