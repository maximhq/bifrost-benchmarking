## Benchmarking with LiteLLM Proxy

LiteLLM provides a proxy that can be used as an alternative backend for benchmarking.

### 1. Run LiteLLM Proxy using Docker

Ensure your `OPENAI_API_KEY` (or other relevant provider keys) environment variable is set:

```bash
export OPENAI_API_KEY="YOUR_OPENAI_API_KEY"
```

Run the LiteLLM Docker container (where `litellm_config.yaml` is located):

```bash
docker run \
    -v $(pwd)/litellm_config.yaml:/app/config.yaml \
    -e OPENAI_API_KEY \
    -p 4000:4000 \
    ghcr.io/berriai/litellm:main-latest \
    --config /app/config.yaml --detailed_debug
```

This will start the LiteLLM proxy, typically listening on `http://0.0.0.0:4000`.

### 2. Run the Benchmark Tool against LiteLLM

In a separate terminal, from the parent directory, run the benchmark tool targeting the LiteLLM proxy.

```bash
go run benchmark.go -provider litellm -port 4000 -rate 100 -duration 20
```

**Note:**

- The `OPENAI_API_KEY` in the `docker run` command is passed as an environment variable _into_ the Docker container. LiteLLM, when configured with `api_key: os.environ/OPENAI_API_KEY` in its `config.yaml`, will pick it up from there.
- Ensure the `$(pwd)/litellm_config.yaml` path correctly points to your configuration file relative to where you run the `docker run` command.
