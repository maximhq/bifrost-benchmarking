package main

import (
	"github.com/valyala/fasthttp"
)

// Provider-native error envelopes for the management-plane endpoints (batches
// and files). The inference handlers share a single OpenAI-shaped error body;
// batch clients parse errors per provider, so these mirror each provider's own
// error format exactly.

// OpenAIAPIErrorBody is the "error" member of an OpenAI error response.
type OpenAIAPIErrorBody struct {
	Message string  `json:"message"`
	Type    string  `json:"type"`
	Param   *string `json:"param"`
	Code    *string `json:"code"`
}

// OpenAIAPIError is an OpenAI error response.
type OpenAIAPIError struct {
	Error OpenAIAPIErrorBody `json:"error"`
}

// AnthropicAPIErrorBody is the "error" member of an Anthropic error response.
type AnthropicAPIErrorBody struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// AnthropicAPIError is an Anthropic error response.
type AnthropicAPIError struct {
	Type      string                `json:"type"` // always "error"
	Error     AnthropicAPIErrorBody `json:"error"`
	RequestID *string               `json:"request_id,omitempty"`
}

// sendOpenAIAPIError writes an OpenAI-shaped error response.
func sendOpenAIAPIError(ctx *fasthttp.RequestCtx, statusCode int, errorType string, message string, param *string, code *string) {
	writeJSON(ctx, statusCode, OpenAIAPIError{Error: OpenAIAPIErrorBody{
		Message: message,
		Type:    errorType,
		Param:   param,
		Code:    code,
	}})
}

// sendAnthropicAPIError writes an Anthropic-shaped error response.
func sendAnthropicAPIError(ctx *fasthttp.RequestCtx, statusCode int, errorType string, message string) {
	writeJSON(ctx, statusCode, AnthropicAPIError{
		Type:      "error",
		Error:     AnthropicAPIErrorBody{Type: errorType, Message: message},
		RequestID: StrPtr(randomID("req_", 24)),
	})
}

// sendProviderRateLimitError writes a 429 in the provider's own error format.
func sendProviderRateLimitError(ctx *fasthttp.RequestCtx, provider string) {
	if provider == "anthropic" {
		sendAnthropicAPIError(ctx, fasthttp.StatusTooManyRequests, "rate_limit_error",
			"Number of request tokens has exceeded your rate limit. Please try again later.")
		return
	}
	sendOpenAIAPIError(ctx, fasthttp.StatusTooManyRequests, "rate_limit_error",
		"Rate limit exceeded. Please retry after some time.", nil, StrPtr("rate_limit_exceeded"))
}

// sendProviderServerError writes a 500 in the provider's own error format.
func sendProviderServerError(ctx *fasthttp.RequestCtx, provider string) {
	if provider == "anthropic" {
		sendAnthropicAPIError(ctx, fasthttp.StatusInternalServerError, "api_error",
			"The server had an error while processing your request. Sorry about that!")
		return
	}
	sendOpenAIAPIError(ctx, fasthttp.StatusInternalServerError, "server_error",
		"The server had an error while processing your request. Sorry about that!", nil, nil)
}

// simulateProviderConditions applies the mocker's shared rate-limit, failure and
// latency simulation to a management-plane call, so batch and file endpoints
// behave under load exactly like the inference ones. It reports whether it has
// already written an error response.
func simulateProviderConditions(ctx *fasthttp.RequestCtx, provider string) bool {
	authHeader := string(ctx.Request.Header.Peek("Authorization"))

	if isKeyRateLimited(ctx) || shouldTriggerTPM(authHeader) {
		sendProviderRateLimitError(ctx, provider)
		return true
	}
	if maybeSendRandomProviderError(ctx, provider) {
		return true
	}
	if shouldFail(authHeader) {
		sendProviderServerError(ctx, provider)
		return true
	}

	simulateLatency(authHeader)
	return false
}
