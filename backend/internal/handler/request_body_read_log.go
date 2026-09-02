package handler

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"syscall"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"golang.org/x/net/http2"
)

// logRequestBodyReadFailure records a bounded, payload-free reason for a body
// read failure. Operators get enough information to distinguish compression
// failures from a disconnected/truncated upload without logging request
// content. kind comes from requestBodyReadErrorKind.
func logRequestBodyReadFailure(reqLog *zap.Logger, req *http.Request, kind string) {
	if reqLog == nil || kind == "" || kind == bodyReadKindNone {
		return
	}

	contentLength := int64(-1)
	contentEncoding := "identity"
	if req != nil {
		contentLength = req.ContentLength
		contentEncoding = requestContentEncodingCategory(req.Header.Get("Content-Encoding"))
	}

	reqLog.Warn("read request body failed",
		zap.String("error_kind", kind),
		zap.String("content_encoding", contentEncoding),
		zap.Int64("content_length", contentLength),
	)
}

// RespondRequestBodyReadFailure 把每个网关入口原本各自重复的"分类 → 记日志 →
// 回错误"收成一处。render 是调用方所属协议的错误写入器（Anthropic、OpenAI、
// Responses、Gemini 适配闭包……），签名一致。导出是为了让 handler 包之外先于
// handler 读取请求体的入口（routes 的 composite 路由中间件）走同一套策略。
// reqLog 为 nil 时退回请求上下文里的 logger，分类信号不会因此丢失。
func RespondRequestBodyReadFailure(
	c *gin.Context,
	reqLog *zap.Logger,
	err error,
	render func(c *gin.Context, status int, errType, message string),
) {
	if reqLog == nil {
		reqLog = requestLogger(c, "")
	}
	var requestCtx context.Context
	if c != nil && c.Request != nil {
		requestCtx = c.Request.Context()
	}
	kind := requestBodyReadErrorKind(requestCtx, err)
	logRequestBodyReadFailure(reqLog, c.Request, kind)

	policy := bodyReadErrorPolicyFor(kind)
	message := policy.Message
	// 保留带上限值的文案，运维要从响应里看到配置的上限是多少。
	if maxErr, ok := extractMaxBytesError(err); ok {
		message = buildBodyTooLargeMessage(maxErr.Limit)
	}
	if !policy.Record {
		markOpsSkipErrorRecord(c)
	}
	render(c, policy.Status, policy.ErrorType, message)
}

func requestContentEncodingCategory(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "identity":
		return "identity"
	case "gzip", "x-gzip":
		return "gzip"
	case "zstd":
		return "zstd"
	case "deflate":
		return "deflate"
	default:
		return "other"
	}
}

// 请求体读取失败的分类结果，写进结构化日志的 error_kind 字段；除 none 之外
// 每个 kind 在 bodyReadErrorPolicies 里都有一条对应的响应策略。
const (
	bodyReadKindNone                       = "none"
	bodyReadKindMaxBytes                   = "max_bytes"
	bodyReadKindUnsupportedContentEncoding = "unsupported_content_encoding"
	bodyReadKindDecodeContentEncoding      = "decode_content_encoding"
	bodyReadKindClientDisconnect           = "client_disconnect"
	bodyReadKindTruncatedBody              = "truncated_body"
	bodyReadKindTransportTimeout           = "transport_timeout"
	bodyReadKindTransport                  = "transport"
	bodyReadKindIORead                     = "io_read"
)

// h2ClientDisconnectedMessage 是两个 HTTP/2 server（x/net/http2 与
// net/http 内置实现）在整条连接断开时用来关闭在途请求体的哨兵错误文案
// （errClientDisconnected）。进程被杀、网络中断、代理重启时客户端根本不会
// 发 RST_STREAM，closeAllStreamsOnConnClose 把这个错误交给每个流的 body
// pipe，所以它才是掉线在读取端最常见的形态。它是未导出的裸 errors.New，
// 没有可供 errors.Is/As 比较的身份，只能按文案匹配。
const h2ClientDisconnectedMessage = "client disconnected"

// requestBodyReadErrorKind classifies a body read failure. ctx is the request
// context (nil when the caller has none); the HTTP/1.1 server cancels it from
// connReader.handleReadError as soon as the connection read fails, which is
// the only reliable disconnect signal on that path.
//
// The context is only a tiebreaker inside the io.ErrUnexpectedEOF branch, not
// a rule of its own. HTTP/2 cancels the request context after it closes the
// body pipe (x/net's closeStream calls CloseWithError before cancelCtx), so an
// h2 read failure races the cancellation and a context-first rule would report
// protocol errors and RST_STREAM(INTERNAL_ERROR) as client disconnects. The
// sentinel and message matches below stay authoritative and keep their order.
func requestBodyReadErrorKind(ctx context.Context, err error) string {
	if err == nil {
		return bodyReadKindNone
	}
	var maxErr *http.MaxBytesError
	if errors.As(err, &maxErr) {
		return bodyReadKindMaxBytes
	}
	lower := strings.ToLower(err.Error())
	if strings.Contains(lower, "decode content-encoding") {
		if strings.Contains(lower, "unsupported content-encoding") {
			return bodyReadKindUnsupportedContentEncoding
		}
		return bodyReadKindDecodeContentEncoding
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.EPIPE) ||
		errors.Is(err, net.ErrClosed) || strings.Contains(lower, h2ClientDisconnectedMessage) {
		return bodyReadKindClientDisconnect
	}
	if errors.Is(err, io.ErrUnexpectedEOF) {
		// net/http 的 body 在 Content-Length 未读满时遇到连接 EOF 就返回裸
		// io.ErrUnexpectedEOF，没有任何断连身份可判。HTTP/1.1 客户端正常关闭
		// （Ctrl-C 后内核发 FIN）走的正是这条路径，而同一个操作若内核发 RST
		// 会命中上面的 ECONNRESET 被判为断连——同一种用户行为两种归类，一半
		// 还带着 400 进了 ops_error_logs。读失败时请求上下文已被取消是这条
		// 路径上唯一可靠的补充信号。
		//
		// 反过来，服务端自己取消请求（超时、shutdown）时的真·截断会被算成
		// 断连：那种情况客户端同样收不到响应，少记一条比把断连当故障告警好。
		if ctx != nil && ctx.Err() != nil {
			return bodyReadKindClientDisconnect
		}
		return bodyReadKindTruncatedBody
	}
	// HTTP/1.1 的中途断连表现为 ECONNRESET 或 io.ErrUnexpectedEOF，而 HTTP/2
	// 客户端主动 RST_STREAM 时读取端拿到的是 StreamError；不单独识别就会全部
	// 落进 io_read。
	//
	// 这里必须用值类型：net/http 内置 h2 实现通过 errors.As(target any) 把自己
	// 的 StreamError 桥接到 x/net 的类型上，只对结构体值生效，指针不会命中。
	var streamErr http2.StreamError
	if errors.As(err, &streamErr) {
		switch streamErr.Code {
		case http2.ErrCodeCancel, http2.ErrCodeNo:
			return bodyReadKindClientDisconnect
		default:
			return bodyReadKindTruncatedBody
		}
	}
	// 不匹配 http2.ConnectionError：两个 h2 实现里 closeStream 是唯一触达 body
	// pipe 的路径，其调用方只传 errClientDisconnected、StreamError 或 handler
	// 侧的哨兵，连接级中断到读取端时已是上面匹配过的断连文案。
	// 读超时是服务端该负责的；ECONNABORTED、EHOSTUNREACH 这类非超时网络错误
	// 是客户端链路的问题，混在一起会把后者谎报成"超时"。
	var netErr net.Error
	if errors.As(err, &netErr) {
		if netErr.Timeout() {
			return bodyReadKindTransportTimeout
		}
		return bodyReadKindTransport
	}
	return bodyReadKindIORead
}

// bodyReadErrorPolicy 是一种读取失败 kind 在响应线上的答案。
//
// ErrorType 只能取 isKnownOpsErrorType 接受的值：不在集合内的类型会被
// normalizeOpsErrorType 归一成 "api_error"，这张表想保留的区分就丢了。
type bodyReadErrorPolicy struct {
	Status    int
	ErrorType string
	Message   string
	// Record 表示这次失败是否该进 ops_error_logs。false 留给运维无法处置、
	// 只会抬高错误率的失败；结构化日志仍带完整 error_kind。
	Record bool
}

// bodyReadErrorFallbackPolicy 是未识别 kind 的答案，逐字等于这张表出现之前
// 每个入口给出的响应，让新增却漏配的 kind 退化为原有行为而不是别的东西。
//
// error type 与状态码同样重要："api_error" 会让 classifyOpsPhase /
// classifyOpsSeverity 把它从 request/P3 抬到 internal/P2，一次未分类的客户端
// 中断就成了值得告警的内部故障。
var bodyReadErrorFallbackPolicy = bodyReadErrorPolicy{
	Status:    http.StatusBadRequest,
	ErrorType: "invalid_request_error",
	Message:   "Failed to read request body",
	Record:    true,
}

var bodyReadErrorPolicies = map[string]bodyReadErrorPolicy{
	// 静态文案只是兜底：调用方拿得到 MaxBytesError 时会换成带上限值的
	// buildBodyTooLargeMessage。
	bodyReadKindMaxBytes: {
		Status:    http.StatusRequestEntityTooLarge,
		ErrorType: "invalid_request_error",
		Message:   "Request body exceeds the configured limit",
		Record:    true,
	},
	// 客户端中途放弃上传。没有人会收到这个响应，服务端也没有可修的缺陷，所以
	// 只记日志不进 ops_error_logs。499 与 concurrency_error_response.go 对
	// context.Canceled 的约定一致；error type 用 invalid_request_error，因为
	// 它是请求侧而不是内部故障。
	bodyReadKindClientDisconnect: {
		Status:    statusClientClosedRequest,
		ErrorType: "invalid_request_error",
		Message:   "Client closed the connection before the request body was fully received",
		Record:    false,
	},
	// 与 client_disconnect 的区别：连接还在，但收到的字节比 Content-Length 声明
	// 的少，可能是链路有问题，值得记录。
	bodyReadKindTruncatedBody: {
		Status:    http.StatusBadRequest,
		ErrorType: "invalid_request_error",
		Message:   "Request body ended prematurely; the declared Content-Length was not received",
		Record:    true,
	},
	// 等待请求体时读超时触发。这是服务端要负责的，所以是 api_error。
	bodyReadKindTransportTimeout: {
		Status:    http.StatusRequestTimeout,
		ErrorType: "api_error",
		Message:   "Timed out while reading the request body",
		Record:    true,
	},
	bodyReadKindTransport: {
		Status:    http.StatusBadRequest,
		ErrorType: "invalid_request_error",
		Message:   "The connection failed while the request body was being read",
		Record:    true,
	},
	bodyReadKindUnsupportedContentEncoding: {
		Status:    http.StatusUnsupportedMediaType,
		ErrorType: "invalid_request_error",
		Message:   "Unsupported Content-Encoding",
		Record:    true,
	},
	bodyReadKindDecodeContentEncoding: {
		Status:    http.StatusBadRequest,
		ErrorType: "invalid_request_error",
		Message:   "Failed to decode the request body with the declared Content-Encoding",
		Record:    true,
	},
	// io_read 是分类器自己的兜底，显式列出以便"每个 kind 都有策略"的检查成立，
	// 让 bodyReadErrorFallbackPolicy 真正只服务未知 kind。
	bodyReadKindIORead: bodyReadErrorFallbackPolicy,
}

func bodyReadErrorPolicyFor(kind string) bodyReadErrorPolicy {
	if policy, ok := bodyReadErrorPolicies[kind]; ok {
		return policy
	}
	return bodyReadErrorFallbackPolicy
}
