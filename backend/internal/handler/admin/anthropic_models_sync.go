package admin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// anthropicModelAggregation 决定多个账号的 /v1/models 如何合成一份列表。
type anthropicModelAggregation string

const (
	// anthropicModelAggregationUnion 用于「分组能路由到哪些模型」——只要有一个
	// 账号支持就算可用。
	anthropicModelAggregationUnion anthropicModelAggregation = "union"
	// anthropicModelAggregationIntersection 用于「同一份白名单要写给每个账号」
	// ——只有所有账号都支持才安全。
	anthropicModelAggregationIntersection anthropicModelAggregation = "intersection"
)

const (
	// 上游 /v1/models 是只读探测，四路并发足以在管理员的耐心范围内跑完几十个
	// 账号，又不至于把管理端的出口连接占满。
	anthropicModelSyncConcurrency = 4
	// 单账号超时独立于整批：一个吊死的代理不该拖垮其余账号的结果。
	anthropicModelSyncAccountTimeout = 15 * time.Second
	// 整批上限，保证接口在任何账号规模下都能在一分钟内返回。
	anthropicModelSyncTotalTimeout = 60 * time.Second
	// 模型目录以天为单位变化，缓存一档可以让「打开弹窗 → 改一处 → 再打开」
	// 不再每次都对每个账号各发一次上游请求。
	anthropicModelSyncCacheTTL = 5 * time.Minute
)

// anthropicModelCandidateTimeout 是分组候选接口给「实时补充」的独立预算。管理端
// 的 HTTP 客户端 30s 超时，而候选响应里静态候选与实时并集是同一份：整批预算若
// 沿用 anthropicModelSyncTotalTimeout（60s），账号池一慢，连静态候选都拿不到，
// 分组编辑弹窗直接空白。这里取远小于前端超时的值，超时就降级为静态候选。
// 是变量而非常量，测试要把它调小以免真的等上十秒。
var anthropicModelCandidateTimeout = 10 * time.Second

var (
	errAnthropicModelSyncUnavailable = errors.New("account test service is not configured")
	errAnthropicModelSyncNoAccounts  = errors.New("no active Anthropic account is available for /v1/models sync")
	errAnthropicModelSyncAllFailed   = errors.New("failed to fetch Anthropic /v1/models from every candidate account")
	errAnthropicModelSyncNoCommon    = errors.New("selected Anthropic accounts have no common models in /v1/models")
)

// anthropicAccountModelFailure 单个账号的失败明细。整批成功与否是聚合结果，
// 但管理员要能知道具体是哪个账号没答上来，否则只能逐个账号手动试。
type anthropicAccountModelFailure struct {
	AccountID int64  `json:"account_id"`
	Name      string `json:"name"`
	Error     string `json:"error"`
}

// anthropicModelSyncResult 聚合后的模型列表与逐账号失败明细。
type anthropicModelSyncResult struct {
	Models   []string
	Failures []anthropicAccountModelFailure
}

type anthropicModelCacheEntry struct {
	models    []string
	expiresAt time.Time
}

var (
	anthropicModelCacheMu sync.Mutex
	anthropicModelCache   = make(map[string]anthropicModelCacheEntry)
)

// fetchAnthropicModelsFromAccounts 并发拉取各账号上游 /v1/models 并按 aggregation
// 合成一份列表。requireAll=false 时部分账号失败不影响整体结果，失败明细随返回值
// 一起给出；requireAll=true 只用于「把同一份白名单写给每个账号」这种必须看到全
// 貌的场景。
func fetchAnthropicModelsFromAccounts(
	ctx context.Context,
	accountTestService *service.AccountTestService,
	accounts []*service.Account,
	aggregation anthropicModelAggregation,
	requireAll bool,
) (anthropicModelSyncResult, error) {
	if accountTestService == nil {
		return anthropicModelSyncResult{}, errAnthropicModelSyncUnavailable
	}

	eligible := make([]*service.Account, 0, len(accounts))
	preflightFailures := make([]anthropicAccountModelFailure, 0)
	for _, account := range accounts {
		if anthropicModelSyncEligible(account) {
			eligible = append(eligible, account)
			continue
		}
		if requireAll && account != nil {
			preflightFailures = append(preflightFailures, anthropicAccountModelFailure{
				AccountID: account.ID,
				Name:      account.Name,
				Error:     "Account is not active or cannot query Anthropic /v1/models",
			})
		}
	}
	if len(eligible) == 0 {
		return anthropicModelSyncResult{Failures: preflightFailures}, errAnthropicModelSyncNoAccounts
	}

	batchCtx, cancel := context.WithTimeout(ctx, anthropicModelSyncTotalTimeout)
	defer cancel()

	type accountOutcome struct {
		models []string
		err    error
	}
	outcomes := make([]accountOutcome, len(eligible))
	semaphore := make(chan struct{}, anthropicModelSyncConcurrency)
	var wg sync.WaitGroup
	for i, account := range eligible {
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case semaphore <- struct{}{}:
				defer func() { <-semaphore }()
			case <-batchCtx.Done():
				outcomes[i].err = batchCtx.Err()
				return
			}
			outcomes[i].models, outcomes[i].err = fetchAccountAnthropicModels(batchCtx, accountTestService, account)
		}()
	}
	wg.Wait()

	successful := make([][]string, 0, len(outcomes))
	failures := append(make([]anthropicAccountModelFailure, 0, len(preflightFailures)+len(outcomes)), preflightFailures...)
	for i, outcome := range outcomes {
		if outcome.err != nil {
			failures = append(failures, anthropicAccountModelFailure{
				AccountID: eligible[i].ID,
				Name:      eligible[i].Name,
				Error:     anthropicModelSyncFailureMessage(outcome.err),
			})
			continue
		}
		successful = append(successful, outcome.models)
	}
	if len(successful) == 0 {
		return anthropicModelSyncResult{Failures: failures}, errAnthropicModelSyncAllFailed
	}
	if requireAll && len(failures) > 0 {
		return anthropicModelSyncResult{Failures: failures}, fmt.Errorf(
			"failed to fetch Anthropic /v1/models from %d of %d accounts", len(failures), len(accounts))
	}

	if aggregation == anthropicModelAggregationIntersection {
		models := intersectSyncedModelIDs(successful)
		if len(models) == 0 {
			return anthropicModelSyncResult{Failures: failures}, errAnthropicModelSyncNoCommon
		}
		return anthropicModelSyncResult{Models: models, Failures: failures}, nil
	}
	return anthropicModelSyncResult{Models: unionSyncedModelIDs(successful), Failures: failures}, nil
}

// anthropicModelSyncEligible 只对能真正发出 /v1/models 的账号发请求：停用账号的
// 凭据往往已经失效，非 oauth/setup-token/apikey 的形态（影子账号等）不持自己的
// 凭据，对它们发请求只会白白攒下一堆失败明细。
func anthropicModelSyncEligible(account *service.Account) bool {
	if account == nil || !account.IsAnthropic() {
		return false
	}
	if account.Status != service.StatusActive {
		return false
	}
	switch account.Type {
	case service.AccountTypeOAuth, service.AccountTypeSetupToken, service.AccountTypeAPIKey:
		return true
	default:
		return false
	}
}

func fetchAccountAnthropicModels(
	ctx context.Context,
	accountTestService *service.AccountTestService,
	account *service.Account,
) ([]string, error) {
	cacheKey := anthropicModelCacheKey(account)
	if models, ok := lookupAnthropicModelCache(cacheKey, time.Now()); ok {
		return models, nil
	}

	accountCtx, cancel := context.WithTimeout(ctx, anthropicModelSyncAccountTimeout)
	defer cancel()

	// FetchUpstreamSupportedModels 的返回值已经过 dedupeAndSortModelIDs 归一化，
	// 且空列表在那一层就报错，这里不再重复一遍。
	models, err := accountTestService.FetchUpstreamSupportedModels(accountCtx, account)
	if err != nil {
		return nil, err
	}
	storeAnthropicModelCache(cacheKey, models, time.Now())
	return models, nil
}

// anthropicModelCacheKey 用账号 ID 加一份凭据指纹作键。不能用 UpdatedAt：被动
// 采样在每个成功的 Anthropic 响应之后走 UpdateExtra（SQL 里固定
// updated_at = NOW()），BatchUpdateLastUsed 同样，于是有流量的账号每次进来键都
// 是新的，5 分钟 TTL 对它们形同虚设。指纹只覆盖决定这次上游调用的凭据，重授权
// 或换 base_url 依然让缓存自然失效。
func anthropicModelCacheKey(account *service.Account) string {
	fingerprint := sha256.Sum256([]byte(strings.Join([]string{
		account.GetCredential("api_key"),
		account.GetCredential("access_token"),
		account.GetCredential("base_url"),
	}, "\x00")))
	return strconv.FormatInt(account.ID, 10) + "@" + hex.EncodeToString(fingerprint[:8])
}

func lookupAnthropicModelCache(key string, now time.Time) ([]string, bool) {
	anthropicModelCacheMu.Lock()
	defer anthropicModelCacheMu.Unlock()

	entry, ok := anthropicModelCache[key]
	if !ok || now.After(entry.expiresAt) {
		return nil, false
	}
	return append([]string(nil), entry.models...), true
}

func storeAnthropicModelCache(key string, models []string, now time.Time) {
	anthropicModelCacheMu.Lock()
	defer anthropicModelCacheMu.Unlock()

	// 换凭据会留下一个用不上的旧键；顺手清掉过期项，避免长期运行的进程把它们攒
	// 成一份只增不减的表。
	for cached, entry := range anthropicModelCache {
		if now.After(entry.expiresAt) {
			delete(anthropicModelCache, cached)
		}
	}
	anthropicModelCache[key] = anthropicModelCacheEntry{
		models:    append([]string(nil), models...),
		expiresAt: now.Add(anthropicModelSyncCacheTTL),
	}
}

// anthropicModelSyncFailureMessage 把内部错误收敛成可以回给管理端的短句。上游
// 客户端的错误里可能带内网地址与凭据片段，不能原样透出。
func anthropicModelSyncFailureMessage(err error) string {
	var syncErr *service.UpstreamModelSyncError
	if errors.As(err, &syncErr) {
		return syncErr.SafeMessage()
	}
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "Timed out while fetching /v1/models"
	case errors.Is(err, context.Canceled):
		return "Fetching /v1/models was canceled"
	default:
		return "Failed to fetch /v1/models"
	}
}

func unionSyncedModelIDs(modelLists [][]string) []string {
	seen := make(map[string]struct{})
	out := make([]string, 0)
	for _, models := range modelLists {
		for _, model := range models {
			if _, ok := seen[model]; ok {
				continue
			}
			seen[model] = struct{}{}
			out = append(out, model)
		}
	}
	return out
}

func intersectSyncedModelIDs(modelLists [][]string) []string {
	if len(modelLists) == 0 {
		return nil
	}
	counts := make(map[string]int)
	for _, models := range modelLists {
		for _, model := range models {
			counts[model]++
		}
	}
	out := make([]string, 0, len(modelLists[0]))
	for _, model := range modelLists[0] {
		if counts[model] == len(modelLists) {
			out = append(out, model)
		}
	}
	return out
}
