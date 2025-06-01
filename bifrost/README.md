# Bifrost Gateway

This directory contains a sample implementation of an API gateway using the Bifrost core library. It sets up an HTTP server to proxy requests to backend AI providers (e.g., OpenAI).

## Features

- Proxies chat completion requests (defaults to OpenAI).
- Configurable port, OpenAI API key, and optional upstream proxy.
- Command-line flags for performance tuning (concurrency, buffer sizes).
- Debug mode (`-debug` flag):
  - Exposes a `/metrics` endpoint with runtime statistics and a recent request log.
  - Enables verbose logging for individual requests via `DebugHandler`.
  - Optionally disables Garbage Collection (for specific benchmarking/testing, not for production).
- Graceful shutdown on `SIGINT`/`SIGTERM` signals.

## Prerequisites

- Go installed on your system.
- An OpenAI API key, set as an environment variable `OPENAI_API_KEY`.

## Getting Started

### 1. Running the Gateway

First, ensure your `OPENAI_API_KEY` environment variable is set:

```bash
export OPENAI_API_KEY="YOUR_OPENAI_API_KEY"
# Or add it to your shell's configuration file (e.g., .zshrc, .bashrc)
```

Then, navigate to the `bifrost` directory and run the gateway using `go run`:

```bash
# Navigate to the bifrost directory
cd bifrost

# Run the gateway
go run main.go
```

The server will start, typically on port `3001` by default.

### 2. Command-Line Flags

You can customize the gateway's behavior using these flags:

- `-port <port_number>`: Port for the server (default: `3001`).
- `-proxy <proxy_url>`: Optional upstream proxy URL (e.g., `http://localhost:8080`).
- `-debug`: Enable debug mode (default: `false`). This enables `/metrics` and detailed logging.
  - **Note:** For the `DebugHandler` to show detailed Bifrost client metrics (like `bifrost_timings`, `provider_metrics`), the Bifrost client itself needs its internal debug mode enabled. See Bifrost core library docs for details.
- `-concurrency <number>`: Bifrost client concurrency level (default: `20000`). Affects HTTP client's `MaxIdleConnsPerHost`.
- `-buffer-size <number>`: Bifrost client buffer size (default: `25000`). Informational for `BaseAccount`.
- `-initial-pool-size <number>`: Initial size for Bifrost's internal object pools (default: `25000`).

## Benchmarking

This gateway can be benchmarked with the `benchmark.go` tool from the parent directory.

### 1. Start the Bifrost Gateway

Ensure your `OPENAI_API_KEY` environment variable is set. In one terminal, from the `bifrost` directory:

```bash
export OPENAI_API_KEY="YOUR_OPENAI_API_KEY" # If not already set
cd bifrost
go run main.go -port 3001
# Add -debug for gateway debug features (see note above about client debug mode).
```

Ensure the port matches the one used in the benchmark command.

### 2. Run the Benchmark Tool

In a separate terminal, from the parent directory:

```bash
go run benchmark.go -provider bifrost -port 3001 -rate 100 -duration 20
```

## Alternative: Direct Bifrost HTTP Transport Benchmarking

For benchmarking the core Bifrost request path without this sample gateway's HTTP server layer, you can integrate Bifrost's HTTP transport directly into your benchmarking setup.

If using the provided `benchmark.go` tool for this, set the `-include-provider-in-request` flag to `true`. This allows the benchmark tool to send a payload specifying the target provider.

For details on direct transport usage, refer to the Bifrost documentation: [Bifrost Transports](https://github.com/maximhq/bifrost/tree/main/transports)
