# Mock OpenAI API Server

This directory contains a simple mock server that mimics the OpenAI API's chat completions endpoint (`/v1/chat/completions`). It's designed for testing and benchmarking, allowing simulation of OpenAI API responses without live server interaction.

## Features

- Responds to `POST` requests at `/v1/chat/completions`.
- Returns a predefined mock chat completion response.
- Simulates network latency via the `-latency` flag.
- Configurable listening port via the `-port` flag.

## Prerequisites

- Go installed on your system.

## Getting Started

### 1. Running the Mock Server

Navigate to the `mocker` directory and use `go run`:

```bash
# Navigate to the mocker directory
cd mocker

# Run the mock server (default: port 8000, 0ms latency)
go run main.go
```

To specify a port and latency:

```bash
go run main.go -port 8080 -latency 100
# This runs on port 8080 with 100ms simulated latency.
```

### 2. Command-Line Flags

- `-port <port_number>`: Port for the mock server (default: `8000`).
- `-latency <milliseconds>`: Simulated latency per request (default: `0`).
