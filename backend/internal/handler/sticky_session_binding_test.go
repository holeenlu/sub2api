//go:build unit

package handler

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// stickyBindingCallPattern matches a handler calling the raw binding methods
// instead of the selection-aware wrappers.
var stickyBindingCallPattern = regexp.MustCompile(`\.BindStickySession(AfterProfitAdmission)?\(`)

// rawStickyBindingAllowlist names the files whose call sites legitimately have
// no selection to bind against.
//
// gemini_v1beta_handler.go rebinds a digest-matched account *before* selection
// runs, so there is nothing to derive the scheduling group from; the boundary
// is documented at the call site.
var rawStickyBindingAllowlist = map[string]int{
	"gemini_v1beta_handler.go": 1,
}

// 粘性绑定必须落在账号真正被选出来的那个分组的命名空间下。传 API Key 自己的
// 分组只在没有兜底时才对，而「借了别的分组账号池」这件事在调用点根本看不出来，
// 于是每加一个绑定点就多一次漏改的机会。
//
// BindSelectionStickySession* 直接收选号结果，调用点无从表达错误的分组；这个
// 测试禁止裸方法重新出现在 handler 层。
func TestHandlerStickyBindingsGoThroughSelection(t *testing.T) {
	entries, err := os.ReadDir(".")
	require.NoError(t, err)

	found := map[string]int{}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(filepath.Clean(name))
		require.NoError(t, err)

		for _, line := range strings.Split(string(src), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "//") {
				continue
			}
			// 包装方法的名字里含有裸方法名，只统计不是包装方法的调用。
			if strings.Contains(line, ".BindSelectionStickySession") {
				continue
			}
			if stickyBindingCallPattern.MatchString(line) {
				found[name]++
			}
		}
	}

	for file, count := range found {
		allowed := rawStickyBindingAllowlist[file]
		require.LessOrEqualf(t, count, allowed,
			"%s calls BindStickySession* directly %d time(s), allowlist permits %d.\n"+
				"Use BindSelectionStickySession[AfterProfitAdmission] so the binding follows the group the "+
				"selection came from, or extend rawStickyBindingAllowlist with a comment explaining why there "+
				"is no selection in scope.", file, count, allowed)
	}

	// 过期的豁免项会掩盖一个其实已经不需要豁免的调用点。
	for file, allowed := range rawStickyBindingAllowlist {
		require.Equalf(t, allowed, found[file],
			"rawStickyBindingAllowlist expects %d raw call(s) in %s but found %d; update the allowlist",
			allowed, file, found[file])
	}
}
