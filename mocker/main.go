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
	if err := json.Unmarshal(ctx.Request.Body(), &req); err != nil {
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
	bigPayload         bool
	auth               string
	withErrors         bool
	failurePercent     int
	failureJitter      int
	tpm                int
	logRaw             bool
	startTime          time.Time
	tpmTriggeredLogged bool
)

func init() {
	flag.StringVar(&host, "host", getEnvString("MOCKER_HOST", "localhost"), "Host address to bind the mock server")
	flag.IntVar(&port, "port", getEnvInt("MOCKER_PORT", 8000), "Port for the mock server to listen on")
	flag.IntVar(&latency, "latency", getEnvInt("MOCKER_LATENCY", 0), "Latency in milliseconds to simulate")
	flag.IntVar(&jitter, "jitter", getEnvInt("MOCKER_JITTER", 0), "Maximum jitter in milliseconds to add to latency (±jitter)")
	flag.BoolVar(&bigPayload, "big-payload", getEnvBool("MOCKER_BIG_PAYLOAD", false), "Use big payload")
	flag.StringVar(&auth, "auth", getEnvString("MOCKER_AUTH", ""), "Add authentication header key")
	flag.BoolVar(&withErrors, "with-errors", getEnvBool("MOCKER_WITH_ERRORS", false), "Enable provider-specific random error responses")
	flag.BoolVar(&withErrors, "witherrors", getEnvBool("MOCKER_WITH_ERRORS", false), "Alias of -with-errors")
	flag.IntVar(&failurePercent, "failure-percent", getEnvInt("MOCKER_FAILURE_PERCENT", 0), "Base failure percentage (0-100)")
	flag.IntVar(&failureJitter, "failure-jitter", getEnvInt("MOCKER_FAILURE_JITTER", 0), "Maximum jitter in percentage points to add to failure rate (±failure-jitter)")
	flag.IntVar(&tpm, "tpm", getEnvInt("MOCKER_TPM", 0), "Seconds after which to trigger TPM (429) scenarios (0 = disabled)")
	flag.BoolVar(&logRaw, "log-raw", getEnvBool("MOCKER_LOG_RAW", false), "Log raw request and response bodies")
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

// simulateLatency handles latency simulation with optional jitter
func simulateLatency() {
	if latency > 0 || jitter > 0 {
		actualLatency := latency
		if jitter > 0 {
			jitterOffset := rand.Intn(2*jitter+1) - jitter
			actualLatency += jitterOffset
			if actualLatency < 0 {
				actualLatency = 0
			}
		}
		if actualLatency > 0 {
			time.Sleep(time.Duration(actualLatency) * time.Millisecond)
		}
	}
}

// shouldFail checks if request should fail based on failure percentage with jitter
func shouldFail() bool {
	if withErrors {
		// In with-errors mode, use provider-specific random errors only.
		return false
	}
	if failurePercent > 0 {
		actualFailurePercent := failurePercent
		if failureJitter > 0 {
			jitterOffset := rand.Intn(2*failureJitter+1) - failureJitter
			actualFailurePercent += jitterOffset
			if actualFailurePercent < 0 {
				actualFailurePercent = 0
			}
			if actualFailurePercent > 100 {
				actualFailurePercent = 100
			}
		}
		return actualFailurePercent > 0 && rand.Intn(100) < actualFailurePercent
	}
	return false
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
	if err := json.NewEncoder(ctx).Encode(chosen.Body); err != nil {
		log.Printf("Error encoding provider error response: %v", err)
		ctx.SetStatusCode(fasthttp.StatusInternalServerError)
		ctx.SetBodyString("Failed to encode error response")
	}
	return true
}

func parseAnthropicModelFromRequest(ctx *fasthttp.RequestCtx) (provider string, model string, stream bool) {
	var req AnthropicRequest
	if err := json.Unmarshal(ctx.Request.Body(), &req); err != nil {
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

func getPerWordLatency(wordsCount int) time.Duration {
	if wordsCount <= 1 {
		return 0
	}

	actualLatency := latency
	if jitter > 0 {
		jitterOffset := rand.Intn(2*jitter+1) - jitter
		actualLatency += jitterOffset
		if actualLatency < 0 {
			actualLatency = 0
		}
	}
	if actualLatency <= 0 {
		return 0
	}
	return time.Duration(actualLatency/(wordsCount-1)) * time.Millisecond
}

func writeSSEJSON(w *bufio.Writer, event string, payload any) {
	data, _ := json.Marshal(payload)
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

// shouldTriggerTPM checks if TPM (429) scenario should be triggered
func shouldTriggerTPM() bool {
	if tpm > 0 && !startTime.IsZero() {
		elapsedSeconds := int(time.Since(startTime).Seconds())
		if elapsedSeconds >= tpm {
			if !tpmTriggeredLogged {
				log.Printf("TPM (429) scenario triggered after %d seconds", elapsedSeconds)
				tpmTriggeredLogged = true
			}
			return true
		}
	}
	return false
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
	if err := json.NewEncoder(ctx).Encode(errorResp); err != nil {
		log.Printf("Error encoding error response: %v", err)
	}
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
	words := getStreamWords(mockContent)
	perWordLatency := getPerWordLatency(len(words))

	ctx.SetBodyStreamWriter(func(w *bufio.Writer) {
		for i, word := range words {
			token := word
			if i < len(words)-1 {
				token += " "
			}
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
			if perWordLatency > 0 && i < len(words)-1 {
				time.Sleep(perWordLatency)
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
	perWordLatency := getPerWordLatency(len(words))

	ctx.SetBodyStreamWriter(func(w *bufio.Writer) {
		start := map[string]any{
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
		writeSSEJSON(w, "message_start", start)
		writeSSEJSON(w, "content_block_start", map[string]any{
			"type":          "content_block_start",
			"index":         0,
			"content_block": AnthropicContentBlock{Type: "text", Text: ""},
		})

		for i, word := range words {
			token := word
			if i < len(words)-1 {
				token += " "
			}
			writeSSEJSON(w, "content_block_delta", map[string]any{
				"type":  "content_block_delta",
				"index": 0,
				"delta": AnthropicTextDelta{
					Type: "text_delta",
					Text: token,
				},
			})
			if perWordLatency > 0 && i < len(words)-1 {
				time.Sleep(perWordLatency)
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
				"output_tokens": len(words),
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
	words := getStreamWords(mockContent)
	perWordLatency := getPerWordLatency(len(words))

	ctx.SetBodyStreamWriter(func(w *bufio.Writer) {
		for i, word := range words {
			token := word
			if i < len(words)-1 {
				token += " "
			}
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
			if perWordLatency > 0 && i < len(words)-1 {
				time.Sleep(perWordLatency)
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
	perWordLatency := getPerWordLatency(len(words))

	ctx.SetBodyStreamWriter(func(w *bufio.Writer) {
		writeSSEJSON(w, "", map[string]any{
			"messageStart": map[string]any{
				"role":  "assistant",
				"model": model,
			},
		})

		for i, word := range words {
			token := word
			if i < len(words)-1 {
				token += " "
			}
			writeSSEJSON(w, "", map[string]any{
				"contentBlockDelta": map[string]any{
					"contentBlockIndex": 0,
					"delta": map[string]any{
						"text": token,
					},
				},
			})
			if perWordLatency > 0 && i < len(words)-1 {
				time.Sleep(perWordLatency)
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
		writeSSEJSON(w, "", map[string]any{
			"metadata": map[string]any{
				"usage": map[string]any{
					"inputTokens":  rand.Intn(1000),
					"outputTokens": len(words),
					"totalTokens":  rand.Intn(1000) + len(words),
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

	if shouldTriggerTPM() {
		sendErrorResponse(ctx, fasthttp.StatusTooManyRequests, "Rate limit exceeded. Please retry after some time.")
		return
	}
	if maybeSendRandomProviderError(ctx, provider) {
		return
	}

	if shouldFail() {
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
	simulateLatency()

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

	randomInputTokens := rand.Intn(1000)
	randomOutputTokens := rand.Intn(1000)

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
	if err := json.NewEncoder(ctx).Encode(mockResp); err != nil {
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

	if shouldTriggerTPM() {
		sendErrorResponse(ctx, fasthttp.StatusTooManyRequests, "Rate limit exceeded. Please retry after some time.")
		return
	}
	if maybeSendRandomProviderError(ctx, provider) {
		return
	}

	if shouldFail() {
		sendErrorResponse(ctx, fasthttp.StatusInternalServerError, "The server had an error while processing your request. Sorry about that!")
		return
	}

	if provider != "" {
		log.Printf("[responses] provider=%s model=%s", provider, model)
	} else {
		log.Printf("[responses] model=%s", model)
	}

	simulateLatency()

	mockContent := "This is a mocked response from the OpenAI mocker server."
	if bigPayload {
		mockContent = strings.Repeat(mockContent, 182)
	}

	randomInputTokens := rand.Intn(1000)
	randomOutputTokens := rand.Intn(1000)

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
	if err := json.NewEncoder(ctx).Encode(resp); err != nil {
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

	if shouldTriggerTPM() {
		sendErrorResponse(ctx, fasthttp.StatusTooManyRequests, "Rate limit exceeded. Please retry after some time.")
		return
	}
	if maybeSendRandomProviderError(ctx, provider) {
		return
	}

	if shouldFail() {
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

	simulateLatency()

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

	randomPromptTokens := rand.Intn(100) + 1

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
	if err := json.NewEncoder(ctx).Encode(resp); err != nil {
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

	if shouldTriggerTPM() {
		sendErrorResponse(ctx, fasthttp.StatusTooManyRequests, "Rate limit exceeded. Please retry after some time.")
		return
	}
	if maybeSendRandomProviderError(ctx, "anthropic") {
		return
	}

	if shouldFail() {
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

	simulateLatency()

	randomInputTokens := rand.Intn(1000)
	randomOutputTokens := rand.Intn(1000)

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
	if err := json.NewEncoder(ctx).Encode(resp); err != nil {
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

	if shouldTriggerTPM() {
		sendErrorResponse(ctx, fasthttp.StatusTooManyRequests, "Rate limit exceeded. Please retry after some time.")
		return
	}
	if maybeSendRandomProviderError(ctx, provider) {
		return
	}

	if shouldFail() {
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

	simulateLatency()

	randomInputTokens := rand.Intn(1000)
	randomOutputTokens := rand.Intn(1000)

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
	if err := json.NewEncoder(ctx).Encode(resp); err != nil {
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

	if shouldTriggerTPM() {
		sendErrorResponse(ctx, fasthttp.StatusTooManyRequests, "Rate limit exceeded. Please retry after some time.")
		return
	}
	if maybeSendRandomProviderError(ctx, "bedrock") {
		return
	}
	if shouldFail() {
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

	simulateLatency()
	randomInputTokens := rand.Intn(1000)
	randomOutputTokens := rand.Intn(1000)
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
	if err := json.NewEncoder(ctx).Encode(resp); err != nil {
		log.Printf("Error encoding bedrock response: %v", err)
		ctx.SetStatusCode(fasthttp.StatusInternalServerError)
		ctx.SetBodyString("Failed to encode response")
	}
}

func healthCheckHandler(ctx *fasthttp.RequestCtx) {
	ctx.SetContentType("application/json")
	ctx.SetStatusCode(fasthttp.StatusOK)
	ctx.SetBodyString(`{"status":"healthy"}`)
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
	case "/chat/completions", "/v1/chat/completions", "/openai/chat/completions", "/openai/v1/chat/completions":
		mockChatCompletionsHandler(ctx)
	case "/responses", "/v1/responses", "/openai/responses", "/openai/v1/responses":
		mockResponsesHandler(ctx)
	case "/embeddings", "/v1/embeddings", "/openai/embeddings", "/openai/v1/embeddings":
		mockEmbeddingsHandler(ctx)
	case "/anthropic/v1/messages", "/anthropic/messages", "/v1/messages":
		mockAnthropicMessagesHandler(ctx)
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

	addr := fmt.Sprintf("%s:%d", host, port)
	if jitter > 0 {
		log.Printf("Mock LLM server (fasthttp) starting on %s with latency %dms ±%dms jitter...\n", addr, latency, jitter)
	} else {
		log.Printf("Mock LLM server (fasthttp) starting on %s with latency %dms...\n", addr, latency)
	}
	if tpm > 0 {
		log.Printf("TPM (429) scenario will be triggered after %d seconds", tpm)
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
