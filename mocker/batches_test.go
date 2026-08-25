package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestSplitManagementPath(t *testing.T) {
	cases := []struct {
		path     string
		segments []string
	}{
		{"/v1/batches", []string{"batches"}},
		{"/batches", []string{"batches"}},
		{"/openai/v1/batches/batch_123/cancel", []string{"batches", "batch_123", "cancel"}},
		{"/openai/batches/batch_123", []string{"batches", "batch_123"}},
		{"/v1/messages/batches/msgbatch_123/results", []string{"messages", "batches", "msgbatch_123", "results"}},
		{"/anthropic/v1/messages/batches", []string{"messages", "batches"}},
		{"/v1/files/file-123/content", []string{"files", "file-123", "content"}},
		{"/v1/chat/completions", []string{"chat", "completions"}},
	}
	for _, tc := range cases {
		got := splitManagementPath(tc.path)
		if strings.Join(got, "/") != strings.Join(tc.segments, "/") {
			t.Fatalf("splitManagementPath(%q) = %v, want %v", tc.path, got, tc.segments)
		}
	}
}

func TestRequestFailsSpreadsFailuresEvenly(t *testing.T) {
	original := batchFailurePercent
	defer func() { batchFailurePercent = original }()

	for _, percent := range []int{0, 10, 25, 50, 100} {
		batchFailurePercent = percent
		failed := 0
		for i := 0; i < 200; i++ {
			if requestFails(i) {
				failed++
			}
		}
		if want := 200 * percent / 100; failed != want {
			t.Fatalf("failure-percent %d produced %d failures over 200 requests, want %d", percent, failed, want)
		}
	}
}

// newTestBatch builds a batch of n requests created window ago.
func newTestBatch(provider string, n int, age time.Duration) *mockBatch {
	requests := make([]mockBatchRequest, 0, n)
	for i := 0; i < n; i++ {
		requests = append(requests, mockBatchRequest{customID: "request-" + string(rune('a'+i)), url: "/v1/chat/completions", model: "gpt-4o-mini"})
	}
	createdAt := time.Now().Add(-age)
	return &mockBatch{
		id:               "batch_test",
		provider:         provider,
		endpoint:         "/v1/chat/completions",
		completionWindow: "24h",
		requests:         requests,
		createdAt:        createdAt,
		expiresAt:        createdAt.Add(completionWindowDuration),
	}
}

func TestOpenAIBatchStatusProgression(t *testing.T) {
	original := batchCompletionMs
	batchCompletionMs = 1000
	defer func() { batchCompletionMs = original }()

	cases := []struct {
		age    time.Duration
		status string
	}{
		{0, "validating"},
		{50 * time.Millisecond, "validating"},
		{500 * time.Millisecond, "in_progress"},
		{950 * time.Millisecond, "finalizing"},
		{1500 * time.Millisecond, "completed"},
	}
	for _, tc := range cases {
		batch := newTestBatch("openai", 10, tc.age)
		if got := batch.openAIStatus(time.Now()); got != tc.status {
			t.Fatalf("age %v: status = %q, want %q", tc.age, got, tc.status)
		}
	}
}

func TestBatchCompletesImmediatelyWithZeroWindow(t *testing.T) {
	original := batchCompletionMs
	batchCompletionMs = 0
	defer func() { batchCompletionMs = original }()

	batch := newTestBatch("openai", 5, 0)
	if got := batch.openAIStatus(time.Now()); got != "completed" {
		t.Fatalf("openAIStatus = %q, want completed", got)
	}
	if got := batch.processedAt(time.Now()); got != 5 {
		t.Fatalf("processedAt = %d, want 5", got)
	}

	anthropicBatch := newTestBatch("anthropic", 5, 0)
	if got := anthropicBatch.anthropicStatus(time.Now()); got != "ended" {
		t.Fatalf("anthropicStatus = %q, want ended", got)
	}
}

func TestCancelFreezesProgress(t *testing.T) {
	originalWindow, originalFailures := batchCompletionMs, batchFailurePercent
	batchCompletionMs, batchFailurePercent = 1000, 0
	defer func() { batchCompletionMs, batchFailurePercent = originalWindow, originalFailures }()

	batch := newTestBatch("openai", 10, 500*time.Millisecond)
	now := time.Now()
	processed := batch.processedAt(now)
	batch.cancelProcessed = processed
	batch.cancelInitiatedAt = &now

	if got := batch.openAIStatus(now); got != "cancelling" {
		t.Fatalf("status right after cancel = %q, want cancelling", got)
	}
	later := now.Add(time.Second)
	if got := batch.openAIStatus(later); got != "cancelled" {
		t.Fatalf("status after the cancel window = %q, want cancelled", got)
	}
	if got := batch.processedAt(later); got != processed {
		t.Fatalf("processedAt kept advancing after cancel: %d, want %d", got, processed)
	}
}

func TestParseOpenAIBatchInput(t *testing.T) {
	input := strings.Join([]string{
		`{"custom_id":"request-1","method":"POST","url":"/v1/chat/completions","body":{"model":"openai/gpt-4o"}}`,
		`{"custom_id":"request-2","method":"POST","url":"/v1/chat/completions","body":{"model":"gpt-4o-mini"}}`,
	}, "\n")

	requests, errors := parseOpenAIBatchInput([]byte(input), "/v1/chat/completions")
	if len(errors) != 0 {
		t.Fatalf("valid input produced errors: %+v", errors)
	}
	if len(requests) != 2 {
		t.Fatalf("parsed %d requests, want 2", len(requests))
	}
	// provider/model prefixes are stripped the same way the inference handlers do.
	if requests[0].model != "gpt-4o" {
		t.Fatalf("model = %q, want gpt-4o", requests[0].model)
	}
}

func TestParseOpenAIBatchInputValidationErrors(t *testing.T) {
	cases := []struct {
		name  string
		input string
		code  string
	}{
		{"unparseable line", `{"custom_id":"a"`, "invalid_json_line"},
		{"missing custom_id", `{"url":"/v1/chat/completions","body":{}}`, "missing_required_parameter"},
		{"duplicate custom_id", "{\"custom_id\":\"a\",\"body\":{}}\n{\"custom_id\":\"a\",\"body\":{}}", "duplicate_custom_id"},
		{"mismatched url", `{"custom_id":"a","url":"/v1/embeddings","body":{}}`, "invalid_url"},
		{"empty file", "", "empty_file"},
	}
	for _, tc := range cases {
		_, errors := parseOpenAIBatchInput([]byte(tc.input), "/v1/chat/completions")
		if len(errors) == 0 {
			t.Fatalf("%s: expected a validation error", tc.name)
		}
		if errors[len(errors)-1].Code != tc.code {
			t.Fatalf("%s: error code = %q, want %q", tc.name, errors[len(errors)-1].Code, tc.code)
		}
	}
}

func TestOpenAIBatchObjectShape(t *testing.T) {
	originalWindow := batchCompletionMs
	batchCompletionMs = 0
	defer func() { batchCompletionMs = originalWindow }()

	batch := newTestBatch("openai", 2, 0)
	encoded, err := json.Marshal(batch.toOpenAIObject(time.Now()))
	if err != nil {
		t.Fatalf("failed to encode batch: %v", err)
	}

	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("failed to decode batch: %v", err)
	}

	// The real API always emits these keys, using null for the ones that do not
	// apply, so clients can rely on their presence.
	for _, field := range []string{
		"id", "object", "endpoint", "errors", "input_file_id", "completion_window", "status",
		"output_file_id", "error_file_id", "created_at", "in_progress_at", "expires_at",
		"finalizing_at", "completed_at", "failed_at", "expired_at", "cancelling_at",
		"cancelled_at", "request_counts", "metadata",
	} {
		if _, ok := decoded[field]; !ok {
			t.Fatalf("batch object is missing field %q", field)
		}
	}
	if string(decoded["object"]) != `"batch"` {
		t.Fatalf("object = %s, want \"batch\"", decoded["object"])
	}
}

func TestAnthropicBatchObjectShape(t *testing.T) {
	originalWindow := batchCompletionMs
	batchCompletionMs = 0
	defer func() { batchCompletionMs = originalWindow }()

	batch := newTestBatch("anthropic", 2, 0)
	object := batch.toAnthropicObject(time.Now(), "http://localhost:8000")

	if object.Type != "message_batch" {
		t.Fatalf("type = %q, want message_batch", object.Type)
	}
	if object.ProcessingStatus != "ended" {
		t.Fatalf("processing_status = %q, want ended", object.ProcessingStatus)
	}
	if object.ResultsURL == nil || !strings.HasSuffix(*object.ResultsURL, "/v1/messages/batches/batch_test/results") {
		t.Fatalf("results_url = %v, want it to point at the results endpoint", object.ResultsURL)
	}
	if _, err := time.Parse(time.RFC3339Nano, object.CreatedAt); err != nil {
		t.Fatalf("created_at %q is not an RFC 3339 timestamp: %v", object.CreatedAt, err)
	}
	if object.RequestCounts.Succeeded != 2 || object.RequestCounts.Processing != 0 {
		t.Fatalf("request_counts = %+v, want 2 succeeded and 0 processing", object.RequestCounts)
	}
}

func TestBuildAnthropicResultsCoversEveryRequest(t *testing.T) {
	originalWindow, originalFailures := batchCompletionMs, batchFailurePercent
	batchCompletionMs, batchFailurePercent = 0, 50
	defer func() { batchCompletionMs, batchFailurePercent = originalWindow, originalFailures }()

	batch := newTestBatch("anthropic", 4, 0)
	lines := strings.Split(strings.TrimSpace(string(buildAnthropicResults(batch, 2))), "\n")
	if len(lines) != 4 {
		t.Fatalf("got %d result lines, want one per request (4)", len(lines))
	}

	wantTypes := []string{"succeeded", "errored", "expired", "expired"}
	for i, line := range lines {
		var result AnthropicBatchResultLine
		if err := json.Unmarshal([]byte(line), &result); err != nil {
			t.Fatalf("result line %d is not valid JSON: %v", i, err)
		}
		if result.Result.Type != wantTypes[i] {
			t.Fatalf("result %d type = %q, want %q", i, result.Result.Type, wantTypes[i])
		}
		if result.CustomID != batch.requests[i].customID {
			t.Fatalf("result %d custom_id = %q, want %q", i, result.CustomID, batch.requests[i].customID)
		}
	}
}

func TestBuildOpenAIBatchOutputSplitsSuccessesAndFailures(t *testing.T) {
	originalFailures := batchFailurePercent
	batchFailurePercent = 50
	defer func() { batchFailurePercent = originalFailures }()

	batch := newTestBatch("openai", 4, 0)
	output, errorOutput := buildOpenAIBatchOutput(batch, 4)

	countLines := func(body []byte) int {
		trimmed := strings.TrimSpace(string(body))
		if trimmed == "" {
			return 0
		}
		return len(strings.Split(trimmed, "\n"))
	}
	if got := countLines(output); got != 2 {
		t.Fatalf("output file has %d lines, want 2", got)
	}
	if got := countLines(errorOutput); got != 2 {
		t.Fatalf("error file has %d lines, want 2", got)
	}

	var line OpenAIBatchOutputLine
	if err := json.Unmarshal([]byte(strings.Split(strings.TrimSpace(string(output)), "\n")[0]), &line); err != nil {
		t.Fatalf("output line is not valid JSON: %v", err)
	}
	if line.Response == nil || line.Response.StatusCode != 200 {
		t.Fatalf("successful output line = %+v, want a 200 response", line.Response)
	}
	if !strings.HasPrefix(line.ID, "batch_req_") {
		t.Fatalf("output line id = %q, want a batch_req_ prefix", line.ID)
	}
}

func TestPaginate(t *testing.T) {
	cases := []struct {
		size, limit, cursor int
		start, end          int
		hasMore             bool
	}{
		{10, 20, -1, 0, 10, false},
		{10, 3, -1, 0, 3, true},
		{10, 3, 2, 3, 6, true},
		{10, 5, 4, 5, 10, false},
		{10, 5, 9, 10, 10, false},
	}
	for _, tc := range cases {
		start, end, hasMore := paginate(tc.size, tc.limit, tc.cursor)
		if start != tc.start || end != tc.end || hasMore != tc.hasMore {
			t.Fatalf("paginate(%d,%d,%d) = (%d,%d,%v), want (%d,%d,%v)",
				tc.size, tc.limit, tc.cursor, start, end, hasMore, tc.start, tc.end, tc.hasMore)
		}
	}
}
