# Mock OpenAI API Server

This directory contains a high-performance mock server built with [fasthttp](https://github.com/valyala/fasthttp) that mimics the OpenAI API endpoints. It's designed for testing and benchmarking, allowing simulation of OpenAI API responses without live server interaction.

## Features

- **High Performance**: Built with fasthttp for maximum throughput and minimal latency
- **Large Payload Support**: Handles request bodies up to 50MB for testing large prompt scenarios
- **OpenAI API Compatibility**: Responds to `POST` requests at `/v1/chat/completions` and `/chat/completions` with realistic response structure
- **OpenAI Responses API Support**: Supports the `/v1/responses` and `/responses` endpoints for OpenAI's responses API format
- **OpenAI Embeddings API Support**: Supports the `/v1/embeddings` and `/embeddings` endpoints for embeddings
- **Latency Simulation**: Configurable response latency via the `-latency` flag
- **Jitter Support**: Adds random variance to latency with the `-jitter` flag for more realistic network conditions
- **Variable Payload Sizes**: Support for both small and large response payloads via the `-big-payload` flag
- **Realistic Token Usage**: Returns random but realistic token usage statistics
- **Configurable Port**: Specify listening port via the `-port` flag
- **Authentication**: Optional authentication header validation via the `-auth` flag
- **Failure Simulation**: Configurable failure rate simulation with `-failure-percent` and `-failure-jitter` flags for testing error handling
- **Rate Limiting Simulation**: Configurable TPM (tokens per minute) rate limit scenarios via the `-tpm` flag to simulate 429 Too Many Requests responses
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

**Rate limiting simulation (TPM):**

```bash
go run main.go -port 8080 -tpm 30
# Returns 429 responses after 30 seconds to simulate rate limits
```

**Complete testing setup:**

```bash
go run main.go -port 8080 -latency 50 -jitter 20 -auth "Bearer test-key" -failure-percent 5 -failure-jitter 2 -tpm 60 -big-payload
# Full-featured mock server with latency, jitter, auth, failure simulation, rate limiting, and large payloads
```

**With raw request/response logging:**

```bash
go run main.go -port 8000 -log-raw
# Logs raw HTTP requests and responses for debugging
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
- `MOCKER_BIG_PAYLOAD`: Use large payloads - set to `true`, `1`, `false`, or `0` (default: `false`)
- `MOCKER_AUTH`: Authentication header value to require (default: `""`)
- `MOCKER_FAILURE_PERCENT`: Base failure percentage 0-100 (default: `0`)
- `MOCKER_FAILURE_JITTER`: Maximum jitter in percentage points (default: `0`)
- `MOCKER_TPM`: Seconds after which to trigger TPM (429) scenarios (default: `0`)
- `MOCKER_LOG_RAW`: Log raw HTTP requests and responses - set to `true`, `1`, `false`, or `0` (default: `false`)

**Example using environment variables:**

```bash
export MOCKER_PORT=8080
export MOCKER_LATENCY=50
export MOCKER_JITTER=20
export MOCKER_BIG_PAYLOAD=true
export MOCKER_AUTH="Bearer my-secret-key"
export MOCKER_TPM=60
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
```

#### Command-Line Flags

- `-host <host_address>`: Host address to bind the mock server (default: `localhost`)
- `-port <port_number>`: Port for the mock server (default: `8000`)
- `-latency <milliseconds>`: Base latency for each response (default: `0`)
- `-jitter <milliseconds>`: Maximum random jitter added to latency, creating a range of ±jitter (default: `0`)
- `-big-payload`: Use large ~10KB response payloads instead of small ones (default: `false`)
- `-auth <auth_header>`: Authentication header value to require. Requests must include this exact value in the `Authorization` header (default: `""`)
- `-failure-percent <percentage>`: Base failure percentage (0-100) for simulating server errors (default: `0`)
- `-failure-jitter <percentage_points>`: Maximum jitter in percentage points to add to failure rate, creating a range of ±failure-jitter (default: `0`)
- `-tpm <seconds>`: Seconds after which to trigger TPM (429) scenarios. After this duration, all requests return HTTP 429 Too Many Requests (default: `0`, disabled)
- `-log-raw`: Log raw HTTP request and response bodies for debugging and inspection (default: `false`)

**Note:** Command-line flags override environment variables. If `-auth` is set to an empty string (`-auth ""`), authentication is disabled. Otherwise, all requests must include the exact authentication header value.

## API Endpoints

The mock server supports the following endpoints:

### Health Check

- `GET /health` - Health check endpoint for load balancers and monitoring. Returns `{"status":"healthy"}` with HTTP 200.

### Chat Completions API

- `POST /v1/chat/completions` - OpenAI-compatible chat completions endpoint
- `POST /chat/completions` - Alternative path for chat completions

Both endpoints return responses in the standard OpenAI chat completion format.

### Responses API

- `POST /v1/responses` - OpenAI-compatible responses endpoint
- `POST /responses` - Alternative path for responses API

Both endpoints return responses in the OpenAI responses API format.

### Embeddings API

- `POST /v1/embeddings` - OpenAI-compatible embeddings endpoint
- `POST /embeddings` - Alternative path for embeddings API

Both endpoints return responses in the OpenAI embeddings API format.

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

The `-tpm` flag allows you to simulate rate limiting scenarios by triggering HTTP 429 (Too Many Requests) responses after a specified number of seconds:

- `-tpm`: Number of seconds before rate limiting starts (0 = disabled)

**Example:** With `-tpm 30`, the server will normally accept all requests, but after 30 seconds, all requests will return HTTP 429.

Rate-limited requests return a `429 Too Many Requests` response with an OpenAI-compatible error response:

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
- Load test rate limit handling in production-like scenarios

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

When the TPM (rate limiting) scenario is triggered, the server logs:

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
- **Development**: Local development without OpenAI API costs or rate limits
- **Multi-Endpoint Testing**: Test chat completions, responses API, and embeddings API endpoints

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
