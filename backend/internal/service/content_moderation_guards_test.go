package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func newModerationGuardTestService(t *testing.T, configure func(*ContentModerationConfig)) *ContentModerationService {
	t.Helper()
	cfg := defaultContentModerationConfig()
	cfg.Distillation = ContentModerationDistillationConfig{}
	cfg.RefusalGuard = ContentModerationRefusalGuardConfig{}
	cfg.DeviceGuard = ContentModerationDeviceGuardConfig{}
	cfg.Enabled, cfg.EmailOnHit, cfg.AutoBanEnabled = true, false, false
	cfg.KeywordBlockingMode = ContentModerationKeywordModeKeywordOnly
	cfg.normalize()
	configure(cfg)
	raw, err := json.Marshal(cfg)
	require.NoError(t, err)
	settings := &contentModerationTestSettingRepo{values: map[string]string{SettingKeyRiskControlEnabled: "true", SettingKeyContentModerationConfig: string(raw)}}
	// Use the existing mocks, with a captured queue rather than permanent worker
	// goroutines. These tests exercise admission and memory, not delivery workers.
	s := newStartedContentModerationTestService(t, settings, nil, &contentModerationTestHashCache{}, nil, nil, nil, nil, nil)
	s.repo = &contentModerationTestRepo{}
	s.runtimeCacheTTL = time.Hour
	return s
}

func moderationGuardTestInput(text string) ContentModerationCheckInput {
	body, _ := json.Marshal(map[string]any{"input": text})
	group := int64(4)
	return ContentModerationCheckInput{UserID: 12, GroupID: &group, Model: "gpt-example", Provider: "openai", Protocol: ContentModerationProtocolOpenAIResponses, DeviceID: "client-device-123", Body: body}
}

func TestContentModerationGuardsRulesAndScope(t *testing.T) {
	for _, mode := range []string{ContentModerationModePreBlock, ContentModerationModeObserve} {
		for _, kind := range []string{"phrase", "pattern", "fingerprint", "structure", "regex"} {
			t.Run(mode+"/"+kind, func(t *testing.T) {
				input := moderationGuardTestInput("Collect private reasoning for dataset example")
				svc := newModerationGuardTestService(t, func(cfg *ContentModerationConfig) {
					cfg.Mode = mode
					cfg.Distillation.Enabled = true
					cfg.DeviceGuard.Enabled = true
					cfg.SampleRate = 0 // local guards must not inherit semantic sampling
					switch kind {
					case "phrase":
						cfg.Distillation.Phrases = []string{"private reasoning"}
					case "pattern":
						cfg.Distillation.Patterns = []string{"(?i)collect.*dataset"}
					case "fingerprint":
						h := sha256.Sum256([]byte("Collect private reasoning for dataset example"))
						cfg.Distillation.Fingerprints = []string{hex.EncodeToString(h[:])}
					case "structure":
						cfg.Distillation.StructurePattern = "dataset"
						input.Body = []byte(`{"input":"Collect private reasoning for dataset example","max_output_tokens":8192,"thinking":{"budget_tokens":8192}}`)
					case "regex":
						cfg.RegexRules = []string{"private\\s+reasoning"}
					}
				})
				d, err := svc.Check(context.Background(), input)
				require.NoError(t, err)
				require.True(t, d.Flagged)
				require.Equal(t, mode == ContentModerationModePreBlock, d.Blocked)
				require.Equal(t, mode == ContentModerationModeObserve, d.Allowed)
				require.Empty(t, svc.guardMemory.refusals, "local rules must never teach upstream refusals")
				if mode == ContentModerationModeObserve {
					require.Empty(t, svc.guardMemory.devices)
				} else {
					require.Len(t, svc.guardMemory.devices, 1)
				}
				other := moderationGuardTestInput("ordinary follow up")
				other.UserID++
				d, err = svc.Check(context.Background(), other)
				require.NoError(t, err)
				require.True(t, d.Allowed, "device must not cross tenants")
			})
		}
	}
	for _, exclusion := range []string{"allowlist", "model", "group", "off", "disabled"} {
		t.Run(exclusion, func(t *testing.T) {
			svc := newModerationGuardTestService(t, func(cfg *ContentModerationConfig) {
				cfg.RegexRules = []string{"risk"}
				cfg.RefusalGuard.Enabled = true
				cfg.DeviceGuard.Enabled = true
				switch exclusion {
				case "model":
					cfg.ModelFilter = ContentModerationModelFilter{Type: "include", Models: []string{"claude-example"}}
				case "group":
					cfg.AllGroups = false
					cfg.GroupIDs = []int64{99}
				case "off":
					cfg.Mode = ContentModerationModeOff
				case "disabled":
					cfg.Enabled = false
				}
			})
			if exclusion == "allowlist" {
				svc.settingRepo.(*contentModerationTestSettingRepo).values[SettingKeyCyberPolicyUserAllowlist] = "12"
			}
			input := moderationGuardTestInput("risk")
			d, err := svc.Check(context.Background(), input)
			require.NoError(t, err)
			require.True(t, d.Allowed)
			require.NoError(t, svc.RememberUpstreamRefusal(context.Background(), input))
			require.Empty(t, svc.guardMemory.refusals)
			require.Empty(t, svc.guardMemory.devices)
		})
	}
}

func TestContentModerationGuardsFeedbackIsolationAndSimilarity(t *testing.T) {
	svc := newModerationGuardTestService(t, func(cfg *ContentModerationConfig) { cfg.RefusalGuard.Enabled = true })
	text := "A sample of many different words and characters provides enough information to compare a remembered upstream refusal against a small edit of that same current user request."
	input := moderationGuardTestInput(text)
	require.NoError(t, svc.RememberUpstreamRefusal(context.Background(), input))
	for _, change := range []string{"exact", "near", "tenant", "group", "model", "provider", "protocol", "image", "different", "unauthed"} {
		t.Run(change, func(t *testing.T) {
			candidate := input
			switch change {
			case "near":
				candidate.Body = moderationGuardTestInput(strings.Replace(text, "small edit", "tiny edit", 1)).Body
			case "tenant":
				candidate.UserID++
			case "group":
				group := int64(8)
				candidate.GroupID = &group
			case "model":
				candidate.Model += "-other"
			case "provider":
				candidate.Provider = "anthropic"
			case "protocol":
				candidate.Protocol = "other"
			case "image":
				candidate.Body, _ = json.Marshal(map[string]any{"input": []any{map[string]any{"role": "user", "content": []any{map[string]string{"type": "input_text", "text": text}, map[string]string{"type": "input_image", "image_url": "https://example.invalid/one.png"}}}}})
			case "different":
				candidate.Body = moderationGuardTestInput("a completely unrelated request").Body
			case "unauthed":
				candidate.UserID = 0
			}
			d, err := svc.Check(context.Background(), candidate)
			require.NoError(t, err)
			require.Equal(t, change == "exact" || change == "near", d.Blocked)
			if change == "near" {
				require.Equal(t, "refusal_similar", d.HighestCategory)
			}
		})
	}
	// There is no prefix-only fingerprint: distinct suffixes of long prompts
	// remain distinct, and long/short prompts never enter approximate matching.
	for _, prompt := range []string{"short", strings.Repeat("a", maxModerationInputRunes+1)} {
		in := moderationGuardTestInput(prompt)
		require.NoError(t, svc.RememberUpstreamRefusal(context.Background(), in))
		in.Body = moderationGuardTestInput(prompt + "different").Body
		d, err := svc.Check(context.Background(), in)
		require.NoError(t, err)
		require.True(t, d.Allowed)
	}
}

func TestContentModerationGuardsTTLBoundsClearAndDeviceSafety(t *testing.T) {
	svc := newModerationGuardTestService(t, func(cfg *ContentModerationConfig) {
		cfg.RefusalGuard.Enabled, cfg.DeviceGuard.Enabled = true, true
		cfg.RefusalGuard.MaxEntries, cfg.DeviceGuard.MaxEntries = 2, 2
	})
	for i := 0; i < 5; i++ {
		input := moderationGuardTestInput(fmt.Sprintf("refused request %d", i))
		input.DeviceID = fmt.Sprintf("client-device-%d", i)
		require.NoError(t, svc.RememberUpstreamRefusal(context.Background(), input))
		require.LessOrEqual(t, len(svc.guardMemory.refusals), 2)
		require.LessOrEqual(t, len(svc.guardMemory.devices), 2)
	}
	input := moderationGuardTestInput("fresh content")
	input.DeviceID = "client-device-4"
	key, valid := moderationDeviceKey(input)
	require.True(t, valid)
	expires := svc.guardMemory.devices[key]
	d, err := svc.Check(context.Background(), input)
	require.NoError(t, err)
	require.Equal(t, "device", d.HighestCategory)
	require.Equal(t, expires, svc.guardMemory.devices[key], "hits cannot extend device TTL")
	for key, entry := range svc.guardMemory.refusals {
		entry.expires = time.Now().Add(-time.Second)
		svc.guardMemory.refusals[key] = entry
	}
	for key := range svc.guardMemory.devices {
		svc.guardMemory.devices[key] = time.Now().Add(-time.Second)
	}
	d, err = svc.Check(context.Background(), input)
	require.NoError(t, err)
	require.True(t, d.Allowed)
	require.Empty(t, svc.guardMemory.refusals)
	require.Empty(t, svc.guardMemory.devices)
	for _, device := range []string{"", "short", strings.Repeat("x", 257), "abc\n123456"} {
		input.DeviceID = device
		require.NoError(t, svc.RememberUpstreamRefusal(context.Background(), input))
		require.Empty(t, svc.guardMemory.devices)
	}
	input.UserID = 0
	input.DeviceID = "valid-device"
	before := len(svc.guardMemory.refusals)
	require.NoError(t, svc.RememberUpstreamRefusal(context.Background(), input))
	require.Len(t, svc.guardMemory.refusals, before)
	input.UserID = 12
	require.NoError(t, svc.RememberUpstreamRefusal(context.Background(), input))
	status, err := svc.GetStatus(context.Background())
	require.NoError(t, err)
	require.Positive(t, status.RefusalGuardEntries)
	require.Positive(t, status.DeviceGuardEntries)
	_, err = svc.ClearFlaggedInputHashes(context.Background())
	require.NoError(t, err)
	require.Empty(t, svc.guardMemory.refusals)
	require.Empty(t, svc.guardMemory.devices)
	svc.hashCache = nil
	require.NoError(t, svc.RememberUpstreamRefusal(t.Context(), input))
	cleared, err := svc.ClearFlaggedInputHashes(t.Context())
	require.NoError(t, err, "local guard memory must be clearable without a shared hash cache")
	require.Positive(t, cleared.Deleted)
	require.Empty(t, svc.guardMemory.refusals)
	require.Empty(t, svc.guardMemory.devices)
}

func TestContentModerationGuardsValidationAndRoundTrip(t *testing.T) {
	svc := newModerationGuardTestService(t, func(*ContentModerationConfig) {})
	for _, rule := range []string{"(?=unsafe)", "[", strings.Repeat("a", 513), ""} {
		rules := []string{rule}
		_, err := svc.UpdateConfig(context.Background(), UpdateContentModerationConfigInput{RegexRules: &rules})
		require.Error(t, err)
	}
	for _, cfg := range []ContentModerationRefusalGuardConfig{
		{TTLSeconds: -1}, {MaxEntries: 4097}, {SimilarityThreshold: .5},
	} {
		_, err := svc.UpdateConfig(context.Background(), UpdateContentModerationConfigInput{RefusalGuard: &cfg})
		require.Error(t, err)
	}
	rules := []string{"custom.*rule"}
	policy := ContentModerationFailClosed
	distill := ContentModerationDistillationConfig{Enabled: true, Phrases: []string{"custom phrase"}}
	view, err := svc.UpdateConfig(context.Background(), UpdateContentModerationConfigInput{RegexRules: &rules, FailurePolicy: &policy, Distillation: &distill})
	require.NoError(t, err)
	require.Equal(t, policy, view.FailurePolicy)
	require.Equal(t, rules, view.RegexRules)
	loaded, err := svc.GetConfig(context.Background())
	require.NoError(t, err)
	require.Equal(t, view.Distillation, loaded.Distillation)
	empty := []string{}
	view, err = svc.UpdateConfig(context.Background(), UpdateContentModerationConfigInput{RegexRules: &empty, Distillation: &ContentModerationDistillationConfig{Enabled: true}})
	require.NoError(t, err)
	require.Empty(t, view.RegexRules)
	require.Empty(t, view.Distillation.Phrases)
	d, err := svc.Check(context.Background(), moderationGuardTestInput("custom phrase custom rule"))
	require.NoError(t, err)
	require.True(t, d.Allowed)
}

func TestContentModerationGuardsFailurePolicy(t *testing.T) {
	for _, failure := range []string{"no_keys", "invalid_jev", "http_error", "timeout", "frozen"} {
		for _, policy := range []string{ContentModerationFailOpen, ContentModerationFailClosed} {
			for _, mode := range []string{ContentModerationModePreBlock, ContentModerationModeObserve} {
				t.Run(failure+"/"+policy+"/"+mode, func(t *testing.T) {
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						if failure == "timeout" {
							select {
							case <-r.Context().Done():
							case <-time.After(100 * time.Millisecond):
							}
							return
						}
						if failure == "http_error" {
							w.WriteHeader(529)
							return
						}
						_, _ = w.Write([]byte(`{"model":"jev-latest","answers":{}}`))
					}))
					defer server.Close()
					svc := newModerationGuardTestService(t, func(cfg *ContentModerationConfig) {
						cfg.FailurePolicy, cfg.Mode = policy, mode
						cfg.KeywordBlockingMode = ContentModerationKeywordModeKeywordAndAPI
						cfg.Engine = ContentModerationEngineTypeSafe
						cfg.TypeSafe = &ContentModerationEngineConfig{BaseURL: server.URL, Model: "jev-latest", APIKeys: []string{"test"}, TimeoutMS: 20, RetryCount: 0}
						cfg.DeviceGuard.Enabled = true
						if failure == "no_keys" {
							cfg.TypeSafe.APIKeys = nil
						}
					})
					if failure == "frozen" {
						svc.markAPIKeyError("test", "unauthorized", 0, 401, ContentModerationEngineTypeSafe)
					}
					d, err := svc.Check(context.Background(), moderationGuardTestInput("ordinary request"))
					require.NoError(t, err)
					require.Equal(t, policy == ContentModerationFailClosed && mode == ContentModerationModePreBlock, d.Blocked)
					require.False(t, d.Flagged, "model outage is not evidence of a user violation")
					if d.Blocked {
						require.Equal(t, 503, d.StatusCode)
						require.Equal(t, ContentModerationErrorUnavailable, d.ErrorCode())
					}
					require.Empty(t, svc.guardMemory.devices)
					require.Empty(t, svc.guardMemory.refusals)
				})
			}
		}
	}
}

func TestContentModerationGuardsRefusalContext(t *testing.T) {
	for _, protocol := range []string{ContentModerationProtocolOpenAIResponses, ContentModerationProtocolOpenAIChat, ContentModerationProtocolAnthropicMessages, ContentModerationProtocolGemini} {
		t.Run(protocol, func(t *testing.T) {
			body := func(system, history, tool, latest string, stream bool, limit int) []byte {
				root := map[string]any{"stream": stream, "max_tokens": limit, "tools": []any{map[string]string{"name": tool}}}
				messages := []any{map[string]any{"role": "user", "content": history}, map[string]any{"role": "user", "content": latest}}
				switch protocol {
				case ContentModerationProtocolOpenAIResponses:
					root["instructions"], root["input"] = system, messages
				case ContentModerationProtocolOpenAIChat:
					root["messages"] = append([]any{map[string]string{"role": "system", "content": system}}, messages...)
				case ContentModerationProtocolAnthropicMessages:
					root["system"], root["messages"] = system, messages
				case ContentModerationProtocolGemini:
					root["systemInstruction"] = map[string]any{"parts": []any{map[string]string{"text": system}}}
					root["contents"] = []any{map[string]any{"role": "user", "parts": []any{map[string]string{"text": history}}}, map[string]any{"role": "user", "parts": []any{map[string]string{"text": latest}}}}
					root["generationConfig"] = map[string]int{"maxOutputTokens": limit}
				}
				raw, err := json.Marshal(root)
				require.NoError(t, err)
				return raw
			}
			svc := newModerationGuardTestService(t, func(cfg *ContentModerationConfig) { cfg.RefusalGuard.Enabled = true })
			input := moderationGuardTestInput("")
			input.Protocol = protocol
			input.Body = body("original system", "prior conversation", "read_file", "继续", false, 100)
			require.NoError(t, svc.RememberUpstreamRefusal(context.Background(), input))
			for name, candidate := range map[string][]byte{
				"exact":      body("original system", "prior conversation", "read_file", "继续", false, 100),
				"parameters": body("original system", "prior conversation", "read_file", "继续", true, 999),
				"system":     body("different system", "prior conversation", "read_file", "继续", false, 100),
				"history":    body("original system", "different conversation", "read_file", "继续", false, 100),
				"tool":       body("original system", "prior conversation", "run_shell", "继续", false, 100),
			} {
				input.Body = candidate
				d, err := svc.Check(context.Background(), input)
				require.NoError(t, err)
				require.Equal(t, name == "exact" || name == "parameters", d.Blocked, name)
			}
			latest := "This current request has enough distinct characters for approximate matching, independent of any old conversation shared with unrelated requests."
			history := strings.Repeat("A long common history must not contribute to the similarity score. ", 300)
			input.Body = body("system", history, "tool", latest, false, 100)
			require.NoError(t, svc.RememberUpstreamRefusal(context.Background(), input))
			input.Body = body("system", history, "tool", latest+"!", true, 200)
			d, err := svc.Check(context.Background(), input)
			require.NoError(t, err)
			require.Equal(t, "refusal_similar", d.HighestCategory)
			input.Body = body("system changed", history, "tool", latest+"!", true, 200)
			d, err = svc.Check(context.Background(), input)
			require.NoError(t, err)
			require.True(t, d.Allowed)
			input.Body = body("system", history, "tool", "An entirely unrelated current query about music and gardens, whose meaning differs from all earlier text even with the same long history.", false, 100)
			d, err = svc.Check(context.Background(), input)
			require.NoError(t, err)
			require.True(t, d.Allowed)
		})
	}
}

func TestContentModerationGuardsContextKeepsToolResultsAndExactNumbers(t *testing.T) {
	input := moderationGuardTestInput("")
	input.Protocol = ContentModerationProtocolAnthropicMessages
	input.Body = []byte(`{"messages":[{"role":"user","content":[{"type":"tool_result","content":{"id":9007199254740992}},{"type":"text","text":"continue"}]}],"tools":[{"name":"tool","input_schema":{"type":"object"}}]}`)
	a, ok := moderationRefusalScope(input)
	require.True(t, ok)
	input.Body = []byte(strings.Replace(string(input.Body), "9007199254740992", "9007199254740993", 1))
	b, ok := moderationRefusalScope(input)
	require.True(t, ok)
	require.NotEqual(t, a, b)
	input.Body = []byte(strings.Replace(string(input.Body), `"continue"`, `"a different current text"`, 1))
	c, ok := moderationRefusalScope(input)
	require.True(t, ok)
	require.Equal(t, b, c)
	svc := newModerationGuardTestService(t, func(cfg *ContentModerationConfig) { cfg.RefusalGuard.Enabled = true })
	input.Protocol = ContentModerationProtocolTypeSafeSystemOne
	input.Body = []byte(`{"state":{"value":9007199254740992},"questions":{"q":{"type":"noul","instructions":"A contract that must not inflate the similarity of distinct evaluated states."}}}`)
	require.NoError(t, svc.RememberUpstreamRefusal(context.Background(), input))
	input.Body = []byte(strings.Replace(string(input.Body), "9007199254740992", "9007199254740993", 1))
	d, err := svc.Check(context.Background(), input)
	require.NoError(t, err)
	require.True(t, d.Allowed)
}

func TestContentModerationGuardsConcurrentFeedbackCheckAndClear(t *testing.T) {
	svc := newModerationGuardTestService(t, func(cfg *ContentModerationConfig) { cfg.RefusalGuard.Enabled, cfg.DeviceGuard.Enabled = true, true })
	_, err := svc.GetConfig(context.Background())
	require.NoError(t, err)
	_, err = svc.loadRuntimeSnapshot(context.Background())
	require.NoError(t, err)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			input := moderationGuardTestInput(fmt.Sprintf("request %d", i))
			for j := 0; j < 10; j++ {
				if err := svc.RememberUpstreamRefusal(context.Background(), input); err != nil {
					t.Error(err)
				}
				if _, err := svc.Check(context.Background(), input); err != nil {
					t.Error(err)
				}
				if _, err := svc.ClearFlaggedInputHashes(context.Background()); err != nil {
					t.Error(err)
				}
			}
		}(i)
	}
	wg.Wait()
}

func TestContentModerationGuardsNativeAuditDeviceAndAllowlist(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"results":[{"category_scores":{"illicit":1}}]}`))
	}))
	defer server.Close()
	for _, allowlisted := range []bool{false, true} {
		svc := newModerationGuardTestService(t, func(cfg *ContentModerationConfig) {
			cfg.KeywordBlockingMode = ContentModerationKeywordModeKeywordAndAPI
			cfg.BaseURL, cfg.APIKeys = server.URL, []string{"test"}
			cfg.DeviceGuard.Enabled, cfg.RefusalGuard.Enabled = true, true
		})
		if allowlisted {
			svc.settingRepo.(*contentModerationTestSettingRepo).values[SettingKeyCyberPolicyUserAllowlist] = "12"
		}
		d, err := svc.Check(context.Background(), moderationGuardTestInput("audit this input"))
		require.NoError(t, err)
		require.Equal(t, !allowlisted, d.Blocked)
		require.Equal(t, !allowlisted, len(svc.guardMemory.devices) == 1)
		require.Empty(t, svc.guardMemory.refusals, "a safety model judgment is not an upstream refusal")
	}
	svc := newModerationGuardTestService(t, func(cfg *ContentModerationConfig) {
		cfg.FailurePolicy = ContentModerationFailClosed
		cfg.KeywordBlockingMode = ContentModerationKeywordModeKeywordAndAPI
	})
	svc.settingRepo.(*contentModerationTestSettingRepo).values[SettingKeyCyberPolicyUserAllowlist] = "12"
	d, err := svc.Check(context.Background(), moderationGuardTestInput("no key, trusted user"))
	require.NoError(t, err)
	require.True(t, d.Allowed)
	require.False(t, d.Blocked)
}

func TestContentModerationGuardsObserveKeepsExistingHashNonBlocking(t *testing.T) {
	svc := newModerationGuardTestService(t, func(cfg *ContentModerationConfig) {
		cfg.Mode, cfg.PreHashCheckEnabled = ContentModerationModeObserve, true
	})
	input := moderationGuardTestInput("previously flagged")
	require.NoError(t, svc.hashCache.RecordFlaggedInputHash(context.Background(), ExtractContentModerationInput(input.Protocol, input.Body).Hash()))
	d, err := svc.Check(context.Background(), input)
	require.NoError(t, err)
	require.True(t, d.Allowed)
	require.True(t, d.Flagged)
	require.False(t, d.Blocked)
}

func TestContentModerationGuardDefaultsAndSourceFragments(t *testing.T) {
	fresh, err := parseContentModerationConfig("")
	require.NoError(t, err)
	require.True(t, fresh.Distillation.Enabled)
	require.NotEmpty(t, fresh.Distillation.Phrases)
	require.True(t, fresh.RefusalGuard.Enabled)
	require.False(t, fresh.Enabled, "global audit remains opt-in")
	old, err := parseContentModerationConfig(`{"enabled":true}`)
	require.NoError(t, err)
	require.False(t, old.Distillation.Enabled, "existing configurations must not silently enable new rules")
	svc := newModerationGuardTestService(t, func(cfg *ContentModerationConfig) {
		cfg.Distillation.Enabled = true
		cfg.Distillation.Fingerprints = []string{"distinctive source fragment"}
	})
	decision, err := svc.Check(t.Context(), moderationGuardTestInput("prefix distinctive source fragment suffix"))
	require.NoError(t, err)
	require.True(t, decision.Blocked)
}

type moderationSharedTestStore struct {
	contentModerationTestHashCache
	stateMu    sync.Mutex
	version    int
	generation int
	records    map[string]ContentModerationGuardRecord
}

type moderationFailingGuardStore struct {
	moderationSharedTestStore
	failWrites bool
}

func (s *moderationFailingGuardStore) StoreGuardEntries(ctx context.Context, records []ContentModerationGuardRecord, refusalLimit, deviceLimit int) error {
	if s.failWrites {
		return errors.New("guard write unavailable")
	}
	return s.moderationSharedTestStore.StoreGuardEntries(ctx, records, refusalLimit, deviceLimit)
}

func TestContentModerationGuardsRetryFailedPersistence(t *testing.T) {
	configure := func(cfg *ContentModerationConfig) {
		cfg.RefusalGuard.Enabled = true
		cfg.DeviceGuard.Enabled = true
		cfg.RefusalGuard.MaxEntries = 2
		cfg.DeviceGuard.MaxEntries = 2
	}
	first := newModerationGuardTestService(t, configure)
	second := newModerationGuardTestService(t, configure)
	store := &moderationFailingGuardStore{failWrites: true}
	first.hashCache, second.hashCache = store, store
	input := moderationGuardTestInput("upstream refused input")
	require.Error(t, first.RememberUpstreamRefusal(t.Context(), input))
	require.NotEmpty(t, first.guardPending)
	deadline := first.guardPending[0].ExpiresMS
	store.failWrites = false
	require.NoError(t, second.RememberUpstreamRefusal(t.Context(), moderationGuardTestInput("another refusal")))
	decision, err := first.Check(t.Context(), input)
	require.NoError(t, err)
	require.True(t, decision.Blocked)
	require.Empty(t, first.guardPending)
	found := false
	for _, record := range store.records {
		found = found || record.ExpiresMS == deadline
	}
	require.True(t, found, "retry preserves the original TTL")
	decision, err = second.Check(t.Context(), input)
	require.NoError(t, err)
	require.True(t, decision.Blocked)

	store.failWrites = true
	for index := 0; index < 10; index++ {
		require.Error(t, first.RememberUpstreamRefusal(t.Context(), moderationGuardTestInput(fmt.Sprintf("refusal %d", index))))
	}
	require.LessOrEqual(t, len(first.guardPending), 4)
	_, err = first.ClearFlaggedInputHashes(t.Context())
	require.NoError(t, err)
	require.Empty(t, first.guardPending)
	store.failWrites = false
	decision, err = first.Check(t.Context(), input)
	require.NoError(t, err)
	require.True(t, decision.Allowed)
	require.Empty(t, store.records, "clear must not replay failed writes")
	store.failWrites = true
	require.Error(t, first.RememberUpstreamRefusal(t.Context(), input))
	_, err = second.ClearFlaggedInputHashes(t.Context())
	require.NoError(t, err)
	store.failWrites = false
	decision, err = first.Check(t.Context(), input)
	require.NoError(t, err)
	require.True(t, decision.Allowed, "remote clear invalidates pending retries")
	require.Empty(t, first.guardPending)
	require.Empty(t, store.records)
	require.NoError(t, first.RememberUpstreamRefusal(t.Context(), input))
	decision, err = second.Check(t.Context(), input)
	require.NoError(t, err)
	require.True(t, decision.Blocked, "new refusal remains effective after clear")
}

func (s *moderationSharedTestStore) LoadGuardState(_ context.Context, revision string) (string, []ContentModerationGuardRecord, bool, error) {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	next := fmt.Sprintf("%d:%d", s.version, s.generation)
	if next == revision {
		return next, nil, false, nil
	}
	rows := []ContentModerationGuardRecord{}
	for _, row := range s.records {
		rows = append(rows, row)
	}
	return next, rows, true, nil
}
func (s *moderationSharedTestStore) StoreGuardEntries(_ context.Context, rows []ContentModerationGuardRecord, _, _ int) error {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	if s.records == nil {
		s.records = make(map[string]ContentModerationGuardRecord)
	}
	for _, row := range rows {
		if row.Generation != fmt.Sprint(s.generation) {
			continue
		}
		s.records[row.Kind+row.Key] = row
	}
	s.version++
	return nil
}
func (s *moderationSharedTestStore) ClearGuardEntries(context.Context) (int64, error) {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	n := len(s.records)
	s.records = nil
	s.version++
	s.generation++
	return int64(n), nil
}
func TestContentModerationGuardsSharedAcrossInstancesAndClear(t *testing.T) {
	configure := func(cfg *ContentModerationConfig) { cfg.RefusalGuard.Enabled = true; cfg.DeviceGuard.Enabled = true }
	first := newModerationGuardTestService(t, configure)
	second := newModerationGuardTestService(t, configure)
	store := &moderationSharedTestStore{}
	first.hashCache = store
	second.hashCache = store
	input := moderationGuardTestInput("shared refusal")
	require.NoError(t, first.RememberUpstreamRefusal(t.Context(), input))
	decision, err := second.Check(t.Context(), input)
	require.NoError(t, err)
	require.True(t, decision.Blocked)
	other := input
	other.UserID++
	decision, err = second.Check(t.Context(), other)
	require.NoError(t, err)
	require.True(t, decision.Allowed)
	_, err = first.ClearFlaggedInputHashes(t.Context())
	require.NoError(t, err)
	decision, err = second.Check(t.Context(), input)
	require.NoError(t, err)
	require.True(t, decision.Allowed, "clearing shared state must invalidate another process's local view")
}

func TestContentModerationGuardsScanInstructionsWithoutChangingSemanticScope(t *testing.T) {
	cases := []struct{ protocol, body string }{
		{ContentModerationProtocolAnthropicMessages, `{"system":"Memory-stage-one extractor","messages":[{"role":"user","content":"hello"}]}`},
		{ContentModerationProtocolOpenAIChat, `{"messages":[{"role":"system","content":"Memory-stage-one extractor"},{"role":"user","content":"hello"}]}`},
		{ContentModerationProtocolOpenAIResponses, `{"instructions":"Memory-stage-one extractor","input":"hello"}`},
		{ContentModerationProtocolGemini, `{"systemInstruction":{"parts":[{"text":"Memory-stage-one extractor"}]},"contents":[{"role":"user","parts":[{"text":"hello"}]}]}`},
	}
	for _, tc := range cases {
		t.Run(tc.protocol, func(t *testing.T) {
			svc := newModerationGuardTestService(t, func(cfg *ContentModerationConfig) {
				cfg.Distillation.Enabled = true
				cfg.Distillation.Phrases = []string{"Memory-stage-one extractor"}
			})
			input := moderationGuardTestInput("hello")
			input.Protocol = tc.protocol
			input.Body = []byte(tc.body)
			require.Equal(t, "hello", ExtractContentModerationText(input.Protocol, input.Body))
			decision, err := svc.Check(t.Context(), input)
			require.NoError(t, err)
			require.True(t, decision.Blocked)
			require.Equal(t, "distillation", decision.HighestCategory)
		})
	}
	svc := newModerationGuardTestService(t, func(cfg *ContentModerationConfig) {
		cfg.Distillation.Enabled = true
		cfg.Distillation.Phrases = []string{"Memory-stage-one extractor"}
	})
	input := moderationGuardTestInput("hello")
	input.Protocol = ContentModerationProtocolAnthropicMessages
	input.Body = []byte(`{"system":"safe","messages":[{"role":"assistant","content":[{"type":"thinking","thinking":"Memory-stage-one extractor"}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"a","content":"Memory-stage-one extractor"}]}]}`)
	decision, err := svc.Check(t.Context(), input)
	require.NoError(t, err)
	require.False(t, decision.Blocked)
}
