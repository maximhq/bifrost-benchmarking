# Mock LLM API Server

This directory contains a high-performance mock server built with [fasthttp](https://github.com/valyala/fasthttp) that mimics OpenAI-, Anthropic-, and GenAI-compatible endpoints. It's designed for testing and benchmarking, allowing simulation of provider API responses without live server interaction.

## Features

- **High Performance**: Built with fasthttp for maximum throughput and minimal latency
- **Large Payload Support**: Handles request bodies up to 50MB for testing large prompt scenarios
- **OpenAI API Compatibility**: Responds to `POST` requests at `/v1/chat/completions` and `/chat/completions` with realistic response structure
- **OpenAI Responses API Support**: Supports the `/v1/responses` and `/responses` endpoints for OpenAI's responses API format
- **OpenAI Embeddings API Support**: Supports the `/v1/embeddings` and `/embeddings` endpoints for embeddings
- **Anthropic Messages API Support**: Supports `POST /anthropic/v1/messages` (and `/anthropic/messages`)
- **GenAI API Support**: Supports `POST /models/{model}:generateContent`, `POST /v1beta/models/{model}:generateContent`, `POST /v1/models/{model}:generateContent`, and `/genai/...` equivalents, including `:streamGenerateContent`
- **Bedrock Converse API Support**: Supports `POST /model/{model}/converse` and `POST /model/{model}/converse-stream` (also with `/bedrock` prefix)
- **Provider Prefix Support**: Accepts provider-prefixed models like `openai/gpt-4o`, `anthropic/claude-3-5-sonnet`, `vertex/gemini-2.0-flash`, `genai/gemini-2.0-flash`, etc.
- **Provider-Specific Error Simulation**: `-with-errors` (or `-witherrors`) returns random provider-native error payloads/codes while keeping a success/error mix
- **Server-Sent Events (SSE) Streaming**: Automatic streaming support for chat completions when `stream: true` is in the request body (SSE format)
- **Latency Simulation**: Configurable response latency via the `-latency` flag
- **Jitter Support**: Adds random variance to latency with the `-jitter` flag for more realistic network conditions
- **Per-Key Latency Targeting**: `-latency-auth-keys` scopes latency/jitter to specific API keys — listed keys are slow, all others respond instantly (mirrors `-tpm-auth-keys`). Entries can override the global config per key with `key=latencyMs` or `key=latencyMs:jitterMs`
- **Per-Key Failure Targeting**: `-failure-auth-keys` scopes the failure percentage to specific API keys — listed keys fail at the configured rate, all others always succeed. Entries can override the global config per key with `key=percent` or `key=percent:jitter`
- **Models List Endpoint**: `GET /v1/models` (and `/models`) returns an OpenAI-shaped model list configurable via `-models`, so gateway-side model discovery works against the mocker
- **Per-Chunk Latency**: For streaming responses, latency is distributed across chunks using deadline-based scheduling so end-to-end wall-clock matches `-latency` regardless of per-chunk serialization overhead
- **Configurable Streaming Granularity**: `-tokens-per-chunk` controls how many words are batched into each SSE delta (default `5`); higher values reduce envelope overhead and more closely match real provider behavior, lower values stress per-chunk parsing
- **Variable Payload Sizes**: Support for both small and large response payloads via the `-big-payload` flag
- **Realistic Token Usage**: Returns random but realistic token usage statistics, or pin exact counts with `-input-tokens` / `-output-tokens` for deterministic billing/usage tests
- **Configurable Port**: Specify listening port via the `-port` flag
- **Authentication**: Optional authentication header validation via the `-auth` flag
- **Failure Simulation**: Configurable failure rate simulation with `-failure-percent` and `-failure-jitter` flags for testing error handling
- **Rate Limiting Simulation**: Configurable TPM (tokens per minute) rate limit scenarios via the `-tpm`, `-tpm-duration`, and `-tpm-auth-keys` flags to simulate 429 Too Many Requests responses with optional time windows and per-key targeting
- **Raw Request/Response Logging**: Optional detailed logging of raw HTTP requests and responses via the `-log-raw` flag for debugging and inspection

## Prerequisites

- Go installed on your system
- Access to the `github.com/maximhq/bifrost/core/schemas` package
- Access to the `github.com/valyala/fasthttp` package

## Getting Started

### 1. Running the Mock Server

Navigate to the `mocker` directory and use `go run`:

```bash
# Navigate to the mocker directory
cd mocker

# Run the mock server (default: port 8000, 0ms latency)
go run main.go
```

### 2. Advanced Usage Examples

**Basic latency simulation:**

```bash
go run main.go -port 8080 -latency 100
# Runs on port 8080 with 100ms fixed latency
```

**Realistic network conditions with jitter:**

```bash
go run main.go -port 8080 -latency 50 -jitter 20
# 50ms base latency with ±20ms random jitter (30-70ms range)
```

**Per-key latency targeting:**

```bash
go run main.go -port 8080 -latency 500 -jitter 100 -latency-auth-keys "key-A,key-B"
# Only key-A and key-B sleep 500ms ±100ms; all other keys respond instantly

go run main.go -port 8080 -latency 500 -jitter 100 -latency-auth-keys "key-A=200,key-B=800:300,key-C"
# Per-key overrides: key-A gets fixed 200ms, key-B gets 800ms ±300ms,
# key-C falls back to the global 500ms ±100ms; all other keys respond instantly
```

**Large payload testing:**

```bash
go run main.go -port 8080 -big-payload
# Returns ~10KB responses instead of small ones
```

**Full simulation:**

```bash
go run main.go -port 8080 -latency 75 -jitter 25 -big-payload
# Comprehensive testing setup with variable latency and large payloads
```

**With authentication:**

```bash
go run main.go -port 8080 -auth "Bearer my-secret-key"
# Requires Authorization header: "Bearer my-secret-key"
```

**Failure rate simulation:**

```bash
go run main.go -port 8080 -failure-percent 10 -failure-jitter 5
# 10% base failure rate with ±5% jitter (5-15% failure range)
```

**Per-key failure targeting:**

```bash
go run main.go -port 8080 -failure-percent 10 -failure-auth-keys "key-A,key-B"
# Only key-A and key-B fail at 10%; all other keys always succeed

go run main.go -port 8080 -failure-auth-keys "slow-key=2,fast-key=10:3,key-C"
# Per-key overrides: slow-key fails 2%, fast-key fails 10% ±3%,
# key-C falls back to the global -failure-percent; all other keys always succeed
```

**Rate limiting simulation (TPM):**

```bash
go run main.go -port 8080 -tpm 30
# Returns 429 responses after 30 seconds to simulate rate limits (stays on forever)

go run main.go -port 8080 -tpm 30 -tpm-duration 60
# Returns 429 responses only between 30s and 90s, then recovers

go run main.go -port 8080 -tpm 30 -tpm-auth-keys "key-A,key-B"
# Only key-A and key-B requests get rate-limited; all other keys are unaffected

go run main.go -port 8080 -tpm 30 -tpm-duration 60 -tpm-auth-keys "Bearer key-A"
# key-A requests are rate-limited between 30s and 90s only
```

**Complete testing setup:**

```bash
go run main.go -port 8080 -latency 50 -jitter 20 -auth "Bearer test-key" -failure-percent 5 -failure-jitter 2 -tpm 60 -tpm-duration 30 -big-payload
# Full-featured mock server with latency, jitter, auth, failure simulation, rate limiting window, and large payloads
```

**With raw request/response logging:**

```bash
go run main.go -port 8000 -log-raw
# Logs raw HTTP requests and responses for debugging
```

**Streaming responses for chat completions:**

```bash
go run main.go -port 8080 -latency 5000
# Send a request with {"stream": true} to get server-sent event stream.
# Total stream wall-clock matches -latency (deadline-based scheduling); each
# SSE delta batches -tokens-per-chunk words (default 5).

go run main.go -port 8080 -latency 5000 -tokens-per-chunk 1
# One word per chunk — ~5x more SSE events on the wire; useful for stressing
# per-chunk parsing.

go run main.go -port 8080 -latency 5000 -tokens-per-chunk 20 -big-payload
# Fewer, fatter chunks (~20 words each) — closer to how OpenAI/Anthropic
# actually batch streaming deltas in production.
```

### 3. Running in Docker

The mocker server can be run in a Docker container for easy deployment and isolation.

**Using Docker Compose:**

For local testing, you can use Docker Compose:

```bash
cd mocker
docker-compose up -d
```

To customize the configuration, modify the environment variables in the `environment` section or edit the `command` section in `docker-compose.yml` before running `docker-compose up`.

### 5. Configuration Options

The mocker server can be configured via **command-line flags** or **environment variables**. Command-line flags take precedence over environment variables.

#### Environment Variables

All configuration options can be set via environment variables, which is especially useful for containerized deployments (Docker, ECS Fargate, Kubernetes, etc.):

- `MOCKER_HOST`: Host address to bind the mock server (default: `localhost`)
- `MOCKER_PORT`: Port for the mock server (default: `8000`)
- `MOCKER_LATENCY`: Base latency in milliseconds (default: `0`)
- `MOCKER_JITTER`: Maximum jitter in milliseconds (default: `0`)
- `MOCKER_LATENCY_AUTH_KEYS`: Comma-separated bearer token values that get the configured latency/jitter; all other keys respond instantly. Entries may carry a per-key override as `key=latencyMs` or `key=latencyMs:jitterMs` (e.g. `key-A=200,key-B=800:300,key-C`); bare keys use the global `MOCKER_LATENCY`/`MOCKER_JITTER`. `Bearer ` prefix is stripped automatically (default: `""`, latency applies to all requests)
- `MOCKER_FAILURE_AUTH_KEYS`: Comma-separated bearer token values subject to the failure percentage; all other keys always succeed. Entries may carry a per-key override as `key=percent` or `key=percent:jitter` (e.g. `slow-key=2,fast-key=10:3,key-C`); bare keys use the global `MOCKER_FAILURE_PERCENT`/`MOCKER_FAILURE_JITTER`. `Bearer ` prefix is stripped automatically (default: `""`, failures apply to all requests)
- `MOCKER_MODELS`: Comma-separated model ids returned by `GET /v1/models` (default: `gpt-4o-mini,gpt-4o,claude-3-5-sonnet-latest,gemini-2.0-flash`)
- `MOCKER_TOKENS_PER_CHUNK`: Words batched into each SSE delta when streaming; must be `>=1` (default: `5`)
- `MOCKER_INPUT_TOKENS`: Fixed input/prompt token count to report in every `usage` block; negative disables (default: `-1`, random/derived per request)
- `MOCKER_OUTPUT_TOKENS`: Fixed output/completion token count to report in every `usage` block; negative disables (default: `-1`, random/derived per request)
- `MOCKER_BIG_PAYLOAD`: Use large payloads - set to `true`, `1`, `false`, or `0` (default: `false`)
- `MOCKER_AUTH`: Authentication header value to require (default: `""`)
- `MOCKER_FAILURE_PERCENT`: Base failure percentage 0-100 (default: `0`)
- `MOCKER_FAILURE_JITTER`: Maximum jitter in percentage points (default: `0`)
- `MOCKER_WITH_ERRORS`: Enable random provider-specific errors (default: `false`)
- `MOCKER_TPM`: Seconds after which to trigger TPM (429) scenarios (default: `0`, disabled)
- `MOCKER_TPM_DURATION`: Duration in seconds for the TPM window; TPM is active from `MOCKER_TPM` to `MOCKER_TPM + MOCKER_TPM_DURATION` seconds (default: `0`, active until server stop)
- `MOCKER_TPM_AUTH_KEYS`: Comma-separated bearer token values that trigger TPM; the `Bearer ` prefix is stripped automatically before comparison, so pass raw tokens (e.g. `key-A,key-B`); other keys are unaffected (default: `""`, all requests)
- `MOCKER_LOG_RAW`: Log raw HTTP requests and responses - set to `true`, `1`, `false`, or `0` (default: `false`)

**Example using environment variables:**

```bash
export MOCKER_PORT=8080
export MOCKER_LATENCY=50
export MOCKER_JITTER=20
export MOCKER_BIG_PAYLOAD=true
export MOCKER_AUTH="Bearer my-secret-key"
export MOCKER_TPM=60
export MOCKER_TPM_DURATION=30
export MOCKER_TPM_AUTH_KEYS="key-A,key-B"
go run main.go
```

**Example in Docker:**

```bash
docker run -p 8000:8000 \
  -e MOCKER_PORT=8000 \
  -e MOCKER_LATENCY=50 \
  -e MOCKER_JITTER=20 \
  -e MOCKER_BIG_PAYLOAD=true \
  -e MOCKER_AUTH="Bearer my-secret-key" \
  mocker-server
```

**Example in docker-compose.yml:**

```yaml
services:
  mocker:
    build: .
    ports:
      - "8000:8000"
    environment:
      - MOCKER_PORT=8000
      - MOCKER_LATENCY=50
      - MOCKER_JITTER=20
      - MOCKER_BIG_PAYLOAD=true
      - MOCKER_AUTH=Bearer my-secret-key
      - MOCKER_TPM=60
      - MOCKER_TPM_DURATION=30
      - MOCKER_TPM_AUTH_KEYS=key-A,key-B
```

#### Command-Line Flags

- `-host <host_address>`: Host address to bind the mock server (default: `localhost`)
- `-port <port_number>`: Port for the mock server (default: `8000`)
- `-latency <milliseconds>`: Base latency for each response (default: `0`)
- `-jitter <milliseconds>`: Maximum random jitter added to latency, creating a range of ±jitter (default: `0`)
- `-latency-auth-keys <keys>`: Comma-separated bearer token values that get the configured latency/jitter; all other keys respond instantly. Entries may carry a per-key override as `key=latencyMs` or `key=latencyMs:jitterMs` (e.g. `key-A=200,key-B=800:300,key-C`); bare keys use the global `-latency`/`-jitter`. The `Bearer ` prefix is stripped automatically (default: `""`, latency applies to all requests)
- `-failure-auth-keys <keys>`: Comma-separated bearer token values subject to `-failure-percent`; all other keys always succeed. Entries may carry a per-key override as `key=percent` or `key=percent:jitter` (e.g. `slow-key=2,fast-key=10:3,key-C`); bare keys use the global `-failure-percent`/`-failure-jitter`. The `Bearer ` prefix is stripped automatically (default: `""`, failures apply to all requests)
- `-models <ids>`: Comma-separated model ids returned by `GET /v1/models` (default: `gpt-4o-mini,gpt-4o,claude-3-5-sonnet-latest,gemini-2.0-flash`)
- `-big-payload`: Use large ~10KB response payloads instead of small ones (default: `false`)
- `-input-tokens <count>`: Fixed input/prompt token count to report in every `usage` block (across OpenAI, Anthropic, Gemini, and Bedrock shapes, streaming and non-streaming). Negative disables it (default: `-1`, random/derived per request)
- `-output-tokens <count>`: Fixed output/completion token count to report in every `usage` block. Negative disables it (default: `-1`, random/derived per request)
- `-auth <auth_header>`: Authentication header value to require. Requests must include this exact value in the `Authorization` header (default: `""`)
- `-failure-percent <percentage>`: Base failure percentage (0-100) for simulating server errors (default: `0`)
- `-failure-jitter <percentage_points>`: Maximum jitter in percentage points to add to failure rate, creating a range of ±failure-jitter (default: `0`)
- `-with-errors` / `-witherrors`: Enable random provider-specific error payloads/codes. Defaults to 20% error rate when enabled unless `-failure-percent` is set
- `-tpm <seconds>`: Seconds after which to trigger TPM (429) scenarios (default: `0`, disabled)
- `-tpm-duration <seconds>`: Duration in seconds for the TPM window. TPM is active from `-tpm` to `-tpm + -tpm-duration` seconds; after the window closes requests succeed again (default: `0`, active until server stop)
- `-tpm-auth-keys <keys>`: Comma-separated bearer token values that should be rate-limited. The `Bearer ` prefix is stripped automatically before comparison, so pass the raw token (e.g. `"key-A,key-B"`). Requests with any other key are unaffected (default: `""`, all requests)
- `-log-raw`: Log raw HTTP request and response bodies for debugging and inspection (default: `false`)

**Note:** Command-line flags override environment variables. If `-auth` is set to an empty string (`-auth ""`), authentication is disabled. Otherwise, all requests must include the exact authentication header value.

## API Endpoints

The mock server supports the following endpoints:

### Health Check

- `GET /health` - Health check endpoint for load balancers and monitoring. Returns `{"status":"healthy"}` with HTTP 200.

### Models List

- `GET /v1/models` - OpenAI-compatible model list (also `/models`, `/openai/v1/models`, `/openai/models`)

Returns the model ids configured via `-models` / `MOCKER_MODELS` in the standard OpenAI list shape (`{"object":"list","data":[{"id":…,"object":"model",…}]}`). The endpoint validates auth (when `-auth` is set) but deliberately skips latency, failure, and TPM simulation — those flags shape inference behavior, while model discovery stays deterministic so gateway-side model catalogs can always populate.

### Chat Completions API

- `POST /v1/chat/completions` - OpenAI-compatible chat completions endpoint
- `POST /chat/completions` - Alternative path for chat completions

Both endpoints support both standard and streaming responses:
- **Standard Response**: Returns a single JSON response
- **Streaming Response**: When the request body contains `"stream": true`, returns a Server-Sent Events (SSE) stream of chunks

**Note:** Streaming is only supported for the chat completions endpoints. Other endpoints (responses, embeddings) do not support streaming.

### Responses API

- `POST /v1/responses` - OpenAI-compatible responses endpoint
- `POST /responses` - Alternative path for responses API

Both endpoints return responses in the OpenAI responses API format.

### Embeddings API

- `POST /v1/embeddings` - OpenAI-compatible embeddings endpoint
- `POST /embeddings` - Alternative path for embeddings API

Both endpoints return responses in the OpenAI embeddings API format.

### Anthropic Messages API

- `POST /anthropic/v1/messages` - Anthropic-compatible messages endpoint
- `POST /anthropic/messages` - Alternative path for Anthropic messages
- `POST /v1/messages` - Anthropic client compatibility path

Returns responses in Anthropic messages format.

### GenAI API

- `POST /models/{model}:generateContent` - Native Gemini-compatible content generation endpoint
- `POST /models/{model}:streamGenerateContent` - Native Gemini-compatible stream endpoint
- `POST /v1beta/models/{model}:generateContent` - Gemini v1beta endpoint
- `POST /v1beta/models/{model}:streamGenerateContent` - Gemini v1beta stream endpoint
- `POST /v1/models/{model}:generateContent` - Gemini v1 endpoint
- `POST /v1/models/{model}:streamGenerateContent` - Gemini v1 stream endpoint
- `POST /genai/v1beta/models/{model}:generateContent` - GenAI-compatible content generation endpoint
- `POST /genai/v1beta/models/{model}:streamGenerateContent` - GenAI stream endpoint (mocked as JSON response)
- `POST /genai/v1/models/{model}:generateContent` - GenAI v1 content generation endpoint
- `POST /genai/v1/models/{model}:streamGenerateContent` - GenAI v1 stream endpoint (mocked as JSON response)

`{model}` can be raw (`gemini-2.0-flash`) or URL-escaped provider prefixed (`vertex%2Fgemini-2.0-flash`).

### Bedrock Converse API

- `POST /model/{model}/converse` - Bedrock-compatible converse endpoint
- `POST /model/{model}/converse-stream` - Bedrock-compatible streaming converse endpoint
- `POST /bedrock/model/{model}/converse` - Same endpoint with `/bedrock` prefix
- `POST /bedrock/model/{model}/converse-stream` - Same streaming endpoint with `/bedrock` prefix

**Note:** All endpoints support the same configuration flags (latency, jitter, auth, failure simulation, etc.) and require the same authentication header if `-auth` is set. The `/health` endpoint does not require authentication and does not simulate latency or failures.

## Response Format

### Chat Completions Response

The mock server returns responses in the standard OpenAI chat completion format:

```json
{
  "id": "cmpl-mock12345",
  "object": "chat.completion",
  "created": 1640995200,
  "model": "gpt-4o-mini",
  "choices": [
    {
      "index": 0,
      "message": {
        "role": "assistant",
        "content": "This is a mocked response..."
      },
      "finish_reason": "stop"
    }
  ],
  "usage": {
    "prompt_tokens": 245,
    "completion_tokens": 167,
    "total_tokens": 412
  }
}
```

### Responses API Response

For the `/v1/responses` and `/responses` endpoints, the mock server returns responses in the OpenAI responses API format:

```json
{
  "id": "resp-mock12345",
  "object": "response",
  "created": 1640995200,
  "model": "gpt-4o-mini",
  "output": [
    {
      "type": "message",
      "message": {
        "role": "assistant",
        "content": [
          {
            "type": "output_text",
            "text": "This is a mocked response..."
          }
        ]
      }
    }
  ],
  "status": "completed",
  "usage": {
    "prompt_tokens": 245,
    "completion_tokens": 167,
    "total_tokens": 412
  }
}
```

### Embeddings API Response

For the `/v1/embeddings` and `/embeddings` endpoints, the mock server returns responses in the OpenAI embeddings API format:

```json
{
  "object": "list",
  "data": [
    {
      "object": "embedding",
      "embedding": [0.123, -0.456, 0.789, ...],
      "index": 0
    }
  ],
  "model": "text-embedding-ada-002",
  "usage": {
    "prompt_tokens": 8,
    "total_tokens": 8
  }
}
```

The embedding vector defaults to 1536 dimensions (standard for `text-embedding-ada-002`). When `-big-payload` is enabled, the vector size increases to 4096 dimensions for larger payload testing.

## Authentication

When the `-auth` flag is set (default: `""`), all requests must include an `Authorization` header with the exact value specified. Requests without the header or with an incorrect value will receive a `403 Forbidden` response.

**Example request with authentication:**

```bash
curl -X POST http://localhost:8000/v1/chat/completions \
  -H "Authorization: Bearer mocker-key" \
  -H "Content-Type: application/json" \
  -d '{
    "messages": [{"role": "user", "content": "Hello"}],
    "model": "gpt-4o-mini"
  }'
```

To disable authentication, set `-auth ""` (empty string).

## Failure Simulation

The `-failure-percent` and `-failure-jitter` flags allow you to simulate server errors for testing error handling and retry logic:

- `-failure-percent`: Base percentage of requests that should fail (0-100)
- `-failure-jitter`: Random variance added to the failure percentage (±jitter percentage points)

**Example:** With `-failure-percent 10 -failure-jitter 5`, the actual failure rate will vary between 5% and 15% per request batch, providing realistic error rate variability.

Failed requests return a `500 Internal Server Error` with an OpenAI-compatible error response:

```json
{
  "event_id": "evt_mock_error_12345",
  "error": {
    "type": "server_error",
    "code": "internal_server_error",
    "message": "The server had an error while processing your request. Sorry about that!"
  }
}
```

## Rate Limiting Simulation (TPM)

Three flags control TPM (429) simulation:

| Flag | Env var | Default | Description |
|------|---------|---------|-------------|
| `-tpm <seconds>` | `MOCKER_TPM` | `0` (disabled) | Seconds after server start when rate limiting begins |
| `-tpm-duration <seconds>` | `MOCKER_TPM_DURATION` | `0` (forever) | Duration of the rate-limit window; TPM is active from `tpm` to `tpm + tpm-duration` seconds |
| `-tpm-auth-keys <keys>` | `MOCKER_TPM_AUTH_KEYS` | `""` (all) | Comma-separated raw bearer token values to rate-limit (`Bearer ` prefix stripped automatically); all other keys are unaffected |

**Behaviour summary:**

- If only `-tpm` is set: all requests return 429 from that second onward.
- If `-tpm-duration` is also set: 429s fire only inside the `[tpm, tpm+tpm-duration)` second window; requests succeed before and after.
- If `-tpm-auth-keys` is set: only requests whose `Authorization` header exactly matches one of the listed values are rate-limited; others always succeed regardless of the time window.

**Examples:**

```bash
# All requests rate-limited after 30 s
go run main.go -tpm 30

# Rate-limited only between 30 s and 90 s, then recovers
go run main.go -tpm 30 -tpm-duration 60

# Only key-A and key-B are rate-limited after 30 s
go run main.go -tpm 30 -tpm-auth-keys "key-A,key-B"

# key-A rate-limited in a 60 s window starting at 30 s; key-B and others unaffected
go run main.go -tpm 30 -tpm-duration 60 -tpm-auth-keys "key-A"
```

Rate-limited requests return a `429 Too Many Requests` response:

```json
{
  "event_id": "evt_mock_error_12345",
  "error": {
    "type": "server_error",
    "code": "internal_server_error",
    "message": "Rate limit exceeded. Please retry after some time."
  }
}
```

**Use cases for TPM simulation:**
- Test client-side retry logic and exponential backoff handling
- Verify that applications gracefully degrade when rate limits are encountered
- Simulate realistic API usage patterns where limits are enforced after certain thresholds
- Test per-key rate limiting — e.g. one API key gets throttled while others remain healthy
- Load test rate limit handling in production-like scenarios

## Streaming Responses

The mock server supports Server-Sent Events (SSE) streaming for the chat completions endpoints. When a request includes `"stream": true` in the JSON body, the server returns a stream of chunks instead of a single response.

**Streaming is only available for chat completions endpoints** (`/v1/chat/completions` and `/chat/completions`). Other endpoints (responses, embeddings) return standard non-streaming responses regardless of the `stream` field.

### How Streaming Works

1. Client sends a POST request with `"stream": true` in the request body
2. Server responds with `Content-Type: text/event-stream`
3. Response body contains multiple `data:` lines, each containing a JSON chunk
4. Each chunk follows the streaming response format with a `delta` field containing partial content
5. Stream ends with a `data: [DONE]` message

### Streaming Response Format

Each chunk in the stream follows this format:

```json
data: {
  "id": "cmpl-mock12345",
  "object": "chat.completion.chunk",
  "created": 1640995200,
  "model": "gpt-4o-mini",
  "choices": [
    {
      "index": 0,
      "delta": {
        "role": "assistant",
        "content": "word "
      },
      "finish_reason": null
    }
  ]
}

data: {
  "id": "cmpl-mock12345",
  "object": "chat.completion.chunk",
  "created": 1640995200,
  "model": "gpt-4o-mini",
  "choices": [
    {
      "index": 0,
      "delta": {
        "content": "next "
      },
      "finish_reason": null
    }
  ]
}

data: [DONE]
```

### Streaming with Latency

When latency is configured with streaming responses, the total latency is distributed evenly across all chunks. For example:
- With `-latency 5000` (5 seconds) and 10 chunks of content
- Each chunk is sent with a ~500ms delay
- Total streaming time is approximately 5 seconds
- Provides realistic streaming behavior with proper timing

**Example request with streaming:**

```bash
curl -X POST http://localhost:8000/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "messages": [{"role": "user", "content": "Hello"}],
    "model": "gpt-4o-mini",
    "stream": true
  }'
```

**Example Python client:**

```python
import requests

response = requests.post(
    'http://localhost:8000/v1/chat/completions',
    json={
        'messages': [{'role': 'user', 'content': 'Hello'}],
        'model': 'gpt-4o-mini',
        'stream': True
    },
    stream=True
)

for line in response.iter_lines():
    if line:
        print(line.decode('utf-8'))
```

**Use cases for streaming:**
- Test client-side streaming response handling
- Verify proper chunk parsing and buffering
- Simulate realistic chat completion experiences
- Load test streaming endpoint performance
- Test timeout handling with long-running streams

## Raw Request/Response Logging

The `-log-raw` flag enables detailed logging of raw HTTP requests and responses for debugging and inspection purposes:

- `-log-raw`: Enable logging of raw HTTP request and response bodies (default: `false`)

**Example:**

```bash
go run main.go -port 8000 -log-raw
```

When enabled, the server logs raw request details:

```
--- Raw Request ---
POST /v1/chat/completions HTTP/1.1
Authorization: Bearer test-key
Content-Type: application/json
...
--- Body ---
{"messages":[{"role":"user","content":"Hello"}]}
--- End Request ---
```

And raw response details:

```
--- Raw Response ---
HTTP/1.1 200 OK
Content-Type: application/json
...
--- Body ---
{"id":"cmpl-mock12345","object":"chat.completion",...}
--- End Response ---
```

**TPM Scenario Logging:**

On startup, the server logs the configured TPM window:

```
TPM (429) scenario will be triggered between 30 and 90 seconds
TPM will only apply to requests with auth keys: Bearer key-A,Bearer key-B
```

When the first request is actually rate-limited, it logs:

```
TPM (429) scenario triggered after X seconds
```

This helps track when rate limiting begins during load tests.

**Use cases for raw logging:**
- Debug client request formatting and headers
- Inspect actual request/response bodies being sent and received
- Troubleshoot authentication or content-type issues
- Verify payload sizes and structure during testing
- Monitor when TPM scenarios activate during benchmarks

## Use Cases

- **Load Testing**: Test API gateway performance with predictable response times
- **Network Simulation**: Simulate various network conditions with latency and jitter
- **Payload Testing**: Test system behavior with different response sizes
- **Error Handling Testing**: Simulate server failures with configurable failure rates
- **Authentication Testing**: Test authentication flows and error handling
- **Streaming Response Testing**: Test chat completion streaming with realistic chunk timing and distribution
- **Development**: Local development without OpenAI API costs or rate limits
- **Multi-Endpoint Testing**: Test chat completions, responses API, and embeddings API endpoints
- **Rate Limit Handling**: Test rate limit behavior and retry logic with configurable TPM scenarios

## Technical Details

### Server Configuration

The mocker uses fasthttp with the following configuration for high-performance benchmarking:

| Setting | Value | Description |
|---------|-------|-------------|
| `MaxRequestBodySize` | 50MB | Maximum allowed request body size |
| `ReadBufferSize` | 16KB | Buffer size for reading incoming requests |
| `ReadTimeout` | 300s | Maximum time to read the full request |
| `WriteTimeout` | 300s | Maximum time to write the full response |
| `IdleTimeout` | 60s | Maximum time to wait for the next request |

This configuration allows the mocker to handle large payloads (up to 50MB) which is useful for testing embedding requests with large text inputs or stress testing with big prompts.
