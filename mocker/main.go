package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/bytedance/sonic"
	"github.com/maximhq/bifrost/core/schemas"
	"github.com/valyala/fasthttp"
)

type OpenAIChatCompletionsResponse struct {
	ID                string                          `json:"id"`                 // Unique identifier for the completion
	Object            string                          `json:"object"`             // Type of completion (text.completion or chat.completion)
	Choices           []schemas.BifrostResponseChoice `json:"choices"`            // Array of completion choices
	Model             string                          `json:"model"`              // Model used for the completion
	Created           int                             `json:"created"`            // Unix timestamp of completion creation
	ServiceTier       *string                         `json:"service_tier"`       // Service tier used for the request
	SystemFingerprint *string                         `json:"system_fingerprint"` // System fingerprint for the request
	Usage             schemas.LLMUsage                `json:"usage"`              // Token usage statistics
}

type OpenAIError struct {
	EventID *string     `json:"event_id,omitempty"`
	Error   *ErrorField `json:"error"`
}

type ErrorField struct {
	Type    *string `json:"type,omitempty"`
	Code    *string `json:"code,omitempty"`
	Message string  `json:"message"`
	Error   error   `json:"error,omitempty"`
}

// GenericRequest represents any incoming request with a model field
type GenericRequest struct {
	Model  string `json:"model"`
	Stream bool   `json:"stream"`
}

// ProviderAliases maps provider aliases to canonical provider IDs.
var ProviderAliases = map[string]string{
	"openai":      "openai",
	"azure":       "azure",
	"anthropic":   "anthropic",
	"bedrock":     "bedrock",
	"cohere":      "cohere",
	"vertex":      "vertex",
	"vertexai":    "vertex",
	"vetex":       "vertex", // common typo
	"google":      "vertex",
	"genai":       "vertex",
	"mistral":     "mistral",
	"ollama":      "ollama",
	"groq":        "groq",
	"sgl":         "sgl",
	"sglang":      "sgl",
	"parasail":    "parasail",
	"perplexity":  "perplexity",
	"cerebras":    "cerebras",
	"gemini":      "gemini",
	"openrouter":  "openrouter",
	"elevenlabs":  "elevenlabs",
	"huggingface": "huggingface",
	"nebius":      "nebius",
	"xai":         "xai",
	"replicate":   "replicate",
	"vllm":        "vllm",
}

func parseProviderAndModel(rawModel string) (provider string, model string) {
	model = strings.TrimSpace(rawModel)
	if model == "" {
		return "", "gpt-4o-mini"
	}
	if strings.Contains(model, "/") {
		parts := strings.SplitN(model, "/", 2)
		if len(parts) == 2 {
			if canonicalProvider, ok := ProviderAliases[strings.ToLower(parts[0])]; ok {
				return canonicalProvider, parts[1]
			}
		}
	}
	return "", model
}

// parseModelFromRequest extracts model and provider from the OpenAI-style request body.
func parseModelFromRequest(ctx *fasthttp.RequestCtx) (provider string, model string, stream bool) {
	var req GenericRequest
	if err := sonic.Unmarshal(ctx.Request.Body(), &req); err != nil {
		return "", "gpt-4o-mini", false
	}
	provider, model = parseProviderAndModel(req.Model)
	return provider, model, req.Stream
}

// ChatStreamResponseChoiceDelta represents partial message information in streaming
type ChatStreamResponseChoiceDelta struct {
	Role    *string `json:"role,omitempty"`
	Content *string `json:"content,omitempty"`
}

// ChatStreamResponseChoice represents a choice in the stream response
type ChatStreamResponseChoice struct {
	Index        int                            `json:"index"`
	Delta        *ChatStreamResponseChoiceDelta `json:"delta,omitempty"`
	FinishReason *string                        `json:"finish_reason"`
}

// ChatCompletionStreamResponse represents a chunk in the streaming response
type ChatCompletionStreamResponse struct {
	ID      string                     `json:"id"`
	Object  string                     `json:"object"`
	Created int                        `json:"created"`
	Model   string                     `json:"model"`
	Choices []ChatStreamResponseChoice `json:"choices"`
}

type AnthropicStreamMessage struct {
	ID           string      `json:"id"`
	Type         string      `json:"type"`
	Role         string      `json:"role"`
	Model        string      `json:"model"`
	Content      []any       `json:"content"`
	StopReason   interface{} `json:"stop_reason"`
	StopSequence interface{} `json:"stop_sequence"`
	Usage        struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

type AnthropicContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type AnthropicTextDelta struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// Minimal schema for the OpenAI v1/responses API
type OpenAIResponsesMessageContent struct {
	Type string `json:"type"` // e.g., "output_text"
	Text string `json:"text"`
}

type OpenAIResponsesOutputItem struct {
	ID      string                          `json:"id"`
	Type    string                          `json:"type"` // e.g., "message"
	Role    string                          `json:"role"`
	Content []OpenAIResponsesMessageContent `json:"content"`
}

type OpenAIResponsesResponse struct {
	ID      string                      `json:"id"`
	Object  string                      `json:"object"` // "response"
	Created int                         `json:"created"`
	Model   string                      `json:"model"`
	Output  []OpenAIResponsesOutputItem `json:"output"`
	Status  string                      `json:"status"` // e.g., "completed"
	Usage   schemas.LLMUsage            `json:"usage"`
}

// OpenAI List Models API structures
type OpenAIModel struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int    `json:"created"`
	OwnedBy string `json:"owned_by"`
}

type OpenAIModelsResponse struct {
	Object string        `json:"object"` // "list"
	Data   []OpenAIModel `json:"data"`
}

// OpenAI Embeddings API structures
type OpenAIEmbeddingData struct {
	Object    string    `json:"object"`    // "embedding"
	Embedding []float64 `json:"embedding"` // Vector of floats
	Index     int       `json:"index"`     // Index of the embedding
}

type OpenAIEmbeddingsResponse struct {
	Object string                `json:"object"` // "list"
	Data   []OpenAIEmbeddingData `json:"data"`   // Array of embedding objects
	Model  string                `json:"model"`  // Model used
	Usage  schemas.LLMUsage      `json:"usage"`  // Token usage
}

type AnthropicRequest struct {
	Model  string `json:"model"`
	Stream bool   `json:"stream"`
}

type AnthropicMessageContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type AnthropicMessageUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type AnthropicMessageResponse struct {
	ID           string                    `json:"id"`
	Type         string                    `json:"type"`
	Role         string                    `json:"role"`
	Model        string                    `json:"model"`
	Content      []AnthropicMessageContent `json:"content"`
	StopReason   string                    `json:"stop_reason"`
	StopSequence interface{}               `json:"stop_sequence"`
	Usage        AnthropicMessageUsage     `json:"usage"`
}

type GenAIPart struct {
	Text string `json:"text"`
}

type GenAIContent struct {
	Parts []GenAIPart `json:"parts"`
	Role  string      `json:"role"`
}

type GenAICandidate struct {
	Content      GenAIContent `json:"content"`
	FinishReason string       `json:"finishReason"`
	Index        int          `json:"index"`
}

type GenAIUsageMetadata struct {
	PromptTokenCount     int `json:"promptTokenCount"`
	CandidatesTokenCount int `json:"candidatesTokenCount"`
	TotalTokenCount      int `json:"totalTokenCount"`
}

type GenAIResponse struct {
	Candidates    []GenAICandidate   `json:"candidates"`
	UsageMetadata GenAIUsageMetadata `json:"usageMetadata"`
	ModelVersion  string             `json:"modelVersion"`
}

type BedrockContent struct {
	Text string `json:"text"`
}

type BedrockMessage struct {
	Role    string           `json:"role"`
	Content []BedrockContent `json:"content"`
}

type BedrockConverseOutput struct {
	Message BedrockMessage `json:"message"`
}

type BedrockUsage struct {
	InputTokens  int `json:"inputTokens"`
	OutputTokens int `json:"outputTokens"`
	TotalTokens  int `json:"totalTokens"`
}

type BedrockMetrics struct {
	LatencyMs int `json:"latencyMs"`
}

type BedrockConverseResponse struct {
	Output     BedrockConverseOutput `json:"output"`
	StopReason string                `json:"stopReason"`
	Usage      BedrockUsage          `json:"usage"`
	Metrics    BedrockMetrics        `json:"metrics"`
}

var (
	host               string
	port               int
	latency            int
	jitter             int
	latencyAuthKeys    string
	tokensPerChunk     int
	fixedInputTokens   int
	fixedOutputTokens  int
	bigPayload         bool
	auth               string
	withErrors         bool
	failurePercent     int
	failureJitter      int
	failureAuthKeys    string
	tpm                int
	tpmDuration        int
	tpmAuthKeys        string
	modelsList         string
	logRaw             bool
	rateLimitedKeys    string
	rateLimitedKeyMap  map[string]bool
	startTime          time.Time
	tpmTriggeredLogged bool

	// Dynamic per-key latency behaviors:
	// spikes = sparse latency outliers, ramp = gradual base drift, step = abrupt base change.
	latencySpikeKeys string
	latencyRampKeys  string
	latencyStepKeys  string
	spikeMap         = map[string]spikeSpec{}
	rampMap          = map[string]int{}
	stepMap          = map[string]stepSpec{}
)

// spikeSpec injects a latency outlier into pct% of requests by multiplying the
// resolved latency by mult. Used to verify the LB's anomaly detector rejects
// spikes from training rather than letting them drag the learned baseline.
type spikeSpec struct {
	pct  int
	mult float64
}

// stepSpec abruptly replaces a key's base latency with toMs once atSec seconds
// have elapsed — simulates a provider whose latency suddenly degrades.
type stepSpec struct {
	atSec int
	toMs  int
}

// parseKVInt parses a "key=a:b" CSV into per-token (a,b) ints (b optional).
func parseKVList(csv string, fn func(token string, a int, b string)) {
	if csv == "" {
		return
	}
	for _, entry := range strings.Split(csv, ",") {
		entry = strings.TrimSpace(entry)
		idx := strings.LastIndex(entry, "=")
		if idx < 0 {
			continue
		}
		token := strings.TrimSpace(entry[:idx])
		aStr, bStr, _ := strings.Cut(entry[idx+1:], ":")
		a, err := strconv.Atoi(strings.TrimSpace(aStr))
		if err != nil {
			continue
		}
		fn(token, a, strings.TrimSpace(bStr))
	}
}

func init() {
	flag.StringVar(&host, "host", getEnvString("MOCKER_HOST", "localhost"), "Host address to bind the mock server")
	flag.IntVar(&port, "port", getEnvInt("MOCKER_PORT", 8000), "Port for the mock server to listen on")
	flag.IntVar(&latency, "latency", getEnvInt("MOCKER_LATENCY", 0), "Latency in milliseconds to simulate")
	flag.IntVar(&jitter, "jitter", getEnvInt("MOCKER_JITTER", 0), "Maximum jitter in milliseconds to add to latency (±jitter)")
	flag.StringVar(&latencyAuthKeys, "latency-auth-keys", getEnvString("MOCKER_LATENCY_AUTH_KEYS", ""), "Comma-separated Authorization header values that get latency; entries may override the global config per key as key=latencyMs, key=latencyMs:jitterMs, or a percentile distribution key=p50:p90:p95:p99; other keys respond instantly (empty = all requests)")
	flag.IntVar(&tokensPerChunk, "tokens-per-chunk", getEnvInt("MOCKER_TOKENS_PER_CHUNK", 5), "Words batched into each SSE delta when streaming (must be >=1)")
	flag.IntVar(&fixedInputTokens, "input-tokens", getEnvInt("MOCKER_INPUT_TOKENS", -1), "Fixed input/prompt token count to report in usage (negative = random/derived per request)")
	flag.IntVar(&fixedOutputTokens, "output-tokens", getEnvInt("MOCKER_OUTPUT_TOKENS", -1), "Fixed output/completion token count to report in usage (negative = random/derived per request)")
	flag.BoolVar(&bigPayload, "big-payload", getEnvBool("MOCKER_BIG_PAYLOAD", false), "Use big payload")
	flag.StringVar(&auth, "auth", getEnvString("MOCKER_AUTH", ""), "Add authentication header key")
	flag.BoolVar(&withErrors, "with-errors", getEnvBool("MOCKER_WITH_ERRORS", false), "Enable provider-specific random error responses")
	flag.BoolVar(&withErrors, "witherrors", getEnvBool("MOCKER_WITH_ERRORS", false), "Alias of -with-errors")
	flag.IntVar(&failurePercent, "failure-percent", getEnvInt("MOCKER_FAILURE_PERCENT", 0), "Base failure percentage (0-100)")
	flag.IntVar(&failureJitter, "failure-jitter", getEnvInt("MOCKER_FAILURE_JITTER", 0), "Maximum jitter in percentage points to add to failure rate (±failure-jitter)")
	flag.StringVar(&failureAuthKeys, "failure-auth-keys", getEnvString("MOCKER_FAILURE_AUTH_KEYS", ""), "Comma-separated Authorization header values subject to the failure percentage; entries may override the global config per key as key=percent or key=percent:jitter; other keys always succeed (empty = all requests)")
	flag.IntVar(&tpm, "tpm", getEnvInt("MOCKER_TPM", 0), "Seconds after which to trigger TPM (429) scenarios (0 = disabled)")
	flag.IntVar(&tpmDuration, "tpm-duration", getEnvInt("MOCKER_TPM_DURATION", 0), "Duration in seconds for TPM window, i.e. tpm to tpm+tpm-duration (0 = until server stop)")
	flag.StringVar(&tpmAuthKeys, "tpm-auth-keys", getEnvString("MOCKER_TPM_AUTH_KEYS", ""), "Comma-separated Authorization header values that trigger TPM (empty = all requests)")
	flag.StringVar(&modelsList, "models", getEnvString("MOCKER_MODELS", "gpt-4o-mini,gpt-4o,claude-3-5-sonnet-latest,gemini-2.0-flash"), "Comma-separated model ids returned by GET /v1/models")
	flag.BoolVar(&logRaw, "log-raw", getEnvBool("MOCKER_LOG_RAW", false), "Log raw request and response bodies")
	flag.StringVar(&rateLimitedKeys, "rate-limited-keys", getEnvString("MOCKER_RATE_LIMITED_KEYS", ""), "Comma-separated list of Authorization header values that always receive 429 (e.g. 'Bearer key-1,Bearer key-2')")
	flag.StringVar(&latencySpikeKeys, "latency-spike-keys", getEnvString("MOCKER_LATENCY_SPIKE_KEYS", ""), "Per-key sparse latency spikes as key=pct:mult (e.g. 'slow-key=10:5' → 10% of requests get 5x latency). Tests outlier rejection.")
	flag.StringVar(&latencyRampKeys, "latency-ramp-keys", getEnvString("MOCKER_LATENCY_RAMP_KEYS", ""), "Per-key linear base-latency drift in ms added per minute elapsed (e.g. 'slow-key=2000'). Tests gradual-drift tracking.")
	flag.StringVar(&latencyStepKeys, "latency-step-keys", getEnvString("MOCKER_LATENCY_STEP_KEYS", ""), "Per-key abrupt base-latency step as key=atSec:toMs (e.g. 'slow-key=30:8000' → at 30s base jumps to 8000ms). Tests abrupt-change handling.")
}

// Helper functions to read environment variables with defaults
func getEnvString(key string, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}

// resolveInputTokens returns the configured fixed input/prompt token count when
// -input-tokens (MOCKER_INPUT_TOKENS) is set (>= 0); otherwise it returns the
// supplied fallback (the existing random or request-derived value).
func resolveInputTokens(fallback int) int {
	if fixedInputTokens >= 0 {
		return fixedInputTokens
	}
	return fallback
}

// resolveOutputTokens returns the configured fixed output/completion token count
// when -output-tokens (MOCKER_OUTPUT_TOKENS) is set (>= 0); otherwise it returns
// the supplied fallback (the existing random or request-derived value).
func resolveOutputTokens(fallback int) int {
	if fixedOutputTokens >= 0 {
		return fixedOutputTokens
	}
	return fallback
}

func getEnvBool(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		if boolValue, err := strconv.ParseBool(value); err == nil {
			return boolValue
		}
		// Also accept "1" and "true" as true, "0" and "false" as false
		if value == "1" || strings.ToLower(value) == "true" {
			return true
		}
		if value == "0" || strings.ToLower(value) == "false" {
			return false
		}
	}
	return defaultValue
}

// StrPtr creates a pointer to a string value.
func StrPtr(s string) *string {
	return &s
}

// authKeyMatches reports whether the request's Authorization header value is in
// the comma-separated key list. An empty list matches every request. The
// "Bearer " prefix is stripped from the header before comparison, so lists hold
// raw token values (same convention as -tpm-auth-keys).
func authKeyMatches(keysCSV string, authHeader string) bool {
	if keysCSV == "" {
		return true
	}
	token := strings.TrimPrefix(authHeader, "Bearer ")
	for _, key := range strings.Split(keysCSV, ",") {
		if strings.TrimSpace(key) == token {
			return true
		}
	}
	return false
}

// latencySpec is the latency configuration resolved for a single request. A key
// is configured in exactly one mode: either avg latency with optional symmetric
// jitter (latencyMs/jitterMs), or a percentile distribution (pctl). The two are
// mutually exclusive — pctl is non-nil only in percentile mode.
type latencySpec struct {
	latencyMs int
	jitterMs  int
	pctl      *percentileSpec
}

// percentileSpec describes a per-key latency distribution by its p50/p90/p95/p99
// quantiles (ms). Per-request latencies are drawn so that, over many requests,
// the observed percentiles converge to these values — see sample.
type percentileSpec struct {
	p50, p90, p95, p99 int
}

// lerp linearly interpolates between a and b at t in [0,1].
func lerp(a, b int, t float64) int {
	return a + int(float64(b-a)*t+0.5)
}

// sample draws one latency (ms) from the distribution defined by the quantiles,
// treating them as points on the inverse CDF and interpolating linearly between
// adjacent breakpoints. A uniform draw u maps to the matching segment so the
// empirical p50/p90/p95/p99 reproduce the configured values. The body below p50
// is mirrored about p50 (floored at 0) to give the bulk a plausible spread.
func (p percentileSpec) sample() int {
	u := rand.Float64()
	switch {
	case u < 0.5:
		low := 2*p.p50 - p.p90
		if low < 0 {
			low = 0
		}
		return lerp(low, p.p50, u/0.5)
	case u < 0.9:
		return lerp(p.p50, p.p90, (u-0.5)/0.4)
	case u < 0.95:
		return lerp(p.p90, p.p95, (u-0.9)/0.05)
	case u < 0.99:
		return lerp(p.p95, p.p99, (u-0.95)/0.04)
	default:
		return p.p99
	}
}

// actualMs returns the latency to apply for one request, with jitter
// (±jitterMs, clamped at 0) rolled in.
func (s latencySpec) actualMs() int {
	actual := s.latencyMs
	if s.jitterMs > 0 {
		actual += rand.Intn(2*s.jitterMs+1) - s.jitterMs
		if actual < 0 {
			actual = 0
		}
	}
	return actual
}

// computeLatencyMs resolves the final latency for one request, layering the
// dynamic per-key behaviors on top of the static spec: abrupt step and linear
// ramp adjust the BASE (so they shift the true distribution the LB should
// learn), then symmetric jitter is rolled in, and finally a sparse spike may
// multiply the result (an outlier the LB should reject, not learn).
func computeLatencyMs(token string, spec latencySpec) int {
	var actual int
	if spec.pctl != nil {
		// Percentile mode: draw from the configured distribution. Step/ramp,
		// which shift a single base value, don't apply here.
		actual = spec.pctl.sample()
	} else {
		base := spec.latencyMs
		if st, ok := stepMap[token]; ok && !startTime.IsZero() && int(time.Since(startTime).Seconds()) >= st.atSec {
			base = st.toMs
		}
		if perMin, ok := rampMap[token]; ok && !startTime.IsZero() {
			base += int(time.Since(startTime).Minutes() * float64(perMin))
		}
		actual = base
		if spec.jitterMs > 0 {
			actual += rand.Intn(2*spec.jitterMs+1) - spec.jitterMs
		}
	}
	if sp, ok := spikeMap[token]; ok && sp.pct > 0 && rand.Intn(100) < sp.pct {
		actual = int(float64(actual) * sp.mult)
	}
	if actual < 0 {
		actual = 0
	}
	return actual
}

// parseLatencyEntry splits one -latency-auth-keys entry into its key and an
// optional override after the last '='. The override is colon-separated and its
// field count selects the mode (mutually exclusive):
//   - "latencyMs"                       -> avg latency, no jitter
//   - "latencyMs:jitterMs"              -> avg latency with symmetric jitter
//   - "p50:p90:p95:p99"                 -> percentile distribution (ascending)
//
// A 3-field override is ambiguous and rejected. The split happens at the last
// '=' only when its right side parses as a valid override, so keys containing
// '=' (e.g. base64 padding) still work as bare entries; anything that fails to
// parse degrades to a bare key.
func parseLatencyEntry(entry string) (key string, spec latencySpec, hasSpec bool) {
	idx := strings.LastIndex(entry, "=")
	if idx < 0 {
		return entry, latencySpec{}, false
	}
	key = strings.TrimSpace(entry[:idx])
	fields := strings.Split(entry[idx+1:], ":")
	vals := make([]int, len(fields))
	for i, f := range fields {
		v, err := strconv.Atoi(f)
		if err != nil || v < 0 {
			return entry, latencySpec{}, false
		}
		vals[i] = v
	}
	switch len(vals) {
	case 1:
		return key, latencySpec{latencyMs: vals[0]}, true
	case 2:
		return key, latencySpec{latencyMs: vals[0], jitterMs: vals[1]}, true
	case 4:
		if !(vals[0] <= vals[1] && vals[1] <= vals[2] && vals[2] <= vals[3]) {
			return entry, latencySpec{}, false
		}
		return key, latencySpec{
			latencyMs: vals[0],
			pctl:      &percentileSpec{p50: vals[0], p90: vals[1], p95: vals[2], p99: vals[3]},
		}, true
	default:
		return entry, latencySpec{}, false
	}
}

// resolveLatencySpec returns the latency/jitter configuration for the request's
// Authorization header and whether the request is subject to latency at all.
// An empty key list matches every request with the global -latency/-jitter.
// Listed keys may be bare ("key-A", globals apply) or carry a per-key override
// ("key-A=200" or "key-A=200:50"); non-listed keys get no latency. The
// "Bearer " prefix is stripped before comparison, same as authKeyMatches.
func resolveLatencySpec(keysCSV string, authHeader string) (latencySpec, bool) {
	if keysCSV == "" {
		return latencySpec{latencyMs: latency, jitterMs: jitter}, true
	}
	token := strings.TrimPrefix(authHeader, "Bearer ")
	for _, entry := range strings.Split(keysCSV, ",") {
		key, spec, hasSpec := parseLatencyEntry(strings.TrimSpace(entry))
		if key != token {
			continue
		}
		if hasSpec {
			return spec, true
		}
		return latencySpec{latencyMs: latency, jitterMs: jitter}, true
	}
	return latencySpec{}, false
}

// simulateLatency handles latency simulation with optional jitter. When
// -latency-auth-keys is set, only requests carrying one of those keys sleep
// (each for its per-key override when given, otherwise the global config);
// everything else responds instantly.
func simulateLatency(authHeader string) {
	spec, ok := resolveLatencySpec(latencyAuthKeys, authHeader)
	if !ok {
		return
	}
	token := strings.TrimPrefix(authHeader, "Bearer ")
	if actual := computeLatencyMs(token, spec); actual > 0 {
		time.Sleep(time.Duration(actual) * time.Millisecond)
	}
}

// failureSpec is the failure configuration resolved for a single request.
type failureSpec struct {
	percent int
	jitter  int
}

// shouldFailNow rolls the dice for one request using percent ±jitter (clamped to
// the 0-100 range) and reports whether this request should fail.
func (s failureSpec) shouldFailNow() bool {
	actual := s.percent
	if s.jitter > 0 {
		actual += rand.Intn(2*s.jitter+1) - s.jitter
		if actual < 0 {
			actual = 0
		}
		if actual > 100 {
			actual = 100
		}
	}
	return actual > 0 && rand.Intn(100) < actual
}

// parseFailureEntry splits one -failure-auth-keys entry into its key and an
// optional "=percent" / "=percent:jitter" override. Mirrors parseLatencyEntry:
// the split happens at the last '=' only when its right side parses as numbers,
// so keys containing '=' (e.g. base64 padding) still work as bare entries.
func parseFailureEntry(entry string) (key string, spec failureSpec, hasSpec bool) {
	idx := strings.LastIndex(entry, "=")
	if idx < 0 {
		return entry, failureSpec{}, false
	}
	pctStr, jitStr, hasJitter := strings.Cut(entry[idx+1:], ":")
	pct, err := strconv.Atoi(pctStr)
	if err != nil || pct < 0 {
		return entry, failureSpec{}, false
	}
	jit := 0
	if hasJitter {
		jit, err = strconv.Atoi(jitStr)
		if err != nil || jit < 0 {
			return entry, failureSpec{}, false
		}
	}
	return strings.TrimSpace(entry[:idx]), failureSpec{percent: pct, jitter: jit}, true
}

// resolveFailureSpec returns the failure percent/jitter for the request's
// Authorization header and whether the request is subject to failures at all.
// An empty key list matches every request with the global
// -failure-percent/-failure-jitter. Listed keys may be bare ("key-A", globals
// apply) or carry a per-key override ("key-A=10" or "key-A=10:5"); non-listed
// keys always succeed. The "Bearer " prefix is stripped before comparison, same
// as authKeyMatches.
func resolveFailureSpec(keysCSV string, authHeader string) (failureSpec, bool) {
	if keysCSV == "" {
		return failureSpec{percent: failurePercent, jitter: failureJitter}, true
	}
	token := strings.TrimPrefix(authHeader, "Bearer ")
	for _, entry := range strings.Split(keysCSV, ",") {
		key, spec, hasSpec := parseFailureEntry(strings.TrimSpace(entry))
		if key != token {
			continue
		}
		if hasSpec {
			return spec, true
		}
		return failureSpec{percent: failurePercent, jitter: failureJitter}, true
	}
	return failureSpec{}, false
}

// shouldFail checks if request should fail based on failure percentage with jitter.
// When -failure-auth-keys is set, only requests carrying one of those keys are
// subject to the failure rate; everything else always succeeds. Listed keys may
// carry a per-key override ("key-A=10:5") to use a different rate than the global
// -failure-percent/-failure-jitter.
func shouldFail(authHeader string) bool {
	if withErrors {
		// In with-errors mode, use provider-specific random errors only.
		return false
	}
	spec, ok := resolveFailureSpec(failureAuthKeys, authHeader)
	if !ok {
		return false
	}
	return spec.shouldFailNow()
}

func effectiveFailurePercent() int {
	actualFailurePercent := failurePercent
	if withErrors && actualFailurePercent == 0 {
		actualFailurePercent = 20
	}
	if failureJitter > 0 {
		jitterOffset := rand.Intn(2*failureJitter+1) - failureJitter
		actualFailurePercent += jitterOffset
	}
	if actualFailurePercent < 0 {
		actualFailurePercent = 0
	}
	if actualFailurePercent > 100 {
		actualFailurePercent = 100
	}
	if withErrors {
		if actualFailurePercent < 1 {
			actualFailurePercent = 1
		}
		if actualFailurePercent > 95 {
			actualFailurePercent = 95
		}
	}
	return actualFailurePercent
}

type providerErrorVariant struct {
	Status int
	Body   map[string]interface{}
}

func inferProviderFromPath(path string) string {
	switch {
	case strings.HasPrefix(path, "/anthropic/") || path == "/v1/messages":
		return "anthropic"
	case strings.HasPrefix(path, "/genai/"), strings.HasPrefix(path, "/models/"), strings.HasPrefix(path, "/v1/models/"), strings.HasPrefix(path, "/v1beta/models/"):
		return "gemini"
	case strings.HasPrefix(path, "/model/"), strings.HasPrefix(path, "/bedrock/model/"):
		return "bedrock"
	case strings.HasPrefix(path, "/openai/"):
		return "openai"
	default:
		return "openai"
	}
}

func providerErrorCatalog(provider string) []providerErrorVariant {
	openAIStyle := []providerErrorVariant{
		{Status: fasthttp.StatusBadRequest, Body: map[string]interface{}{"error": map[string]interface{}{"type": "invalid_request_error", "code": "invalid_request_error", "message": "Invalid request body"}}},
		{Status: fasthttp.StatusUnauthorized, Body: map[string]interface{}{"error": map[string]interface{}{"type": "authentication_error", "code": "invalid_api_key", "message": "Incorrect API key provided"}}},
		{Status: fasthttp.StatusTooManyRequests, Body: map[string]interface{}{"error": map[string]interface{}{"type": "rate_limit_error", "code": "rate_limit_exceeded", "message": "Rate limit exceeded"}}},
		{Status: fasthttp.StatusInternalServerError, Body: map[string]interface{}{"error": map[string]interface{}{"type": "server_error", "code": "internal_server_error", "message": "Internal server error"}}},
	}

	switch provider {
	case "anthropic":
		return []providerErrorVariant{
			{Status: fasthttp.StatusBadRequest, Body: map[string]interface{}{"type": "error", "error": map[string]interface{}{"type": "invalid_request_error", "message": "Invalid request"}}},
			{Status: fasthttp.StatusUnauthorized, Body: map[string]interface{}{"type": "error", "error": map[string]interface{}{"type": "authentication_error", "message": "Invalid API key"}}},
			{Status: fasthttp.StatusTooManyRequests, Body: map[string]interface{}{"type": "error", "error": map[string]interface{}{"type": "rate_limit_error", "message": "Rate limit exceeded"}}},
			{Status: fasthttp.StatusInternalServerError, Body: map[string]interface{}{"type": "error", "error": map[string]interface{}{"type": "api_error", "message": "Internal server error"}}},
		}
	case "bedrock":
		return []providerErrorVariant{
			{Status: fasthttp.StatusBadRequest, Body: map[string]interface{}{"__type": "ValidationException", "message": "Malformed input request"}},
			{Status: fasthttp.StatusUnauthorized, Body: map[string]interface{}{"__type": "UnrecognizedClientException", "message": "The security token included in the request is invalid"}},
			{Status: fasthttp.StatusTooManyRequests, Body: map[string]interface{}{"__type": "ThrottlingException", "message": "Rate exceeded"}},
			{Status: fasthttp.StatusServiceUnavailable, Body: map[string]interface{}{"__type": "ServiceUnavailableException", "message": "Service unavailable"}},
		}
	case "gemini", "vertex":
		return []providerErrorVariant{
			{Status: fasthttp.StatusBadRequest, Body: map[string]interface{}{"error": map[string]interface{}{"code": 400, "message": "Invalid argument", "status": "INVALID_ARGUMENT"}}},
			{Status: fasthttp.StatusUnauthorized, Body: map[string]interface{}{"error": map[string]interface{}{"code": 401, "message": "Request had invalid authentication credentials", "status": "UNAUTHENTICATED"}}},
			{Status: fasthttp.StatusTooManyRequests, Body: map[string]interface{}{"error": map[string]interface{}{"code": 429, "message": "Quota exceeded", "status": "RESOURCE_EXHAUSTED"}}},
			{Status: fasthttp.StatusInternalServerError, Body: map[string]interface{}{"error": map[string]interface{}{"code": 500, "message": "Internal error", "status": "INTERNAL"}}},
		}
	case "cohere":
		return []providerErrorVariant{
			{Status: fasthttp.StatusBadRequest, Body: map[string]interface{}{"message": "invalid request", "type": "invalid_request_error"}},
			{Status: fasthttp.StatusUnauthorized, Body: map[string]interface{}{"message": "invalid api key", "type": "authentication_error"}},
			{Status: fasthttp.StatusTooManyRequests, Body: map[string]interface{}{"message": "too many requests", "type": "rate_limit_error"}},
			{Status: fasthttp.StatusInternalServerError, Body: map[string]interface{}{"message": "internal error", "type": "server_error"}},
		}
	case "elevenlabs":
		return []providerErrorVariant{
			{Status: fasthttp.StatusBadRequest, Body: map[string]interface{}{"detail": map[string]interface{}{"status": "invalid_request", "message": "Invalid request"}}},
			{Status: fasthttp.StatusUnauthorized, Body: map[string]interface{}{"detail": map[string]interface{}{"status": "unauthorized", "message": "Invalid API key"}}},
			{Status: fasthttp.StatusTooManyRequests, Body: map[string]interface{}{"detail": map[string]interface{}{"status": "too_many_requests", "message": "Rate limit exceeded"}}},
			{Status: fasthttp.StatusInternalServerError, Body: map[string]interface{}{"detail": map[string]interface{}{"status": "internal_server_error", "message": "Internal server error"}}},
		}
	default:
		return openAIStyle
	}
}

func maybeSendRandomProviderError(ctx *fasthttp.RequestCtx, provider string) bool {
	if !withErrors {
		return false
	}
	rate := effectiveFailurePercent()
	if rate <= 0 || rand.Intn(100) >= rate {
		return false
	}
	if provider == "" {
		provider = inferProviderFromPath(string(ctx.Path()))
	}
	variants := providerErrorCatalog(provider)
	if len(variants) == 0 {
		return false
	}
	chosen := variants[rand.Intn(len(variants))]
	ctx.SetContentType("application/json")
	ctx.SetStatusCode(chosen.Status)
	if err := sonic.ConfigDefault.NewEncoder(ctx).Encode(chosen.Body); err != nil {
		log.Printf("Error encoding provider error response: %v", err)
		ctx.SetStatusCode(fasthttp.StatusInternalServerError)
		ctx.SetBodyString("Failed to encode error response")
	}
	return true
}

func parseAnthropicModelFromRequest(ctx *fasthttp.RequestCtx) (provider string, model string, stream bool) {
	var req AnthropicRequest
	if err := sonic.Unmarshal(ctx.Request.Body(), &req); err != nil {
		return "", "claude-3-5-sonnet-latest", false
	}
	provider, model = parseProviderAndModel(req.Model)
	if model == "" || model == "gpt-4o-mini" {
		model = "claude-3-5-sonnet-latest"
	}
	return provider, model, req.Stream
}

func parseGenAIModelFromPath(path string) (provider string, model string) {
	modelPart := ""
	switch {
	case strings.HasPrefix(path, "/models/"):
		modelPart = strings.TrimPrefix(path, "/models/")
	case strings.HasPrefix(path, "/v1beta/models/"):
		modelPart = strings.TrimPrefix(path, "/v1beta/models/")
	case strings.HasPrefix(path, "/v1/models/"):
		modelPart = strings.TrimPrefix(path, "/v1/models/")
	case strings.HasPrefix(path, "/genai/v1beta/models/"):
		modelPart = strings.TrimPrefix(path, "/genai/v1beta/models/")
	case strings.HasPrefix(path, "/genai/v1/models/"):
		modelPart = strings.TrimPrefix(path, "/genai/v1/models/")
	default:
		return "", "gemini-2.0-flash"
	}

	if sep := strings.Index(modelPart, ":"); sep >= 0 {
		modelPart = modelPart[:sep]
	}
	if decoded, err := url.PathUnescape(modelPart); err == nil {
		modelPart = decoded
	}
	if modelPart == "" {
		return "gemini", "gemini-2.0-flash"
	}
	provider, parsedModel := parseProviderAndModel(modelPart)
	if provider == "" {
		provider = "gemini"
	}
	return provider, parsedModel
}

func parseBedrockModelFromPath(path string) (model string, isConverse bool, isStream bool) {
	trimmed := strings.TrimPrefix(path, "/bedrock")
	parts := strings.Split(strings.Trim(trimmed, "/"), "/")
	if len(parts) != 3 || parts[0] != "model" {
		return "", false, false
	}
	model = parts[1]
	switch parts[2] {
	case "converse":
		return model, true, false
	case "converse-stream":
		return model, true, true
	default:
		return "", false, false
	}
}

func setSSEHeaders(ctx *fasthttp.RequestCtx) {
	ctx.SetContentType("text/event-stream; charset=utf-8")
	ctx.SetStatusCode(fasthttp.StatusOK)
	ctx.Response.Header.Set("Cache-Control", "no-cache")
	ctx.Response.Header.Set("Connection", "close")
	ctx.Response.Header.Set("X-Accel-Buffering", "no")
	ctx.Response.Header.Set("Transfer-Encoding", "chunked")
	ctx.Response.ImmediateHeaderFlush = true
	ctx.SetConnectionClose()
}

func getStreamWords(content string) []string {
	words := strings.Fields(content)
	if len(words) == 0 {
		return []string{"mock"}
	}
	return words
}

// buildStreamChunks groups words into delta strings, batching `tokensPerChunk`
// words per chunk. Each chunk except the last has a trailing space so clients
// concatenating deltas reproduce the original text without word merging.
func buildStreamChunks(words []string) []string {
	n := tokensPerChunk
	if n < 1 {
		n = 1
	}
	chunks := make([]string, 0, (len(words)+n-1)/n)
	for i := 0; i < len(words); i += n {
		end := i + n
		if end > len(words) {
			end = len(words)
		}
		chunk := strings.Join(words[i:end], " ")
		if end < len(words) {
			chunk += " "
		}
		chunks = append(chunks, chunk)
	}
	return chunks
}

// getStreamTotalLatency returns the total target wall-clock duration for a
// streaming response, with jitter applied once. Streaming handlers use this
// with deadline-based sleeping so per-iteration overhead (json marshal, flush
// syscall, time.Sleep granularity) is absorbed into the next gap rather than
// accumulating into end-to-end drift. Respects -latency-auth-keys: requests
// from non-listed keys stream at full speed, and per-key overrides
// ("key=latencyMs:jitterMs") take precedence over the global config.
func getStreamTotalLatency(authHeader string) time.Duration {
	spec, ok := resolveLatencySpec(latencyAuthKeys, authHeader)
	if !ok {
		return 0
	}
	token := strings.TrimPrefix(authHeader, "Bearer ")
	actual := computeLatencyMs(token, spec)
	if actual <= 0 {
		return 0
	}
	return time.Duration(actual) * time.Millisecond
}

// sleepUntilStreamDeadline sleeps until the wall-clock deadline for the (i+1)-th
// gap of `gaps` total, anchored at `start`. If we're already past the deadline
// (because earlier chunks ran long), it returns immediately.
func sleepUntilStreamDeadline(start time.Time, total time.Duration, i, gaps int) {
	if total <= 0 || gaps <= 0 {
		return
	}
	deadline := start.Add(total * time.Duration(i+1) / time.Duration(gaps))
	if d := time.Until(deadline); d > 0 {
		time.Sleep(d)
	}
}

func writeSSEJSON(w *bufio.Writer, event string, payload any) {
	data, _ := sonic.Marshal(payload)
	if event != "" {
		_, _ = w.WriteString("event: " + event + "\n")
	}
	_, _ = w.WriteString(fmt.Sprintf("data: %s\n\n", string(data)))
	_ = w.Flush()
}

func writeSSEDataLine(w *bufio.Writer, payload string) {
	_, _ = w.WriteString(fmt.Sprintf("data: %s\n\n", payload))
	_ = w.Flush()
}

// shouldTriggerTPM checks if TPM (429) scenario should be triggered for the given auth header value.
func shouldTriggerTPM(authHeader string) bool {
	if tpm <= 0 || startTime.IsZero() {
		return false
	}
	if tpmAuthKeys != "" {
		token := strings.TrimPrefix(authHeader, "Bearer ")
		matched := false
		for _, key := range strings.Split(tpmAuthKeys, ",") {
			if strings.TrimSpace(key) == token {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	elapsedSeconds := int(time.Since(startTime).Seconds())
	if elapsedSeconds < tpm {
		return false
	}
	if tpmDuration > 0 && elapsedSeconds >= tpm+tpmDuration {
		return false
	}
	if !tpmTriggeredLogged {
		log.Printf("TPM (429) scenario triggered after %d seconds", elapsedSeconds)
		tpmTriggeredLogged = true
	}
	return true
}

// sendErrorResponse sends a standardized error response
func sendErrorResponse(ctx *fasthttp.RequestCtx, statusCode int, message string) {
	errorResp := OpenAIError{
		EventID: StrPtr("evt_mock_error_12345"),
		Error: &ErrorField{
			Type:    StrPtr("server_error"),
			Code:    StrPtr("internal_server_error"),
			Message: message,
		},
	}
	ctx.SetContentType("application/json")
	ctx.SetStatusCode(statusCode)
	if err := sonic.ConfigDefault.NewEncoder(ctx).Encode(errorResp); err != nil {
		log.Printf("Error encoding error response: %v", err)
	}
}

// sendRateLimitResponse sends a 429 rate_limit_error response
func sendRateLimitResponse(ctx *fasthttp.RequestCtx) {
	errorResp := OpenAIError{
		EventID: StrPtr("evt_mock_ratelimit_12345"),
		Error: &ErrorField{
			Type:    StrPtr("rate_limit_error"),
			Code:    StrPtr("rate_limit_exceeded"),
			Message: "Rate limit exceeded. Please retry after some time.",
		},
	}
	ctx.SetContentType("application/json")
	ctx.SetStatusCode(fasthttp.StatusTooManyRequests)
	if err := json.NewEncoder(ctx).Encode(errorResp); err != nil {
		log.Printf("Error encoding rate limit response: %v", err)
	}
}

// isKeyRateLimited returns true if the request's Authorization header is in the rate-limited set
func isKeyRateLimited(ctx *fasthttp.RequestCtx) bool {
	if len(rateLimitedKeyMap) == 0 {
		return false
	}
	authHeader := string(ctx.Request.Header.Peek("Authorization"))
	return rateLimitedKeyMap[authHeader]
}

// checkAuth validates authorization header
func checkAuth(ctx *fasthttp.RequestCtx) bool {
	if auth != "" {
		authorizationHeader := string(ctx.Request.Header.Peek("Authorization"))
		if authorizationHeader == "" {
			ctx.SetStatusCode(fasthttp.StatusForbidden)
			ctx.SetBodyString("Forbidden: Missing authentication header 'Authorization'")
			return false
		}
		if authorizationHeader != auth {
			log.Printf("Invalid authentication header 'Authorization': %s", authorizationHeader)
			ctx.SetStatusCode(fasthttp.StatusForbidden)
			ctx.SetBodyString("Forbidden: Invalid authentication header 'Authorization'")
			return false
		}
	}
	return true
}

// checkMethod validates HTTP method is POST
func checkMethod(ctx *fasthttp.RequestCtx) bool {
	if !ctx.IsPost() {
		ctx.SetStatusCode(fasthttp.StatusMethodNotAllowed)
		ctx.SetBodyString("Only POST method is allowed")
		return false
	}
	return true
}

// sendStreamingResponse sends a streaming chat completion response in SSE format
func sendOpenAIStreamingResponse(ctx *fasthttp.RequestCtx, model string, mockContent string) {
	setSSEHeaders(ctx)
	tokens := buildStreamChunks(getStreamWords(mockContent))
	gaps := len(tokens) - 1
	totalLatency := getStreamTotalLatency(string(ctx.Request.Header.Peek("Authorization")))

	ctx.SetBodyStreamWriter(func(w *bufio.Writer) {
		start := time.Now()
		for i, token := range tokens {
			role := (*string)(nil)
			if i == 0 {
				role = StrPtr("assistant")
			}
			chunk := ChatCompletionStreamResponse{
				ID:      "cmpl-mock12345",
				Object:  "chat.completion.chunk",
				Created: int(time.Now().Unix()),
				Model:   model,
				Choices: []ChatStreamResponseChoice{
					{
						Index: 0,
						Delta: &ChatStreamResponseChoiceDelta{
							Role:    role,
							Content: StrPtr(token),
						},
						FinishReason: nil,
					},
				},
			}
			writeSSEJSON(w, "", chunk)
			if i < gaps {
				sleepUntilStreamDeadline(start, totalLatency, i, gaps)
			}
		}

		finalChunk := ChatCompletionStreamResponse{
			ID:      "cmpl-mock12345",
			Object:  "chat.completion.chunk",
			Created: int(time.Now().Unix()),
			Model:   model,
			Choices: []ChatStreamResponseChoice{
				{
					Index:        0,
					Delta:        &ChatStreamResponseChoiceDelta{},
					FinishReason: StrPtr("stop"),
				},
			},
		}
		writeSSEJSON(w, "", finalChunk)
		writeSSEDataLine(w, "[DONE]")
	})
}

func sendAnthropicStreamingResponse(ctx *fasthttp.RequestCtx, model string, mockContent string) {
	setSSEHeaders(ctx)
	words := getStreamWords(mockContent)
	tokens := buildStreamChunks(words)
	gaps := len(tokens) - 1
	totalLatency := getStreamTotalLatency(string(ctx.Request.Header.Peek("Authorization")))

	ctx.SetBodyStreamWriter(func(w *bufio.Writer) {
		startMsg := map[string]any{
			"type": "message_start",
			"message": AnthropicStreamMessage{
				ID:           "msg_mock12345",
				Type:         "message",
				Role:         "assistant",
				Model:        model,
				Content:      []any{},
				StopReason:   nil,
				StopSequence: nil,
			},
		}
		writeSSEJSON(w, "message_start", startMsg)
		writeSSEJSON(w, "content_block_start", map[string]any{
			"type":          "content_block_start",
			"index":         0,
			"content_block": AnthropicContentBlock{Type: "text", Text: ""},
		})

		start := time.Now()
		for i, token := range tokens {
			writeSSEJSON(w, "content_block_delta", map[string]any{
				"type":  "content_block_delta",
				"index": 0,
				"delta": AnthropicTextDelta{
					Type: "text_delta",
					Text: token,
				},
			})
			if i < gaps {
				sleepUntilStreamDeadline(start, totalLatency, i, gaps)
			}
		}

		writeSSEJSON(w, "content_block_stop", map[string]any{
			"type":  "content_block_stop",
			"index": 0,
		})
		writeSSEJSON(w, "message_delta", map[string]any{
			"type": "message_delta",
			"delta": map[string]any{
				"stop_reason":   "end_turn",
				"stop_sequence": nil,
			},
			"usage": map[string]any{
				"output_tokens": resolveOutputTokens(len(words)),
			},
		})
		writeSSEJSON(w, "message_stop", map[string]any{
			"type": "message_stop",
		})
		writeSSEDataLine(w, "[DONE]")
	})
}

func sendGenAIStreamingResponse(ctx *fasthttp.RequestCtx, model string, mockContent string) {
	setSSEHeaders(ctx)
	tokens := buildStreamChunks(getStreamWords(mockContent))
	gaps := len(tokens) - 1
	totalLatency := getStreamTotalLatency(string(ctx.Request.Header.Peek("Authorization")))

	ctx.SetBodyStreamWriter(func(w *bufio.Writer) {
		start := time.Now()
		for i, token := range tokens {
			chunk := map[string]any{
				"candidates": []map[string]any{
					{
						"content": map[string]any{
							"parts": []map[string]any{{"text": token}},
							"role":  "model",
						},
						"index":        0,
						"finishReason": "",
					},
				},
				"modelVersion": model,
			}
			writeSSEJSON(w, "", chunk)
			if i < gaps {
				sleepUntilStreamDeadline(start, totalLatency, i, gaps)
			}
		}

		finalChunk := map[string]any{
			"candidates": []map[string]any{
				{
					"content": map[string]any{
						"parts": []map[string]any{},
						"role":  "model",
					},
					"index":        0,
					"finishReason": "STOP",
				},
			},
			"modelVersion": model,
		}
		writeSSEJSON(w, "", finalChunk)
		writeSSEDataLine(w, "[DONE]")
	})
}

func sendBedrockConverseStreamingResponse(ctx *fasthttp.RequestCtx, model string, mockContent string) {
	setSSEHeaders(ctx)
	words := getStreamWords(mockContent)
	tokens := buildStreamChunks(words)
	gaps := len(tokens) - 1
	totalLatency := getStreamTotalLatency(string(ctx.Request.Header.Peek("Authorization")))

	ctx.SetBodyStreamWriter(func(w *bufio.Writer) {
		writeSSEJSON(w, "", map[string]any{
			"messageStart": map[string]any{
				"role":  "assistant",
				"model": model,
			},
		})

		start := time.Now()
		for i, token := range tokens {
			writeSSEJSON(w, "", map[string]any{
				"contentBlockDelta": map[string]any{
					"contentBlockIndex": 0,
					"delta": map[string]any{
						"text": token,
					},
				},
			})
			if i < gaps {
				sleepUntilStreamDeadline(start, totalLatency, i, gaps)
			}
		}

		writeSSEJSON(w, "", map[string]any{
			"contentBlockStop": map[string]any{
				"contentBlockIndex": 0,
			},
		})
		writeSSEJSON(w, "", map[string]any{
			"messageStop": map[string]any{
				"stopReason": "end_turn",
			},
		})
		streamInputTokens := resolveInputTokens(rand.Intn(1000))
		streamOutputTokens := resolveOutputTokens(len(words))
		writeSSEJSON(w, "", map[string]any{
			"metadata": map[string]any{
				"usage": map[string]any{
					"inputTokens":  streamInputTokens,
					"outputTokens": streamOutputTokens,
					"totalTokens":  streamInputTokens + streamOutputTokens,
				},
			},
		})
		writeSSEDataLine(w, "[DONE]")
	})
}

func mockChatCompletionsHandler(ctx *fasthttp.RequestCtx) {
	if !checkAuth(ctx) || !checkMethod(ctx) {
		return
	}
	provider, model, stream := parseModelFromRequest(ctx)

	if isKeyRateLimited(ctx) || shouldTriggerTPM(string(ctx.Request.Header.Peek("Authorization"))) {
		sendRateLimitResponse(ctx)
		return
	}
	if maybeSendRandomProviderError(ctx, provider) {
		return
	}

	if shouldFail(string(ctx.Request.Header.Peek("Authorization"))) {
		sendErrorResponse(ctx, fasthttp.StatusInternalServerError, "The server had an error while processing your request. Sorry about that!")
		return
	}
	if provider != "" {
		log.Printf("[chat/completions] provider=%s model=%s stream=%v", provider, model, stream)
	} else {
		log.Printf("[chat/completions] model=%s stream=%v", model, stream)
	}

	mockContent := "This is a mocked response from the OpenAI mocker server."
	if bigPayload {
		mockContent = strings.Repeat(mockContent, 182)
	}

	// Check if streaming is requested
	if stream {
		if provider == "anthropic" {
			sendAnthropicStreamingResponse(ctx, model, mockContent)
		} else {
			sendOpenAIStreamingResponse(ctx, model, mockContent)
		}
		return
	}

	// Non-streaming requests get the full latency upfront
	simulateLatency(string(ctx.Request.Header.Peek("Authorization")))

	// Non-streaming response
	mockChoiceMessage := schemas.BifrostResponseChoiceMessage{
		Role:    schemas.ModelChatMessageRole("assistant"),
		Content: StrPtr(mockContent),
	}
	mockChoice := schemas.BifrostResponseChoice{
		Index:        0,
		Message:      mockChoiceMessage,
		FinishReason: StrPtr("stop"),
	}

	randomInputTokens := resolveInputTokens(rand.Intn(1000))
	randomOutputTokens := resolveOutputTokens(rand.Intn(1000))

	mockResp := OpenAIChatCompletionsResponse{
		ID:      "cmpl-mock12345",
		Object:  "chat.completion",
		Created: int(time.Now().Unix()),
		Model:   model,
		Choices: []schemas.BifrostResponseChoice{mockChoice},
		Usage: schemas.LLMUsage{
			PromptTokens:     randomInputTokens,
			CompletionTokens: randomOutputTokens,
			TotalTokens:      randomInputTokens + randomOutputTokens,
		},
	}

	ctx.SetContentType("application/json")
	ctx.SetStatusCode(fasthttp.StatusOK)
	if err := sonic.ConfigDefault.NewEncoder(ctx).Encode(mockResp); err != nil {
		log.Printf("Error encoding mock response: %v", err)
		ctx.SetStatusCode(fasthttp.StatusInternalServerError)
		ctx.SetBodyString("Failed to encode response")
	}
}

func mockResponsesHandler(ctx *fasthttp.RequestCtx) {
	if !checkAuth(ctx) || !checkMethod(ctx) {
		return
	}
	provider, model, _ := parseModelFromRequest(ctx)

	if isKeyRateLimited(ctx) || shouldTriggerTPM(string(ctx.Request.Header.Peek("Authorization"))) {
		sendRateLimitResponse(ctx)
		return
	}
	if maybeSendRandomProviderError(ctx, provider) {
		return
	}

	if shouldFail(string(ctx.Request.Header.Peek("Authorization"))) {
		sendErrorResponse(ctx, fasthttp.StatusInternalServerError, "The server had an error while processing your request. Sorry about that!")
		return
	}

	if provider != "" {
		log.Printf("[responses] provider=%s model=%s", provider, model)
	} else {
		log.Printf("[responses] model=%s", model)
	}

	simulateLatency(string(ctx.Request.Header.Peek("Authorization")))

	mockContent := "This is a mocked response from the OpenAI mocker server."
	if bigPayload {
		mockContent = strings.Repeat(mockContent, 182)
	}

	randomInputTokens := resolveInputTokens(rand.Intn(1000))
	randomOutputTokens := resolveOutputTokens(rand.Intn(1000))

	resp := OpenAIResponsesResponse{
		ID:      "resp-mock12345",
		Object:  "response",
		Created: int(time.Now().Unix()),
		Model:   model,
		Output: []OpenAIResponsesOutputItem{
			{
				ID:   "msg_mock12345",
				Type: "message",
				Role: "assistant",
				Content: []OpenAIResponsesMessageContent{
					{
						Type: "output_text",
						Text: mockContent,
					},
				},
			},
		},
		Status: "completed",
		Usage: schemas.LLMUsage{
			PromptTokens:     randomInputTokens,
			CompletionTokens: randomOutputTokens,
			TotalTokens:      randomInputTokens + randomOutputTokens,
		},
	}

	ctx.SetContentType("application/json")
	ctx.SetStatusCode(fasthttp.StatusOK)
	if err := sonic.ConfigDefault.NewEncoder(ctx).Encode(resp); err != nil {
		log.Printf("Error encoding mock response: %v", err)
		ctx.SetStatusCode(fasthttp.StatusInternalServerError)
		ctx.SetBodyString("Failed to encode response")
	}
}

func mockEmbeddingsHandler(ctx *fasthttp.RequestCtx) {
	if !checkAuth(ctx) || !checkMethod(ctx) {
		return
	}
	provider, model, _ := parseModelFromRequest(ctx)

	if isKeyRateLimited(ctx) || shouldTriggerTPM(string(ctx.Request.Header.Peek("Authorization"))) {
		sendRateLimitResponse(ctx)
		return
	}
	if maybeSendRandomProviderError(ctx, provider) {
		return
	}

	if shouldFail(string(ctx.Request.Header.Peek("Authorization"))) {
		sendErrorResponse(ctx, fasthttp.StatusInternalServerError, "The server had an error while processing your request. Sorry about that!")
		return
	}

	if model == "gpt-4o-mini" {
		// Default for embeddings if no model specified
		model = "text-embedding-ada-002"
	}
	if provider != "" {
		log.Printf("[embeddings] provider=%s model=%s", provider, model)
	} else {
		log.Printf("[embeddings] model=%s", model)
	}

	simulateLatency(string(ctx.Request.Header.Peek("Authorization")))

	embeddingDimensions := 1536
	if bigPayload {
		embeddingDimensions = 4096
	}

	embedding := make([]float64, embeddingDimensions)
	for i := range embedding {
		embedding[i] = rand.Float64()*2 - 1
	}

	numInputs := 1
	embeddingData := make([]OpenAIEmbeddingData, numInputs)
	for i := 0; i < numInputs; i++ {
		embeddingData[i] = OpenAIEmbeddingData{
			Object:    "embedding",
			Embedding: embedding,
			Index:     i,
		}
	}

	randomPromptTokens := resolveInputTokens(rand.Intn(100) + 1)

	resp := OpenAIEmbeddingsResponse{
		Object: "list",
		Data:   embeddingData,
		Model:  model,
		Usage: schemas.LLMUsage{
			PromptTokens: randomPromptTokens,
			TotalTokens:  randomPromptTokens,
		},
	}

	ctx.SetContentType("application/json")
	ctx.SetStatusCode(fasthttp.StatusOK)
	if err := sonic.ConfigDefault.NewEncoder(ctx).Encode(resp); err != nil {
		log.Printf("Error encoding embeddings response: %v", err)
		ctx.SetStatusCode(fasthttp.StatusInternalServerError)
		ctx.SetBodyString("Failed to encode response")
	}
}

func mockAnthropicMessagesHandler(ctx *fasthttp.RequestCtx) {
	if !checkAuth(ctx) || !checkMethod(ctx) {
		return
	}
	provider, model, stream := parseAnthropicModelFromRequest(ctx)

	if shouldTriggerTPM(string(ctx.Request.Header.Peek("Authorization"))) {
		sendErrorResponse(ctx, fasthttp.StatusTooManyRequests, "Rate limit exceeded. Please retry after some time.")
		return
	}
	if maybeSendRandomProviderError(ctx, "anthropic") {
		return
	}

	if shouldFail(string(ctx.Request.Header.Peek("Authorization"))) {
		sendErrorResponse(ctx, fasthttp.StatusInternalServerError, "The server had an error while processing your request. Sorry about that!")
		return
	}

	if provider != "" {
		log.Printf("[anthropic/messages] provider=%s model=%s stream=%v", provider, model, stream)
	} else {
		log.Printf("[anthropic/messages] model=%s stream=%v", model, stream)
	}

	mockContent := "This is a mocked response from the Bifrost mocker server."
	if bigPayload {
		mockContent = strings.Repeat(mockContent, 182)
	}

	if stream {
		sendAnthropicStreamingResponse(ctx, model, mockContent)
		return
	}

	simulateLatency(string(ctx.Request.Header.Peek("Authorization")))

	randomInputTokens := resolveInputTokens(rand.Intn(1000))
	randomOutputTokens := resolveOutputTokens(rand.Intn(1000))

	resp := AnthropicMessageResponse{
		ID:           "msg_mock12345",
		Type:         "message",
		Role:         "assistant",
		Model:        model,
		Content:      []AnthropicMessageContent{{Type: "text", Text: mockContent}},
		StopReason:   "end_turn",
		StopSequence: nil,
		Usage: AnthropicMessageUsage{
			InputTokens:  randomInputTokens,
			OutputTokens: randomOutputTokens,
		},
	}

	ctx.SetContentType("application/json")
	ctx.SetStatusCode(fasthttp.StatusOK)
	if err := sonic.ConfigDefault.NewEncoder(ctx).Encode(resp); err != nil {
		log.Printf("Error encoding anthropic response: %v", err)
		ctx.SetStatusCode(fasthttp.StatusInternalServerError)
		ctx.SetBodyString("Failed to encode response")
	}
}

func mockGenAIGenerateContentHandler(ctx *fasthttp.RequestCtx) {
	if !checkAuth(ctx) || !checkMethod(ctx) {
		return
	}
	provider, model := parseGenAIModelFromPath(string(ctx.Path()))
	isStreamPath := strings.Contains(string(ctx.Path()), ":streamGenerateContent")

	if shouldTriggerTPM(string(ctx.Request.Header.Peek("Authorization"))) {
		sendErrorResponse(ctx, fasthttp.StatusTooManyRequests, "Rate limit exceeded. Please retry after some time.")
		return
	}
	if maybeSendRandomProviderError(ctx, provider) {
		return
	}

	if shouldFail(string(ctx.Request.Header.Peek("Authorization"))) {
		sendErrorResponse(ctx, fasthttp.StatusInternalServerError, "The server had an error while processing your request. Sorry about that!")
		return
	}

	if provider != "" {
		log.Printf("[genai/generateContent] provider=%s model=%s stream=%v", provider, model, isStreamPath)
	} else {
		log.Printf("[genai/generateContent] model=%s stream=%v", model, isStreamPath)
	}

	mockContent := "This is a mocked response from the Bifrost mocker server."
	if bigPayload {
		mockContent = strings.Repeat(mockContent, 182)
	}

	if isStreamPath {
		sendGenAIStreamingResponse(ctx, model, mockContent)
		return
	}

	simulateLatency(string(ctx.Request.Header.Peek("Authorization")))

	randomInputTokens := resolveInputTokens(rand.Intn(1000))
	randomOutputTokens := resolveOutputTokens(rand.Intn(1000))

	resp := GenAIResponse{
		Candidates: []GenAICandidate{
			{
				Content: GenAIContent{
					Role:  "model",
					Parts: []GenAIPart{{Text: mockContent}},
				},
				FinishReason: "STOP",
				Index:        0,
			},
		},
		UsageMetadata: GenAIUsageMetadata{
			PromptTokenCount:     randomInputTokens,
			CandidatesTokenCount: randomOutputTokens,
			TotalTokenCount:      randomInputTokens + randomOutputTokens,
		},
		ModelVersion: model,
	}

	ctx.SetContentType("application/json")
	ctx.SetStatusCode(fasthttp.StatusOK)
	if err := sonic.ConfigDefault.NewEncoder(ctx).Encode(resp); err != nil {
		log.Printf("Error encoding genai response: %v", err)
		ctx.SetStatusCode(fasthttp.StatusInternalServerError)
		ctx.SetBodyString("Failed to encode response")
	}
}

func mockBedrockConverseHandler(ctx *fasthttp.RequestCtx) {
	if !checkAuth(ctx) || !checkMethod(ctx) {
		return
	}
	model, isConverse, isStream := parseBedrockModelFromPath(string(ctx.Path()))

	if shouldTriggerTPM(string(ctx.Request.Header.Peek("Authorization"))) {
		sendErrorResponse(ctx, fasthttp.StatusTooManyRequests, "Rate limit exceeded. Please retry after some time.")
		return
	}
	if maybeSendRandomProviderError(ctx, "bedrock") {
		return
	}
	if shouldFail(string(ctx.Request.Header.Peek("Authorization"))) {
		sendErrorResponse(ctx, fasthttp.StatusInternalServerError, "The server had an error while processing your request. Sorry about that!")
		return
	}
	if !isConverse {
		ctx.SetStatusCode(fasthttp.StatusNotFound)
		ctx.SetBodyString("Not found")
		return
	}
	if model == "" {
		model = "amazon.nova-micro-v1:0"
	}

	log.Printf("[bedrock/converse] model=%s stream=%v", model, isStream)
	mockContent := "This is a mocked response from the Bifrost mocker server."
	if bigPayload {
		mockContent = strings.Repeat(mockContent, 182)
	}
	if isStream {
		sendBedrockConverseStreamingResponse(ctx, model, mockContent)
		return
	}

	simulateLatency(string(ctx.Request.Header.Peek("Authorization")))
	randomInputTokens := resolveInputTokens(rand.Intn(1000))
	randomOutputTokens := resolveOutputTokens(rand.Intn(1000))
	resp := BedrockConverseResponse{
		Output: BedrockConverseOutput{
			Message: BedrockMessage{
				Role:    "assistant",
				Content: []BedrockContent{{Text: mockContent}},
			},
		},
		StopReason: "end_turn",
		Usage: BedrockUsage{
			InputTokens:  randomInputTokens,
			OutputTokens: randomOutputTokens,
			TotalTokens:  randomInputTokens + randomOutputTokens,
		},
		Metrics: BedrockMetrics{LatencyMs: latency},
	}
	ctx.SetContentType("application/json")
	ctx.SetStatusCode(fasthttp.StatusOK)
	if err := sonic.ConfigDefault.NewEncoder(ctx).Encode(resp); err != nil {
		log.Printf("Error encoding bedrock response: %v", err)
		ctx.SetStatusCode(fasthttp.StatusInternalServerError)
		ctx.SetBodyString("Failed to encode response")
	}
}

func mockModelsHandler(ctx *fasthttp.RequestCtx) {
	if !checkAuth(ctx) {
		return
	}

	if string(ctx.Method()) != "GET" {
		sendErrorResponse(ctx, fasthttp.StatusMethodNotAllowed, "Only GET method is allowed")
		return
	}

	now := int(time.Now().Unix())
	models := []OpenAIModel{
		{ID: "gpt-4o", Object: "model", Created: now, OwnedBy: "openai"},
		{ID: "gpt-4o-mini", Object: "model", Created: now, OwnedBy: "openai"},
		{ID: "gpt-4", Object: "model", Created: now, OwnedBy: "openai"},
		{ID: "gpt-3.5-turbo", Object: "model", Created: now, OwnedBy: "openai"},
		{ID: "text-embedding-ada-002", Object: "model", Created: now, OwnedBy: "openai"},
		{ID: "text-embedding-3-small", Object: "model", Created: now, OwnedBy: "openai"},
		{ID: "text-embedding-3-large", Object: "model", Created: now, OwnedBy: "openai"},
	}

	resp := OpenAIModelsResponse{
		Object: "list",
		Data:   models,
	}

	ctx.SetContentType("application/json")
	ctx.SetStatusCode(fasthttp.StatusOK)
	if err := json.NewEncoder(ctx).Encode(resp); err != nil {
		log.Printf("Error encoding models response: %v", err)
		ctx.SetStatusCode(fasthttp.StatusInternalServerError)
		ctx.SetBodyString("Failed to encode response")
	}
}

func healthCheckHandler(ctx *fasthttp.RequestCtx) {
	ctx.SetContentType("application/json")
	ctx.SetStatusCode(fasthttp.StatusOK)
	ctx.SetBodyString(`{"status":"healthy"}`)
}

type OpenAIModelEntry struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int    `json:"created"`
	OwnedBy string `json:"owned_by"`
}

type OpenAIModelsListResponse struct {
	Object string             `json:"object"`
	Data   []OpenAIModelEntry `json:"data"`
}

// mockListModelsHandler serves GET /v1/models with the ids configured via
// -models. It validates auth but deliberately skips latency, failure, and TPM
// simulation: those flags shape inference behavior, while model discovery
// should stay deterministic so gateway-side model catalogs can always populate.
func mockListModelsHandler(ctx *fasthttp.RequestCtx) {
	if !checkAuth(ctx) {
		return
	}
	if !ctx.IsGet() {
		ctx.SetStatusCode(fasthttp.StatusMethodNotAllowed)
		ctx.SetBodyString("Only GET method is allowed")
		return
	}

	ids := strings.Split(modelsList, ",")
	data := make([]OpenAIModelEntry, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		data = append(data, OpenAIModelEntry{
			ID:      id,
			Object:  "model",
			Created: int(startTime.Unix()),
			OwnedBy: "mocker",
		})
	}

	log.Printf("[models] returning %d model(s)", len(data))
	resp := OpenAIModelsListResponse{Object: "list", Data: data}
	ctx.SetContentType("application/json")
	ctx.SetStatusCode(fasthttp.StatusOK)
	if err := sonic.ConfigDefault.NewEncoder(ctx).Encode(resp); err != nil {
		log.Printf("Error encoding models response: %v", err)
		ctx.SetStatusCode(fasthttp.StatusInternalServerError)
		ctx.SetBodyString("Failed to encode response")
	}
}

// logRawRequest prints the raw request line, all headers, and body to stdout (only if logRaw is enabled).
func logRawRequest(ctx *fasthttp.RequestCtx) {
	if !logRaw {
		return
	}
	req := &ctx.Request
	// Request line: METHOD URI HTTP/VERSION
	log.Printf("--- Raw Request ---\n%s %s %s", req.Header.Method(), req.URI().String(), req.Header.Protocol())
	// Headers
	req.Header.VisitAll(func(key, value []byte) {
		log.Printf("%s: %s", key, value)
	})
	// Body
	body := req.Body()
	if len(body) > 0 {
		log.Printf("--- Body ---\n%s", body)
	} else {
		log.Printf("--- Body --- (empty)")
	}
	log.Printf("--- End Request ---")
}

// logRawResponse prints the raw response status, headers, and body to stdout (only if logRaw is enabled).
func logRawResponse(ctx *fasthttp.RequestCtx) {
	if !logRaw {
		return
	}
	resp := &ctx.Response
	// Response line: HTTP/VERSION STATUS_CODE STATUS_MESSAGE
	log.Printf("--- Raw Response ---\nHTTP/1.1 %d %s", resp.StatusCode(), fasthttp.StatusMessage(resp.StatusCode()))
	// Headers
	resp.Header.VisitAll(func(key, value []byte) {
		log.Printf("%s: %s", key, value)
	})
	// Body
	body := resp.Body()
	if len(body) > 0 {
		log.Printf("--- Body ---\n%s", body)
	} else {
		log.Printf("--- Body --- (empty)")
	}
	log.Printf("--- End Response ---")
}

// router handles routing requests to appropriate handlers
func router(ctx *fasthttp.RequestCtx) {
	logRawRequest(ctx)
	path := string(ctx.Path())

	switch path {
	case "/health":
		healthCheckHandler(ctx)
	case "/models", "/openai/models", "/openai/v1/models":
		mockListModelsHandler(ctx)
	case "/chat/completions", "/v1/chat/completions", "/openai/chat/completions", "/openai/v1/chat/completions":
		mockChatCompletionsHandler(ctx)
	case "/responses", "/v1/responses", "/openai/responses", "/openai/v1/responses":
		mockResponsesHandler(ctx)
	case "/embeddings", "/v1/embeddings", "/openai/embeddings", "/openai/v1/embeddings":
		mockEmbeddingsHandler(ctx)
	case "/anthropic/v1/messages", "/anthropic/messages", "/v1/messages":
		mockAnthropicMessagesHandler(ctx)
	case "/v1/models":
		mockModelsHandler(ctx)
	default:
		if _, isConverse, _ := parseBedrockModelFromPath(path); isConverse {
			mockBedrockConverseHandler(ctx)
			return
		}
		if (strings.HasPrefix(path, "/models/") ||
			strings.HasPrefix(path, "/v1beta/models/") ||
			strings.HasPrefix(path, "/v1/models/") ||
			strings.HasPrefix(path, "/genai/v1beta/models/") ||
			strings.HasPrefix(path, "/genai/v1/models/")) &&
			(strings.Contains(path, ":generateContent") || strings.Contains(path, ":streamGenerateContent")) {
			mockGenAIGenerateContentHandler(ctx)
			return
		}
		ctx.SetStatusCode(fasthttp.StatusNotFound)
		ctx.SetBodyString("Not found")
	}
}

func main() {
	flag.Parse()

	startTime = time.Now()

	rateLimitedKeyMap = make(map[string]bool)
	if rateLimitedKeys != "" {
		for _, k := range strings.Split(rateLimitedKeys, ",") {
			if k = strings.TrimSpace(k); k != "" {
				rateLimitedKeyMap[k] = true
			}
		}
		log.Printf("Per-key rate limiting enabled for %d key(s)", len(rateLimitedKeyMap))
	}

	// Parse dynamic per-key latency behaviors.
	parseKVList(latencySpikeKeys, func(token string, pct int, b string) {
		mult := 5.0
		if b != "" {
			if m, err := strconv.ParseFloat(b, 64); err == nil {
				mult = m
			}
		}
		spikeMap[token] = spikeSpec{pct: pct, mult: mult}
		log.Printf("Latency spikes for %q: %d%% of requests x%.1f", token, pct, mult)
	})
	parseKVList(latencyRampKeys, func(token string, perMin int, _ string) {
		rampMap[token] = perMin
		log.Printf("Latency ramp for %q: +%dms per minute", token, perMin)
	})
	parseKVList(latencyStepKeys, func(token string, atSec int, b string) {
		toMs, _ := strconv.Atoi(b)
		stepMap[token] = stepSpec{atSec: atSec, toMs: toMs}
		log.Printf("Latency step for %q: at %ds base -> %dms", token, atSec, toMs)
	})

	addr := fmt.Sprintf("%s:%d", host, port)
	if jitter > 0 {
		log.Printf("Mock LLM server (fasthttp) starting on %s with latency %dms ±%dms jitter...\n", addr, latency, jitter)
	} else {
		log.Printf("Mock LLM server (fasthttp) starting on %s with latency %dms...\n", addr, latency)
	}
	if latencyAuthKeys != "" {
		log.Printf("Latency will only apply to requests with auth keys: %s", latencyAuthKeys)
	}
	if failureAuthKeys != "" {
		log.Printf("Failure simulation will only apply to requests with auth keys: %s", failureAuthKeys)
	}
	if fixedInputTokens >= 0 {
		log.Printf("Reporting a fixed input token count of %d in usage", fixedInputTokens)
	}
	if fixedOutputTokens >= 0 {
		log.Printf("Reporting a fixed output token count of %d in usage", fixedOutputTokens)
	}
	if tpm > 0 {
		if tpmDuration > 0 {
			log.Printf("TPM (429) scenario will be triggered between %d and %d seconds", tpm, tpm+tpmDuration)
		} else {
			log.Printf("TPM (429) scenario will be triggered after %d seconds", tpm)
		}
		if tpmAuthKeys != "" {
			log.Printf("TPM will only apply to requests with auth keys: %s", tpmAuthKeys)
		}
	}
	log.Printf("Max request body size: 50MB")

	// Create fasthttp server with 50MB max request body size
	server := &fasthttp.Server{
		Handler:            router,
		MaxRequestBodySize: 50 * 1024 * 1024, // 50MB
		ReadBufferSize:     1024 * 16,        // 16KB read buffer
		ReadTimeout:        300 * time.Second,
		WriteTimeout:       300 * time.Second,
		IdleTimeout:        60 * time.Second,
	}

	if err := server.ListenAndServe(addr); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
