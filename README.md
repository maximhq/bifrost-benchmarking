# API Provider Benchmarking Tool

A comprehensive command-line tool for benchmarking API providers with advanced monitoring capabilities. Built with Go and Vegeta, it tests multiple providers simultaneously, tracks server-side memory usage, and provides detailed performance analytics.

## Features

- **Multi-Provider Support**: Benchmark Bifrost, LiteLLM, Portkey, and Helicone simultaneously or individually
- **Advanced Metrics**: Latency percentiles, throughput, success rates, and server memory monitoring
- **Real-time Memory Tracking**: Monitor server-side RAM usage during benchmarks
- **Dynamic Payload Generation**: Support for small and large payloads with dynamic content injection
- **Environment-based Configuration**: Automatic port detection via `.env` file
- **Comprehensive Results**: JSON output with aggregated metrics and historical data
- **Error Analysis**: Track and categorize failed requests and drop reasons
- **Cooldown Management**: Configurable rest periods between provider tests

## Prerequisites

- Go (version 1.19 or higher)
- `.env` file with provider port configurations (see Environment Setup)
- Target API providers must be running and accessible

## Environment Setup

Create a `.env` file in the project root with provider ports:

```env
BIFROST_PORT=3001
LITELLM_PORT=4000
PORTKEY_PORT=8787
HELICONE_PORT=3002
OPENAI_API_KEY=sk-your-openai-api-key
```

## Installation

```bash
# Clone or download the project
git clone <repository-url>
cd maxim-bifrost-benchmarks

# Install dependencies
go mod tidy

# Build the executable (optional)
go build benchmark.go
```

## Usage

### Basic Commands

**Run all providers (default configuration):**

```bash
go run benchmark.go
# Tests all configured providers with 500 RPS for 10 seconds each
```

**Benchmark a specific provider:**

```bash
go run benchmark.go -provider bifrost
```

**High-intensity testing:**

```bash
go run benchmark.go -rate 1000 -duration 30 -provider litellm
```

**Large payload testing:**

```bash
go run benchmark.go -big-payload -provider portkey -duration 60
```

### Command-Line Flags

| Flag           | Type   | Default      | Description                                                     |
| -------------- | ------ | ------------ | --------------------------------------------------------------- |
| `-rate`        | int    | 500          | Requests per second to send                                     |
| `-duration`    | int    | 10           | Test duration in seconds                                        |
| `-output`      | string | results.json | Output file for benchmark results                               |
| `-cooldown`    | int    | 60           | Cooldown period between tests (seconds)                         |
| `-provider`    | string | ""           | Specific provider to test (bifrost, litellm, portkey, helicone) |
| `-big-payload` | bool   | false        | Use large ~10KB payloads instead of small ones                  |
| `-model`       | string | gpt-4o-mini  | Model identifier to use in requests                             |
| `-suffix`      | string | v1           | URL route suffix (e.g., v1, v2)                                 |

### Advanced Examples

**Production load simulation:**

```bash
go run benchmark.go -rate 2000 -duration 300 -cooldown 120 -big-payload
# 2000 RPS for 5 minutes each provider, 2-minute cooldowns, large payloads
```

**Quick smoke test:**

```bash
go run benchmark.go -rate 100 -duration 5 -cooldown 10
# Light testing: 100 RPS for 5 seconds each, 10-second cooldowns
```

**Single provider deep dive:**

```bash
go run benchmark.go -provider bifrost -rate 1500 -duration 120 -output bifrost_detailed.json
# Focus test: Bifrost only, 1500 RPS for 2 minutes
```

## Output & Results

Results are saved in JSON format with detailed metrics for each provider:

### Sample Output Structure

```json
{
  "bifrost": {
    "requests": 5000,
    "rate": 500.12,
    "success_rate": 99.8,
    "mean_latency_ms": 45.2,
    "p50_latency_ms": 42.1,
    "p99_latency_ms": 156.7,
    "max_latency_ms": 203.4,
    "throughput_rps": 498.5,
    "timestamp": "2024-01-15T10:30:00Z",
    "status_code_counts": {
      "200": 4990,
      "500": 10
    },
    "server_peak_memory_mb": 256.7,
    "server_avg_memory_mb": 189.3,
    "drop_reasons": {
      "HTTP 500": 10
    }
  }
}
```

### Key Metrics Explained

- **Success Rate**: Percentage of HTTP 200 responses
- **Latency Metrics**: P50, P99, mean, and max response times in milliseconds
- **Throughput**: Actual requests processed per second
- **Memory Tracking**: Peak and average server RAM usage during test
- **Drop Reasons**: Categorized failure analysis (timeouts, HTTP errors, etc.)

## Architecture

The tool uses:

- **Vegeta**: High-performance HTTP load testing library
- **gopsutil**: System monitoring for memory usage tracking
- **Dynamic Targeting**: Real-time payload generation with request indexing
- **Concurrent Monitoring**: Parallel memory sampling during load tests
- **Process Discovery**: Automatic server process detection by port

## Payload Types

### Small Payload (~200 bytes)

```json
{
  "messages": [
    {
      "role": "user",
      "content": "This is a benchmark request #123 at 2024-01-15T10:30:00Z. How are you?"
    }
  ],
  "model": "openai/gpt-4o-mini"
}
```

### Large Payload (~10KB)

Extended payload with comprehensive AI proxy gateway analysis questions, suitable for testing larger request handling.

## Provider-Specific Features

- **Portkey**: Automatic `x-portkey-config` header injection with OpenAI API key
- **Dynamic Content**: Request indexing and timestamps in all payloads
- **Memory Monitoring**: Per-provider server process tracking
- **Error Categorization**: Provider-specific failure analysis

## Troubleshooting

**Common Issues:**

1. **"No process found on port"**: Ensure your provider is running and the `.env` file has correct ports
2. **"Provider not found"**: Check provider name spelling (bifrost, litellm, portkey, helicone)
3. **Memory monitoring fails**: Run with sufficient permissions to access process information
4. **High latency/timeouts**: Reduce rate or increase server resources

**Debug Tips:**

- Start with low rates (`-rate 50`) to verify basic connectivity
- Use single provider tests to isolate issues
- Check server logs during benchmark runs
- Monitor system resources on both client and server sides

## Performance Considerations

- **Client Resources**: High rates (>2000 RPS) may require tuning client machine resources
- **Network**: Consider network bandwidth and latency in results interpretation
- **Server Capacity**: Monitor target server CPU/memory to avoid resource exhaustion
- **Cooldown Periods**: Allow servers to recover between intensive tests

This tool is designed for comprehensive API performance analysis in development, staging, and production environments.
