#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat >&2 <<'EOF'
Usage: ./run.sh opus|jev|litellm

Configure the relevant local endpoint and key before running. See README.md.
Optional: TB_N_CONCURRENT (default: 3).
EOF
  exit 2
}

[[ $# -eq 1 ]] || usage
arm=$1
concurrency=${TB_N_CONCURRENT:-3}

case "$arm" in
  opus|jev)
    : "${BIFROST_BENCH_ANTHROPIC_URL:?Set BIFROST_BENCH_ANTHROPIC_URL to the Bifrost Anthropic-compatible endpoint}"
    : "${BIFROST_BENCH_VK:?Set BIFROST_BENCH_VK to the Bifrost benchmark virtual key}"
    base_url=$BIFROST_BENCH_ANTHROPIC_URL
    auth_token=$BIFROST_BENCH_VK
    model=anthropic/claude-opus-5-5
    ;;
  litellm)
    : "${LITELLM_ANTHROPIC_BASE_URL:?Set LITELLM_ANTHROPIC_BASE_URL to the LiteLLM Anthropic-compatible endpoint}"
    : "${LITELLM_API_KEY:?Set LITELLM_API_KEY to the LiteLLM virtual key}"
    base_url=$LITELLM_ANTHROPIC_BASE_URL
    auth_token=$LITELLM_API_KEY
    model=benchmarking-heuristic-v2
    ;;
  *) usage ;;
esac

[[ "$concurrency" =~ ^[1-9][0-9]*$ ]] || { echo "TB_N_CONCURRENT must be a positive integer" >&2; exit 2; }

jobs_dir="$(cd "$(dirname "$0")" && pwd)/jobs"
mkdir -p "$jobs_dir"
args=(
  --dataset terminal-bench/terminal-bench-2-1
  --include-task-name terminal-bench/adaptive-rejection-sampler
  --include-task-name terminal-bench/bn-fit-modify
  --include-task-name terminal-bench/break-filter-js-from-html
  --include-task-name terminal-bench/build-cython-ext
  --include-task-name terminal-bench/build-pmars
  --include-task-name terminal-bench/build-pov-ray
  --include-task-name terminal-bench/caffe-cifar-10
  --include-task-name terminal-bench/cancel-async-tasks
  --include-task-name terminal-bench/chess-best-move
  --include-task-name terminal-bench/circuit-fibsqrt
  --include-task-name terminal-bench/cobol-modernization
  --include-task-name terminal-bench/code-from-image
  --include-task-name terminal-bench/compile-compcert
  --include-task-name terminal-bench/configure-git-webserver
  --include-task-name terminal-bench/constraints-scheduling
  --include-task-name terminal-bench/count-dataset-tokens
  --include-task-name terminal-bench/crack-7z-hash
  --include-task-name terminal-bench/custom-memory-heap-crash
  --include-task-name terminal-bench/db-wal-recovery
  --include-task-name terminal-bench/distribution-search
  --include-task-name terminal-bench/extract-elf
  --include-task-name terminal-bench/feal-differential-cryptanalysis
  --include-task-name terminal-bench/feal-linear-cryptanalysis
  --include-task-name terminal-bench/filter-js-from-html
  --include-task-name terminal-bench/financial-document-processor
  --include-task-name terminal-bench/fix-code-vulnerability
  --include-task-name terminal-bench/fix-git
  --include-task-name terminal-bench/fix-ocaml-gc
  --include-task-name terminal-bench/gcode-to-text
  --include-task-name terminal-bench/git-leak-recovery
  --include-task-name terminal-bench/git-multibranch
  --include-task-name terminal-bench/gpt2-codegolf
  --include-task-name terminal-bench/headless-terminal
  --include-task-name terminal-bench/hf-model-inference
  --include-task-name terminal-bench/install-windows-3.11
  --include-task-name terminal-bench/kv-store-grpc
  --include-task-name terminal-bench/large-scale-text-editing
  --include-task-name terminal-bench/largest-eigenval
  --include-task-name terminal-bench/log-summary-date-ranges
  --include-task-name terminal-bench/mailman
  --include-task-name terminal-bench/make-mips-interpreter
  --include-task-name terminal-bench/mcmc-sampling-stan
  --include-task-name terminal-bench/merge-diff-arc-agi-task
  --include-task-name terminal-bench/model-extraction-relu-logits
  --include-task-name terminal-bench/modernize-scientific-stack
  --include-task-name terminal-bench/mteb-leaderboard
  --include-task-name terminal-bench/mteb-retrieve
  --include-task-name terminal-bench/multi-source-data-merger
  --include-task-name terminal-bench/nginx-request-logging
  --include-task-name terminal-bench/openssl-selfsigned-cert
  --include-task-name terminal-bench/overfull-hbox
  --include-task-name terminal-bench/path-tracing
  --include-task-name terminal-bench/path-tracing-reverse
  --include-task-name terminal-bench/polyglot-c-py
  --include-task-name terminal-bench/portfolio-optimization
  --include-task-name terminal-bench/prove-plus-comm
  --include-task-name terminal-bench/pypi-server
  --include-task-name terminal-bench/pytorch-model-cli
  --include-task-name terminal-bench/pytorch-model-recovery
  --include-task-name terminal-bench/raman-fitting
  --include-task-name terminal-bench/regex-chess
  --include-task-name terminal-bench/regex-log
  --include-task-name terminal-bench/reshard-c4-data
  --include-task-name terminal-bench/rstan-to-pystan
  --include-task-name terminal-bench/sam-cell-seg
  --include-task-name terminal-bench/sanitize-git-repo
  --include-task-name terminal-bench/schemelike-metacircular-eval
  --include-task-name terminal-bench/sparql-university
  --include-task-name terminal-bench/sqlite-db-truncate
  --include-task-name terminal-bench/sqlite-with-gcov
  --include-task-name terminal-bench/torch-pipeline-parallelism
  --include-task-name terminal-bench/torch-tensor-parallelism
  --include-task-name terminal-bench/train-fasttext
  --include-task-name terminal-bench/tune-mjcf
  --include-task-name terminal-bench/video-processing
  --include-task-name terminal-bench/vulnerable-secret
  --include-task-name terminal-bench/winning-avg-corewars
)

run=(uvx --from 'harbor[modal]==0.23.0' harbor run "${args[@]}"
  --agent claude-code
  --model "$model"
  --agent-kwarg version=2.1.280
  --agent-env "ANTHROPIC_BASE_URL=$base_url"
  --agent-env "ANTHROPIC_AUTH_TOKEN=$auth_token"
  --agent-env CLAUDE_CODE_DISABLE_NONSTREAMING_FALLBACK=1
  --agent-env CLAUDE_CODE_MAX_RETRIES=2
  --env modal
  --n-attempts 1
  --n-concurrent "$concurrency"
  --max-retries 0
  --jobs-dir "$jobs_dir"
  --job-name "tb21-${arm}-77-$(date -u +%Y%m%dT%H%M%SZ)")

if command -v caffeinate >/dev/null 2>&1; then
  caffeinate -i "${run[@]}"
else
  "${run[@]}"
fi
