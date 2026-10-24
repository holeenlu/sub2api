package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/authz"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tidwall/gjson"
)

func (s *AccountTestService) testOpenAICloudConnection(c *gin.Context, account *Account, model, prompt string) error {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Minute)
	defer cancel()
	gateway := s.openaiGatewayService
	actor, ok := authz.FromContext(ctx)
	if !ok || !actor.ManagementRequest || actor.UserID <= 0 || authz.Revalidate(ctx) != nil {
		return s.sendErrorAndEnd(c, "Cloud verification requires a current account-management session")
	}
	if gateway == nil || gateway.cloudService == nil || gateway.cloudSessions == nil || !account.IsOpenAICloudEligible() {
		return s.sendErrorAndEnd(c, "Cloud verification is unavailable for this account")
	}
	model = strings.TrimSpace(model)
	if model == "" || len(model) > 200 {
		return s.sendErrorAndEnd(c, "Select a Cloud model to verify")
	}
	s.sendEvent(c, TestEvent{Type: "test_start", Model: model})
	initialAccess, initialErr := gateway.cloudService.GetAccessToken(ctx, account.ID)
	if initialErr != nil {
		return s.sendErrorAndEnd(c, "Cloud authorization is unavailable")
	}
	initialGeneration, initialSettings := initialAccess.Generation, cloudSettingsDigest(initialAccess.Account)
	capabilities := OpenAICloudCapabilities{}
	result, err := gateway.runCloudProbe(ctx, actor.UserID, account.ID, model, prompt, func(event json.RawMessage) error {
		if gjson.GetBytes(event, "type").String() == "response.output_text.delta" {
			s.sendEvent(c, TestEvent{Type: "content", Text: gjson.GetBytes(event, "delta").String()})
		}
		return c.Request.Context().Err()
	})
	data := map[string]any{"kind": "cloud_result", "response_id": "", "status": "failed", "model": model, "output": []any{}, "usage": nil, "created_at": time.Now().Unix(), "completed_at": time.Now().Unix()}
	if result != nil && len(result.Response) > 0 {
		var response map[string]any
		if json.Unmarshal(result.Response, &response) == nil {
			for _, key := range []string{"id", "status", "model", "output", "usage", "created_at"} {
				if v, found := response[key]; found {
					if key == "id" {
						data["response_id"] = v
					} else {
						data[key] = v
					}
				}
			}
		}
	}
	if err == nil && (result == nil || !result.UsageCredible) {
		err = errors.New("Cloud verification returned no credible result")
	}
	if err == nil && result != nil && result.UsageCredible {
		capabilities = OpenAICloudCapabilities{Text: true, Tools: true, Usage: true, Settlement: true, ImageInput: true, EncryptedContext: true, WebSearch: true, ImageGeneration: true, VerifiedAt: time.Now()}
		access, accessErr := gateway.cloudService.GetAccessToken(ctx, account.ID)
		if accessErr != nil {
			err = accessErr
		} else if access.Generation != initialGeneration || cloudSettingsDigest(access.Account) != initialSettings {
			err = ErrOpenAICloudAuthorization
		} else if authz.Revalidate(ctx) != nil {
			err = authz.ErrPermissionDenied
		} else {
			err = gateway.cloudService.RecordVerifiedModel(ctx, account.ID, initialGeneration, initialSettings, model, capabilities)
		}
	}
	if err != nil {
		data["status"] = "failed"
		data["error"] = cloudProbePublicError(err, result)
		capabilities = OpenAICloudCapabilities{}
	}
	data["capabilities"] = capabilities
	s.sendEvent(c, TestEvent{Type: "test_complete", Success: err == nil, Model: model, Data: data})
	return err
}

// Every check uses production sessions, protocol mapping and shared Redis. The
// dedicated managers own only this probe's actors, so cancellation cannot stop
// a customer's session. The second manager exercises cross-instance tools.
func (s *OpenAIGatewayService) runCloudProbe(ctx context.Context, adminID, accountID int64, model, prompt string, emit func(json.RawMessage) error) (last *OpenAICloudResult, probeErr error) {
	stage, check := "authorization", "access"
	defer func() {
		if probeErr != nil {
			probeErr = &cloudProbeFailure{stage: stage, check: check, cause: probeErr}
		}
	}()
	access, err := s.cloudService.GetAccessToken(ctx, accountID)
	if err != nil {
		return nil, err
	}
	store, ok := s.cache.(OpenAICloudSessionStore)
	if !ok {
		return nil, errors.New("Cloud shared state is unavailable")
	}
	owner := OpenAICloudOwner{Probe: true, UserID: adminID, AccountID: accountID, GrantGeneration: access.Generation, Model: model, SettingsDigest: cloudSettingsDigest(access.Account)}
	tracked := &cloudProbeStore{OpenAICloudSessionStore: store}
	local := NewOpenAICloudSessions(tracked, s.connectCloudSession, s.validateCloudSessionOwner)
	remote := NewOpenAICloudSessions(tracked, s.connectCloudSession, s.validateCloudSessionOwner)
	closed := make(map[string]bool)
	defer func() {
		cleanupErr := cleanupCloudProbe(ctx, tracked, local, remote, owner, access, closed)
		if probeErr == nil && cleanupErr != nil {
			stage, check = "cleanup", "archive"
		}
		probeErr = errors.Join(probeErr, cleanupErr)
	}()
	seen := make(map[string]int)
	run := func(body map[string]any, root string, manager *OpenAICloudSessions) (*OpenAICloudResult, error) {
		check = "request"
		if err := authz.Revalidate(ctx); err != nil {
			return nil, err
		}
		body["model"], body["stream"] = model, true
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		result, err := manager.Run(ctx, OpenAICloudRequest{Owner: owner, Body: raw, RootRequestID: root}, func(event json.RawMessage) error {
			kind := gjson.GetBytes(event, "type").String()
			if !json.Valid(event) || kind == "" {
				return errors.New("Cloud verification received an invalid public event")
			}
			seen[kind]++
			if emit != nil {
				return emit(event)
			}
			return nil
		})
		if result != nil {
			last = result
		}
		if err == nil && result == nil {
			err = errors.New("Cloud verification returned no result")
		}
		if err == nil {
			check = "evidence"
		}
		return result, err
	}
	finish := func(result *OpenAICloudResult, root, expected string) error {
		check = "result"
		if err := cloudProbeCompleted(result, root, model); err != nil {
			return err
		}
		if seen["response.completed"] == 0 || expected != "" && (strings.TrimSpace(cloudAnswerText(result.Response)) != expected || seen["response.output_text.delta"] == 0) {
			return errors.New("Cloud verification answer or public event stream is incomplete")
		}
		check = "settlement"
		if err := s.WaitCloudProbeSettlement(ctx, root); err != nil {
			return err
		}
		check = "close_session"
		if err := remote.CloseCompleted(ctx, owner, result.SessionID); err != nil {
			return err
		}
		closed[result.SessionID] = true
		return nil
	}
	newRoot := func() string {
		seen = make(map[string]int)
		return "cloud_probe_" + uuid.NewString()
	}

	stage = "client_tool"
	root := newRoot()
	value := "CLOUD_VERIFIED_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	tool := map[string]any{"type": "function", "name": "sub2api_cloud_echo", "description": "Echo the verification value. Executed by the client; no external actions.", "parameters": map[string]any{"type": "object", "properties": map[string]any{"value": map[string]string{"type": "string"}}, "required": []string{"value"}, "additionalProperties": false}}
	instruction := "This is a protocol check. Call sub2api_cloud_echo once with value " + value + ", then answer with exactly the returned text. Do not use any other tools."
	if strings.TrimSpace(prompt) != "" {
		instruction += " The administrator's test note is: " + prompt
	}
	body := map[string]any{"input": instruction, "tools": []any{tool}, "reasoning": map[string]string{"effort": "low"}}
	first, err := run(body, root, local)
	if err != nil {
		return last, err
	}
	responseID, callID, err := cloudProbeToolCall(first, "sub2api_cloud_echo", map[string]any{"value": value})
	if err != nil {
		return last, err
	}
	body["previous_response_id"] = responseID
	body["input"] = []any{map[string]any{"type": "function_call_output", "call_id": callID, "output": value}}
	result, err := run(body, root, remote)
	if err != nil {
		return last, err
	}
	if result.SessionID != first.SessionID || seen["response.function_call_arguments.delta"] == 0 || seen["response.completed"] != 2 {
		return last, errors.New("Cloud cross-manager echo tool continuation was not verified")
	}
	if err = finish(result, root, value); err != nil {
		return last, err
	}

	stage, check = "image_input", "fixture"
	imageURL, err := cloudProbePNG()
	if err != nil {
		return last, err
	}
	root = newRoot()
	result, err = run(map[string]any{"input": []any{map[string]any{"role": "user", "content": []any{map[string]any{"type": "input_text", "text": cloudProbeIdentifyColors}, map[string]any{"type": "input_image", "image_url": imageURL, "detail": "auto"}}}}}, root, local)
	if err != nil {
		return last, err
	}
	if err = finish(result, root, "BLUE YELLOW"); err != nil {
		return last, fmt.Errorf("Cloud inline PNG verification failed: %w", err)
	}

	root = newRoot()
	stage = "tool_image"
	tool = map[string]any{"type": "function", "name": "sub2api_cloud_screenshot", "description": "Return a synthetic screenshot with two solid panels, with no external actions.", "parameters": map[string]any{"type": "object", "properties": map[string]any{}, "additionalProperties": false}}
	first, err = run(map[string]any{"tools": []any{tool}, "input": "Call sub2api_cloud_screenshot exactly once, then inspect the returned image. " + cloudProbeIdentifyColors}, root, local)
	if err != nil {
		return last, err
	}
	responseID, callID, err = cloudProbeToolCall(first, "sub2api_cloud_screenshot", map[string]any{})
	if err != nil {
		return last, err
	}
	result, err = run(map[string]any{"previous_response_id": responseID, "input": []any{map[string]any{"type": "function_call_output", "call_id": callID, "output": []any{map[string]any{"type": "input_text", "text": "Synthetic screenshot attached."}, map[string]any{"type": "input_image", "image_url": imageURL, "detail": "auto"}}}}}, root, remote)
	if err != nil {
		return last, err
	}
	if result.SessionID != first.SessionID || seen["response.function_call_arguments.delta"] == 0 || seen["response.completed"] != 2 {
		return last, errors.New("Cloud cross-manager image tool continuation was not verified")
	}
	if err = finish(result, root, "BLUE YELLOW"); err != nil {
		return last, fmt.Errorf("Cloud tool PNG verification failed: %w", err)
	}

	root = newRoot()
	stage = "encrypted_context_create"
	initial := map[string]any{"role": "user", "content": "Calculate 17 multiplied by 23, then add 29. Return only the integer. Do not use tools."}
	first, err = run(map[string]any{"input": []any{initial}, "reasoning": map[string]string{"effort": "medium"}, "include": []string{"reasoning.encrypted_content"}}, root, local)
	if err != nil {
		return last, err
	}
	history, err := cloudProbeEncryptedHistory(first, initial)
	if err != nil {
		return last, err
	}
	if err = finish(first, root, "420"); err != nil {
		return last, err
	}
	root = newRoot()
	stage = "encrypted_context_replay"
	history = append(history, map[string]any{"role": "user", "content": "Square the previous result. Return only the integer. Do not use tools."})
	result, err = run(map[string]any{"input": history, "reasoning": map[string]string{"effort": "medium"}, "include": []string{"reasoning.encrypted_content"}}, root, local)
	if err != nil {
		return last, err
	}
	if result.SessionID == first.SessionID || result.ThreadID == first.ThreadID {
		return last, errors.New("Cloud encrypted context did not reach an independent thread")
	}
	if err = finish(result, root, "176400"); err != nil {
		return last, fmt.Errorf("Cloud encrypted context verification failed: %w", err)
	}

	root = newRoot()
	stage = "web_search"
	result, err = run(map[string]any{"tools": []any{map[string]any{"type": "web_search"}}, "include": []string{"web_search_call.action.sources"}, "input": "Use web search now to find the official OpenAI Responses API documentation. Return its official documentation URL in one short sentence. Do not rely on memory and do not use any other tool."}, root, local)
	if err != nil {
		return last, err
	}
	if err = cloudProbeSearchEvidence(result, seen); err != nil {
		return last, err
	}
	if err = finish(result, root, ""); err != nil {
		return last, err
	}

	root = newRoot()
	stage = "image_generation"
	result, err = run(map[string]any{"tools": []any{map[string]any{"type": "image_generation"}}, "input": "Use the image generation tool to generate exactly one simple square image: one red circle centered on a plain white background, no text. Return the generated image, not instructions or code. Do not use any other tool."}, root, local)
	if err != nil {
		return last, err
	}
	if err = cloudProbeImageEvidence(result, seen); err != nil {
		return last, err
	}
	if err = finish(result, root, ""); err != nil {
		return last, err
	}
	return result, nil
}

// Only gateway-controlled stage labels and classified public errors reach the
// result export. Raw transport/storage errors may contain private material.
type cloudProbeFailure struct {
	stage, check string
	cause        error
}

func (e *cloudProbeFailure) Error() string {
	return "Cloud verification failed at " + e.stage + "/" + e.check
}
func (e *cloudProbeFailure) Unwrap() error { return e.cause }

func cloudProbePublicError(err error, result *OpenAICloudResult) map[string]string {
	code, message := "cloud_verification_failed", "Cloud verification could not be recorded."
	var failure *cloudProbeFailure
	if errors.As(err, &failure) {
		message = failure.Error() + "."
	}
	if errors.Is(err, ErrOpenAICloudProbeSettlementFailed) {
		return map[string]string{"code": "cloud_probe_settlement_failed", "message": message + " The probe settlement was rejected; no customer charge was recorded."}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return map[string]string{"code": "cloud_verification_timeout", "message": message + " Verification timed out."}
	}
	if errors.Is(err, context.Canceled) {
		return map[string]string{"code": "cloud_verification_canceled", "message": message + " Verification was canceled."}
	}
	status, upstreamCode, detail, known := cloudPublicError(err)
	// Terminal turn errors travel through Redis as a public response, not as
	// a Go error. Reclassify only the known code/status, never echo its message.
	if !known && result != nil && result.ErrorStatus >= 400 && result.ErrorStatus <= 599 {
		upstream := &cloudUpstreamError{HTTPStatus: result.ErrorStatus, Code: gjson.GetBytes(result.Response, "error.code").String()}
		status, upstreamCode, detail = cloudUpstreamErrorResponse(upstream)
		known = true
	}
	if known {
		code = upstreamCode
		message += fmt.Sprintf(" %s (HTTP %d)", detail, status)
	}
	return map[string]string{"code": code, "message": message}
}

func cloudProbeCompleted(result *OpenAICloudResult, root, model string) error {
	if result == nil || result.RootRequestID != root || result.SessionID == "" || result.ThreadID == "" || !result.Terminal || result.AwaitingTools || result.ErrorStatus != 0 || !result.UsageCredible || result.Usage == nil || result.Usage.InputTokens <= 0 || result.Usage.OutputTokens < 0 || result.Usage.TotalTokens != result.Usage.InputTokens+result.Usage.OutputTokens || !json.Valid(result.Response) || gjson.GetBytes(result.Response, "status").String() != "completed" || gjson.GetBytes(result.Response, "model").String() != model {
		return errors.New("Cloud verification lacks a completed result with trusted usage")
	}
	return nil
}

func cloudProbeToolCall(result *OpenAICloudResult, name string, expected map[string]any) (string, string, error) {
	if result == nil || !result.AwaitingTools || result.Terminal || result.Usage != nil || !json.Valid(result.Response) {
		return "", "", errors.New("Cloud did not yield the expected client tool without premature usage")
	}
	responseID := gjson.GetBytes(result.Response, "id").String()
	var callID string
	for _, item := range gjson.GetBytes(result.Response, "output").Array() {
		if item.Get("type").String() != "function_call" {
			continue
		}
		var arguments map[string]any
		if callID != "" || item.Get("name").String() != name || item.Get("call_id").String() == "" || json.Unmarshal([]byte(item.Get("arguments").String()), &arguments) != nil || arguments == nil || len(arguments) != len(expected) {
			return "", "", errors.New("Cloud called an unexpected verification tool")
		}
		for key, value := range expected {
			if arguments[key] != value {
				return "", "", errors.New("Cloud verification tool arguments changed")
			}
		}
		callID = item.Get("call_id").String()
	}
	if responseID == "" || callID == "" {
		return "", "", errors.New("Cloud verification tool reference is missing")
	}
	return responseID, callID, nil
}

const cloudProbeIdentifyColors = "Identify the solid color of the left panel and then the right panel in the image. Reply with exactly the two English color names in uppercase, separated by one space. No other text."

func cloudProbePNG() (string, error) {
	bitmap := image.NewRGBA(image.Rect(0, 0, 160, 96))
	for y := 0; y < 96; y++ {
		for x := 0; x < 160; x++ {
			pixel := color.RGBA{R: 255, G: 255, A: 255}
			if x < 80 {
				pixel = color.RGBA{B: 255, A: 255}
			}
			bitmap.SetRGBA(x, y, pixel)
		}
	}
	var data bytes.Buffer
	if err := png.Encode(&data, bitmap); err != nil {
		return "", err
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(data.Bytes()), nil
}

func cloudProbeEncryptedHistory(result *OpenAICloudResult, initial any) ([]any, error) {
	if result == nil {
		return nil, errors.New("Cloud encrypted context response is missing")
	}
	var response struct {
		Output []json.RawMessage `json:"output"`
	}
	if json.Unmarshal(result.Response, &response) != nil {
		return nil, errors.New("Cloud encrypted context response is invalid")
	}
	history := []any{initial}
	encrypted := false
	for _, raw := range response.Output {
		if gjson.GetBytes(raw, "type").String() == "reasoning" {
			var fields map[string]json.RawMessage
			if json.Unmarshal(raw, &fields) != nil {
				return nil, errors.New("Cloud reasoning item is invalid")
			}
			for key := range fields {
				if key != "id" && key != "type" && key != "status" && key != "summary" && key != "encrypted_content" {
					return nil, errors.New("Cloud reasoning exposed a private field")
				}
			}
			cipher := gjson.GetBytes(raw, "encrypted_content")
			if cipher.Type == gjson.String && cipher.String() != "" {
				encrypted = true
			}
		}
		// Preserve every public output item, including the opaque ciphertext.
		history = append(history, raw)
	}
	if !encrypted {
		return nil, errors.New("Cloud did not return real encrypted context")
	}
	return history, nil
}

func cloudProbeSearchEvidence(result *OpenAICloudResult, events map[string]int) error {
	if result == nil || result.Usage == nil {
		return errors.New("Cloud search usage is missing")
	}
	calls, sources := 0, 0
	for _, item := range gjson.GetBytes(result.Response, "output").Array() {
		kind := item.Get("type").String()
		if kind == "function_call" || kind == "custom_tool_call" {
			return errors.New("Cloud exposed hosted search as a client tool")
		}
		if kind != "web_search_call" {
			continue
		}
		calls++
		action := item.Get("action")
		var fields map[string]any
		if json.Unmarshal([]byte(action.Raw), &fields) != nil {
			return errors.New("Cloud search action is invalid")
		}
		if _, err := cloudSearchAction(fields); err != nil {
			return errors.New("Cloud search action is unsupported or incomplete")
		}
		if item.Get("status").String() != "completed" || action.Get("type").String() == "search" && action.Get("query").String() == "" && len(action.Get("queries").Array()) == 0 {
			return errors.New("Cloud search action is incomplete")
		}
		for _, source := range action.Get("sources").Array() {
			if source.Get("type").String() != "url" || !cloudPublicURL(source.Get("url").String()) {
				return errors.New("Cloud search source is invalid")
			}
			sources++
		}
	}
	if calls == 0 || sources == 0 || result.Usage.WebSearchCalls != calls || events["response.web_search_call.completed"] != calls {
		return errors.New("Cloud search lacks real sources, events or matching usage")
	}
	return nil
}

func cloudProbeImageEvidence(result *OpenAICloudResult, events map[string]int) error {
	if result == nil || result.Usage == nil {
		return errors.New("Cloud image generation usage is missing")
	}
	count := 0
	for _, item := range gjson.GetBytes(result.Response, "output").Array() {
		kind := item.Get("type").String()
		if kind == "function_call" || kind == "custom_tool_call" {
			return errors.New("Cloud exposed image generation as a client tool")
		}
		if kind != "image_generation_call" {
			continue
		}
		count++
		if count != 1 || item.Get("status").String() != "completed" || item.Get("output_format").String() != "png" {
			return errors.New("Cloud image generation did not complete exactly one PNG")
		}
		encoded := item.Get("result").String()
		if encoded == "" || len(encoded) > OpenAICloudMaxResultBytes {
			return errors.New("Cloud generated image data is missing or too large")
		}
		data, err := base64.StdEncoding.Strict().DecodeString(encoded)
		if err != nil {
			return errors.New("Cloud generated image is not valid base64")
		}
		config, err := png.DecodeConfig(bytes.NewReader(data))
		if err != nil || config.Width <= 0 || config.Height <= 0 || config.Width > 8192 || config.Height > 8192 || int64(config.Width)*int64(config.Height) > 64<<20 {
			return errors.New("Cloud generated PNG dimensions are invalid")
		}
		reader := bytes.NewReader(data)
		if _, err = png.Decode(reader); err != nil || reader.Len() != 0 {
			return errors.New("Cloud generated PNG did not fully decode")
		}
		size := fmt.Sprintf("%dx%d", config.Width, config.Height)
		tier, ok := ClassifyImageBillingTier(size)
		if !ok || item.Get("size").String() != size || len(result.Usage.ImageOutputSizes) != 1 || result.Usage.ImageOutputSizes[0] != size || len(result.Usage.ImageSizes) != 1 || result.Usage.ImageSizes[tier] != 1 {
			return errors.New("Cloud image pixels do not match measured billing dimensions")
		}
	}
	if count != 1 || result.Usage.ImageCount != 1 || events["response.image_generation_call.completed"] != 1 {
		return errors.New("Cloud image generation lacks real output, events or matching usage")
	}
	return nil
}

// Run can fail without returning its session ID. Track only creations made by
// this probe so cleanup can still find its threads, without scanning user data.
type cloudProbeStore struct {
	OpenAICloudSessionStore
	mu  sync.Mutex
	ids []string
}

func (s *cloudProbeStore) CreateCloudSession(ctx context.Context, record *OpenAICloudSessionRecord) error {
	s.mu.Lock()
	s.ids = append(s.ids, record.SessionID)
	s.mu.Unlock()
	return s.OpenAICloudSessionStore.CreateCloudSession(ctx, record)
}

func cleanupCloudProbe(ctx context.Context, store *cloudProbeStore, local, remote *OpenAICloudSessions, owner OpenAICloudOwner, access *OpenAICloudAccess, closed map[string]bool) error {
	local.Close()
	remote.Close()
	store.mu.Lock()
	ids := append([]string(nil), store.ids...)
	store.mu.Unlock()
	if len(ids) == len(closed) {
		return nil
	}
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 20*time.Second)
	defer cancel()
	// Give actor cancellation time to interrupt pending work and archive on its
	// live connection before trying an archive-only cleanup connection.
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		local.mu.Lock()
		active := len(local.actors)
		local.mu.Unlock()
		if active == 0 {
			break
		}
		select {
		case <-cleanup.Done():
			return errors.New("Cloud verification session cleanup did not finish")
		case <-ticker.C:
		}
	}
	var rpc *cloudRPC
	defer func() {
		if rpc != nil {
			_ = rpc.Close()
		}
	}()
	var cleanupErr error
	for _, id := range ids {
		if closed[id] {
			continue
		}
		record, err := store.LoadCloudSession(cleanup, id)
		if err != nil {
			cleanupErr = errors.Join(cleanupErr, errors.New("Cloud verification session cleanup could not read its state"))
			continue
		}
		if record == nil || record.ThreadID == "" {
			continue
		}
		if record.OpenAICloudOwner != owner {
			cleanupErr = errors.Join(cleanupErr, errors.New("Cloud verification cleanup ownership changed"))
			continue
		}
		if rpc == nil {
			// This connection has only thread/archive calls. It never starts,
			// resumes, injects history, or replays a failed inference turn.
			rpc, err = dialOpenAICloud(cleanup, access.AccessToken, access.Account.GetChatGPTAccountID(), cloudAccountProxy(access.Account))
			if err != nil {
				return errors.Join(cleanupErr, errors.New("Cloud verification could not archive its thread"))
			}
		}
		if _, err = rpc.Call(cleanup, "thread/archive", map[string]any{"threadId": record.ThreadID}); err != nil {
			cleanupErr = errors.Join(cleanupErr, errors.New("Cloud verification could not confirm thread archival"))
		}
	}
	return cleanupErr
}

func cloudAnswerText(response []byte) string {
	items := gjson.GetBytes(response, "output").Array()
	hasFinal := false
	for _, item := range items {
		if item.Get("type").String() == "message" && item.Get("role").String() == "assistant" && item.Get("phase").String() == "final_answer" {
			hasFinal = true
			break
		}
	}
	var texts []string
	for _, item := range items {
		if item.Get("type").String() != "message" || item.Get("role").String() != "assistant" || hasFinal && item.Get("phase").String() != "final_answer" {
			continue
		}
		for _, content := range item.Get("content").Array() {
			if content.Get("type").String() == "output_text" {
				texts = append(texts, content.Get("text").String())
			}
		}
	}
	return strings.Join(texts, "")
}
