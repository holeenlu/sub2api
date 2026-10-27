package dto

import (
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/authz"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestUsageLogFromService_IncludesOpenAIWSMode(t *testing.T) {
	t.Parallel()

	wsLog := &service.UsageLog{
		RequestID:    "req_1",
		Model:        "gpt-5.3-codex",
		OpenAIWSMode: true,
	}
	httpLog := &service.UsageLog{
		RequestID:    "resp_1",
		Model:        "gpt-5.3-codex",
		OpenAIWSMode: false,
	}

	require.True(t, UsageLogFromService(wsLog).OpenAIWSMode)
	require.False(t, UsageLogFromService(httpLog).OpenAIWSMode)
	require.True(t, UsageLogFromServiceAdmin(wsLog).OpenAIWSMode)
	require.False(t, UsageLogFromServiceAdmin(httpLog).OpenAIWSMode)
}

func TestUsageLogFromService_PreservesNativeCompactionAndStream(t *testing.T) {
	t.Parallel()

	log := &service.UsageLog{
		RequestID:          "resp_compaction",
		Model:              "gpt-5.6-sol",
		RequestType:        service.RequestTypeStream,
		Stream:             true,
		NativeCompactionV2: true,
	}

	userDTO := UsageLogFromService(log)
	adminDTO := UsageLogFromServiceAdmin(log)
	require.Equal(t, "stream", userDTO.RequestType)
	require.True(t, userDTO.Stream)
	require.True(t, userDTO.NativeCompactionV2)
	require.Equal(t, "stream", adminDTO.RequestType)
	require.True(t, adminDTO.Stream)
	require.True(t, adminDTO.NativeCompactionV2)
}

func TestUsageLogFromService_PrefersRequestTypeForLegacyFields(t *testing.T) {
	t.Parallel()

	log := &service.UsageLog{
		RequestID:    "req_2",
		Model:        "gpt-5.3-codex",
		RequestType:  service.RequestTypeWSV2,
		Stream:       false,
		OpenAIWSMode: false,
	}

	userDTO := UsageLogFromService(log)
	adminDTO := UsageLogFromServiceAdmin(log)

	require.Equal(t, "ws_v2", userDTO.RequestType)
	require.True(t, userDTO.Stream)
	require.True(t, userDTO.OpenAIWSMode)
	require.Equal(t, "ws_v2", adminDTO.RequestType)
	require.True(t, adminDTO.Stream)
	require.True(t, adminDTO.OpenAIWSMode)
}

func TestUsageCleanupTaskFromService_RequestTypeMapping(t *testing.T) {
	t.Parallel()

	requestType := int16(service.RequestTypeStream)
	task := &service.UsageCleanupTask{
		ID:     1,
		Status: service.UsageCleanupStatusPending,
		Filters: service.UsageCleanupFilters{
			RequestType: &requestType,
		},
	}

	dtoTask := UsageCleanupTaskFromService(task)
	require.NotNil(t, dtoTask)
	require.NotNil(t, dtoTask.Filters.RequestType)
	require.Equal(t, "stream", *dtoTask.Filters.RequestType)
}

func TestRequestTypeStringPtrNil(t *testing.T) {
	t.Parallel()
	require.Nil(t, requestTypeStringPtr(nil))
}

func TestUsageLogFromService_IncludesServiceTierForUserAndAdmin(t *testing.T) {
	t.Parallel()

	serviceTier := "priority"
	inboundEndpoint := "/v1/chat/completions"
	upstreamEndpoint := "/v1/responses"
	log := &service.UsageLog{
		RequestID:             "req_3",
		Model:                 "gpt-5.4",
		ServiceTier:           &serviceTier,
		InboundEndpoint:       &inboundEndpoint,
		UpstreamEndpoint:      &upstreamEndpoint,
		AccountRateMultiplier: f64Ptr(1.5),
	}

	userDTO := UsageLogFromService(log)
	adminDTO := UsageLogFromServiceAdmin(log)

	require.NotNil(t, userDTO.ServiceTier)
	require.Equal(t, serviceTier, *userDTO.ServiceTier)
	require.NotNil(t, userDTO.InboundEndpoint)
	require.Equal(t, inboundEndpoint, *userDTO.InboundEndpoint)
	require.Nil(t, userDTO.UpstreamEndpoint)
	require.NotNil(t, adminDTO.ServiceTier)
	require.Equal(t, serviceTier, *adminDTO.ServiceTier)
	require.NotNil(t, adminDTO.InboundEndpoint)
	require.Equal(t, inboundEndpoint, *adminDTO.InboundEndpoint)
	require.NotNil(t, adminDTO.UpstreamEndpoint)
	require.Equal(t, upstreamEndpoint, *adminDTO.UpstreamEndpoint)
	require.NotNil(t, adminDTO.AccountRateMultiplier)
	require.InDelta(t, 1.5, *adminDTO.AccountRateMultiplier, 1e-12)
}

func TestUsageLogFromService_UsesRequestedModelAndKeepsUpstreamAdminOnly(t *testing.T) {
	t.Parallel()

	upstreamModel := "claude-sonnet-4-20250514"
	upstreamResponseModel := "claude-sonnet-4-20250513"
	upstreamModelMismatch := true
	log := &service.UsageLog{
		RequestID:             "req_4",
		Model:                 upstreamModel,
		RequestedModel:        "claude-sonnet-4",
		UpstreamModel:         &upstreamModel,
		UpstreamResponseModel: &upstreamResponseModel,
		UpstreamModelMismatch: &upstreamModelMismatch,
	}

	userDTO := UsageLogFromService(log)
	adminDTO := UsageLogFromServiceAdmin(log)

	require.Equal(t, "claude-sonnet-4", userDTO.Model)
	require.Equal(t, "claude-sonnet-4", adminDTO.Model)

	userJSON, err := json.Marshal(userDTO)
	require.NoError(t, err)
	require.NotContains(t, string(userJSON), "upstream_model")
	require.NotContains(t, string(userJSON), "upstream_response_model")
	require.NotContains(t, string(userJSON), "upstream_model_mismatch")

	adminJSON, err := json.Marshal(adminDTO)
	require.NoError(t, err)
	require.Contains(t, string(adminJSON), `"upstream_model":"claude-sonnet-4-20250514"`)
	require.Contains(t, string(adminJSON), `"upstream_response_model":"claude-sonnet-4-20250513"`)
	require.Contains(t, string(adminJSON), `"upstream_model_mismatch":true`)
}

func TestUsageLogFromService_KeepsUserBillingAndIPWithoutAdminCostFields(t *testing.T) {
	t.Parallel()

	ipAddress := "203.0.113.10"
	accountRateMultiplier := 1.5
	accountStatsCost := 0.21
	log := &service.UsageLog{
		RequestID:             "req_user_visible_billing",
		Model:                 "gpt-5.4",
		InputCost:             0.01,
		OutputCost:            0.02,
		CacheCreationCost:     0.03,
		CacheReadCost:         0.04,
		TotalCost:             0.10,
		ActualCost:            0.08,
		RateMultiplier:        0.8,
		IPAddress:             &ipAddress,
		AccountRateMultiplier: &accountRateMultiplier,
		AccountStatsCost:      &accountStatsCost,
	}

	userDTO := UsageLogFromService(log)
	require.Equal(t, 0.01, userDTO.InputCost)
	require.Equal(t, 0.02, userDTO.OutputCost)
	require.Equal(t, 0.03, userDTO.CacheCreationCost)
	require.Equal(t, 0.04, userDTO.CacheReadCost)
	require.Equal(t, 0.10, userDTO.TotalCost)
	require.Equal(t, 0.08, userDTO.ActualCost)
	require.Equal(t, 0.8, userDTO.RateMultiplier)
	require.NotNil(t, userDTO.IPAddress)
	require.Equal(t, ipAddress, *userDTO.IPAddress)

	userJSON, err := json.Marshal(userDTO)
	require.NoError(t, err)
	require.NotContains(t, string(userJSON), "account_rate_multiplier")
	require.NotContains(t, string(userJSON), "account_stats_cost")
	require.NotContains(t, string(userJSON), "account_cost")
}

func TestUsageLogFromService_UsersSeeRequestedReasoningEffortOnly(t *testing.T) {
	t.Parallel()

	requested := "max"
	forwarded := "xhigh"
	log := &service.UsageLog{
		RequestID:                "req_effort",
		Model:                    "gpt-5.4",
		ReasoningEffort:          &forwarded,
		RequestedReasoningEffort: &requested,
	}

	userDTO := UsageLogFromService(log)
	adminDTO := UsageLogFromServiceAdmin(log)

	require.NotNil(t, userDTO.ReasoningEffort)
	require.Equal(t, requested, *userDTO.ReasoningEffort)
	require.NotNil(t, adminDTO.ReasoningEffort)
	require.Equal(t, requested, *adminDTO.ReasoningEffort)
	require.NotNil(t, adminDTO.UpstreamReasoningEffort)
	require.Equal(t, forwarded, *adminDTO.UpstreamReasoningEffort)

	userJSON, err := json.Marshal(userDTO)
	require.NoError(t, err)
	require.Contains(t, string(userJSON), `"reasoning_effort":"max"`)
	require.NotContains(t, string(userJSON), "upstream_reasoning_effort")
	require.NotContains(t, string(userJSON), "requested_reasoning_effort")

	adminJSON, err := json.Marshal(adminDTO)
	require.NoError(t, err)
	require.Contains(t, string(adminJSON), `"reasoning_effort":"max"`)
	require.Contains(t, string(adminJSON), `"upstream_reasoning_effort":"xhigh"`)
}

func TestUsageLogFromService_OmitsUpstreamReasoningEffortWhenUnmapped(t *testing.T) {
	t.Parallel()

	effort := "high"
	log := &service.UsageLog{
		RequestID:                "req_effort_same",
		Model:                    "gpt-5.4",
		ReasoningEffort:          &effort,
		RequestedReasoningEffort: &effort,
	}

	adminDTO := UsageLogFromServiceAdmin(log)
	require.NotNil(t, adminDTO.ReasoningEffort)
	require.Equal(t, effort, *adminDTO.ReasoningEffort)
	require.Nil(t, adminDTO.UpstreamReasoningEffort)

	adminJSON, err := json.Marshal(adminDTO)
	require.NoError(t, err)
	require.NotContains(t, string(adminJSON), "upstream_reasoning_effort")
}

func TestUsageLogFromService_FallsBackToLegacyModelWhenRequestedModelMissing(t *testing.T) {
	t.Parallel()

	log := &service.UsageLog{
		RequestID: "req_legacy",
		Model:     "claude-3",
	}

	userDTO := UsageLogFromService(log)
	adminDTO := UsageLogFromServiceAdmin(log)

	require.Equal(t, "claude-3", userDTO.Model)
	require.Equal(t, "claude-3", adminDTO.Model)
}

func TestUsageLogFromService_CacheWriteTTLBreakdown(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		log  service.UsageLog
	}{
		{"openai", service.UsageLog{CacheCreationTokens: 34, CacheCreation30mTokens: 13, CacheCreation12hTokens: 21}},
		{"historical_unknown_ttl", service.UsageLog{CacheCreationTokens: 34}},
		{"anthropic", service.UsageLog{CacheCreationTokens: 34, CacheCreation5mTokens: 13, CacheCreation1hTokens: 21}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			userDTO := UsageLogFromService(&tc.log)
			adminDTO := UsageLogFromServiceAdmin(&tc.log)
			for _, got := range []any{userDTO, adminDTO} {
				body, err := json.Marshal(got)
				require.NoError(t, err)
				var fields map[string]any
				require.NoError(t, json.Unmarshal(body, &fields))
				require.Equal(t, float64(tc.log.CacheCreationTokens), fields["cache_creation_tokens"])
				require.Equal(t, float64(tc.log.CacheCreation5mTokens), fields["cache_creation_5m_tokens"])
				require.Equal(t, float64(tc.log.CacheCreation1hTokens), fields["cache_creation_1h_tokens"])
				require.Equal(t, float64(tc.log.CacheCreation30mTokens), fields["cache_creation_30m_tokens"])
				require.Equal(t, float64(tc.log.CacheCreation12hTokens), fields["cache_creation_12h_tokens"])
			}
		})
	}
}

func TestUsageLogFromService_IncludesImageBillingMetadataForUserAndAdmin(t *testing.T) {
	t.Parallel()

	imageSize := "4K"
	inputSize := "1024x1024"
	outputSize := "3840x2160"
	source := "output"
	log := &service.UsageLog{
		RequestID:          "req_image_metadata",
		Model:              "gpt-image-2",
		ImageCount:         2,
		ImageSize:          &imageSize,
		ImageInputSize:     &inputSize,
		ImageOutputSize:    &outputSize,
		ImageSizeSource:    &source,
		ImageSizeBreakdown: map[string]int{"4K": 2},
	}

	userDTO := UsageLogFromService(log)
	adminDTO := UsageLogFromServiceAdmin(log)

	for _, got := range []*UsageLog{userDTO, &adminDTO.UsageLog} {
		require.Equal(t, 2, got.ImageCount)
		require.NotNil(t, got.ImageSize)
		require.Equal(t, imageSize, *got.ImageSize)
		require.NotNil(t, got.ImageInputSize)
		require.Equal(t, inputSize, *got.ImageInputSize)
		require.NotNil(t, got.ImageOutputSize)
		require.Equal(t, outputSize, *got.ImageOutputSize)
		require.NotNil(t, got.ImageSizeSource)
		require.Equal(t, source, *got.ImageSizeSource)
		require.Equal(t, map[string]int{"4K": 2}, got.ImageSizeBreakdown)
	}
}

func TestUsageLogFromService_PreservesHistoricalMissingImageSize(t *testing.T) {
	t.Parallel()

	log := &service.UsageLog{
		RequestID:  "req_legacy_image_missing_size",
		Model:      "gpt-image-2",
		ImageCount: 1,
		ImageSize:  nil,
	}

	dto := UsageLogFromService(log)
	require.Equal(t, 1, dto.ImageCount)
	require.Nil(t, dto.ImageSize)
	require.Nil(t, dto.ImageInputSize)
	require.Nil(t, dto.ImageOutputSize)
	require.Nil(t, dto.ImageSizeSource)
	require.Nil(t, dto.ImageSizeBreakdown)

	body, err := json.Marshal(dto)
	require.NoError(t, err)
	require.Contains(t, string(body), `"image_size":null`)
	require.NotContains(t, string(body), `"image_size":"2K"`)
}

func f64Ptr(value float64) *float64 {
	return &value
}

func TestUsageLogFromService_LatencyMetricsAndLegacyExport(t *testing.T) {
	zero, firstText := 0, 75
	metrics := &service.UsageLatencyMetrics{Version: 1, FirstEventMs: &zero, FirstTextMs: &firstText, DurationMs: 100}
	for _, snapshot := range []*service.UsageLatencyMetrics{nil, metrics} {
		log := &service.UsageLog{LatencyMetrics: snapshot}
		for _, value := range []any{UsageLogFromService(log), UsageLogFromServiceAdmin(log)} {
			data, err := json.Marshal(value)
			require.NoError(t, err)
			var decoded map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(data, &decoded))
			require.Contains(t, decoded, "latency_metrics", "legacy rows have explicit null, not fabricated v1 data")
			if snapshot == nil {
				require.JSONEq(t, "null", string(decoded["latency_metrics"]))
			} else {
				require.JSONEq(t, `{"version":1,"first_event_ms":0,"first_content_ms":null,"first_text_ms":75,"duration_ms":100}`, string(decoded["latency_metrics"]))
			}
		}
	}
	// The admin NDJSON export uses this same permission serializer and DTO.
	// Timing is request metadata and must survive an operator without cost access.
	actor := authz.Subject{Role: authz.Admin, Permissions: []string{"usage.read"}}
	data, err := authz.MarshalResponse(map[string]any{"type": "rows", "items": []any{UsageLogFromServiceAdmin(&service.UsageLog{LatencyMetrics: metrics})}}, "/api/v1/admin/usage/export", actor, false)
	require.NoError(t, err)
	var frame struct {
		Items []struct {
			LatencyMetrics *service.UsageLatencyMetrics `json:"latency_metrics"`
		} `json:"items"`
	}
	require.NoError(t, json.Unmarshal(data, &frame))
	require.Len(t, frame.Items, 1)
	require.Equal(t, metrics, frame.Items[0].LatencyMetrics)
}

func TestUsageAccountIdentityRoleProjection(t *testing.T) {
	log := &service.UsageLog{ID: 1, UserID: 2, AccountID: 991, ActualCost: 0.5, AuthorizationMode: "native", Account: &service.Account{ID: 991, Name: "private-upstream@example.com"}}
	userJSON, err := json.Marshal(UsageLogFromService(log))
	require.NoError(t, err)
	require.NotContains(t, string(userJSON), "account_id")
	require.NotContains(t, string(userJSON), "private-upstream")
	for _, path := range []string{"/api/v1/admin/usage", "/api/v1/admin/usage/export", "/api/v1/admin/ops/request-errors/:id", "/api/v1/admin/ops/upstream-errors/:id"} {
		for _, role := range []string{authz.Admin, authz.SuperAdmin} {
			t.Run(path+"/"+role, func(t *testing.T) {
				dto := UsageLogFromServiceAdmin(log)
				actor := authz.Subject{Role: role, Permissions: []string{"usage.read", "usage.export", "accounts.read", "billing.cost.read"}}
				payload, err := authz.MarshalResponse(map[string]any{"items": []any{dto}, "nested": map[string]any{"account_name": "private-upstream@example.com"}, "upstream_errors": `[{"account_id":991,"account_name":"private-upstream@example.com","upstream_status_code":429},{"kind":"scheduling_trace","detail":"{\"events\":[{\"account_id\":991,\"duration_ms\":80}]}"}]`}, path, actor, false)
				require.NoError(t, err)
				if role == authz.SuperAdmin {
					require.Contains(t, string(payload), `"account_id":991`)
					require.Contains(t, string(payload), "private-upstream@example.com")
					require.Contains(t, string(payload), `"authorization_mode":"native"`)
				} else {
					for _, hidden := range []string{`"account"`, `"account_id"`, `"account_name"`, `"authorization_mode"`, "private-upstream"} {
						require.NotContains(t, string(payload), hidden)
					}
				}
				var response map[string]json.RawMessage
				require.NoError(t, json.Unmarshal(payload, &response))
				var events string
				require.NoError(t, json.Unmarshal(response["upstream_errors"], &events))
				require.Contains(t, events, "429")
				require.Contains(t, events, "80")
				if role != authz.SuperAdmin {
					require.NotContains(t, events, "account_id")
					require.NotContains(t, events, "account_name")
				}
				require.Contains(t, string(payload), `"actual_cost":0.5`)
				require.Equal(t, "private-upstream@example.com", dto.Account.Name, "response projection must not mutate shared source data")
			})
		}
	}
}

func TestUsageLogSamplingDetailsReachesUserAdminAndExport(t *testing.T) {
	details := &service.UsageSamplingDetails{Version: 1, Scope: "cloud_turn", PricingMode: "token", Samples: []service.UsageSamplingSegment{{InputTokens: 20, CacheReadTokens: 80, OutputTokens: 3}}}
	log := &service.UsageLog{SamplingDetails: details}
	for _, value := range []any{UsageLogFromService(log), UsageLogFromServiceAdmin(log)} {
		raw, err := json.Marshal(value)
		require.NoError(t, err)
		var wire struct {
			Details *service.UsageSamplingDetails `json:"sampling_details"`
		}
		require.NoError(t, json.Unmarshal(raw, &wire))
		require.Equal(t, details, wire.Details)
	}
}
