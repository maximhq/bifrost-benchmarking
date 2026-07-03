# Bifrost Benchmarking

Benchmarking and load-testing tools for [Bifrost](https://github.com/maximhq/bifrost), the fastest and most scalable AI gateway. This is the companion repo to [Run Your Own Benchmarks](https://docs.getbifrost.ai/benchmarking/run-your-own-benchmarks) — everything here is open source, and PRs are welcome.

## What's in this repo

| Tool | What it is | Use it when you want to |
| --- | --- | --- |
| [`benchmark.go`](#gateway-benchmark-benchmarkgo) (this directory) | Gateway comparison benchmark built on Vegeta | Compare Bifrost against LiteLLM, Portkey, or raw OpenAI — latency percentiles, throughput, and server memory usage |
| [`hitter/`](hitter/README.md) | Standalone load generator for chat completions | Load-test a single Bifrost deployment with realistic traffic: multiple models/providers, streaming, virtual keys, PDF attachments |
| [`mocker/`](mocker/README.md) | Mock LLM provider server (fasthttp) | Simulate OpenAI / Anthropic / Gemini / Bedrock endpoints with configurable latency, failures, and rate limits — no API costs, no provider noise |
| [`mcp-code-mode-benchmark/`](mcp-code-mode-benchmark/README.md) | MCP Code Mode benchmark (Python) | Reproduce our token/latency/pass-rate numbers for [Bifrost's MCP Code Mode](https://docs.getbifrost.ai/mcp/code-mode) |

The gateway benchmark is documented in full below. The other three tools each have their own README — follow the links above.

**Typical setup:** run the **mocker** as a stand-in provider, point the gateways at it, and drive load with **`benchmark.go`** (to compare gateways) or the **hitter** (to stress Bifrost in isolation). Using the mocker isolates *gateway overhead* from provider latency and keeps runs free and reproducible.

## Quickstart

```bash
git clone https://github.com/maximhq/bifrost-benchmarking.git
cd bifrost-benchmarking
go mod tidy
go build benchmark.go
```

**1. Start the mock provider** (optional, but recommended — see [mocker/README.md](mocker/README.md) for all flags):

```bash
cd mocker && go run main.go -port 8000
```

**2. Start Bifrost** and point an OpenAI provider at the mocker (base URL `http://localhost:8000`, any dummy API key):

```bash
npx -y @maximhq/bifrost
# or: docker run -p 8080:8080 maximhq/bifrost
```

Setup details, performance tuning (concurrency, buffer and pool sizes), and Docker networking notes for reaching the mocker from inside a container are in the [Bifrost docs](https://docs.getbifrost.ai/quickstart/gateway/setting-up).

**3. Create a `.env`** in the repo root with the port of each gateway you plan to benchmark:

```env
BIFROST_PORT=8080
LITELLM_PORT=4000
PORTKEY_PORT=8787
OPENAI_API_KEY=sk-...   # only needed for the openai/portkey providers
```

**4. Run the benchmark:**

```bash
./benchmark -provider bifrost -rate 500 -duration 30
```

Results land in `results.json` (keyed by provider, latest run per provider).

## Gateway benchmark (`benchmark.go`)

A command-line tool that fires load at one or more gateways and records latency percentiles, throughput, success rates, drop reasons, and server-side memory usage (it finds the server process by port and samples RSS during the run).

### Flags

| Flag | Type | Default | Description |
| --- | --- | --- | --- |
| `-rate` | int | 0 (required\*) | Requests per second (mutually exclusive with `-users`) |
| `-users` | int | 0 (required\*) | Concurrent users to maintain (mutually exclusive with `-rate`) |
| `-duration` | int | 10 | Test duration in seconds |
| `-timeout` | int | 300 | Request timeout in seconds (set to duration + expected backend latency) |
| `-output` | string | results.json | Output file for results |
| `-cooldown` | int | 60 | Cooldown between provider tests in seconds |
| `-provider` | string | "" | Provider to benchmark: `bifrost`, `litellm`, `portkey`, or `openai`. **Empty runs all four** |
| `-big-payload` | bool | false | Use a ~10KB payload instead of the ~200B default |
| `-model` | string | gpt-4o-mini | Model to put in the request payload |
| `-suffix` | string | v1 | URL route suffix (e.g. `v1`) |
| `-prompt-file` | string | "" | Path to a file whose content is used as the prompt |
| `-path` | string | chat/completions | API path to hit (e.g. `chat/completions` or `embeddings`) |
| `-request-type` | string | chat | `chat` or `embedding` — controls payload shape |
| `-host` | string | localhost | Host address of the gateway servers |
| `-ramp-up` | bool | false | Gradually ramp users up (only with `-users`, requires `-ramp-up-duration`) |
| `-ramp-up-duration` | int | 0 | Seconds to ramp from 1 to `-users` users |
| `-debug` | bool | false | Detailed logging and periodic status updates during the run |

\* Exactly one of `-rate` or `-users` must be provided.

> **Note:** omitting `-provider` benchmarks *all* providers sequentially — including `openai`, which sends real requests to `api.openai.com` using your `OPENAI_API_KEY`. Pass `-provider` explicitly unless that's what you want.

### Rate-based vs concurrent-users mode

**`-rate` (fixed RPS, via Vegeta):** sends requests at a constant rate regardless of response times. Best for measuring throughput capacity and latency under a known load.

```bash
./benchmark -provider bifrost -rate 1000 -duration 30
```

**`-users` (fixed concurrency, via the `pkg/concurrent` semaphore):** keeps exactly N requests in flight — as one completes, the next is dispatched. Throughput becomes `≈ users / avg_latency`. Best for simulating connection pools and realistic client behavior.

```bash
./benchmark -provider bifrost -users 250 -duration 60
```

**Ramp-up** (only with `-users`): grow from 1 to N users over a window, then hold.

```bash
./benchmark -provider bifrost -users 500 -duration 600 -ramp-up -ramp-up-duration 120
```

### More examples

```bash
# Compare two gateways back to back with large payloads
./benchmark -provider bifrost -rate 2000 -duration 300 -big-payload -output bifrost.json
./benchmark -provider litellm -rate 2000 -duration 300 -big-payload -output litellm.json

# Quick smoke test
./benchmark -provider bifrost -rate 100 -duration 5 -cooldown 10

# High-latency backend: allow requests started late in the run to finish
./benchmark -provider bifrost -rate 500 -duration 600 -timeout 1200

# Embeddings with a large prompt file
./benchmark -provider bifrost -request-type embedding -path embeddings \
  -prompt-file 10kbprompt.txt -model text-embedding-3-small -rate 10 -duration 30
```

### Payloads

`chat` requests look like `{"messages":[{"role":"user","content":"<prompt>"}],"model":"openai/<model>"}`; `embedding` requests use `{"input":"<prompt>","model":"openai/<model>"}` (the raw OpenAI provider drops the `openai/` prefix). The request index and timestamp are prepended to every prompt to defeat prompt caching. With `-prompt-file`, the whole file becomes the prompt — `10kbprompt.txt` and `50kbprompt.txt` in the repo root are ready-made fixtures. Portkey requests automatically get an `x-portkey-config` header carrying your OpenAI key.

### Output

Each run writes per-provider metrics to the output file:

```json
{
  "bifrost": {
    "requests": 5000,
    "success_rate": 99.8,
    "mean_latency_ms": 45.2,
    "p50_latency_ms": 42.1,
    "p99_latency_ms": 156.7,
    "max_latency_ms": 203.4,
    "throughput_rps": 498.5,
    "status_code_counts": { "200": 4990, "500": 10 },
    "server_peak_memory_mb": 256.7,
    "server_avg_memory_mb": 189.3,
    "drop_reasons": { "HTTP 500": 10 }
  }
}
```

Memory stats come from sampling the RSS of the process listening on the provider's configured port, so run the tool on the same machine as the gateways (or expect empty memory stats).

### Troubleshooting

- **"No process found on port"** — the gateway isn't running, or the `.env` port is wrong. The benchmark still runs; only memory stats are skipped.
- **"Attack for [Provider] timed out"** — raise `-timeout`; it must cover `duration + backend latency`.
- **Low request counts with `-users`** — expected: total requests ≈ `users × duration / avg_latency`. High backend latency means few requests.
- **All requests failing** — verify connectivity first: `curl -X POST http://localhost:8080/v1/chat/completions -H "Content-Type: application/json" -d '{"messages":[{"role":"user","content":"Hi"}],"model":"gpt-4o-mini"}'`

## Repo layout

```
benchmark.go              # gateway comparison benchmark (documented above)
pkg/concurrent/           # semaphore-based concurrency engine for -users mode
hitter/                   # load generator for Bifrost — see hitter/README.md
mocker/                   # mock LLM provider server — see mocker/README.md
mcp-code-mode-benchmark/  # MCP Code Mode benchmark — see its README.md
10kbprompt.txt            # prompt fixtures for -prompt-file / large-payload runs
50kbprompt.txt
```

## Contributing

PRs are welcome. If you add flags or change behavior in a tool, update that tool's README in the same PR — the root README only documents `benchmark.go` and links out for everything else.

## License

Licensed under the [Apache License 2.0](LICENSE).
