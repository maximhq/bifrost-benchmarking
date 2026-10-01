# RouterArena model-routing benchmark

This benchmark compares answer quality, spend, and latency for fixed Claude Opus 5.5, Bifrost JEV, LiteLLM heuristic v2, and Bifrost semantic routing. Each answer was scored with RouterArena's dataset-specific verifier.

## Full answer-quality comparison

RouterArena has 8,400 prompts. The paired comparison uses the same **8,378 task IDs** for each arm; 22 IDs were excluded during shared-set preparation because at least one arm lacked a usable terminal outcome. No-text outcomes within the paired set score zero, keeping the denominator consistent.


| Arm                  | Tasks | Mean task score | Solved         | Reported cost | Cost per solved task | Mean / P95 latency |
| -------------------- | ----- | --------------- | -------------- | ------------- | -------------------- | ------------------ |
| Opus 5.5 baseline    | 8,378 | 80.81%          | 6,451 (77.00%) | $53.45        | $0.00829             | 4.23s / 8.21s      |
| Bifrost JEV          | 8,378 | 77.78%          | 6,222 (74.27%) | $34.21        | $0.00550             | 4.96s / 10.68s     |
| LiteLLM heuristic v2 | 8,378 | 75.85%          | 6,097 (72.77%) | $44.55        | $0.00731             | 5.06s / 8.48s      |
| Bifrost semantic     | 8,378 | 74.99%          | 5,998 (71.59%) | ~$41.04       | $0.00684             | 5.21s / 10.98s     |


Mean task score includes partial credit where the task verifier supports it. “Solved” counts only a score of 1.0. JEV uses the highest-scoring valid answer per task, with lower cost breaking score ties; the two paired tasks without a valid answer score zero. Its $34.21 is the selected-answer cost, including recorded routing cost; total spend across all JEV request logs was $55.96. Latency is end-to-end for valid selected answers and includes JEV's in-gateway classification. Semantic spend is approximate from LLM logs (~$41.04); a separate ~$0.421 embedding-routing estimate was not independently reconciled against that figure. These are local gateway benchmark results, not an official RouterArena leaderboard submission.

The comparison uses RouterArena's `full` split at revision `[a4a062ce3313b56bb09c042e1bc37b61d34e3bd8](https://huggingface.co/datasets/RouteWorks/RouterArena/tree/a4a062ce3313b56bb09c042e1bc37b61d34e3bd8)`. The evaluator assigns zero to unusable generations; refusals or length-limited answers returned as valid generations are scored by the dataset verifier. See the [official scoring and cost aggregation](https://github.com/RouteWorks/RouterArena/blob/main/llm_evaluation/run.py#L989-L1028) and [generation-validity check](https://github.com/RouteWorks/RouterArena/blob/main/llm_evaluation/run.py#L119-L129).

This is a comparison of the configured systems, not a router-only test with identical model pools. Bifrost used its Haiku 4.5 / Sonnet 5 / Opus 5.5 tiers; the configured LiteLLM alias may also use Fable 5.1. Differences can therefore reflect both routing and model choice.

Selected answer-model split:


| Router               | Haiku 4.5    | Sonnet 5      | Opus 5.5       | Total |
| -------------------- | ------------ | ------------- | -------------- | ----- |
| Bifrost Jev          | 2,124        | 6,102         | 152            | 8,378 |
| LiteLLM Heuristic v2 | 1,957 ( ~$3) | 2,582 ( ~$19) | 3,839 (~$22.5) | 8,378 |
| Bifrost semantic     | 2,779        | 2,382         | 3,217          | 8,378 |

The JEV counts include two no-text tasks assigned to Sonnet 5; the other 8,376 counts correspond to valid answers.




## Reproduce the answer-quality benchmark

1. Prepare RouterArena and its scorer assets:
  ```sh
   git clone https://github.com/RouteWorks/RouterArena.git /path/to/RouterArena
   cd /path/to/RouterArena
   uv sync
   uv run python ./scripts/process_datasets/prep_datasets.py
  ```
2. Set local endpoints and keys in your shell. The harness does not read or write credentials:
  ```sh
   export BIFROST_BASE_URL=http://127.0.0.1:8080
   export BIFROST_VK='your-bifrost-key'
   export LITELLM_BASE_URL=http://127.0.0.1:4000
   export LITELLM_API_KEY='your-litellm-key'
   export ROUTERARENA_DIR=/path/to/RouterArena
  ```
   Configure Bifrost's active classifier and tier rules for the arm you are running. The harness calls Bifrost using the same chat-completions request for fixed Opus and JEV; gateway configuration determines the route. LiteLLM uses the `benchmarking-heuristic-v2` alias. This measures the configured end-to-end systems, not just classifier implementations.
3. Check the harness without sending model requests, then run a small preflight:
  ```sh
   uv run --project "$ROUTERARENA_DIR" python model-routing-benchmark/router-arena/run_benchmark.py \
     --routerarena-root "$ROUTERARENA_DIR" --limit 20 --dry-run

   uv run --project "$ROUTERARENA_DIR" python model-routing-benchmark/router-arena/run_benchmark.py \
     --routerarena-root "$ROUTERARENA_DIR" --limit 20 \
     --output-dir model-routing-benchmark/router-arena/runs/preflight-20
  ```
4. After checking the preflight and estimating spend, run the 8,400-row comparison:
  ```sh
   uv run --project "$ROUTERARENA_DIR" python model-routing-benchmark/router-arena/run_benchmark.py \
     --routerarena-root "$ROUTERARENA_DIR" --concurrency 16 \
     --output-dir model-routing-benchmark/router-arena/runs/full
  ```

The script supports `--arms opus-baseline`, `--arms bifrost-jev`, `--arms litellm-v2`, and `--arms bifrost-semantic` for individual arms. Bifrost semantic routing needs its own active semantic configuration and virtual key. Run outputs are written under `router-arena/runs/` and ignored by Git. They include full prompt and answer text; do not commit them. RouterArena's dataset card did not declare a license when checked for this study, so prompts and answers are intentionally not included here.
