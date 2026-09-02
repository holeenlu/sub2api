package service

// 无可用账号兜底分组（groups.fallback_group_id_on_no_account）。
//
// 语义：当前分组选不出可用账号——账号被封、周/日配额耗尽、限流冷却中、
// 手动置为不可调度、模型全被过滤——调度层把 groupID 换成配置的兜底分组重跑
// 一次选号，借用它的账号池继续服务。
//
// 只借账号，不改计费归属：价格倍率、利润门的下游倍率 D、配额与订阅扣减、
// usage_logs.group_id 全部仍按 API Key 自己的分组结算。这一点由
// WithGatewayTokenRequestPricing 在请求入口钉住的 billing group 保证
// （见 gateway_request_pricing.go），与既有的 Claude Code 降级分组
// （fallback_group_id）行为一致。
//
// 换组后跟着走的是调度侧的东西：账号池、粘性会话命名空间
// sticky_session:{groupID}:{hash}、渠道定价限制、模型路由、利润门的门配置。
//
// 只有容量性质的失败会触发换组。分组的显式策略拒绝——渠道定价把模型列为受限、
// 目标分组是 claude_code_only——不换组：那是管理员对这个分组下的禁令，借别的
// 分组账号池绕过它等于让用户用 G 的 Key 调到 G 明令禁止的模型，还按 G 计费。
// 这类错误由 ErrSchedulingPolicyRejected 标记，见 isNoAccountFallbackTriggerError。

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
)

// ErrSchedulingPolicyRejected 标记「分组的显式策略拒绝了这次请求」，把它与
// 「分组里一个可用账号都挑不出来」区分开：后者是容量问题，正是兜底链要解决的；
// 前者是管理员对这个分组的配置结果，借别的分组账号池重跑等于绕过禁令。
//
// 这类错误同时仍是 ErrNoAvailableAccounts（对外的状态码与文案不变），只是不再
// 触发换组。
var ErrSchedulingPolicyRejected = errors.New("scheduling policy rejected")

// schedulingPolicyRejection 给一个选号错误盖上策略拒绝标记，同时保留原错误链。
type schedulingPolicyRejection struct{ err error }

func (e schedulingPolicyRejection) Error() string { return e.err.Error() }

func (e schedulingPolicyRejection) Unwrap() error { return e.err }

func (e schedulingPolicyRejection) Is(target error) bool {
	return target == ErrSchedulingPolicyRejected
}

// newChannelModelRestrictedError 构造「本分组的渠道定价配置禁用了这个模型」。
func newChannelModelRestrictedError(requestedModel string) error {
	return schedulingPolicyRejection{
		err: fmt.Errorf("%w supporting model: %s (channel pricing restriction)", ErrNoAvailableAccounts, requestedModel),
	}
}

type noAccountFallbackActiveCtxKey struct{}

// withNoAccountFallbackActive 标记「当前调用栈已有一层在管兜底链」。
// 选号入口之间存在嵌套调用（如 SelectAccountWithLoadAwareness 内部会再调
// SelectAccountForModelWithExclusions），没有这个标记内层会先换组返回一个
// 属于兜底分组的账号，而外层的粘性与槽位逻辑仍按原分组走，两边对不上。
func withNoAccountFallbackActive(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, noAccountFallbackActiveCtxKey{}, struct{}{})
}

func noAccountFallbackActive(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	_, ok := ctx.Value(noAccountFallbackActiveCtxKey{}).(struct{})
	return ok
}

// isNoAccountFallbackTriggerError 判断选号错误是否值得换组重试。
// 只认「池子空了」这一类错误：配置错误（分组不存在）、上下文取消等一律不换，
// 换了也是白换，还会放大故障时的延迟；策略拒绝同样不换，见
// ErrSchedulingPolicyRejected。
func isNoAccountFallbackTriggerError(err error) bool {
	if err == nil || errors.Is(err, ErrSchedulingPolicyRejected) {
		return false
	}
	return errors.Is(err, ErrNoAvailableAccounts) || errors.Is(err, ErrNoAvailableCompactAccounts)
}

// groupLiteLoader 按 ID 读取分组配置（不含账号计数聚合）。调用方应优先命中
// ctx 里认证中间件放入的分组快照，再回源仓库。
type groupLiteLoader func(ctx context.Context, groupID int64) (*Group, error)

// noAccountFallbackChain 沿 fallback_group_id_on_no_account 逐跳解析兜底分组，
// 一条链只创建一次：已加载的当前分组留在 source 上，下一跳只需读目标分组，
// 每跳恰好一次回源。
type noAccountFallbackChain struct {
	origin *int64
	load   groupLiteLoader
	// hopContext 为换组后的每一跳准备 ctx（nil 表示原样传递）。GatewayService
	// 借它把目标分组放进 ctxkey.Group，跳内的分组解析零回源；OpenAI 族不能这么做：
	// 它的利润门直接拿 ctxkey.Group 当计费分组。
	//
	// 这个钩子本身是「计费分组」与「调度分组」挤在同一把 ctx key 上的症状。
	// 彻底的修法是让 OpenAI 族也像 gateway 族那样在请求入口钉住一把独立的
	// billing group key（resolveOpenAIProfitControlGate 优先读它），之后两族都
	// 无条件换 ctx 分组，这个字段就可以删掉。在那之前，两族各自的约定由
	// TestGatewayNoAccountFallbackKeepsBillingGroup 与
	// TestOpenAINoAccountFallbackKeepsBillingGroup 锁住。
	hopContext func(ctx context.Context, target *Group) context.Context

	source  *Group
	visited map[int64]struct{}
	hops    int
}

func newNoAccountFallbackChain(origin *int64, load groupLiteLoader, hopContext func(context.Context, *Group) context.Context) *noAccountFallbackChain {
	chain := &noAccountFallbackChain{origin: origin, load: load, hopContext: hopContext, visited: map[int64]struct{}{}}
	if origin != nil && *origin > 0 {
		chain.visited[*origin] = struct{}{}
	}
	return chain
}

func (c *noAccountFallbackChain) loadGroup(ctx context.Context, groupID int64) *Group {
	if c.load == nil {
		return nil
	}
	group, err := c.load(ctx, groupID)
	if err != nil {
		// 跑在选号失败的冷路径上：读不到就当作「没有兜底」，不把配置读取故障
		// 放大成额外的请求失败。
		slog.Debug("scheduling.group_fallback_load_failed", "group_id", groupID, "error", err)
		return nil
	}
	return group
}

// next 返回兜底链上的下一跳分组。nil 表示到此为止：没配兜底、目标读不到、
// 目标非 active、目标与来源不同平台、成环，或已达 MaxNoAccountFallbackHops。
func (c *noAccountFallbackChain) next(ctx context.Context) *Group {
	if c == nil || c.origin == nil || *c.origin <= 0 || c.hops >= MaxNoAccountFallbackHops {
		return nil
	}
	if c.source == nil {
		if c.source = c.loadGroup(ctx, *c.origin); c.source == nil {
			return nil
		}
	}
	targetID := c.source.FallbackGroupIDOnNoAccount
	if targetID == nil || *targetID <= 0 {
		return nil
	}
	if _, seen := c.visited[*targetID]; seen {
		return nil
	}
	target := c.loadGroup(ctx, *targetID)
	if target == nil || target.Status != StatusActive {
		return nil
	}
	// 选号本身按平台过滤，异平台分组永远选不出账号；保存时已校验，这里再挡一次
	// 是因为来源/目标分组的平台可能在配置后被单独改动。
	if target.Platform != c.source.Platform {
		return nil
	}
	c.visited[target.ID] = struct{}{}
	c.hops++
	slog.Info("scheduling.group_fallback_on_no_account",
		"from_group_id", c.source.ID,
		"to_group_id", target.ID,
		"hop", c.hops)
	c.source = target
	return target
}

// noAccountFallbackAttempt 在指定分组内跑一次完整选号。
type noAccountFallbackAttempt[T any] func(ctx context.Context, groupID *int64) (T, error)

// schedulingGroupStamper 由「记得住自己是从哪个分组选出来的」选号结果实现。
type schedulingGroupStamper interface {
	stampSchedulingGroupID(groupID *int64)
}

// stampSchedulingGroup 在链上每一次成功返回前补记实际选号的分组：调用方只要
// 把选号包进链，结果就一定带着正确的分组，新增入口漏写不再是隐患。attempt
// 自己已经记过（Claude Code 降级把分组解析成了别的分组）时保留它的值，那才是
// 账号真正的来源。返回值为非选号结果类型（如 *Account）时什么也不做。
func stampSchedulingGroup[T any](result T, groupID *int64, err error) (T, error) {
	if err != nil {
		return result, err
	}
	if stamper, ok := any(result).(schedulingGroupStamper); ok {
		stamper.stampSchedulingGroupID(groupID)
	}
	return result, nil
}

// runWithNoAccountFallback 把一次选号包进兜底链：起点分组选不出账号时依次换成
// 链上的兜底分组重试，返回第一次成功的结果，否则返回最后一跳的错误。
// 嵌套调用（ctx 已带标记）只跑起点，由最外层管链。
func runWithNoAccountFallback[T any](ctx context.Context, chain *noAccountFallbackChain, attempt noAccountFallbackAttempt[T]) (T, error) {
	if noAccountFallbackActive(ctx) {
		result, err := attempt(ctx, chain.origin)
		return stampSchedulingGroup(result, chain.origin, err)
	}
	ctx = withNoAccountFallbackActive(ctx)
	result, err := attempt(ctx, chain.origin)
	return continueNoAccountFallback(ctx, chain, result, err, attempt)
}

// continueNoAccountFallback 从「起点已经试过、结果是 (result, err)」接着沿链走。
// 起点在别处已扫过一遍的调用方（Images 的能力降级）直接用它，不再重复扫起点；
// ctx 必须已带 withNoAccountFallbackActive 标记。
func continueNoAccountFallback[T any](ctx context.Context, chain *noAccountFallbackChain, result T, err error, attempt noAccountFallbackAttempt[T]) (T, error) {
	for isNoAccountFallbackTriggerError(err) {
		target := chain.next(ctx)
		if target == nil {
			return result, err
		}
		hopCtx := ctx
		if chain.hopContext != nil {
			hopCtx = chain.hopContext(ctx, target)
		}
		targetID := target.ID
		hopResult, hopErr := attempt(hopCtx, &targetID)
		if hopErr == nil {
			return stampSchedulingGroup(hopResult, &targetID, nil)
		}
		if errors.Is(hopErr, ErrClaudeCodeOnly) || errors.Is(hopErr, ErrSchedulingPolicyRejected) {
			// 目标分组的策略把本请求挡在外面（claude_code_only、渠道模型禁令）：
			// 这一跳对本请求不可用，但不能让它替换掉「无可用账号」成为最终答复，
			// 也不能就此终止——继续沿链走，上一跳的错误保留。
			continue
		}
		result, err = hopResult, hopErr
	}
	// 起点自己就服务了这次请求（err == nil），或链走不下去：前者补记起点分组，
	// 后者由 stampSchedulingGroup 按 err != nil 原样返回。
	return stampSchedulingGroup(result, chain.origin, err)
}
