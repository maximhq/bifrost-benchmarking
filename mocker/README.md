# Mock OpenAI API Server

This directory contains a mock server that mimics the OpenAI API's chat completions endpoint (`/v1/chat/completions`). It's designed for testing and benchmarking, allowing simulation of OpenAI API responses without live server interaction.

## Features

- **OpenAI API Compatibility**: Responds to `POST` requests at `/v1/chat/completions` with realistic response structure
- **Latency Simulation**: Configurable response latency via the `-latency` flag
- **Jitter Support**: Adds random variance to latency with the `-jitter` flag for more realistic network conditions
- **Variable Payload Sizes**: Support for both small and large response payloads via the `-big-payload` flag
- **Realistic Token Usage**: Returns random but realistic token usage statistics
- **Configurable Port**: Specify listening port via the `-port` flag

## Prerequisites

- Go installed on your system
- Access to the `github.com/maximhq/bifrost/core/schemas` package

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

### 3. Command-Line Flags

- `-port <port_number>`: Port for the mock server (default: `8000`)
- `-latency <milliseconds>`: Base latency for each response (default: `0`)
- `-jitter <milliseconds>`: Maximum random jitter added to latency, creating a range of ±jitter (default: `0`)
- `-big-payload`: Use large ~10KB response payloads instead of small ones (default: `false`)

## Response Format

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

## Use Cases

- **Load Testing**: Test API gateway performance with predictable response times
- **Network Simulation**: Simulate various network conditions with latency and jitter
- **Payload Testing**: Test system behavior with different response sizes
- **Development**: Local development without OpenAI API costs or rate limits
