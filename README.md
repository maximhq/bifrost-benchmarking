# API Provider Benchmarking Tool

This tool is designed to benchmark the performance of various API providers. It sends a configurable number of requests per second for a specified duration and measures key performance indicators like latency, success rate, throughput, and server-side resource usage (memory).

## Prerequisites

- Go (version 1.23 or higher recommended)
- The target API provider/server must be running and accessible.

## Building

To build the benchmark executable:

```bash
go build benchmark.go
```

This will create an executable named `benchmark` (or `benchmark.exe` on Windows) in the current directory.

## Running the Benchmark

You can run the benchmark using `go run benchmark.go` or by first building it and then running the executable (`./benchmark`).

The tool uses command-line flags to configure the benchmark parameters:

- `-provider <name>`: **Required**. Specifies the name of the provider being benchmarked (e.g., `bifrost`, `litellm`, `your-custom-provider`). This name is used for logging and in the results file.
- `-port <port_number>`: **Required**. The port number on which the target provider's API server is listening (e.g., `3001`, `8080`).
- `-endpoint <path>`: The specific API endpoint to target (default: `v1/chat/completions`). Example: `v1/embeddings`.
- `-rate <number>`: The number of requests per second to send (default: `500`).
- `-duration <seconds>`: The duration of the test in seconds (default: `10`).
- `-output <filename.json>`: The file to save the benchmark results to (default: `results.json`).
- `-include-provider-in-request`: A boolean flag (true/false). If set to `true`, the provider name specified with the `-provider` flag will be included in the request payload under the "provider" key. This is useful for gateways that route requests based on a provider field in the payload (default: `false`).
- `-big-payload`: A boolean flag (true/false). If set to `true`, a larger, more complex payload will be used for requests. If `false` (default), a smaller, simpler payload is used.

### Example Usage

To benchmark a provider named "bifrost" running on port `3001`, sending 100 requests per second for 30 seconds, and saving results to `bifrost_results.json`:

```bash
go run benchmark.go -provider bifrost -port 3001 -rate 100 -duration 30 -output bifrost_results.json
```

Or, if you have built the executable:

```bash
./benchmark -provider bifrost -port 3001 -rate 100 -duration 30 -output bifrost_results.json
```

To benchmark a provider "litellm" on port `8000`, using the bigger payload and including the provider name in the request:

```bash
go run benchmark.go -provider litellm -port 8000 -big-payload=true -include-provider-in-request=true
```

## Output

The benchmark results are saved in JSON format in the specified output file. The results are stored in a map where the key is the lowercase provider name. This allows results from multiple benchmark runs (even for different providers) to be aggregated into a single file. Each entry includes:

- Request counts
- Success rates
- Latency metrics (mean, P50, P99, max)
- Throughput
- Timestamp of the benchmark
- Status code counts
- Server memory usage (before, after, peak, average) in MB
- Reasons for dropped requests

## Extending

The tool can be used to benchmark any API provider that has an HTTP endpoint. The payload structure in `initializeProvider` can be modified or made more dynamic if different providers require vastly different request bodies.
