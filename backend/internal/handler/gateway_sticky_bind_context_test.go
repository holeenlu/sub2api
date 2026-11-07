//go:build unit

package handler

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// 转发成功后的粘性续期发生在响应已经写回之后，此时流式客户端往往已经断开，
// 请求 context 早被取消。续期若挂在请求 context 上就会以 context canceled 静默
// 失败，绑定在会话仍然活跃时到期，下一条请求换号并重建整份上游 prompt cache。
func TestDetachedStickyBindContextSurvivesClientDisconnect(t *testing.T) {
	requestCtx, cancelRequest := context.WithCancel(context.Background())
	bindCtx, cancelBind := detachedStickyBindContext(requestCtx)
	defer cancelBind()

	cancelRequest()
	require.ErrorIs(t, requestCtx.Err(), context.Canceled)

	require.NoError(t, bindCtx.Err(), "客户端断开不应取消绑定 context")

	deadline, ok := bindCtx.Deadline()
	require.True(t, ok, "绑定 context 必须自带截止时间，Redis 不可用时不能挂住请求 goroutine")
	require.LessOrEqual(t, time.Until(deadline), stickySessionBindTimeout)
}

// 绑定 context 仍然受自己的超时约束。
func TestDetachedStickyBindContextExpiresOnItsOwnDeadline(t *testing.T) {
	bindCtx, cancelBind := detachedStickyBindContext(context.Background())
	defer cancelBind()

	require.NoError(t, bindCtx.Err())
	cancelBind()
	require.ErrorIs(t, bindCtx.Err(), context.Canceled)
}

// 请求 context 上携带的值必须保留：绑定链路下游仍会读取分组等请求域信息。
func TestDetachedStickyBindContextKeepsRequestValues(t *testing.T) {
	type ctxKey struct{}

	requestCtx, cancelRequest := context.WithCancel(context.WithValue(context.Background(), ctxKey{}, "group-1"))
	defer cancelRequest()

	bindCtx, cancelBind := detachedStickyBindContext(requestCtx)
	defer cancelBind()

	cancelRequest()
	require.Equal(t, "group-1", bindCtx.Value(ctxKey{}))
}
