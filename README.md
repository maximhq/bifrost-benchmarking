# API Provider Benchmarking Tool

A comprehensive command-line tool for benchmarking API providers with advanced monitoring capabilities. Built with Go and Vegeta, it tests multiple providers simultaneously, tracks server-side memory usage, and provides detailed performance analytics.

## Features

- **Multi-Provider Support**: Benchmark Bifrost, LiteLLM, Portkey, and Helicone simultaneously or individually
- **Advanced Metrics**: Latency percentiles, throughput, success rates, and server memory monitoring
- **Real-time Memory Tracking**: Monitor server-side RAM usage during benchmarks
- **Dynamic Payload Generation**: Support for small and large payloads with dynamic content injection
- **Custom Prompt Files**: Load prompts from files for large payload testing (up to 50MB+)
- **Embeddings Support**: Benchmark embeddings endpoints with configurable request types
- **Environment-based Configuration**: Automatic port detection via `.env` file
- **Comprehensive Results**: JSON output with aggregated metrics and historical data
- **Error Analysis**: Track and categorize failed requests and drop reasons
- **Cooldown Management**: Configurable rest periods between provider tests

## Prerequisites

- Go (version 1.19 or higher)
- `.env` file with provider port configurations (see Environment Setup)
- Target API providers must be running and accessible

## Setting Up Bifrost for Benchmarking

Bifrost serves as a high-performance HTTP API gateway that provides seamless connections to various AI providers like OpenAI, Anthropic, and AWS Bedrock. This section covers the complete setup process for optimal benchmarking performance.

### 1. Install Bifrost

#### Option A: Using NPX (Recommended for Quick Start)

```bash
npx -y @maximhq/bifrost
```

This command downloads and runs the latest version of Bifrost instantly without requiring a separate installation.

#### Option B: Using Docker

```bash
# Pull the Bifrost image
docker pull maximhq/bifrost

# Run Bifrost with port mapping
docker run -p 8080:8080 maximhq/bifrost

# For persistent configuration across restarts
docker run -p 8080:8080 -v $(pwd)/data:/app/data maximhq/bifrost
```

After installation, Bifrost will be accessible at `http://localhost:8080` with a web-based management interface.

📖 **For detailed installation instructions:** [Bifrost Setup Guide](https://docs.getbifrost.ai/quickstart/gateway/setting-up)

### 2. Run the Mock Server (Optional - For Testing Without External APIs)

If you want to test without making actual API calls to external providers:

```bash
# Navigate to the mocker directory
cd mocker

# Run the mock OpenAI server (basic setup)
go run main.go -port 8000

# With custom latency and jitter for realistic testing
go run main.go -port 8000 -latency 50 -jitter 20 -big-payload

# With authentication enabled
go run main.go -port 8000 -auth "Bearer my-secret-key"

# With failure simulation for error handling testing
go run main.go -port 8000 -failure-percent 10 -failure-jitter 5

# Complete setup with all features
go run main.go -port 8000 -latency 50 -jitter 20 -auth "Bearer test-key" -failure-percent 5 -failure-jitter 2 -big-payload
```

**Available Mock Server Flags:**

- `-host`: Host address (default: `localhost`)
- `-port`: Port number (default: `8000`)
- `-latency`: Base latency in milliseconds (default: `0`)
- `-jitter`: Maximum latency jitter in milliseconds (default: `0`)
- `-big-payload`: Use large ~10KB response payloads (default: `false`)
- `-auth`: Authentication header value to require (default: `""`)
- `-failure-percent`: Base failure percentage 0-100 (default: `0`)
- `-failure-jitter`: Maximum failure rate jitter in percentage points (default: `0`)

**Supported Endpoints:**

- `/v1/chat/completions` and `/chat/completions` - Chat completions API
- `/v1/responses` and `/responses` - Responses API
- `/v1/embeddings` and `/embeddings` - Embeddings API

See the [mocker README](mocker/README.md) for detailed documentation.

### 3. Configure Providers in Bifrost

**For Real API Testing:**

1. Access the Bifrost Web UI at `http://localhost:8080`
2. Navigate to **Providers** section
3. Add your desired provider (OpenAI, Anthropic, etc.) with API keys
4. Configure routing rules as needed

**For Mock Testing (Optional):**

1. In the Bifrost Web UI, go to **Providers**.
2. Add an OpenAI provider with:

   - **Base URL**: Use the host URL where your mock server is running:

     - For Docker Installation:

       - **macOS / Windows (Docker Desktop):**  
         `http://host.docker.internal:<mock server port>`
       - **Linux:** Docker doesn’t provide `host.docker.internal` by default. Find your host’s Docker bridge IP using:

         ```bash

         ip addr show docker0
         ```

         Usually, the IP is `172.17.0.1`. Then configure the provider URL as:  
         `http://172.17.0.1:<mock server port>`

   - **API Key**: Any dummy key (e.g., `sk-mock-key`)

> **Note:** Alternatively, on Linux, you can run the container with host networking (`--network host`) and use `http://127.0.0.1:<mock server port>` inside the container.

### 4. Optimize Performance Settings

For accurate benchmarking results, configure these performance settings in the Bifrost Web UI:

**Buffer and Pool Configuration:**

1. Navigate to **Providers** → **OpenAI** → **Performance Settings**
2. Set **Concurrency**: `>7500` (recommended for high-load testing for concurrent connections)
3. Set **Buffer Size**: `>10000` (recommended for high-load testing)
4. Navigate to **Settings**
5. Set **Initial Pool Size**: `>10000` (recommended for high-load testing)

### 5. Disable Unnecessary Plugins (Optional)

To minimize latency overhead and memory usage during benchmarking:

1. Access **Settings** in the Bifrost Web UI
2. Disable any non-essential plugins such as:
   - Logging plugins (if detailed logs aren't required)
   - Governance plugins (if not needed for testing)
   - Semantic caching (if not needed for testing)

**Note:** Plugin overhead is typically minimal as nothing is computed in the hot path, but disabling them ensures the purest performance measurements.

### 6. Verify Bifrost Configuration

Before running benchmarks, verify your setup:

```bash
# Test Bifrost connectivity
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "messages": [{"role": "user", "content": "Hello"}],
    "model": "gpt-4o-mini"
  }'
```

Expected response: JSON with completion data or mock response (depending on your provider configuration).

### 7. Update Environment Configuration

Update your `.env` file to match your Bifrost setup:

```env
BIFROST_PORT=8080  # Default Bifrost port
# ... other provider ports
```

📖 **For advanced configuration options:** [Bifrost Documentation](https://docs.getbifrost.ai)

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

| Flag           | Type   | Default           | Description                                                     |
| -------------- | ------ | ----------------- | --------------------------------------------------------------- |
| `-rate`        | int    | 500               | Requests per second to send                                     |
| `-duration`    | int    | 10                | Test duration in seconds                                        |
| `-output`      | string | results.json      | Output file for benchmark results                               |
| `-cooldown`    | int    | 60                | Cooldown period between tests (seconds)                         |
| `-provider`    | string | ""                | Specific provider to test (bifrost, litellm, portkey, helicone) |
| `-big-payload` | bool   | false             | Use large ~10KB payloads instead of small ones                  |
| `-model`       | string | gpt-4o-mini       | Model identifier to use in requests                             |
| `-suffix`      | string | v1                | URL route suffix (e.g., v1, v2)                                 |
| `-prompt-file` | string | ""                | Path to a file containing the prompt to use                     |
| `-path`        | string | chat/completions  | API path to hit (e.g., 'chat/completions' or 'embeddings')      |
| `-request-type`| string | chat              | Type of request: 'chat' or 'embedding'                          |

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

### Embeddings Benchmarking

The benchmark tool supports testing embeddings endpoints with custom prompts:

**Basic embeddings test:**

```bash
go run benchmark.go -provider bifrost -request-type embedding -path embeddings -model text-embedding-ada-002
# Tests embeddings endpoint with default prompt
```

**Large prompt embeddings test (using prompt file):**

```bash
# First create a large prompt file (e.g., 10MB)
go run benchmark.go -provider bifrost -request-type embedding -path embeddings -prompt-file prompt.txt -model text-embedding-3-small -rate 10 -duration 30
# Tests embeddings with large prompts from file
```

**Custom path testing:**

```bash
go run benchmark.go -provider bifrost -path v1/embeddings -suffix "" -request-type embedding
# Uses full path without suffix duplication
```

### Prompt File Format

When using `-prompt-file`, the tool reads the entire file content as the prompt. The request index and timestamp are automatically prepended to prevent LLM prompt caching:

```
<request_index> <timestamp> <your prompt content>
```

For example, if your prompt file contains "Hello world", the actual request will be:
```
1 2024-01-15T10:30:00Z Hello world
```

### Request Types

The `-request-type` flag determines the payload format:

**Chat (default):**
```json
{
  "messages": [{"role": "user", "content": "<prompt>"}],
  "model": "openai/<model>"
}
```

**Embedding:**
```json
{
  "input": "<prompt>",
  "model": "openai/<model>"
}
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
