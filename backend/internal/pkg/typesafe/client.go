package typesafe

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/util/urlvalidator"
)

const (
	DefaultBaseURL = "https://api.typesafe.ai"
	SystemOnePath  = "/v1/systemone"
	JevLatestModel = "jev-latest"
)

type Question struct {
	Type         string `json:"type"`
	Instructions string `json:"instructions"`
}

type Request struct {
	Model     string              `json:"model"`
	State     string              `json:"state"`
	Questions map[string]Question `json:"questions"`
}

type Result struct {
	Model  string
	Scores map[string]float64
	Usage  Usage
}

type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type SystemOneResponse struct {
	Body  []byte
	Model string
	Usage Usage
	// ObservedUsage is authoritative for settlement. Usage remains the legacy
	// numeric projection used by account diagnostics; an absent measurement is
	// not a measured zero and must never be billed through that projection.
	ObservedUsage SystemOneUsage
}

// SystemOneUsage preserves independently observed counters. Nil means missing
// or invalid; a non-nil zero is an explicit upstream measurement.
type SystemOneUsage struct {
	InputTokens  *int64
	OutputTokens *int64
}

func (u SystemOneUsage) Complete() bool { return u.InputTokens != nil && u.OutputTokens != nil }

// SystemOneURL accepts a server root, API base or full System One endpoint.
// Host allowlists, private-network access and HTTP policy remain the caller's
// responsibility; this only validates URL syntax and normalizes the API path.
func SystemOneURL(baseURL string) (string, error) {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	normalized, err := urlvalidator.ValidateURLFormat(baseURL, true)
	if err != nil {
		return "", errors.New("typesafe invalid endpoint")
	}
	u, err := url.Parse(normalized)
	if err != nil || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return "", errors.New("typesafe invalid endpoint")
	}
	base := strings.TrimRight(u.String(), "/")
	path := strings.ToLower(strings.TrimRight(u.EscapedPath(), "/"))
	switch {
	case strings.HasSuffix(path, SystemOnePath):
		base = base[:len(base)-len(SystemOnePath)]
	case strings.HasSuffix(path, "/v1"):
		base = base[:len(base)-len("/v1")]
	}
	endpoint, err := url.JoinPath(base, SystemOnePath)
	if err != nil {
		return "", errors.New("typesafe invalid endpoint")
	}
	return endpoint, nil
}

func NewSystemOneRequest(ctx context.Context, baseURL, key string, body []byte) (*http.Request, error) {
	endpoint, err := SystemOneURL(baseURL)
	if err != nil {
		return nil, errors.New("typesafe invalid endpoint")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, errors.New("typesafe invalid request")
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	return req, nil
}

// MaxSystemOneResponseBytes bounds a buffered System One response body.
const MaxSystemOneResponseBytes = 4 << 20

var ErrSystemOneResponseTooLarge = errors.New("typesafe response exceeds size limit")

func DecodeSystemOneResponse(r io.Reader) (*SystemOneResponse, error) {
	body, err := io.ReadAll(io.LimitReader(r, MaxSystemOneResponseBytes+1))
	if err != nil {
		return nil, errors.New("typesafe invalid response")
	}
	// A truncated body would otherwise surface as a misleading "invalid JSON".
	if len(body) > MaxSystemOneResponseBytes {
		return nil, ErrSystemOneResponseTooLarge
	}
	if !json.Valid(body) {
		return nil, errors.New("typesafe invalid response")
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(body, &envelope); err != nil || envelope == nil {
		return nil, errors.New("typesafe invalid response")
	}
	// The upstream already answered (and charged); an unexpected model or usage
	// shape must not discard the answer, so both are decoded leniently.
	var model string
	_ = json.Unmarshal(envelope["model"], &model)
	var usage map[string]json.RawMessage
	_ = json.Unmarshal(envelope["usage"], &usage)
	input := systemOneTokenCount(usage["input_tokens"])
	output := systemOneTokenCount(usage["output_tokens"])
	result := &SystemOneResponse{
		Body:          body,
		Model:         model,
		ObservedUsage: SystemOneUsage{InputTokens: input, OutputTokens: output},
	}
	if input != nil {
		result.Usage.InputTokens = int(*input)
	}
	if output != nil {
		result.Usage.OutputTokens = int(*output)
	}
	return result, nil
}

// maxSystemOneTokenCount bounds a reported token count before int conversion.
const maxSystemOneTokenCount = 1 << 40

// systemOneTokenCount retains integer/decimal/numeric-string compatibility and
// the previous nearest-integer rounding, without float64 overflow/precision loss.
// Excessive or out-of-range values are unknown, never clamped into a charge.
func systemOneTokenCount(raw json.RawMessage) *int64 {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return nil
	}
	if raw[0] == '"' {
		var text string
		if json.Unmarshal(raw, &text) != nil {
			return nil
		}
		raw = json.RawMessage(strings.TrimSpace(text))
	}
	var number json.Number
	if json.Unmarshal(raw, &number) != nil {
		return nil
	}
	text := number.String()
	// Bound exact-number work independently of the response body's byte limit.
	if len(text) == 0 || len(text) > 128 {
		return nil
	}
	if pos := strings.LastIndexAny(text, "eE"); pos >= 0 {
		exponent, err := strconv.Atoi(text[pos+1:])
		if err != nil || exponent < -512 || exponent > 512 { return nil }
	}
	value, ok := new(big.Rat).SetString(text)
	if !ok || value.Sign() < 0 || value.Cmp(new(big.Rat).SetInt64(maxSystemOneTokenCount)) > 0 { return nil }
	whole, remainder := new(big.Int), new(big.Int)
	whole.QuoRem(value.Num(), value.Denom(), remainder)
	if remainder.Lsh(remainder, 1).Cmp(value.Denom()) >= 0 { whole.Add(whole, big.NewInt(1)) }
	if !whole.IsInt64() || whole.Int64() > int64(^uint(0)>>1) { return nil }
	count := whole.Int64()
	return &count
}

// Evaluate performs one attempt. The caller owns timeouts, retries and key rotation.
func Evaluate(ctx context.Context, client *http.Client, baseURL, key string, input Request) (*Result, int, error) {
	body, err := json.Marshal(input)
	if err != nil {
		return nil, 0, errors.New("typesafe invalid request")
	}
	req, err := NewSystemOneRequest(ctx, baseURL, key, body)
	if err != nil {
		return nil, 0, err
	}
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, 0, ctx.Err()
		}
		return nil, 0, errors.New("typesafe transport unavailable")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Do not log provider error bodies: they may echo user input or credentials.
		return nil, resp.StatusCode, fmt.Errorf("typesafe API status %d", resp.StatusCode)
	}
	var out struct {
		Model   string `json:"model"`
		Usage   Usage  `json:"usage"`
		Answers map[string]struct {
			Type string   `json:"type"`
			Noul *float64 `json:"noul"`
		} `json:"answers"`
	}
	raw, readErr := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if readErr != nil || len(raw) > 1<<20 || json.Unmarshal(raw, &out) != nil || strings.TrimSpace(out.Model) == "" {
		return nil, resp.StatusCode, errors.New("typesafe invalid response")
	}
	result := &Result{Model: out.Model, Usage: out.Usage, Scores: make(map[string]float64, len(input.Questions))}
	for id := range input.Questions {
		answer, ok := out.Answers[id]
		if !ok || answer.Type != "noul" || answer.Noul == nil || math.IsNaN(*answer.Noul) || math.IsInf(*answer.Noul, 0) || *answer.Noul < 0 || *answer.Noul > 1 {
			return nil, resp.StatusCode, fmt.Errorf("typesafe invalid answer for %s", id)
		}
		result.Scores[id] = *answer.Noul
	}
	return result, resp.StatusCode, nil
}
