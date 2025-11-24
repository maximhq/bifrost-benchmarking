package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"strings"
	"time"

	"github.com/maximhq/bifrost/core/schemas"
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

var (
	host           string
	port           int
	latency        int
	jitter         int
	bigPayload     bool
	auth           string
	failurePercent int
	failureJitter  int
)

func init() {
	flag.StringVar(&host, "host", "localhost", "Host address to bind the mock server")
	flag.IntVar(&port, "port", 8000, "Port for the mock server to listen on")
	flag.IntVar(&latency, "latency", 0, "Latency in milliseconds to simulate")
	flag.IntVar(&jitter, "jitter", 0, "Maximum jitter in milliseconds to add to latency (±jitter)")
	flag.BoolVar(&bigPayload, "big-payload", false, "Use big payload")
	flag.StringVar(&auth, "auth", "", "Add authentication header key")
	flag.IntVar(&failurePercent, "failure-percent", 0, "Base failure percentage (0-100)")
	flag.IntVar(&failureJitter, "failure-jitter", 0, "Maximum jitter in percentage points to add to failure rate (±failure-jitter)")
}

// StrPtr creates a pointer to a string value.
func StrPtr(s string) *string {
	return &s
}

func mockChatCompletionsHandler(w http.ResponseWriter, r *http.Request) {
	// Check authentication header
	if auth != "" {
		// Check Authorization header
		authorizationHeader := r.Header.Get("Authorization")
		if authorizationHeader == "" {
			http.Error(w, "Forbidden: Missing authentication header 'Authorization'", http.StatusForbidden)
			return
		}
		if authorizationHeader != auth {
			log.Printf("Invalid authentication header 'Authorization': %s", authorizationHeader)
			http.Error(w, "Forbidden: Invalid authentication header 'Authorization'", http.StatusForbidden)
			return
		}
	}

	if r.Method != http.MethodPost {
		http.Error(w, "Only POST method is allowed", http.StatusMethodNotAllowed)
		return
	}

	// Check if this request should fail based on failure percentage with jitter
	if failurePercent > 0 {
		actualFailurePercent := failurePercent
		if failureJitter > 0 {
			// Add random jitter: ±failureJitter percentage points
			jitterOffset := rand.Intn(2*failureJitter+1) - failureJitter
			actualFailurePercent += jitterOffset
			// Ensure failure percentage stays within 0-100 bounds
			if actualFailurePercent < 0 {
				actualFailurePercent = 0
			}
			if actualFailurePercent > 100 {
				actualFailurePercent = 100
			}
		}

		// Generate random number 0-99 and check if it's less than failure percentage
		if actualFailurePercent > 0 && rand.Intn(100) < actualFailurePercent {
			// Return error response
			errorResp := OpenAIError{
				EventID: StrPtr("evt_mock_error_12345"),
				Error: &ErrorField{
					Type:    StrPtr("server_error"),
					Code:    StrPtr("internal_server_error"),
					Message: "The server had an error while processing your request. Sorry about that!",
				},
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			if err := json.NewEncoder(w).Encode(errorResp); err != nil {
				log.Printf("Error encoding error response: %v", err)
				http.Error(w, "Failed to encode error response", http.StatusInternalServerError)
			}
			return
		}
	}

	// Simulate latency with optional jitter
	if latency > 0 || jitter > 0 {
		actualLatency := latency
		if jitter > 0 {
			// Add random jitter: ±jitter milliseconds
			jitterOffset := rand.Intn(2*jitter+1) - jitter
			actualLatency += jitterOffset
			// Ensure latency doesn't go negative
			if actualLatency < 0 {
				actualLatency = 0
			}
		}
		if actualLatency > 0 {
			time.Sleep(time.Duration(actualLatency) * time.Millisecond)
		}
	}

	mockContent := "This is a mocked response from the OpenAI mocker server."
	if bigPayload {
		// Repeat content to generate approximately 10KB response
		// Each repetition is ~55 chars, so ~182 repetitions ≈ 10KB
		mockContent = strings.Repeat(mockContent, 182)
	}

	// Create a mock response
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
		Model:   "gpt-4o-mini",
		Choices: []schemas.BifrostResponseChoice{mockChoice},
		Usage: schemas.LLMUsage{
			PromptTokens:     randomInputTokens,
			CompletionTokens: randomOutputTokens,
			TotalTokens:      randomInputTokens + randomOutputTokens,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(mockResp); err != nil {
		log.Printf("Error encoding mock response: %v", err)
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
	}
}

func mockResponsesHandler(w http.ResponseWriter, r *http.Request) {
	// Check authentication header
	if auth != "" {
		authorizationHeader := r.Header.Get("Authorization")
		if authorizationHeader == "" {
			http.Error(w, "Forbidden: Missing authentication header 'Authorization'", http.StatusForbidden)
			return
		}
		if authorizationHeader != auth {
			log.Printf("Invalid authentication header 'Authorization': %s", authorizationHeader)
			http.Error(w, "Forbidden: Invalid authentication header 'Authorization'", http.StatusForbidden)
			return
		}
	}

	if r.Method != http.MethodPost {
		http.Error(w, "Only POST method is allowed", http.StatusMethodNotAllowed)
		return
	}

	// Failure simulation
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
		if actualFailurePercent > 0 && rand.Intn(100) < actualFailurePercent {
			errorResp := OpenAIError{
				EventID: StrPtr("evt_mock_error_12345"),
				Error: &ErrorField{
					Type:    StrPtr("server_error"),
					Code:    StrPtr("internal_server_error"),
					Message: "The server had an error while processing your request. Sorry about that!",
				},
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			if err := json.NewEncoder(w).Encode(errorResp); err != nil {
				log.Printf("Error encoding error response: %v", err)
				http.Error(w, "Failed to encode error response", http.StatusInternalServerError)
			}
			return
		}
	}

	// Simulate latency with optional jitter
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
		Model:   "gpt-4o-mini",
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

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Printf("Error encoding mock response: %v", err)
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
	}
}

func main() {
	flag.Parse()

	// Register handlers (include OpenAI-compatible paths)
	http.HandleFunc("/chat/completions", mockChatCompletionsHandler)
	http.HandleFunc("/v1/chat/completions", mockChatCompletionsHandler)
	http.HandleFunc("/responses", mockResponsesHandler)
	http.HandleFunc("/v1/responses", mockResponsesHandler)

	addr := fmt.Sprintf("%s:%d", host, port)
	if jitter > 0 {
		log.Printf("Mock OpenAI server starting on %s with latency %dms ±%dms jitter...\n", addr, latency, jitter)
	} else {
		log.Printf("Mock OpenAI server starting on %s with latency %dms...\n", addr, latency)
	}
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
