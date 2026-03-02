#!/bin/bash

# Load test across ALL Bifrost-supported providers at 10 RPS
# Runs all providers in parallel for 1 minute, then repeats with --stream

RPS=10
DURATION="60s"
URL="http://localhost:8080/v1/chat/completions"
EXTRA_FLAGS="${@}" # pass-through any extra flags like --url, --virtual-key, etc.

# provider=models (one per line)
ENTRIES=(
  "openai=gpt-4o,gpt-4o-mini"
  "anthropic=claude-sonnet-4-20250514"
  "gemini=gemini-2.0-flash"
  "groq=llama-3.3-70b-versatile"
  "mistral=mistral-large-latest"
  "ollama=llama3"
  "sgl=Llama-3-8B-Instruct"
  "parasail=Llama-3-8B-Instruct"
  "perplexity=sonar-pro"
  "cerebras=llama-3.3-70b"
  "openrouter=gpt-4o"
)

PIDS=()

run_round() {
  local mode="$1"
  local stream_flag="$2"

  echo ""
  echo "##############################################"
  echo "# Round: $mode (all providers in parallel)"
  echo "##############################################"
  echo ""

  PIDS=()

  for entry in "${ENTRIES[@]}"; do
    provider="${entry%%=*}"
    models="${entry#*=}"

    echo "Starting $provider ($mode) — models: $models"

    go run main.go \
      --rps "$RPS" \
      --duration "$DURATION" \
      --url "$URL" \
      --providers "$provider" \
      --models "$models" \
      --verbose \
      $stream_flag \
      $EXTRA_FLAGS \
      2>&1 | sed "s/^/[$provider|$mode] /" &

    PIDS+=($!)
  done

  echo ""
  echo "Waiting for all $mode processes to finish..."
  for pid in "${PIDS[@]}"; do
    wait "$pid"
  done

  echo ""
  echo "--- $mode round complete ---"
  echo ""
}

run_round "non-stream" ""
run_round "stream" "--stream"

echo "All load tests complete."
