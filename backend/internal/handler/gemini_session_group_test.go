//go:build unit

package handler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Antigravity 转发拿到的会话分组既用于重试路径清粘性绑定，也用于转发结束后的
// 重绑，必须和绑定侧是同一个分组。绑定走 BindSelectionStickySession*，落在选号
// 实际使用的分组下；这里如果传 API Key 自己的分组，无可用账号兜底借了别的分组
// 账号池时就会去原分组空删，被限流的账号继续被粘住。
func TestForwardGeminiSessionGroupFollowsSelection(t *testing.T) {
	entries, err := os.ReadDir(".")
	require.NoError(t, err)

	callSites := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(filepath.Clean(name))
		require.NoError(t, err)

		lines := strings.Split(string(src), "\n")
		for i, line := range lines {
			if strings.HasPrefix(strings.TrimSpace(line), "//") ||
				!strings.Contains(line, "WithForwardGeminiSession(") {
				continue
			}
			callSites++
			// 分组要么就地由 SelectionGroupID 求得，要么来自同一函数里这样求得的
			// 局部变量；后者在调用点上方最多几行内可见。
			window := strings.Join(lines[max(0, i-12):i+1], "\n")
			require.Containsf(t, window, "SelectionGroupID(",
				"%s:%d passes a session group to WithForwardGeminiSession that is not derived from "+
					"service.SelectionGroupID; the retry path would then clear the sticky binding in the "+
					"wrong group when the no-account fallback borrowed another group's pool", name, i+1)
		}
	}
	require.NotZero(t, callSites, "no WithForwardGeminiSession call sites found; update this test")
}
