package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/Wei-Shaw/sub2api/internal/service/basispoints"
	"github.com/gin-gonic/gin"
)

// Optional cache interface keeps unrelated gateway cache implementations unchanged.
// All values are small progress digests; no image, tool arguments or messages persist.
type ExcelBPSImagePolicyCache interface {
	LoadBPSImageProgress(context.Context, string) (string, error)
	SaveBPSImageProgress(context.Context, string, string, string) error
	ClaimBPSImageWarning(context.Context, string) (bool, error)
	ResetBPSImageWarning(context.Context, string) error
}
type excelImagePolicyState struct {
	history        *basispoints.ImageHistory
	cache          ExcelBPSImagePolicyCache
	scope          string
	previous       string
	split          int
	compact        bool
	maxImages      int
	reconciledBody []byte
	checkpoints    []basispoints.ImageCheckpoint
	historyBytes   int64
	addedBytes     int64
}
type excelImagePolicyCacheState struct {
	Progress    basispoints.ImageProgress
	Checkpoints []basispoints.ImageCheckpoint
}
type excelImagePolicyError struct {
	Status        int
	Code, Message string
	diagnostics   map[string]any
}

func (e *excelImagePolicyError) Error() string { return e.Message }
func imagePolicyError(status int, code, message string) error {
	return &excelImagePolicyError{Status: status, Code: code, Message: message}
}

func (s *OpenAIGatewayService) prepareExcelImagePolicy(ctx context.Context, c *gin.Context, body []byte, settings ExcelBPSImageRelaySettings, scope string, trusted bool) (_ *excelImagePolicyState, policyErr error) {
	state := &excelImagePolicyState{maxImages: settings.Limits.MaxImages}
	if !settings.Enabled {
		return state, nil
	}
	if settings.Policy == "" {
		settings.Policy = DefaultExcelBPSImageLimitPolicy
	}
	history, err := basispoints.InspectImageHistory(body)
	if err != nil {
		return nil, err
	}
	state.history = history
	state.compact = isOpenAIResponsesCompactPath(c) || HasCompactionTriggerInInput(body)
	native := settings.Mode == ExcelBPSImageModeNative || settings.Mode == ""
	var totalBytes int64
	const nativeMaxBytes = int64(basispoints.NativeImageRequestBytes)
	defer func() {
		var failure *excelImagePolicyError
		if errors.As(policyErr, &failure) {
			failure.diagnostics = map[string]any{
				"image_policy": settings.Policy, "native_mode": native,
				"inline_image_count": history.Count, "image_count_limit": settings.Limits.MaxImages,
				"trusted_identity": trusted, "compaction_attempted": false, "generation_started": false,
			}
			if native {
				failure.diagnostics["decoded_image_bytes"] = totalBytes
				failure.diagnostics["native_request_limit_bytes"] = nativeMaxBytes
			}
		}
	}()
	if native {
		totalBytes, err = basispoints.InlineImageBytes(history.Input, basispoints.NativeImageMaxMiB)
		if err != nil {
			return nil, err
		}
	}
	// An explicitly disabled policy never initiates billable compaction. Still
	// diagnose the hard byte limit before attachment planning, even without Redis
	// or a stable identity, instead of returning an opaque generic 400.
	if settings.Policy == "off" {
		if native && totalBytes > nativeMaxBytes {
			if state.compact {
				return nil, imagePolicyError(400, "basispoints_image_compaction_too_large", "The compact input itself exceeds 32 MiB of decoded images; reduce image sizes or start a new conversation")
			}
			return nil, imagePolicyError(400, "basispoints_image_auto_compact_disabled", fmt.Sprintf("Decoded inline images total %.2f MiB, exceeding the 32 MiB native limit; automatic history compaction is disabled (excel_bps_image_limit_policy=off). Set it to auto_compact in /admin/settings to compact separable history, or resize/split images. Retrying unchanged will not help. 图片累计 %.2f MiB；自动历史压缩已关闭，请在系统设置中启用自动压缩，或缩小、拆分图片。", float64(totalBytes)/(1<<20), float64(totalBytes)/(1<<20)))
		}
		return state, nil
	}
	cache, ok := s.cache.(ExcelBPSImagePolicyCache)
	if !ok || !trusted {
		if state.compact && totalBytes > nativeMaxBytes {
			return nil, imagePolicyError(400, "basispoints_image_compaction_too_large", "The compact input itself exceeds 32 MiB of decoded images; reduce image sizes or start a new conversation")
		}
		if state.compact || (totalBytes <= nativeMaxBytes && (history.Count < settings.Limits.MaxImages-settings.WarningRemaining || (settings.Policy == "auto_compact" && history.Count <= settings.Limits.MaxImages))) {
			return state, nil
		}
		return nil, imagePolicyError(503, "basispoints_image_policy_state_unavailable", "Image policy requires a stable conversation identity and available session state; retry with the same conversation ID")
	}
	state.cache = cache
	state.scope = fmt.Sprintf("%s/policy:%s/limit:%d/warn:%d/reserve:%d", scope, settings.Policy, settings.Limits.MaxImages, settings.WarningRemaining, settings.CompactReserve)
	state.previous, err = cache.LoadBPSImageProgress(ctx, state.scope)
	if err != nil {
		if state.compact {
			return state, nil
		}
		return nil, imagePolicyError(503, "basispoints_image_policy_state_unavailable", "Image policy session state is unavailable; retry later")
	}
	var cached excelImagePolicyCacheState
	if state.previous != "" {
		if err := json.Unmarshal([]byte(state.previous), &cached); err != nil {
			return nil, imagePolicyError(503, "basispoints_image_policy_state_unavailable", "Invalid image session state; run compact manually")
		}
	}
	state.checkpoints = cached.Checkpoints
	if len(cached.Checkpoints) > 0 {
		changed, e := history.Reconcile(cached.Checkpoints)
		if e != nil {
			return nil, imagePolicyError(400, "basispoints_image_history_boundary", e.Error())
		}
		if changed {
			state.reconciledBody, err = history.Body()
			if err != nil {
				return nil, err
			}
			if native {
				totalBytes, err = basispoints.InlineImageBytes(history.Input, basispoints.NativeImageMaxMiB)
				if err != nil {
					return nil, err
				}
			}
		}
	}
	n := history.Count
	limit := settings.Limits.MaxImages
	if state.compact {
		if totalBytes > nativeMaxBytes {
			return nil, imagePolicyError(400, "basispoints_image_compaction_too_large", "The compact input itself exceeds 32 MiB of decoded images; reduce image sizes or start a new conversation. 本次压缩输入本身超过 32 MiB，请缩小图片或新建会话。")
		}
		return state, nil
	} // Ordinary and warning thresholds never block explicit compact.
	if settings.Policy == "warn" {
		if totalBytes > nativeMaxBytes {
			return nil, imagePolicyError(400, "basispoints_image_bytes_limit_reached", fmt.Sprintf("Decoded inline images total %.2f MiB, exceeding the 32 MiB native limit; enable automatic image compaction for separable history, or reduce image sizes. 图片累计 %.2f MiB 超过原生 32 MiB 上限；可启用自动历史压缩，或缩小图片。", float64(totalBytes)/(1<<20), float64(totalBytes)/(1<<20)))
		}
		if n > limit-settings.CompactReserve {
			return nil, imagePolicyError(400, "basispoints_image_limit_reached", "Image limit reached. Run compact before adding more images. 图片空间不足，请先执行 compact 压缩。")
		}
		if n >= limit-settings.WarningRemaining {
			first, e := cache.ClaimBPSImageWarning(ctx, state.scope)
			if e != nil {
				return nil, imagePolicyError(503, "basispoints_image_policy_state_unavailable", "Image policy session state is unavailable; retry later")
			}
			if first {
				remaining := limit - settings.CompactReserve - n
				return nil, imagePolicyError(400, "basispoints_image_compact_recommended", fmt.Sprintf("按当前请求计算，还可新增 %d 张图片，建议先执行 compact 压缩；直接重试可继续。Based on this request, %d additional image slots remain; run compact or retry to continue.", remaining, remaining))
			}
		}
		return state, nil
	}
	if n <= limit && totalBytes <= nativeMaxBytes {
		return state, nil
	}
	if !isSupportedImagePolicyCodex(c) {
		return nil, imagePolicyError(400, "basispoints_image_auto_compact_unsupported", "Automatic image compaction requires a Codex client; run compact manually")
	}
	var progress *basispoints.ImageProgress
	if state.previous != "" {
		var p basispoints.ImageProgress
		if json.Unmarshal([]byte(state.previous), &p) == nil && p.Digest != "" {
			progress = &p
		} else {
			var cached excelImagePolicyCacheState
			if json.Unmarshal([]byte(state.previous), &cached) == nil && cached.Progress.Digest != "" {
				progress = &cached.Progress
			}
		}
	}
	split, err := history.Split(progress)
	if err != nil {
		return nil, imagePolicyError(400, "basispoints_image_history_boundary", err.Error())
	}
	added := basispoints.CountInlineImages(history.Input[split:])
	if added > limit {
		return nil, imagePolicyError(400, "basispoints_image_batch_too_large", fmt.Sprintf("本次新增 %d 张图片超过单次上限 %d，请减少图片或分批发送。Too many new images in one request.", added, limit))
	}
	if split == 0 || basispoints.CountInlineImages(history.Input[:split]) > limit {
		return nil, imagePolicyError(400, "basispoints_image_history_limit", "Existing history exceeds the compaction image limit; increase the image limit temporarily and run compact")
	}
	if native {
		state.addedBytes, err = basispoints.InlineImageBytes(history.Input[split:], basispoints.NativeImageMaxMiB)
		if err != nil {
			return nil, err
		}
		state.historyBytes = totalBytes - state.addedBytes
		if state.addedBytes > nativeMaxBytes {
			return nil, imagePolicyError(400, "basispoints_image_batch_too_large", "New images alone exceed 32 MiB; compacting history cannot reduce them. Resize or split the new images. 本次新增图片自身超过 32 MiB，请缩小图片或分批发送。")
		}
		if state.historyBytes > nativeMaxBytes {
			return nil, imagePolicyError(400, "basispoints_image_history_limit", "History alone exceeds the 32 MiB compaction budget; reduce historical image sizes or start a new conversation. 历史图片自身超过 32 MiB，无法安全执行单次压缩，请缩小历史图片或新建会话。")
		}
	}
	if len(state.checkpoints) >= 16 {
		return nil, imagePolicyError(400, "basispoints_image_checkpoint_limit", "Run compact manually to reset the image history checkpoint chain")
	}
	state.split = split
	// Native preflight validates both partitions independently before any upload;
	// relay retains its existing aggregate byte protection.
	state.maxImages = n
	return state, nil
}
func isSupportedImagePolicyCodex(c *gin.Context) bool {
	if c == nil || c.Request == nil {
		return false
	}
	// Existing transport identity, not an authorization boundary. State remains scoped
	// by the authenticated API key/account regardless of user-supplied client headers.
	return openai.IsCodexOfficialClientRequestStrict(c.Request.UserAgent())
}

// Persist before exposing a compact window, including when generation later fails.
// CAS conflicts abort before any semantic client output.
func (p *excelImagePolicyState) checkpoint(ctx context.Context, window []any) error {
	cp, err := p.history.Checkpoint(p.split, window)
	if err != nil {
		return err
	}
	next := append(append([]basispoints.ImageCheckpoint{}, p.checkpoints...), cp)
	raw, err := json.Marshal(excelImagePolicyCacheState{Progress: p.history.Progress(), Checkpoints: next})
	if err != nil {
		return err
	}
	if err = p.cache.SaveBPSImageProgress(ctx, p.scope, p.previous, string(raw)); err != nil {
		return err
	}
	p.previous = string(raw)
	p.checkpoints = next
	return nil
}
func (p *excelImagePolicyState) finish(ctx context.Context, reset bool) {
	if p == nil || p.cache == nil || p.history == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	if p.compact {
		p.checkpoints = nil
	}
	progress := excelImagePolicyCacheState{Progress: p.history.Progress(), Checkpoints: p.checkpoints}
	raw, err := json.Marshal(progress)
	if err == nil {
		_ = p.cache.SaveBPSImageProgress(ctx, p.scope, p.previous, string(raw))
	}
	if reset {
		_ = p.cache.ResetBPSImageWarning(ctx, p.scope)
	}
}
func addImagePolicyUsage(to *OpenAIUsage, from OpenAIUsage) {
	to.InputTokens += from.InputTokens
	to.OutputTokens += from.OutputTokens
	to.ImageInputTokens += from.ImageInputTokens
	to.ImageOutputTokens += from.ImageOutputTokens
	to.ImageCacheReadTokens += from.ImageCacheReadTokens
	to.CacheCreationInputTokens += from.CacheCreationInputTokens
	to.CacheCreation5mTokens += from.CacheCreation5mTokens
	to.CacheCreation1hTokens += from.CacheCreation1hTokens
	to.CacheCreation30mTokens += from.CacheCreation30mTokens
	to.CacheCreation12hTokens += from.CacheCreation12hTokens
	to.CacheReadInputTokens += from.CacheReadInputTokens
}

func subtractImagePolicyUsage(to *OpenAIUsage, from OpenAIUsage) {
	from.InputTokens = -from.InputTokens
	from.OutputTokens = -from.OutputTokens
	from.ImageInputTokens = -from.ImageInputTokens
	from.ImageOutputTokens = -from.ImageOutputTokens
	from.ImageCacheReadTokens = -from.ImageCacheReadTokens
	from.CacheCreationInputTokens = -from.CacheCreationInputTokens
	from.CacheCreation5mTokens = -from.CacheCreation5mTokens
	from.CacheCreation1hTokens = -from.CacheCreation1hTokens
	from.CacheCreation30mTokens = -from.CacheCreation30mTokens
	from.CacheCreation12hTokens = -from.CacheCreation12hTokens
	from.CacheReadInputTokens = -from.CacheReadInputTokens
	addImagePolicyUsage(to, from)
}
