// Package lib provides utility functions and shared types for the Bifrost gateway,
// including account management and debug handlers.
package lib

import (
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	bifrost "github.com/maximhq/bifrost/core"
	"github.com/maximhq/bifrost/core/schemas"
	"github.com/valyala/fasthttp"
)

// RequestMetrics holds timing metrics extracted from Bifrost's internal request processing.
// These metrics provide insights into different stages within the Bifrost client.
type RequestMetrics struct {
	QueueWaitTime    time.Duration `json:"queue_wait_time"`    // Time spent waiting in the request queue.
	KeySelectionTime time.Duration `json:"key_selection_time"` // Time taken to select an API key.
	PluginPreTime    time.Duration `json:"plugin_pre_time"`    // Time spent in pre-request plugins.
	PluginPostTime   time.Duration `json:"plugin_post_time"`   // Time spent in post-request plugins.
	RequestCount     int64         `json:"request_count"`      // Count of requests processed (may be specific to a context if metrics are aggregated).
	ErrorCount       int64         `json:"error_count"`        // Count of errors encountered (similarly context-specific).
}

// ProviderMetrics holds provider-specific timing metrics, detailing stages of request processing
// by the underlying provider client within Bifrost (e.g., OpenAI client).
type ProviderMetrics struct {
	MessageFormatting      time.Duration `json:"message_formatting"`       // Time spent formatting messages for the provider.
	ParamsPreparation      time.Duration `json:"params_preparation"`       // Time spent preparing request parameters.
	RequestBodyPreparation time.Duration `json:"request_body_preparation"` // Time spent preparing the HTTP request body.
	JSONMarshaling         time.Duration `json:"json_marshaling"`          // Time spent marshaling JSON payloads.
	RequestSetup           time.Duration `json:"request_setup"`            // Time for overall request setup before HTTP call.
	HTTPRequest            time.Duration `json:"http_request"`             // Time taken for the actual HTTP request to the provider.
	ErrorHandling          time.Duration `json:"error_handling"`           // Time spent in error handling routines.
	ResponseParsing        time.Duration `json:"response_parsing"`         // Time spent parsing the provider's response.
	RequestSizeInBytes     int64         `json:"request_size_in_bytes"`    // Size of the request payload sent to the provider.
	ResponseSizeInBytes    int64         `json:"response_size_in_bytes"`   // Size of the response payload received from the provider.
}

// TimingStats aggregates RequestMetrics and ProviderMetrics from multiple requests
// to calculate and display average performance statistics.
type TimingStats struct {
	mu              sync.Mutex        // Mutex to protect concurrent access to stats fields.
	totalRequests   int               // Total number of requests for which stats have been recorded.
	metrics         []RequestMetrics  // Slice of Bifrost RequestMetrics from each request.
	timings         []time.Duration   // Slice of overall request durations (currently not directly populated here but structure exists).
	providerMetrics []ProviderMetrics // Slice of ProviderMetrics from each request.
}

// ServerMetrics tracks high-level server operational metrics such as total requests,
// errors, and last error details.
type ServerMetrics struct {
	mu                 sync.Mutex // Mutex to protect concurrent access to server metrics fields.
	TotalRequests      int64      // Total number of requests received by the server.
	SuccessfulRequests int64      // Number of requests that completed successfully.
	DroppedRequests    int64      // Number of requests dropped (e.g., due to timeout).
	QueueSize          int64      // Current or peak queue size (if applicable, currently not directly tracked here).
	ErrorCount         int64      // Total number of errors encountered during request processing.
	LastError          error      // The last error encountered by the server.
	LastErrorTime      time.Time  // Timestamp of the last error.
}

var (
	// stats is a global instance of TimingStats used to collect performance data.
	stats = &TimingStats{}
	// serverMetrics is a global instance of ServerMetrics for tracking server operation.
	serverMetrics = &ServerMetrics{}
)

// formatSmartDuration converts a nanosecond duration into a human-readable string,
// automatically selecting appropriate units (s, ms, µs, ns).
// Parameters:
//
//	ns: Duration in nanoseconds.
//
// Returns a formatted string representation of the duration.
func formatSmartDuration(ns int64) string {
	avg := float64(ns)
	switch {
	case avg >= 1e9:
		return fmt.Sprintf("%.2f s", avg/1e9)
	case avg >= 1e6:
		return fmt.Sprintf("%.2f ms", avg/1e6)
	case avg >= 1e3:
		return fmt.Sprintf("%.2f µs", avg/1e3)
	default:
		return fmt.Sprintf("%d ns", ns)
	}
}

// PrintStats calculates and prints aggregated average timing statistics based on
// the data collected in the global `stats` variable. It also prints server metrics.
// This function is typically called when the server is shutting down in debug mode.
func PrintStats() {
	stats.mu.Lock()
	defer stats.mu.Unlock()

	if stats.totalRequests == 0 {
		fmt.Println("No requests processed to calculate PrintStats")
		return
	}

	// Calculate averages for Bifrost metrics
	var totalMetrics RequestMetrics
	numMetricsRecords := len(stats.metrics)
	if numMetricsRecords > 0 {
		for _, m := range stats.metrics {
			totalMetrics.QueueWaitTime += m.QueueWaitTime
			totalMetrics.KeySelectionTime += m.KeySelectionTime
			totalMetrics.PluginPreTime += m.PluginPreTime
			totalMetrics.PluginPostTime += m.PluginPostTime
			totalMetrics.RequestCount += m.RequestCount // Note: This sums up counts from individual reports
			totalMetrics.ErrorCount += m.ErrorCount     // Note: This sums up counts from individual reports
		}
	}

	// Calculate averages for provider timings
	var totalProviderMetrics ProviderMetrics
	numProviderMetricsRecords := len(stats.providerMetrics)
	if numProviderMetricsRecords > 0 {
		for _, t := range stats.providerMetrics {
			totalProviderMetrics.MessageFormatting += t.MessageFormatting
			totalProviderMetrics.ParamsPreparation += t.ParamsPreparation
			totalProviderMetrics.RequestBodyPreparation += t.RequestBodyPreparation
			totalProviderMetrics.JSONMarshaling += t.JSONMarshaling
			totalProviderMetrics.RequestSetup += t.RequestSetup
			totalProviderMetrics.HTTPRequest += t.HTTPRequest
			totalProviderMetrics.ErrorHandling += t.ErrorHandling
			totalProviderMetrics.ResponseParsing += t.ResponseParsing
			totalProviderMetrics.RequestSizeInBytes += t.RequestSizeInBytes
			totalProviderMetrics.ResponseSizeInBytes += t.ResponseSizeInBytes
		}
	}

	// Calculate averages for overall timings (currently not directly populated in DebugHandler)
	var totalTimings time.Duration
	numOverallTimings := len(stats.timings)
	if numOverallTimings > 0 {
		for _, t := range stats.timings {
			totalTimings += t
		}
	}

	// Print final metrics
	serverMetrics.mu.Lock()
	fmt.Printf("\nServer Metrics:\n")
	fmt.Printf("Total Requests Handled by Server: %d\n", serverMetrics.TotalRequests)
	fmt.Printf("Successful Requests: %d\n", serverMetrics.SuccessfulRequests)
	fmt.Printf("Dropped Requests (e.g., timeout): %d\n", serverMetrics.DroppedRequests)
	fmt.Printf("Total Error Count: %d\n", serverMetrics.ErrorCount)
	if serverMetrics.LastError != nil {
		fmt.Printf("Last Error: %v\n", serverMetrics.LastError)
		fmt.Printf("Last Error Time: %v\n", serverMetrics.LastErrorTime)
	} else {
		fmt.Println("Last Error: None")
	}
	serverMetrics.mu.Unlock()

	fmt.Printf("\nAggregated Timing Statistics (based on %d recorded requests with metrics):\n", stats.totalRequests)

	fmt.Printf("\nBifrost Core Metrics (averages per request with metrics):\n")
	if numMetricsRecords > 0 {
		fmt.Printf("Queue Wait Time: %s\n", formatSmartDuration(totalMetrics.QueueWaitTime.Nanoseconds()/int64(numMetricsRecords)))
		fmt.Printf("Key Selection Time: %s\n", formatSmartDuration(totalMetrics.KeySelectionTime.Nanoseconds()/int64(numMetricsRecords)))
		fmt.Printf("Plugin Pre Time: %s\n", formatSmartDuration(totalMetrics.PluginPreTime.Nanoseconds()/int64(numMetricsRecords)))
		fmt.Printf("Plugin Post Time: %s\n", formatSmartDuration(totalMetrics.PluginPostTime.Nanoseconds()/int64(numMetricsRecords)))
	} else {
		fmt.Println("No Bifrost core timing data available.")
	}

	fmt.Printf("\nProvider Client Metrics (averages per request with metrics):\n")
	if numProviderMetricsRecords > 0 {
		fmt.Printf("Message Formatting: %s\n", formatSmartDuration(totalProviderMetrics.MessageFormatting.Nanoseconds()/int64(numProviderMetricsRecords)))
		fmt.Printf("Params Preparation: %s\n", formatSmartDuration(totalProviderMetrics.ParamsPreparation.Nanoseconds()/int64(numProviderMetricsRecords)))
		fmt.Printf("Request Body Preparation: %s\n", formatSmartDuration(totalProviderMetrics.RequestBodyPreparation.Nanoseconds()/int64(numProviderMetricsRecords)))
		fmt.Printf("JSON Marshaling: %s\n", formatSmartDuration(totalProviderMetrics.JSONMarshaling.Nanoseconds()/int64(numProviderMetricsRecords)))
		fmt.Printf("Request Setup: %s\n", formatSmartDuration(totalProviderMetrics.RequestSetup.Nanoseconds()/int64(numProviderMetricsRecords)))
		fmt.Printf("HTTP Request to Provider: %s\n", formatSmartDuration(totalProviderMetrics.HTTPRequest.Nanoseconds()/int64(numProviderMetricsRecords)))
		fmt.Printf("Error Handling (provider client): %s\n", formatSmartDuration(totalProviderMetrics.ErrorHandling.Nanoseconds()/int64(numProviderMetricsRecords)))
		fmt.Printf("Response Parsing (provider client): %s\n", formatSmartDuration(totalProviderMetrics.ResponseParsing.Nanoseconds()/int64(numProviderMetricsRecords)))
		fmt.Printf("Avg Request Size to Provider: %.2f KB\n", float64(totalProviderMetrics.RequestSizeInBytes)/float64(numProviderMetricsRecords)/1024.0)
		fmt.Printf("Avg Response Size from Provider: %.2f KB\n", float64(totalProviderMetrics.ResponseSizeInBytes)/float64(numProviderMetricsRecords)/1024.0)
	} else {
		fmt.Println("No provider client timing data available.")
	}

	// Only calculate average timings if we have data
	if len(stats.timings) > 0 {
		avgTimings := float64(totalTimings) / float64(len(stats.timings)) / float64(time.Nanosecond)
		fmt.Printf("\nAverage Timings: %.2f ms\n", avgTimings)
	}
}

// ChatRequest defines the expected structure for incoming chat completion requests to the debug handler.
type ChatRequest struct {
	Messages []schemas.Message `json:"messages"` // A list of messages comprising the conversation so far.
	Model    string            `json:"model"`    // ID of the model to use (e.g., "gpt-4o-mini").
}

// DebugHandler is a fasthttp.RequestHandler that wraps the Bifrost client's ChatCompletionRequest method.
// It captures detailed timing metrics from Bifrost and the provider, records them, and handles timeouts.
// This handler is intended for use in debug mode to provide insights into request processing performance.
// Parameters:
//
//	client: An initialized Bifrost client instance.
//
// Returns a fasthttp.RequestHandler function.
func DebugHandler(client *bifrost.Bifrost) func(ctx *fasthttp.RequestCtx) {
	return func(ctx *fasthttp.RequestCtx) {
		// Track incoming request for server metrics
		serverMetrics.mu.Lock()
		serverMetrics.TotalRequests++
		serverMetrics.mu.Unlock()

		var chatReq ChatRequest
		if err := json.Unmarshal(ctx.PostBody(), &chatReq); err != nil {
			serverMetrics.mu.Lock()
			serverMetrics.ErrorCount++
			serverMetrics.LastError = fmt.Errorf("invalid request format: %v", err)
			serverMetrics.LastErrorTime = time.Now()
			serverMetrics.mu.Unlock()

			ctx.SetStatusCode(fasthttp.StatusBadRequest)
			ctx.SetBodyString(fmt.Sprintf("invalid request format: %v", err))
			return
		}

		if len(chatReq.Messages) == 0 {
			serverMetrics.mu.Lock()
			serverMetrics.ErrorCount++
			serverMetrics.LastError = fmt.Errorf("messages array is required")
			serverMetrics.LastErrorTime = time.Now()
			serverMetrics.mu.Unlock()

			ctx.SetStatusCode(fasthttp.StatusBadRequest)
			ctx.SetBodyString("Messages array is required")
			return
		}

		// Create Bifrost request object.
		bifrostReq := &schemas.BifrostRequest{
			Provider: schemas.OpenAI, // Assuming OpenAI provider for this handler
			Model:    chatReq.Model,
			Input: schemas.RequestInput{
				ChatCompletionInput: &chatReq.Messages,
			},
		}

		// Make Bifrost API call with a timeout.
		done := make(chan struct{}) // Channel to signal completion of the Bifrost request.
		var bifrostResp *schemas.BifrostResponse
		var bifrostErr *schemas.BifrostError

		go func() {
			// Call Bifrost client; this is a blocking call.
			bifrostResp, bifrostErr = client.ChatCompletionRequest(ctx, bifrostReq)
			close(done) // Signal that the call has returned.
		}()

		select {
		case <-done:
			// Request completed (successfully or with an error from Bifrost).
		case <-time.After(30 * time.Second): // Timeout for the entire operation.
			serverMetrics.mu.Lock()
			serverMetrics.DroppedRequests++
			serverMetrics.LastError = fmt.Errorf("request timed out after 30 seconds in DebugHandler")
			serverMetrics.LastErrorTime = time.Now()
			serverMetrics.mu.Unlock()

			ctx.SetStatusCode(fasthttp.StatusGatewayTimeout)
			ctx.SetBodyString("Request timed out")
			return
		}

		if bifrostErr != nil {
			serverMetrics.mu.Lock()
			serverMetrics.ErrorCount++
			serverMetrics.LastError = bifrostErr.Error.Error
			serverMetrics.LastErrorTime = time.Now()
			serverMetrics.mu.Unlock()

			ctx.SetStatusCode(fasthttp.StatusInternalServerError)
			ctx.SetContentType("application/json")
			ctx.SetStatusCode(fasthttp.StatusInternalServerError)
			json.NewEncoder(ctx).Encode(bifrostErr)
			return
		}

		// Track successful request.
		serverMetrics.mu.Lock()
		serverMetrics.SuccessfulRequests++
		serverMetrics.mu.Unlock()

		// Extract timing information from response if Bifrost is in debug mode and provides it.
		stats.mu.Lock()
		stats.totalRequests++ // Increment for stats calculation base

		if rawResponse, ok := bifrostResp.ExtraFields.RawResponse.(map[string]interface{}); ok {
			// Process bifrost_timings
			if metrics, ok := rawResponse["bifrost_timings"]; ok {
				// Convert to JSON bytes first
				jsonBytes, err := json.Marshal(metrics)
				if err != nil {
					fmt.Printf("Error marshaling bifrost_timings: %v\n", err)
					return
				}
				// Unmarshal into RequestMetrics
				var requestMetrics RequestMetrics
				if err := json.Unmarshal(jsonBytes, &requestMetrics); err != nil {
					fmt.Printf("Error unmarshaling bifrost_timings: %v\n", err)
					return
				}
				stats.metrics = append(stats.metrics, requestMetrics)
			}

			// Process provider_metrics
			if metrics, ok := rawResponse["provider_metrics"]; ok {
				// Convert to JSON bytes first
				jsonBytes, err := json.Marshal(metrics)
				if err != nil {
					fmt.Printf("Error marshaling provider_metrics: %v\n", err)
					return
				}

				// Unmarshal into ProviderMetrics
				var providerMetrics ProviderMetrics
				if err := json.Unmarshal(jsonBytes, &providerMetrics); err != nil {
					fmt.Printf("Error unmarshaling provider_metrics: %v\n", err)
					return
				}

				stats.providerMetrics = append(stats.providerMetrics, providerMetrics)
			}
		}

		stats.mu.Unlock()

		// Send response to the original caller.
		ctx.SetContentType("application/json")

		// Add recovery to prevent panics during JSON encoding of the final response.
		defer func() {
			if r := recover(); r != nil {
				ctx.SetStatusCode(fasthttp.StatusInternalServerError)
				ctx.SetBodyString(fmt.Sprintf("Panic during response encoding: %v", r))
				log.Printf("Panic during response encoding: %v", r)
			}
		}()

		// Additional safety check before encoding.
		if bifrostResp == nil {
			// This case should ideally be caught by bifrostErr != nil check, but as a safeguard:
			log.Println("Error: DebugHandler reached point of encoding nil bifrostResp when bifrostErr was also nil.")
			ctx.SetStatusCode(fasthttp.StatusInternalServerError)
			ctx.SetBodyString("Error: nil response from Bifrost and no explicit error reported")
			return
		}

		// Encode the final response to be sent to the client.
		if err := json.NewEncoder(ctx).Encode(bifrostResp); err != nil {
			ctx.SetStatusCode(fasthttp.StatusInternalServerError)
			ctx.SetBodyString(fmt.Sprintf("Error encoding response: %v", err))
			log.Printf("Error encoding response: %v", err)
		}
	}
}

// GetMetricsHandler creates a fasthttp.RequestHandler that serves current server operational metrics.
// These metrics are collected in the global `serverMetrics` variable.
// The response is JSON formatted.
func GetMetricsHandler() func(ctx *fasthttp.RequestCtx) {
	return func(ctx *fasthttp.RequestCtx) {
		serverMetrics.mu.Lock()
		defer serverMetrics.mu.Unlock()

		metrics := map[string]interface{}{
			"total_requests":      serverMetrics.TotalRequests,
			"successful_requests": serverMetrics.SuccessfulRequests,
			"dropped_requests":    serverMetrics.DroppedRequests,
			"error_count":         serverMetrics.ErrorCount,
			"last_error":          serverMetrics.LastError,
			"last_error_time":     serverMetrics.LastErrorTime,
			"current_time":        time.Now(),
		}

		ctx.SetContentType("application/json")
		json.NewEncoder(ctx).Encode(metrics)
	}
}
