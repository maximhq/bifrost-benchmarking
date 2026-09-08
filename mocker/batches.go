package main

import (
	"bytes"
	"log"
	"math/rand"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bytedance/sonic"
	"github.com/maximhq/bifrost/core/schemas"
	"github.com/valyala/fasthttp"
)

// Batch API mocks for OpenAI (/v1/batches, file-based) and Anthropic
// (/v1/messages/batches, inline requests). Both mirror the wire shape of the
// real APIs: same paths, same object fields, same status vocabularies, same
// JSONL result formats.
//
// A batch advances purely as a function of elapsed time, so nothing runs in the
// background and polling is deterministic: -batch-completion-ms is the
// wall-clock a batch takes from creation to completion (0, the default,
// completes it immediately), and -batch-failure-percent decides how many of the
// requests inside it come back as per-request errors.

const (
	// Share of the completion window spent in each OpenAI pre-terminal status.
	batchValidatingFraction = 0.1
	batchFinalizingFraction = 0.9
	// A cancellation lingers in cancelling/canceling for this share of the window.
	batchCancelFraction = 0.1
	// completionWindowDuration is how long a batch stays alive before expiring;
	// both providers document a 24h window.
	completionWindowDuration = 24 * time.Hour
)

// openAIBatchEndpoints are the endpoints POST /v1/batches accepts.
var openAIBatchEndpoints = []string{"/v1/chat/completions", "/v1/embeddings", "/v1/completions", "/v1/responses"}

// OpenAIBatchObject mirrors the OpenAI Batch object. Absent timestamps are
// serialized as null rather than omitted, exactly like the real API.
type OpenAIBatchObject struct {
	ID               string                   `json:"id"`
	Object           string                   `json:"object"` // "batch"
	Endpoint         string                   `json:"endpoint"`
	Errors           *OpenAIBatchErrors       `json:"errors"`
	InputFileID      string                   `json:"input_file_id"`
	CompletionWindow string                   `json:"completion_window"`
	Status           string                   `json:"status"`
	OutputFileID     *string                  `json:"output_file_id"`
	ErrorFileID      *string                  `json:"error_file_id"`
	CreatedAt        int64                    `json:"created_at"`
	InProgressAt     *int64                   `json:"in_progress_at"`
	ExpiresAt        *int64                   `json:"expires_at"`
	FinalizingAt     *int64                   `json:"finalizing_at"`
	CompletedAt      *int64                   `json:"completed_at"`
	FailedAt         *int64                   `json:"failed_at"`
	ExpiredAt        *int64                   `json:"expired_at"`
	CancellingAt     *int64                   `json:"cancelling_at"`
	CancelledAt      *int64                   `json:"cancelled_at"`
	RequestCounts    OpenAIBatchRequestCounts `json:"request_counts"`
	Metadata         map[string]string        `json:"metadata"`
}

// OpenAIBatchRequestCounts mirrors Batch.request_counts.
type OpenAIBatchRequestCounts struct {
	Total     int `json:"total"`
	Completed int `json:"completed"`
	Failed    int `json:"failed"`
}

// OpenAIBatchErrors mirrors Batch.errors, populated when input validation fails.
type OpenAIBatchErrors struct {
	Object string             `json:"object"` // "list"
	Data   []OpenAIBatchError `json:"data"`
}

// OpenAIBatchError is a single input-validation failure.
type OpenAIBatchError struct {
	Code    string  `json:"code"`
	Message string  `json:"message"`
	Param   *string `json:"param"`
	Line    *int    `json:"line"`
}

// OpenAIBatchListResponse mirrors the response of GET /v1/batches.
type OpenAIBatchListResponse struct {
	Object  string              `json:"object"` // "list"
	Data    []OpenAIBatchObject `json:"data"`
	FirstID *string             `json:"first_id,omitempty"`
	LastID  *string             `json:"last_id,omitempty"`
	HasMore bool                `json:"has_more"`
}

// openAIBatchCreateRequest mirrors the body of POST /v1/batches.
type openAIBatchCreateRequest struct {
	InputFileID      string            `json:"input_file_id"`
	Endpoint         string            `json:"endpoint"`
	CompletionWindow string            `json:"completion_window"`
	Metadata         map[string]string `json:"metadata"`
}

// openAIBatchInputLine is one line of the uploaded JSONL input file.
type openAIBatchInputLine struct {
	CustomID string         `json:"custom_id"`
	Method   string         `json:"method"`
	URL      string         `json:"url"`
	Body     map[string]any `json:"body"`
}

// OpenAIBatchOutputLine is one line of the generated output (or error) file.
type OpenAIBatchOutputLine struct {
	ID       string                     `json:"id"`
	CustomID string                     `json:"custom_id"`
	Response *OpenAIBatchOutputResponse `json:"response"`
	Error    *OpenAIBatchOutputError    `json:"error"`
}

// OpenAIBatchOutputResponse is the per-request HTTP response captured in output.
type OpenAIBatchOutputResponse struct {
	StatusCode int    `json:"status_code"`
	RequestID  string `json:"request_id"`
	Body       any    `json:"body"`
}

// OpenAIBatchOutputError describes a non-HTTP failure of a single request.
type OpenAIBatchOutputError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// OpenAITextCompletionResponse backs batches targeting /v1/completions.
type OpenAITextCompletionResponse struct {
	ID      string                       `json:"id"`
	Object  string                       `json:"object"` // "text_completion"
	Created int                          `json:"created"`
	Model   string                       `json:"model"`
	Choices []OpenAITextCompletionChoice `json:"choices"`
	Usage   schemas.LLMUsage             `json:"usage"`
}

// OpenAITextCompletionChoice is a single legacy completion choice.
type OpenAITextCompletionChoice struct {
	Text         string      `json:"text"`
	Index        int         `json:"index"`
	Logprobs     interface{} `json:"logprobs"`
	FinishReason string      `json:"finish_reason"`
}

// AnthropicBatchObject mirrors the Anthropic Message Batch object.
type AnthropicBatchObject struct {
	ID                string                      `json:"id"`
	Type              string                      `json:"type"` // "message_batch"
	ProcessingStatus  string                      `json:"processing_status"`
	RequestCounts     AnthropicBatchRequestCounts `json:"request_counts"`
	EndedAt           *string                     `json:"ended_at"`
	CreatedAt         string                      `json:"created_at"`
	ExpiresAt         string                      `json:"expires_at"`
	ArchivedAt        *string                     `json:"archived_at"`
	CancelInitiatedAt *string                     `json:"cancel_initiated_at"`
	ResultsURL        *string                     `json:"results_url"`
}

// AnthropicBatchRequestCounts mirrors MessageBatch.request_counts.
type AnthropicBatchRequestCounts struct {
	Processing int `json:"processing"`
	Succeeded  int `json:"succeeded"`
	Errored    int `json:"errored"`
	Canceled   int `json:"canceled"`
	Expired    int `json:"expired"`
}

// AnthropicBatchListResponse mirrors GET /v1/messages/batches.
type AnthropicBatchListResponse struct {
	Data    []AnthropicBatchObject `json:"data"`
	HasMore bool                   `json:"has_more"`
	FirstID *string                `json:"first_id"`
	LastID  *string                `json:"last_id"`
}

// AnthropicBatchDeleteResponse mirrors DELETE /v1/messages/batches/{id}.
type AnthropicBatchDeleteResponse struct {
	ID   string `json:"id"`
	Type string `json:"type"` // "message_batch_deleted"
}

// anthropicBatchCreateRequest mirrors the body of POST /v1/messages/batches.
type anthropicBatchCreateRequest struct {
	Requests []anthropicBatchRequestItem `json:"requests"`
}

type anthropicBatchRequestItem struct {
	CustomID string         `json:"custom_id"`
	Params   map[string]any `json:"params"`
}

// AnthropicBatchResultLine is one line of the JSONL served by the results endpoint.
type AnthropicBatchResultLine struct {
	CustomID string                   `json:"custom_id"`
	Result   AnthropicBatchResultData `json:"result"`
}

// AnthropicBatchResultData carries the outcome of a single batched request.
type AnthropicBatchResultData struct {
	Type    string                    `json:"type"` // succeeded | errored | canceled | expired
	Message *AnthropicMessageResponse `json:"message,omitempty"`
	Error   *AnthropicAPIError        `json:"error,omitempty"`
}

// mockBatchRequest is one request inside a batch, as parsed at creation time.
type mockBatchRequest struct {
	customID string
	url      string
	model    string
}

// mockBatch is the mocker's view of a submitted batch. Its status is derived
// from wall-clock elapsed time rather than mutated by a worker.
type mockBatch struct {
	id               string
	provider         string // "openai" or "anthropic"
	endpoint         string
	inputFileID      string
	completionWindow string
	metadata         map[string]string
	requests         []mockBatchRequest
	createdAt        time.Time
	expiresAt        time.Time

	validationErrors []OpenAIBatchError

	cancelInitiatedAt *time.Time
	cancelProcessed   int

	outputFileID string
	errorFileID  string
	resultsBuilt bool
	results      []byte // Anthropic results JSONL
}

// batchStore holds submitted batches for the lifetime of the process.
var batchStore = struct {
	mu    sync.Mutex
	byID  map[string]*mockBatch
	order []string // insertion order, oldest first
}{byID: make(map[string]*mockBatch)}

func batchWindow() time.Duration {
	if batchCompletionMs <= 0 {
		return 0
	}
	return time.Duration(batchCompletionMs) * time.Millisecond
}

func batchCancelWindow() time.Duration {
	return time.Duration(float64(batchWindow()) * batchCancelFraction)
}

// requestFails decides, deterministically, whether the index-th request of a
// batch comes back as a per-request error. Failures are spread evenly across the
// batch rather than front-loaded, so partial progress always mixes both
// outcomes and any prefix of n requests holds n*percent/100 failures.
func requestFails(index int) bool {
	if batchFailurePercent <= 0 {
		return false
	}
	if batchFailurePercent >= 100 {
		return true
	}
	return (index*batchFailurePercent)/100 != ((index+1)*batchFailurePercent)/100
}

// processedAt reports how many of the batch's requests have finished by now.
func (b *mockBatch) processedAt(now time.Time) int {
	if len(b.validationErrors) > 0 {
		return 0
	}
	if b.cancelInitiatedAt != nil {
		return b.cancelProcessed
	}
	window := batchWindow()
	if window <= 0 {
		return len(b.requests)
	}
	start := time.Duration(float64(window) * batchValidatingFraction)
	end := time.Duration(float64(window) * batchFinalizingFraction)
	elapsed := now.Sub(b.createdAt)
	switch {
	case elapsed <= start:
		return 0
	case elapsed >= end:
		return len(b.requests)
	default:
		return int(float64(len(b.requests)) * float64(elapsed-start) / float64(end-start))
	}
}

// splitOutcomes splits the first processed requests into successes and failures.
func (b *mockBatch) splitOutcomes(processed int) (succeeded int, failed int) {
	for i := 0; i < processed; i++ {
		if requestFails(i) {
			failed++
		} else {
			succeeded++
		}
	}
	return succeeded, failed
}

// openAIStatus derives the OpenAI batch status at now.
func (b *mockBatch) openAIStatus(now time.Time) string {
	if len(b.validationErrors) > 0 {
		return "failed"
	}
	if b.cancelInitiatedAt != nil {
		if now.Sub(*b.cancelInitiatedAt) < batchCancelWindow() {
			return "cancelling"
		}
		return "cancelled"
	}

	window := batchWindow()
	elapsed := now.Sub(b.createdAt)
	if elapsed >= window {
		return "completed"
	}
	if now.After(b.expiresAt) {
		return "expired"
	}
	if elapsed < time.Duration(float64(window)*batchValidatingFraction) {
		return "validating"
	}
	if elapsed < time.Duration(float64(window)*batchFinalizingFraction) {
		return "in_progress"
	}
	return "finalizing"
}

// anthropicStatus derives the Anthropic processing_status at now.
func (b *mockBatch) anthropicStatus(now time.Time) string {
	if b.cancelInitiatedAt != nil {
		if now.Sub(*b.cancelInitiatedAt) < batchCancelWindow() {
			return "canceling"
		}
		return "ended"
	}
	if now.Sub(b.createdAt) >= batchWindow() || now.After(b.expiresAt) {
		return "ended"
	}
	return "in_progress"
}

// isOpenAITerminal reports whether an OpenAI batch can still change state.
func isOpenAITerminal(status string) bool {
	switch status {
	case "completed", "cancelled", "expired", "failed":
		return true
	}
	return false
}

func unixPtr(t time.Time) *int64 {
	unix := t.Unix()
	return &unix
}

func rfc3339Ptr(t time.Time) *string {
	formatted := t.UTC().Format(time.RFC3339Nano)
	return &formatted
}

// ensureResults materializes a finished batch's results the first time they are
// observed: generated files for OpenAI, an in-memory JSONL body for Anthropic.
// Callers must hold batchStore.mu.
func (b *mockBatch) ensureResults(now time.Time) {
	if b.resultsBuilt || len(b.validationErrors) > 0 {
		return
	}

	if b.provider == "anthropic" {
		if b.anthropicStatus(now) != "ended" {
			return
		}
		b.results = buildAnthropicResults(b, b.processedAt(now))
		b.resultsBuilt = true
		return
	}

	if !isOpenAITerminal(b.openAIStatus(now)) {
		return
	}
	output, errorOutput := buildOpenAIBatchOutput(b, b.processedAt(now))
	if len(output) > 0 {
		b.outputFileID = storeFile(b.id+"_output.jsonl", "batch_output", output).ID
	}
	if len(errorOutput) > 0 {
		b.errorFileID = storeFile(b.id+"_error.jsonl", "batch_output", errorOutput).ID
	}
	b.resultsBuilt = true
}

// toOpenAIObject renders the batch in OpenAI's wire format.
func (b *mockBatch) toOpenAIObject(now time.Time) OpenAIBatchObject {
	b.ensureResults(now)

	status := b.openAIStatus(now)
	processed := b.processedAt(now)
	succeeded, failed := b.splitOutcomes(processed)

	obj := OpenAIBatchObject{
		ID:               b.id,
		Object:           "batch",
		Endpoint:         b.endpoint,
		InputFileID:      b.inputFileID,
		CompletionWindow: b.completionWindow,
		Status:           status,
		CreatedAt:        b.createdAt.Unix(),
		ExpiresAt:        unixPtr(b.expiresAt),
		RequestCounts: OpenAIBatchRequestCounts{
			Total:     len(b.requests),
			Completed: succeeded,
			Failed:    failed,
		},
		Metadata: b.metadata,
	}

	if len(b.validationErrors) > 0 {
		obj.Errors = &OpenAIBatchErrors{Object: "list", Data: b.validationErrors}
		obj.FailedAt = unixPtr(b.createdAt)
		return obj
	}

	window := batchWindow()
	if inProgressAt := b.createdAt.Add(time.Duration(float64(window) * batchValidatingFraction)); !now.Before(inProgressAt) {
		obj.InProgressAt = unixPtr(inProgressAt)
	}
	if finalizingAt := b.createdAt.Add(time.Duration(float64(window) * batchFinalizingFraction)); !now.Before(finalizingAt) {
		obj.FinalizingAt = unixPtr(finalizingAt)
	}
	if status == "completed" {
		obj.CompletedAt = unixPtr(b.createdAt.Add(window))
	}
	if status == "expired" {
		obj.ExpiredAt = unixPtr(b.expiresAt)
	}
	if b.cancelInitiatedAt != nil {
		obj.CancellingAt = unixPtr(*b.cancelInitiatedAt)
		if status == "cancelled" {
			obj.CancelledAt = unixPtr(b.cancelInitiatedAt.Add(batchCancelWindow()))
		}
	}
	if b.outputFileID != "" {
		obj.OutputFileID = StrPtr(b.outputFileID)
	}
	if b.errorFileID != "" {
		obj.ErrorFileID = StrPtr(b.errorFileID)
	}

	return obj
}

// toAnthropicObject renders the batch in Anthropic's wire format. baseURL is the
// origin the caller reached the mocker on, so results_url stays resolvable.
func (b *mockBatch) toAnthropicObject(now time.Time, baseURL string) AnthropicBatchObject {
	b.ensureResults(now)

	status := b.anthropicStatus(now)
	processed := b.processedAt(now)
	succeeded, errored := b.splitOutcomes(processed)

	counts := AnthropicBatchRequestCounts{
		Processing: len(b.requests) - processed,
		Succeeded:  succeeded,
		Errored:    errored,
	}
	if status == "ended" && counts.Processing > 0 {
		// Whatever never ran before the batch ended was cancelled or expired.
		if b.cancelInitiatedAt != nil {
			counts.Canceled = counts.Processing
		} else {
			counts.Expired = counts.Processing
		}
		counts.Processing = 0
	}

	obj := AnthropicBatchObject{
		ID:               b.id,
		Type:             "message_batch",
		ProcessingStatus: status,
		RequestCounts:    counts,
		CreatedAt:        b.createdAt.UTC().Format(time.RFC3339Nano),
		ExpiresAt:        b.expiresAt.UTC().Format(time.RFC3339Nano),
	}

	if status == "ended" {
		endedAt := b.createdAt.Add(batchWindow())
		if b.cancelInitiatedAt != nil {
			endedAt = b.cancelInitiatedAt.Add(batchCancelWindow())
		}
		obj.EndedAt = rfc3339Ptr(endedAt)
		obj.ResultsURL = StrPtr(baseURL + "/v1/messages/batches/" + b.id + "/results")
	}
	if b.cancelInitiatedAt != nil {
		obj.CancelInitiatedAt = rfc3339Ptr(*b.cancelInitiatedAt)
	}

	return obj
}

// batchMockContent is the assistant text every batched request resolves to.
func batchMockContent(provider string) string {
	content := "This is a mocked response from the OpenAI mocker server."
	if provider == "anthropic" {
		content = "This is a mocked response from the Bifrost mocker server."
	}
	if bigPayload {
		content = strings.Repeat(content, 182)
	}
	return content
}

// buildOpenAIBatchOutput renders the JSONL for the output and error files of the
// first processed requests.
func buildOpenAIBatchOutput(b *mockBatch, processed int) ([]byte, []byte) {
	var output, errorOutput bytes.Buffer
	content := batchMockContent("openai")

	for i := 0; i < processed; i++ {
		request := b.requests[i]
		line := OpenAIBatchOutputLine{
			ID:       randomID("batch_req_", 24),
			CustomID: request.customID,
		}

		target := &output
		if requestFails(i) {
			target = &errorOutput
			line.Response = &OpenAIBatchOutputResponse{
				StatusCode: fasthttp.StatusInternalServerError,
				RequestID:  randomID("req_", 24),
				Body: OpenAIAPIError{Error: OpenAIAPIErrorBody{
					Message: "The server had an error while processing your request. Sorry about that!",
					Type:    "server_error",
				}},
			}
		} else {
			line.Response = &OpenAIBatchOutputResponse{
				StatusCode: fasthttp.StatusOK,
				RequestID:  randomID("req_", 24),
				Body:       buildOpenAIBatchBody(request.url, request.model, content),
			}
		}

		encoded, err := sonic.ConfigDefault.Marshal(line)
		if err != nil {
			log.Printf("Error encoding batch output line: %v", err)
			continue
		}
		target.Write(encoded)
		target.WriteByte('\n')
	}

	return output.Bytes(), errorOutput.Bytes()
}

// buildOpenAIBatchBody renders the response body a single batched request would
// have received from the endpoint it targeted.
func buildOpenAIBatchBody(endpoint string, model string, content string) any {
	inputTokens := resolveInputTokens(rand.Intn(1000))
	outputTokens := resolveOutputTokens(rand.Intn(1000))
	usage := schemas.LLMUsage{
		PromptTokens:     inputTokens,
		CompletionTokens: outputTokens,
		TotalTokens:      inputTokens + outputTokens,
	}

	switch endpoint {
	case "/v1/embeddings":
		dimensions := 1536
		if bigPayload {
			dimensions = 4096
		}
		embedding := make([]float64, dimensions)
		for i := range embedding {
			embedding[i] = rand.Float64()*2 - 1
		}
		promptTokens := resolveInputTokens(rand.Intn(100) + 1)
		return OpenAIEmbeddingsResponse{
			Object: "list",
			Data:   []OpenAIEmbeddingData{{Object: "embedding", Embedding: embedding, Index: 0}},
			Model:  model,
			Usage:  schemas.LLMUsage{PromptTokens: promptTokens, TotalTokens: promptTokens},
		}
	case "/v1/responses":
		return OpenAIResponsesResponse{
			ID:      randomID("resp_", 24),
			Object:  "response",
			Created: int(time.Now().Unix()),
			Model:   model,
			Output: []OpenAIResponsesOutputItem{{
				ID:      randomID("msg_", 24),
				Type:    "message",
				Role:    "assistant",
				Content: []OpenAIResponsesMessageContent{{Type: "output_text", Text: content}},
			}},
			Status: "completed",
			Usage:  usage,
		}
	case "/v1/completions":
		return OpenAITextCompletionResponse{
			ID:      randomID("cmpl-", 24),
			Object:  "text_completion",
			Created: int(time.Now().Unix()),
			Model:   model,
			Choices: []OpenAITextCompletionChoice{{Text: content, Index: 0, FinishReason: "stop"}},
			Usage:   usage,
		}
	default:
		return OpenAIChatCompletionsResponse{
			ID:      randomID("chatcmpl-", 24),
			Object:  "chat.completion",
			Created: int(time.Now().Unix()),
			Model:   model,
			Choices: []schemas.BifrostResponseChoice{{
				Index: 0,
				Message: schemas.BifrostResponseChoiceMessage{
					Role:    schemas.ModelChatMessageRole("assistant"),
					Content: StrPtr(content),
				},
				FinishReason: StrPtr("stop"),
			}},
			Usage: usage,
		}
	}
}

// buildAnthropicResults renders the JSONL served by the results endpoint: one
// line per request, in submission order.
func buildAnthropicResults(b *mockBatch, processed int) []byte {
	var buf bytes.Buffer
	content := batchMockContent("anthropic")

	unfinished := "expired"
	if b.cancelInitiatedAt != nil {
		unfinished = "canceled"
	}

	for i, request := range b.requests {
		line := AnthropicBatchResultLine{CustomID: request.customID}
		switch {
		case i >= processed:
			line.Result = AnthropicBatchResultData{Type: unfinished}
		case requestFails(i):
			line.Result = AnthropicBatchResultData{
				Type: "errored",
				Error: &AnthropicAPIError{
					Type: "error",
					Error: AnthropicAPIErrorBody{
						Type:    "invalid_request_error",
						Message: "The server had an error while processing your request. Sorry about that!",
					},
				},
			}
		default:
			inputTokens := resolveInputTokens(rand.Intn(1000))
			outputTokens := resolveOutputTokens(rand.Intn(1000))
			line.Result = AnthropicBatchResultData{
				Type: "succeeded",
				Message: &AnthropicMessageResponse{
					ID:         randomID("msg_", 24),
					Type:       "message",
					Role:       "assistant",
					Model:      request.model,
					Content:    []AnthropicMessageContent{{Type: "text", Text: content}},
					StopReason: "end_turn",
					Usage: AnthropicMessageUsage{
						InputTokens:  inputTokens,
						OutputTokens: outputTokens,
					},
				},
			}
		}

		encoded, err := sonic.ConfigDefault.Marshal(line)
		if err != nil {
			log.Printf("Error encoding batch result line: %v", err)
			continue
		}
		buf.Write(encoded)
		buf.WriteByte('\n')
	}

	return buf.Bytes()
}

// registerBatch stores a freshly created batch.
func registerBatch(b *mockBatch) {
	batchStore.mu.Lock()
	defer batchStore.mu.Unlock()
	batchStore.byID[b.id] = b
	batchStore.order = append(batchStore.order, b.id)
}

// batchesNewestFirst returns the stored batches for a provider, newest first.
// Callers must hold batchStore.mu.
func batchesNewestFirst(provider string) []*mockBatch {
	batches := make([]*mockBatch, 0, len(batchStore.order))
	for i := len(batchStore.order) - 1; i >= 0; i-- {
		b, ok := batchStore.byID[batchStore.order[i]]
		if ok && b.provider == provider {
			batches = append(batches, b)
		}
	}
	return batches
}

// modelFromParams pulls the model out of a batched request body, falling back to
// a provider default when the caller left it out.
func modelFromParams(params map[string]any, fallback string) string {
	if params != nil {
		if raw, ok := params["model"].(string); ok && raw != "" {
			_, model := parseProviderAndModel(raw)
			return model
		}
	}
	return fallback
}

// ---------------------------------------------------------------------------
// OpenAI: /v1/batches
// ---------------------------------------------------------------------------

// mockOpenAIBatchesHandler serves POST /v1/batches (create) and GET /v1/batches
// (list).
func mockOpenAIBatchesHandler(ctx *fasthttp.RequestCtx) {
	if !checkAuth(ctx) {
		return
	}
	if simulateProviderConditions(ctx, "openai") {
		return
	}

	switch {
	case ctx.IsPost():
		handleOpenAIBatchCreate(ctx)
	case ctx.IsGet():
		handleOpenAIBatchList(ctx)
	default:
		sendOpenAIAPIError(ctx, fasthttp.StatusMethodNotAllowed, "invalid_request_error",
			"Not allowed to "+string(ctx.Method())+" on /v1/batches.", nil, nil)
	}
}

func handleOpenAIBatchCreate(ctx *fasthttp.RequestCtx) {
	var request openAIBatchCreateRequest
	if err := sonic.Unmarshal(ctx.PostBody(), &request); err != nil {
		sendOpenAIAPIError(ctx, fasthttp.StatusBadRequest, "invalid_request_error",
			"We could not parse the JSON body of your request.", nil, StrPtr("invalid_json"))
		return
	}

	if request.InputFileID == "" {
		sendOpenAIAPIError(ctx, fasthttp.StatusBadRequest, "invalid_request_error",
			"Missing required parameter: 'input_file_id'.", StrPtr("input_file_id"), StrPtr("missing_required_parameter"))
		return
	}
	if request.Endpoint == "" {
		sendOpenAIAPIError(ctx, fasthttp.StatusBadRequest, "invalid_request_error",
			"Missing required parameter: 'endpoint'.", StrPtr("endpoint"), StrPtr("missing_required_parameter"))
		return
	}
	if !containsString(openAIBatchEndpoints, request.Endpoint) {
		sendOpenAIAPIError(ctx, fasthttp.StatusBadRequest, "invalid_request_error",
			"Invalid value: '"+request.Endpoint+"'. Supported values are: "+strings.Join(openAIBatchEndpoints, ", ")+".",
			StrPtr("endpoint"), StrPtr("invalid_value"))
		return
	}
	if request.CompletionWindow == "" {
		sendOpenAIAPIError(ctx, fasthttp.StatusBadRequest, "invalid_request_error",
			"Missing required parameter: 'completion_window'.", StrPtr("completion_window"), StrPtr("missing_required_parameter"))
		return
	}
	if request.CompletionWindow != "24h" {
		sendOpenAIAPIError(ctx, fasthttp.StatusBadRequest, "invalid_request_error",
			"Invalid value: '"+request.CompletionWindow+"'. Supported values are: 24h.",
			StrPtr("completion_window"), StrPtr("invalid_value"))
		return
	}

	inputFile, ok := lookupFile(request.InputFileID)
	if !ok {
		sendOpenAIAPIError(ctx, fasthttp.StatusNotFound, "invalid_request_error",
			"No such File object: "+request.InputFileID, StrPtr("input_file_id"), nil)
		return
	}

	requests, validationErrors := parseOpenAIBatchInput(inputFile.content, request.Endpoint)

	now := time.Now()
	batch := &mockBatch{
		id:               randomID("batch_", 24),
		provider:         "openai",
		endpoint:         request.Endpoint,
		inputFileID:      request.InputFileID,
		completionWindow: request.CompletionWindow,
		metadata:         request.Metadata,
		requests:         requests,
		createdAt:        now,
		expiresAt:        now.Add(completionWindowDuration),
		validationErrors: validationErrors,
	}
	registerBatch(batch)

	log.Printf("[batches] created id=%s endpoint=%s requests=%d", batch.id, batch.endpoint, len(batch.requests))

	batchStore.mu.Lock()
	obj := batch.toOpenAIObject(time.Now())
	batchStore.mu.Unlock()
	writeJSON(ctx, fasthttp.StatusOK, obj)
}

// parseOpenAIBatchInput reads the uploaded JSONL, returning the parsed requests
// and any validation errors, which fail the batch the way OpenAI's do.
func parseOpenAIBatchInput(content []byte, endpoint string) ([]mockBatchRequest, []OpenAIBatchError) {
	var requests []mockBatchRequest
	var errors []OpenAIBatchError
	seen := make(map[string]bool)

	lineNumber := 0
	for _, raw := range bytes.Split(content, []byte("\n")) {
		raw = bytes.TrimSpace(raw)
		if len(raw) == 0 {
			continue
		}
		lineNumber++
		line := lineNumber

		var parsed openAIBatchInputLine
		if err := sonic.Unmarshal(raw, &parsed); err != nil {
			errors = append(errors, OpenAIBatchError{
				Code:    "invalid_json_line",
				Message: "This line is not parseable as valid JSON.",
				Line:    &line,
			})
			continue
		}
		if parsed.CustomID == "" {
			errors = append(errors, OpenAIBatchError{
				Code:    "missing_required_parameter",
				Message: "Missing required parameter: 'custom_id'.",
				Param:   StrPtr("custom_id"),
				Line:    &line,
			})
			continue
		}
		if seen[parsed.CustomID] {
			errors = append(errors, OpenAIBatchError{
				Code:    "duplicate_custom_id",
				Message: "The custom_id '" + parsed.CustomID + "' is not unique in this file.",
				Param:   StrPtr("custom_id"),
				Line:    &line,
			})
			continue
		}
		if parsed.URL != "" && parsed.URL != endpoint {
			errors = append(errors, OpenAIBatchError{
				Code:    "invalid_url",
				Message: "The URL provided for this request does not match the batch endpoint '" + endpoint + "'.",
				Param:   StrPtr("url"),
				Line:    &line,
			})
			continue
		}

		seen[parsed.CustomID] = true
		url := parsed.URL
		if url == "" {
			url = endpoint
		}
		requests = append(requests, mockBatchRequest{
			customID: parsed.CustomID,
			url:      url,
			model:    modelFromParams(parsed.Body, "gpt-4o-mini"),
		})
	}

	if len(requests) == 0 && len(errors) == 0 {
		errors = append(errors, OpenAIBatchError{
			Code:    "empty_file",
			Message: "The input file is empty. Please ensure that your batch contains at least one request.",
			Param:   StrPtr("input_file_id"),
		})
	}

	return requests, errors
}

func handleOpenAIBatchList(ctx *fasthttp.RequestCtx) {
	now := time.Now()
	after := string(ctx.QueryArgs().Peek("after"))

	batchStore.mu.Lock()
	batches := batchesNewestFirst("openai")
	cursorIndex := -1
	for i, b := range batches {
		if b.id == after && after != "" {
			cursorIndex = i
			break
		}
	}
	start, end, hasMore := paginate(len(batches), queryLimit(ctx, 20), cursorIndex)
	data := make([]OpenAIBatchObject, 0, end-start)
	for _, b := range batches[start:end] {
		data = append(data, b.toOpenAIObject(now))
	}
	batchStore.mu.Unlock()

	resp := OpenAIBatchListResponse{Object: "list", Data: data, HasMore: hasMore}
	if len(data) > 0 {
		resp.FirstID = StrPtr(data[0].ID)
		resp.LastID = StrPtr(data[len(data)-1].ID)
	}

	log.Printf("[batches] listing %d batch(es)", len(data))
	writeJSON(ctx, fasthttp.StatusOK, resp)
}

// mockOpenAIBatchRetrieveHandler serves GET /v1/batches/{batch_id}.
func mockOpenAIBatchRetrieveHandler(ctx *fasthttp.RequestCtx, batchID string) {
	if !checkAuth(ctx) {
		return
	}
	if simulateProviderConditions(ctx, "openai") {
		return
	}
	if !ctx.IsGet() {
		sendOpenAIAPIError(ctx, fasthttp.StatusMethodNotAllowed, "invalid_request_error",
			"Not allowed to "+string(ctx.Method())+" on /v1/batches/{batch_id}.", nil, nil)
		return
	}

	batchStore.mu.Lock()
	batch, ok := batchStore.byID[batchID]
	if !ok || batch.provider != "openai" {
		batchStore.mu.Unlock()
		sendOpenAIBatchNotFound(ctx, batchID)
		return
	}
	obj := batch.toOpenAIObject(time.Now())
	batchStore.mu.Unlock()

	log.Printf("[batches] retrieve id=%s status=%s", obj.ID, obj.Status)
	writeJSON(ctx, fasthttp.StatusOK, obj)
}

// mockOpenAIBatchCancelHandler serves POST /v1/batches/{batch_id}/cancel.
func mockOpenAIBatchCancelHandler(ctx *fasthttp.RequestCtx, batchID string) {
	if !checkAuth(ctx) {
		return
	}
	if simulateProviderConditions(ctx, "openai") {
		return
	}
	if !ctx.IsPost() {
		sendOpenAIAPIError(ctx, fasthttp.StatusMethodNotAllowed, "invalid_request_error",
			"Not allowed to "+string(ctx.Method())+" on /v1/batches/{batch_id}/cancel.", nil, nil)
		return
	}

	now := time.Now()

	batchStore.mu.Lock()
	batch, ok := batchStore.byID[batchID]
	if !ok || batch.provider != "openai" {
		batchStore.mu.Unlock()
		sendOpenAIBatchNotFound(ctx, batchID)
		return
	}
	if status := batch.openAIStatus(now); isOpenAITerminal(status) {
		batchStore.mu.Unlock()
		sendOpenAIAPIError(ctx, fasthttp.StatusConflict, "invalid_request_error",
			"Cannot cancel a batch with status '"+status+"'.", nil, nil)
		return
	}
	if batch.cancelInitiatedAt == nil {
		batch.cancelProcessed = batch.processedAt(now)
		batch.cancelInitiatedAt = &now
	}
	obj := batch.toOpenAIObject(now)
	batchStore.mu.Unlock()

	log.Printf("[batches] cancel id=%s status=%s", obj.ID, obj.Status)
	writeJSON(ctx, fasthttp.StatusOK, obj)
}

func sendOpenAIBatchNotFound(ctx *fasthttp.RequestCtx, batchID string) {
	sendOpenAIAPIError(ctx, fasthttp.StatusNotFound, "invalid_request_error",
		"No batch found with id '"+batchID+"'.", nil, nil)
}

// ---------------------------------------------------------------------------
// Anthropic: /v1/messages/batches
// ---------------------------------------------------------------------------

// mockAnthropicBatchesHandler serves POST /v1/messages/batches (create) and GET
// /v1/messages/batches (list).
func mockAnthropicBatchesHandler(ctx *fasthttp.RequestCtx) {
	if !checkAuth(ctx) {
		return
	}
	if simulateProviderConditions(ctx, "anthropic") {
		return
	}

	switch {
	case ctx.IsPost():
		handleAnthropicBatchCreate(ctx)
	case ctx.IsGet():
		handleAnthropicBatchList(ctx)
	default:
		sendAnthropicAPIError(ctx, fasthttp.StatusMethodNotAllowed, "invalid_request_error",
			"Method not allowed: "+string(ctx.Method()))
	}
}

func handleAnthropicBatchCreate(ctx *fasthttp.RequestCtx) {
	var request anthropicBatchCreateRequest
	if err := sonic.Unmarshal(ctx.PostBody(), &request); err != nil {
		sendAnthropicAPIError(ctx, fasthttp.StatusBadRequest, "invalid_request_error",
			"Could not parse the JSON body of your request.")
		return
	}
	if len(request.Requests) == 0 {
		sendAnthropicAPIError(ctx, fasthttp.StatusBadRequest, "invalid_request_error",
			"requests: List should have at least 1 item after validation, not 0")
		return
	}
	if len(request.Requests) > 100000 {
		sendAnthropicAPIError(ctx, fasthttp.StatusBadRequest, "invalid_request_error",
			"requests: List should have at most 100000 items after validation")
		return
	}

	requests := make([]mockBatchRequest, 0, len(request.Requests))
	seen := make(map[string]bool, len(request.Requests))
	for i, item := range request.Requests {
		if item.CustomID == "" {
			sendAnthropicAPIError(ctx, fasthttp.StatusBadRequest, "invalid_request_error",
				"requests."+strconv.Itoa(i)+".custom_id: Field required")
			return
		}
		if seen[item.CustomID] {
			sendAnthropicAPIError(ctx, fasthttp.StatusBadRequest, "invalid_request_error",
				"requests."+strconv.Itoa(i)+".custom_id: Duplicate custom_id '"+item.CustomID+"'; custom_id values must be unique within a batch")
			return
		}
		seen[item.CustomID] = true
		requests = append(requests, mockBatchRequest{
			customID: item.CustomID,
			url:      "/v1/messages",
			model:    modelFromParams(item.Params, "claude-3-5-sonnet-latest"),
		})
	}

	now := time.Now()
	batch := &mockBatch{
		id:               randomID("msgbatch_", 24),
		provider:         "anthropic",
		endpoint:         "/v1/messages",
		completionWindow: "24h",
		requests:         requests,
		createdAt:        now,
		expiresAt:        now.Add(completionWindowDuration),
	}
	registerBatch(batch)

	log.Printf("[messages/batches] created id=%s requests=%d", batch.id, len(batch.requests))

	batchStore.mu.Lock()
	obj := batch.toAnthropicObject(time.Now(), requestBaseURL(ctx))
	batchStore.mu.Unlock()
	writeJSON(ctx, fasthttp.StatusOK, obj)
}

func handleAnthropicBatchList(ctx *fasthttp.RequestCtx) {
	now := time.Now()
	baseURL := requestBaseURL(ctx)
	afterID := string(ctx.QueryArgs().Peek("after_id"))
	beforeID := string(ctx.QueryArgs().Peek("before_id"))

	batchStore.mu.Lock()
	batches := batchesNewestFirst("anthropic")
	if beforeID != "" {
		for i, b := range batches {
			if b.id == beforeID {
				batches = batches[:i]
				break
			}
		}
	}
	cursorIndex := -1
	for i, b := range batches {
		if b.id == afterID && afterID != "" {
			cursorIndex = i
			break
		}
	}
	start, end, hasMore := paginate(len(batches), queryLimit(ctx, 20), cursorIndex)
	data := make([]AnthropicBatchObject, 0, end-start)
	for _, b := range batches[start:end] {
		data = append(data, b.toAnthropicObject(now, baseURL))
	}
	batchStore.mu.Unlock()

	resp := AnthropicBatchListResponse{Data: data, HasMore: hasMore}
	if len(data) > 0 {
		resp.FirstID = StrPtr(data[0].ID)
		resp.LastID = StrPtr(data[len(data)-1].ID)
	}

	log.Printf("[messages/batches] listing %d batch(es)", len(data))
	writeJSON(ctx, fasthttp.StatusOK, resp)
}

// mockAnthropicBatchByIDHandler serves GET and DELETE on
// /v1/messages/batches/{batch_id}.
func mockAnthropicBatchByIDHandler(ctx *fasthttp.RequestCtx, batchID string) {
	if !checkAuth(ctx) {
		return
	}
	if simulateProviderConditions(ctx, "anthropic") {
		return
	}

	now := time.Now()

	switch {
	case ctx.IsGet():
		batchStore.mu.Lock()
		batch, ok := batchStore.byID[batchID]
		if !ok || batch.provider != "anthropic" {
			batchStore.mu.Unlock()
			sendAnthropicBatchNotFound(ctx)
			return
		}
		obj := batch.toAnthropicObject(now, requestBaseURL(ctx))
		batchStore.mu.Unlock()

		log.Printf("[messages/batches] retrieve id=%s status=%s", obj.ID, obj.ProcessingStatus)
		writeJSON(ctx, fasthttp.StatusOK, obj)
	case ctx.IsDelete():
		batchStore.mu.Lock()
		batch, ok := batchStore.byID[batchID]
		if !ok || batch.provider != "anthropic" {
			batchStore.mu.Unlock()
			sendAnthropicBatchNotFound(ctx)
			return
		}
		if batch.anthropicStatus(now) != "ended" {
			batchStore.mu.Unlock()
			sendAnthropicAPIError(ctx, fasthttp.StatusBadRequest, "invalid_request_error",
				"Cannot delete a message batch that is in progress. Cancel it first, then delete it once it has ended.")
			return
		}
		delete(batchStore.byID, batchID)
		for i, id := range batchStore.order {
			if id == batchID {
				batchStore.order = append(batchStore.order[:i], batchStore.order[i+1:]...)
				break
			}
		}
		batchStore.mu.Unlock()

		log.Printf("[messages/batches] deleted id=%s", batchID)
		writeJSON(ctx, fasthttp.StatusOK, AnthropicBatchDeleteResponse{ID: batchID, Type: "message_batch_deleted"})
	default:
		sendAnthropicAPIError(ctx, fasthttp.StatusMethodNotAllowed, "invalid_request_error",
			"Method not allowed: "+string(ctx.Method()))
	}
}

// mockAnthropicBatchCancelHandler serves POST
// /v1/messages/batches/{batch_id}/cancel.
func mockAnthropicBatchCancelHandler(ctx *fasthttp.RequestCtx, batchID string) {
	if !checkAuth(ctx) {
		return
	}
	if simulateProviderConditions(ctx, "anthropic") {
		return
	}
	if !ctx.IsPost() {
		sendAnthropicAPIError(ctx, fasthttp.StatusMethodNotAllowed, "invalid_request_error",
			"Method not allowed: "+string(ctx.Method()))
		return
	}

	now := time.Now()

	batchStore.mu.Lock()
	batch, ok := batchStore.byID[batchID]
	if !ok || batch.provider != "anthropic" {
		batchStore.mu.Unlock()
		sendAnthropicBatchNotFound(ctx)
		return
	}
	// Cancelling an already ended batch is a no-op upstream, so report its
	// current state instead of inventing a failure.
	if batch.cancelInitiatedAt == nil && batch.anthropicStatus(now) == "in_progress" {
		batch.cancelProcessed = batch.processedAt(now)
		batch.cancelInitiatedAt = &now
	}
	obj := batch.toAnthropicObject(now, requestBaseURL(ctx))
	batchStore.mu.Unlock()

	log.Printf("[messages/batches] cancel id=%s status=%s", obj.ID, obj.ProcessingStatus)
	writeJSON(ctx, fasthttp.StatusOK, obj)
}

// mockAnthropicBatchResultsHandler serves GET
// /v1/messages/batches/{batch_id}/results as JSONL, one result per line.
func mockAnthropicBatchResultsHandler(ctx *fasthttp.RequestCtx, batchID string) {
	if !checkAuth(ctx) {
		return
	}
	if simulateProviderConditions(ctx, "anthropic") {
		return
	}
	if !ctx.IsGet() {
		sendAnthropicAPIError(ctx, fasthttp.StatusMethodNotAllowed, "invalid_request_error",
			"Method not allowed: "+string(ctx.Method()))
		return
	}

	now := time.Now()

	batchStore.mu.Lock()
	batch, ok := batchStore.byID[batchID]
	if !ok || batch.provider != "anthropic" {
		batchStore.mu.Unlock()
		sendAnthropicBatchNotFound(ctx)
		return
	}
	if batch.anthropicStatus(now) != "ended" {
		batchStore.mu.Unlock()
		sendAnthropicAPIError(ctx, fasthttp.StatusNotFound, "not_found_error",
			"Results are not yet available for this message batch; it is still processing.")
		return
	}
	batch.ensureResults(now)
	results := batch.results
	batchStore.mu.Unlock()

	log.Printf("[messages/batches] results id=%s bytes=%d", batchID, len(results))
	ctx.SetContentType("application/x-jsonl")
	ctx.SetStatusCode(fasthttp.StatusOK)
	ctx.SetBody(results)
}

func sendAnthropicBatchNotFound(ctx *fasthttp.RequestCtx) {
	sendAnthropicAPIError(ctx, fasthttp.StatusNotFound, "not_found_error", "Not found")
}

// ---------------------------------------------------------------------------
// Routing
// ---------------------------------------------------------------------------

// splitManagementPath normalizes a batch/file path into its meaningful
// segments, dropping the optional provider prefix and API version the mocker
// accepts on every route (e.g. "/openai/v1/batches/b/cancel" -> [batches b cancel]).
func splitManagementPath(path string) []string {
	segments := strings.Split(strings.Trim(path, "/"), "/")
	for len(segments) > 0 && segments[0] == "" {
		segments = segments[1:]
	}
	if len(segments) > 0 && (segments[0] == "openai" || segments[0] == "anthropic") {
		segments = segments[1:]
	}
	if len(segments) > 0 && (segments[0] == "v1" || segments[0] == "v1beta") {
		segments = segments[1:]
	}
	return segments
}

// handleManagementRoute dispatches the batch and file endpoints of both
// providers. It reports whether path belonged to one of them.
func handleManagementRoute(ctx *fasthttp.RequestCtx, path string) bool {
	segments := splitManagementPath(path)
	if len(segments) == 0 {
		return false
	}

	switch segments[0] {
	case "files":
		switch {
		case len(segments) == 1:
			mockFilesHandler(ctx)
		case len(segments) == 2:
			mockFileByIDHandler(ctx, segments[1])
		case len(segments) == 3 && segments[2] == "content":
			mockFileContentHandler(ctx, segments[1])
		default:
			return false
		}
		return true

	case "batches":
		switch {
		case len(segments) == 1:
			mockOpenAIBatchesHandler(ctx)
		case len(segments) == 2:
			mockOpenAIBatchRetrieveHandler(ctx, segments[1])
		case len(segments) == 3 && segments[2] == "cancel":
			mockOpenAIBatchCancelHandler(ctx, segments[1])
		default:
			return false
		}
		return true

	case "messages":
		if len(segments) < 2 || segments[1] != "batches" {
			return false
		}
		switch {
		case len(segments) == 2:
			mockAnthropicBatchesHandler(ctx)
		case len(segments) == 3:
			mockAnthropicBatchByIDHandler(ctx, segments[2])
		case len(segments) == 4 && segments[3] == "cancel":
			mockAnthropicBatchCancelHandler(ctx, segments[2])
		case len(segments) == 4 && segments[3] == "results":
			mockAnthropicBatchResultsHandler(ctx, segments[2])
		default:
			return false
		}
		return true
	}

	return false
}

// requestBaseURL reconstructs the origin the caller used, so generated URLs
// (Anthropic's results_url) point back at this mocker.
func requestBaseURL(ctx *fasthttp.RequestCtx) string {
	scheme := string(ctx.URI().Scheme())
	if scheme == "" {
		scheme = "http"
	}
	return scheme + "://" + string(ctx.Host())
}
