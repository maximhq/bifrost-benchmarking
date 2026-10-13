# Bifrost complexity-router benchmarks

These evaluations measure end-to-end task completion through Bifrost's complexity router, including verifier score, selected model spend, and latency. They use different datasets and scoring methods, so compare systems within each benchmark rather than comparing percentages across datasets.

## Task-completion benchmarks

### Terminal-Bench 2.1

77 shared tasks, scored by Terminal-Bench's task verifiers. The comparison includes fixed Claude Opus 5.5, Bifrost JEV, LiteLLM heuristic v2, and Bifrost semantic routing with defaults. Modal compute is excluded from spend.

| System | Solved | Spend | Spend per solved task |
| --- | ---: | ---: | ---: |
| Opus 5.5 baseline | 74/77 (96.1%) | $21.233 | $0.287 |
| Bifrost JEV | 73/77 (94.8%) | $20.583 | $0.282 |
| LiteLLM heuristic v2 | 40/77 (51.9%) | $28.009 | $0.700 |
| Bifrost semantic (defaults) | 55/77 (71.4%) | $30.712 | $0.558 |

See [Terminal-Bench details and run instructions](terminal-bench/README.md).

### RouterArena

8,378 shared prompts from RouterArena's 8,400-item full split, scored with its dataset-specific verifiers. “Mean task score” includes partial credit where supported; “solved” requires a score of 1.0.

| System | Mean task score | Solved | Reported cost | Cost per solved task | Mean / P95 latency |
| --- | ---: | ---: | ---: | ---: | ---: |
| Opus 5.5 baseline | 80.81% | 6,451/8,378 (77.00%) | $53.45 | $0.00829 | 4.23s / 8.21s |
| Bifrost JEV | 77.78% | 6,222/8,378 (74.27%) | $34.21 | $0.00550 | 4.96s / 10.68s |
| LiteLLM heuristic v2 | 75.85% | 6,097/8,378 (72.77%) | $44.55 | $0.00731 | 5.06s / 8.48s |
| Bifrost semantic | 74.99% | 5,998/8,378 (71.59%) | ~$41.04 | $0.00684 | 5.21s / 10.98s |

JEV's reported RouterArena result selects the highest-scoring valid answer per task, with lower cost breaking ties; tasks without a valid answer score zero. Its $34.21 is selected-answer spend including recorded JEV routing cost; all logged JEV requests totaled $55.96. Latency covers valid selected responses, including in-gateway classification. The semantic spend is approximate based on LLM logs; the separate ~$0.421 embedding-routing estimate was not independently reconciled against that figure.

See [RouterArena results and reproduction instructions](router-arena/README.md).
