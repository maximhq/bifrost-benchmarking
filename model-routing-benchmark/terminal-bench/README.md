# Terminal-Bench 2.1 model routing Benchmark

This report compares fixed Claude Opus 5.5, Bifrost JEV, Bifrost Semantic with defaults, and LiteLLM heuristic v2 on the same 77 Terminal-Bench 2.1 tasks. It records verifier outcomes and gateway spend; Modal compute is excluded.

## Results

Each arm is compared on the same 77 tasks. “Cost per solved task” is total gateway model spend divided by verifier-passed tasks. Cost includes sessions that completed with a verifier failure; Modal compute is excluded.


| Run                         | Solved | Solve rate | Gateway spend | Cost per solved task |
| --------------------------- | ------ | ---------- | ------------- | -------------------- |
| Opus 5.5 fixed baseline     | 74/77  | 96.1%      | $21.233       | $0.287               |
| Bifrost JEV router          | 73/77  | 94.8%      | $20.583       | $**0.282**           |
| LiteLLM Heuristic v2        | 40/77  | 51.9%      | $28.009       | $0.700               |
| Bifrost Semantic (defaults) | 55/77  | 71.4%      | $30.712       | $0.558               |


The selected outcomes contain one task session per arm per task. Cost is from Bifrost `public.logs` for the Bifrost arms and LiteLLM `public.LiteLLM_SpendLogs` for LiteLLM. Failed verifier sessions are included in spend. For Bifrost Semantic with defaults, all 77 trials have verifier results: 55 pass and 22 fail. The 77 selected Semantic sessions cost $30.712395 in gateway logs ($0.558407 per solved task). Modal compute is excluded.

## Findings

- Bifrost Semantic (defaults) solved **55/77 tasks (71.4%)** at **$30.712395** gateway spend, or **$0.558407 per solved task**.
- JEV spent **$20.583278** and solved **73/77 tasks (94.8%)**. Compared with fixed Opus, that is **3.1% lower total spend** and **1.7% lower spend per solved task** ($0.281963 vs. $0.286939), while solving one fewer task.
- JEV sessions used Opus 5.5 in **31** task sessions, Sonnet 5 in **48**, and Haiku 4.5 in **4**; these counts overlap when a task session used multiple models.
- JEV's four verifier failures were build-pov-ray, filter-js-from-html, raman-fitting, and train-fasttext.
- LiteLLM solved **40/77 tasks** for **$28.009110**. Its total spend was **31.9% higher than Opus** and **36.1% higher than JEV**; its $0.700228 per solved task was **2.44× Opus** and **2.48× JEV**.
- In LiteLLM's selected sessions, **58 tasks used Haiku only** and 23 passed (39.7%); **15 used Sonnet only** and 14 passed (93.3%); **4 used both models** and 3 passed. In total, 62 sessions included Haiku and 19 included Sonnet, so four sessions switched between them. This pattern is consistent with the router sending too many benchmark tasks to Haiku for their needs; it does not by itself prove that model choice caused each failure.
- The largest spend came from repeated long-context work: the ten highest-cost tasks totaled **$12.323**. The leaders were `circuit-fibsqrt` ($2.343, 24 requests, Sonnet), `fix-ocaml-gc` ($2.196, 88 requests, Sonnet), and `mailman` ($1.439, 70 requests, both models).
- Prompt caching was active: LiteLLM recorded 100.4M cache-read input tokens out of 103.8M prompt tokens (96.7%), plus 2.85M cache-write tokens. Cache reads are discounted, not free. The high spend is mainly the volume of repeated requests and long prompts, not an absence of cache hits.

## Model usage by router

Each router has 77 selected task sessions. Session counts below overlap when a task session used more than one model. Requests, tokens, and spend are grouped by the model that handled each request. Token totals are prompt plus completion tokens.


| Router                    | Model     | Sessions using model | Requests | Tokens | Gateway spend | Share of router spend |
| ------------------------- | --------- | -------------------- | -------- | ------ | ------------- | --------------------- |
| Bifrost JEV               | Opus 5.5  | 31                   | 263      | 8.64M  | $10.681749    | 51.9%                 |
| Bifrost JEV               | Sonnet 5  | 48                   | 739      | 21.74M | $9.418236     | 45.8%                 |
| Bifrost JEV               | Haiku 4.5 | 4                    | 58       | 2.17M  | $0.483293     | 2.3%                  |
| LiteLLM heuristic v2      | Haiku 4.5 | 62                   | 1,969    | 87.20M | $17.318678    | 61.8%                 |
| LiteLLM heuristic v2      | Sonnet 5  | 19                   | 386      | 18.33M | $10.690432    | 38.2%                 |
| LiteLLM heuristic v2      | Opus 5.5  | - (never)            | -        | -      | -             | -                     |
| Bifrost Semantic defaults | Sonnet 5  | 45                   | 1,038    | 40.90M | $21.122272    | 68.8%                 |
| Bifrost Semantic defaults | Opus 5.5  | 25                   | 152      | 4.43M  | $6.959027     | 22.7%                 |
| Bifrost Semantic defaults | Haiku 4.5 | 16                   | 298      | 8.97M  | $2.631096     | 8.6%                  |


Semantic spent **$9.479 more than the fixed Opus baseline** (+44.6%). Its Sonnet requests alone cost $21.122 across 1,038 requests—almost the baseline's entire $21.234 spend. Opus and Haiku requests contributed a further $9.590. This is the spend split across selected sessions; it does not by itself establish why each task generated that request volume.

LiteLLM was run in **every-request** mode, so tool-continuation requests were classified/routed too. Its model and token figures in the table come from selected sessions in LiteLLM spend logs.

## Per-task results (77)

Each cell shows the Harbor verifier result and gateway model spend for the selected task session. JEV, LiteLLM, and Semantic also list every distinct model logged during that session; a `+` means the session used more than one model. The baseline was configured for Claude Opus 5.5; where the logs also show Opus 4.8 calls, both model names are listed. Verifier failures are included in spend; attempts with no verifier result are excluded. Costs are rounded to six decimals.

The reconciled 77-task session ledger includes `break-filter-js-from-html`, matching the headline totals.


| Task                              | Opus 5.5 baseline (result · cost · model if different) | Bifrost JEV (result · cost · model(s))             | LiteLLM every request (result · cost · model(s)) | Bifrost Semantic defaults (result · cost · model(s)) |
| --------------------------------- | ------------------------------------------------------ | -------------------------------------------------- | ------------------------------------------------ | ---------------------------------------------------- |
| `adaptive-rejection-sampler`      | Pass · $0.777256                                       | Pass · $0.476409 · Opus 5.5                        | Fail · $0.798347 · Haiku 4.5                     | Pass · $0.568594 · Opus 5.5                          |
| `bn-fit-modify`                   | Pass · $0.132457                                       | Pass · $0.163404 · Sonnet 5                        | Pass · $0.364514 · Sonnet 5                      | Pass · $0.145144 · Sonnet 5                          |
| `break-filter-js-from-html`       | Pass · $0.632928 · Opus 5.5 + Opus 4.8                 | Pass · $0.173808 · Sonnet 5                        | Fail · $0.967523 · Haiku 4.5                     | Pass · $0.338514 · Sonnet 5                          |
| `build-cython-ext`                | Pass · $0.377246                                       | Pass · $0.390768 · Opus 5.5                        | Fail · $0.596966 · Haiku 4.5                     | Pass · $0.592160 · Sonnet 5                          |
| `build-pmars`                     | Pass · $0.101373                                       | Pass · $0.150413 · Sonnet 5                        | Pass · $0.125828 · Haiku 4.5                     | Pass · $0.195156 · Sonnet 5                          |
| `build-pov-ray`                   | Pass · $0.266514                                       | Fail · $0.192848 · Sonnet 5 + Haiku 4.5            | Pass · $0.424136 · Sonnet 5 + Haiku 4.5          | Fail · $0.913197 · Haiku 4.5 + Opus 5.5 + Sonnet 5   |
| `caffe-cifar-10`                  | Pass · $0.376449                                       | Pass · $0.339441 · Opus 5.5                        | Pass · $0.784421 · Haiku 4.5                     | Pass · $0.708922 · Sonnet 5                          |
| `cancel-async-tasks`              | Pass · $0.129725                                       | Pass · $0.090381 · Sonnet 5                        | Pass · $0.193320 · Sonnet 5                      | Fail · $0.071287 · Sonnet 5                          |
| `chess-best-move`                 | Fail · $0.128425                                       | Pass · $0.543746 · Sonnet 5                        | Fail · $0.131300 · Haiku 4.5                     | Fail · $0.135579 · Haiku 4.5                         |
| `circuit-fibsqrt`                 | Pass · $0.183015                                       | Pass · $0.171682 · Opus 5.5                        | Pass · $2.343073 · Sonnet 5                      | Pass · $0.962825 · Opus 5.5 + Sonnet 5               |
| `cobol-modernization`             | Pass · $0.711339                                       | Pass · $0.632962 · Opus 5.5                        | Pass · $0.345217 · Haiku 4.5                     | Fail · $0.919384 · Sonnet 5                          |
| `code-from-image`                 | Pass · $0.058753                                       | Pass · $0.035040 · Sonnet 5                        | Pass · $0.020526 · Haiku 4.5                     | Pass · $0.025717 · Haiku 4.5                         |
| `compile-compcert`                | Fail · $0.155886                                       | Pass · $0.525582 · Sonnet 5                        | Pass · $0.537233 · Haiku 4.5                     | Fail · $1.437836 · Sonnet 5                          |
| `configure-git-webserver`         | Pass · $0.115667                                       | Pass · $0.227417 · Sonnet 5                        | Fail · $0.011044 · Haiku 4.5                     | Fail · $0.136503 · Sonnet 5                          |
| `constraints-scheduling`          | Pass · $0.090425                                       | Pass · $0.111941 · Sonnet 5                        | Pass · $0.146375 · Sonnet 5                      | Pass · $0.116140 · Sonnet 5                          |
| `count-dataset-tokens`            | Pass · $0.166559                                       | Pass · $0.083649 · Sonnet 5 + Haiku 4.5            | Fail · $0.044664 · Haiku 4.5                     | Pass · $0.131327 · Opus 5.5 + Sonnet 5               |
| `crack-7z-hash`                   | Pass · $0.588175 · Opus 5.5 + Opus 4.8                 | Pass · $0.132134 · Sonnet 5                        | Pass · $0.249067 · Haiku 4.5                     | Pass · $0.166859 · Sonnet 5                          |
| `custom-memory-heap-crash`        | Pass · $0.337251                                       | Pass · $0.143249 · Opus 5.5                        | Fail · $0.649045 · Haiku 4.5                     | Fail · $0.882173 · Sonnet 5                          |
| `db-wal-recovery`                 | Pass · $0.057003                                       | Pass · $0.059717 · Opus 5.5                        | Fail · $0.195976 · Haiku 4.5                     | Pass · $0.147042 · Opus 5.5                          |
| `distribution-search`             | Pass · $0.063365                                       | Pass · $0.061136 · Opus 5.5                        | Pass · $0.331203 · Sonnet 5                      | Pass · $0.190150 · Opus 5.5                          |
| `extract-elf`                     | Pass · $0.229144                                       | Pass · $0.142548 · Sonnet 5                        | Pass · $0.139524 · Haiku 4.5                     | Pass · $0.420940 · Haiku 4.5                         |
| `feal-differential-cryptanalysis` | Pass · $0.111832                                       | Pass · $0.128422 · Opus 5.5                        | Pass · $0.568622 · Sonnet 5                      | Pass · $1.051711 · Haiku 4.5 + Sonnet 5              |
| `feal-linear-cryptanalysis`       | Pass · $0.166416                                       | Pass · $0.179497 · Opus 5.5                        | Fail · $0.736093 · Haiku 4.5                     | Pass · $1.031698 · Sonnet 5                          |
| `filter-js-from-html`             | Pass · $0.222254                                       | Fail · $0.097261 · Sonnet 5                        | Fail · $0.286485 · Sonnet 5                      | Fail · $0.155283 · Sonnet 5                          |
| `financial-document-processor`    | Pass · $0.470129                                       | Pass · $0.205165 · Sonnet 5 + Haiku 4.5            | Fail · $0.193579 · Haiku 4.5                     | Fail · $0.120174 · Haiku 4.5                         |
| `fix-code-vulnerability`          | Pass · $0.051058                                       | Pass · $0.068182 · Sonnet 5                        | Pass · $0.332072 · Haiku 4.5                     | Pass · $0.068834 · Sonnet 5                          |
| `fix-git`                         | Pass · $0.123014                                       | Pass · $0.089792 · Sonnet 5                        | Pass · $0.034619 · Haiku 4.5                     | Pass · $0.053956 · Haiku 4.5                         |
| `fix-ocaml-gc`                    | Pass · $0.396408                                       | Pass · $0.471990 · Opus 5.5                        | Pass · $2.195627 · Sonnet 5                      | Pass · $0.987067 · Sonnet 5                          |
| `gcode-to-text`                   | Pass · $0.823957                                       | Pass · $0.153996 · Sonnet 5                        | Fail · $0.037227 · Haiku 4.5                     | Fail · $0.023614 · Haiku 4.5                         |
| `git-leak-recovery`               | Pass · $0.079443                                       | Pass · $0.061055 · Sonnet 5                        | Pass · $0.035836 · Haiku 4.5                     | Pass · $0.157571 · Opus 5.5                          |
| `git-multibranch`                 | Pass · $0.115981                                       | Pass · $0.291258 · Sonnet 5                        | Pass · $0.385329 · Haiku 4.5                     | Pass · $0.112453 · Opus 5.5                          |
| `gpt2-codegolf`                   | Pass · $0.283343                                       | Pass · $0.301256 · Opus 5.5                        | Fail · $0.019329 · Haiku 4.5                     | Pass · $0.372772 · Opus 5.5                          |
| `headless-terminal`               | Pass · $0.116597                                       | Pass · $0.329354 · Sonnet 5                        | Pass · $0.056090 · Haiku 4.5                     | Pass · $0.116510 · Opus 5.5                          |
| `hf-model-inference`              | Pass · $0.076629                                       | Pass · $0.066369 · Sonnet 5                        | Fail · $0.042572 · Haiku 4.5                     | Pass · $0.077549 · Opus 5.5                          |
| `install-windows-3.11`            | Pass · $0.272857                                       | Pass · $0.340175 · Sonnet 5                        | Fail · $0.443781 · Haiku 4.5                     | Fail · $0.223019 · Haiku 4.5                         |
| `kv-store-grpc`                   | Pass · $0.068264                                       | Pass · $0.115149 · Sonnet 5                        | Fail · $0.067826 · Haiku 4.5                     | Pass · $0.115670 · Sonnet 5                          |
| `large-scale-text-editing`        | Pass · $0.072964                                       | Pass · $0.056798 · Opus 5.5                        | Pass · $0.417103 · Haiku 4.5                     | Pass · $0.129915 · Sonnet 5                          |
| `largest-eigenval`                | Pass · $0.403739                                       | Pass · $0.331976 · Opus 5.5                        | Pass · $0.166959 · Haiku 4.5                     | Pass · $0.180369 · Haiku 4.5                         |
| `log-summary-date-ranges`         | Pass · $0.090773                                       | Pass · $0.089443 · Sonnet 5                        | Pass · $0.104977 · Sonnet 5                      | Fail · $0.047876 · Haiku 4.5                         |
| `mailman`                         | Pass · $0.419296                                       | Pass · $0.772192 · Sonnet 5                        | Pass · $1.439244 · Sonnet 5 + Haiku 4.5          | Pass · $0.599609 · Sonnet 5                          |
| `make-mips-interpreter`           | Pass · $1.155752                                       | Pass · $1.141081 · Opus 5.5                        | Fail · $0.884449 · Haiku 4.5                     | Fail · $2.296215 · Opus 5.5 + Sonnet 5               |
| `mcmc-sampling-stan`              | Pass · $0.124998                                       | Pass · $0.385157 · Opus 5.5 + Sonnet 5 + Haiku 4.5 | Fail · $0.559517 · Haiku 4.5                     | Pass · $0.276355 · Sonnet 5                          |
| `merge-diff-arc-agi-task`         | Pass · $0.113465                                       | Pass · $0.115273 · Opus 5.5                        | Pass · $0.141390 · Haiku 4.5                     | Pass · $0.106649 · Opus 5.5                          |
| `model-extraction-relu-logits`    | Pass · $0.157108                                       | Pass · $0.104582 · Opus 5.5                        | Fail · $0.070189 · Haiku 4.5                     | Pass · $0.760474 · Sonnet 5                          |
| `modernize-scientific-stack`      | Pass · $0.077596                                       | Pass · $0.059125 · Sonnet 5                        | Pass · $0.070282 · Haiku 4.5                     | Pass · $0.057490 · Sonnet 5                          |
| `mteb-leaderboard`                | Fail · $0.236769                                       | Pass · $0.226410 · Sonnet 5                        | Pass · $0.243544 · Sonnet 5 + Haiku 4.5          | Pass · $0.314591 · Opus 5.5 + Sonnet 5               |
| `mteb-retrieve`                   | Pass · $0.089452                                       | Pass · $0.117839 · Sonnet 5                        | Fail · $0.039819 · Haiku 4.5                     | Pass · $0.084115 · Sonnet 5                          |
| `multi-source-data-merger`        | Pass · $0.092170                                       | Pass · $0.075592 · Sonnet 5                        | Pass · $0.241773 · Haiku 4.5                     | Pass · $0.175482 · Opus 5.5                          |
| `nginx-request-logging`           | Pass · $0.077952                                       | Pass · $0.072330 · Sonnet 5                        | Pass · $0.182092 · Sonnet 5                      | Pass · $0.077240 · Haiku 4.5                         |
| `openssl-selfsigned-cert`         | Pass · $0.090829                                       | Pass · $0.075879 · Sonnet 5                        | Fail · $0.072273 · Haiku 4.5                     | Fail · $0.056976 · Sonnet 5                          |
| `overfull-hbox`                   | Pass · $0.104783                                       | Pass · $0.359573 · Sonnet 5                        | Pass · $0.170971 · Haiku 4.5                     | Pass · $0.151903 · Opus 5.5                          |
| `path-tracing`                    | Pass · $0.397762                                       | Pass · $0.354080 · Opus 5.5                        | Fail · $0.937900 · Haiku 4.5                     | Fail · $1.640561 · Sonnet 5                          |
| `path-tracing-reverse`            | Pass · $0.618319                                       | Pass · $0.597275 · Opus 5.5                        | Fail · $0.807638 · Haiku 4.5                     | Pass · $0.607745 · Opus 5.5                          |
| `polyglot-c-py`                   | Pass · $0.109775                                       | Pass · $0.272031 · Sonnet 5                        | Pass · $0.238980 · Sonnet 5                      | Pass · $0.450359 · Haiku 4.5                         |
| `portfolio-optimization`          | Pass · $0.177338                                       | Pass · $0.103661 · Sonnet 5                        | Pass · $0.069469 · Haiku 4.5                     | Pass · $0.198533 · Opus 5.5                          |
| `prove-plus-comm`                 | Pass · $0.057490                                       | Pass · $0.048016 · Sonnet 5                        | Pass · $0.054945 · Sonnet 5                      | Pass · $0.057050 · Opus 5.5                          |
| `pypi-server`                     | Pass · $0.164449                                       | Pass · $0.087537 · Sonnet 5                        | Pass · $0.141918 · Sonnet 5                      | Pass · $0.071701 · Sonnet 5                          |
| `pytorch-model-cli`               | Pass · $0.151640                                       | Pass · $0.139681 · Sonnet 5                        | Fail · $0.141750 · Haiku 4.5                     | Pass · $0.155504 · Sonnet 5                          |
| `pytorch-model-recovery`          | Pass · $0.134473                                       | Pass · $0.074080 · Sonnet 5                        | Pass · $0.109064 · Haiku 4.5                     | Pass · $0.097666 · Sonnet 5                          |
| `raman-fitting`                   | Pass · $0.106313                                       | Fail · $0.605333 · Sonnet 5                        | Fail · $0.098410 · Haiku 4.5                     | Fail · $0.340646 · Haiku 4.5                         |
| `regex-chess`                     | Pass · $0.739305                                       | Pass · $0.579900 · Opus 5.5                        | Fail · $0.442647 · Haiku 4.5                     | Pass · $0.470836 · Opus 5.5                          |
| `regex-log`                       | Pass · $0.068691                                       | Pass · $0.103740 · Sonnet 5                        | Pass · $0.125715 · Haiku 4.5                     | Pass · $0.107526 · Sonnet 5                          |
| `reshard-c4-data`                 | Pass · $0.417268                                       | Pass · $0.348692 · Opus 5.5                        | Pass · $1.032884 · Sonnet 5                      | Pass · $1.008129 · Sonnet 5                          |
| `rstan-to-pystan`                 | Pass · $0.179788                                       | Pass · $0.248429 · Opus 5.5                        | Pass · $0.234682 · Haiku 4.5                     | Pass · $0.172757 · Opus 5.5                          |
| `sam-cell-seg`                    | Pass · $0.402785                                       | Pass · $0.387811 · Opus 5.5                        | Fail · $0.325876 · Haiku 4.5                     | Pass · $0.752222 · Opus 5.5 + Sonnet 5               |
| `sanitize-git-repo`               | Pass · $0.175248                                       | Pass · $0.294548 · Opus 5.5 + Sonnet 5             | Fail · $0.142502 · Haiku 4.5                     | Fail · $0.247019 · Haiku 4.5                         |
| `schemelike-metacircular-eval`    | Pass · $2.434817                                       | Pass · $1.767987 · Opus 5.5                        | Fail · $0.546650 · Haiku 4.5                     | Pass · $2.256013 · Sonnet 5                          |
| `sparql-university`               | Pass · $0.182702                                       | Pass · $0.166245 · Opus 5.5                        | Fail · $0.081857 · Haiku 4.5                     | Fail · $0.356748 · Sonnet 5                          |
| `sqlite-db-truncate`              | Pass · $0.086778                                       | Pass · $0.095870 · Sonnet 5                        | Fail · $0.174571 · Haiku 4.5                     | Fail · $0.122748 · Haiku 4.5                         |
| `sqlite-with-gcov`                | Pass · $0.068791                                       | Pass · $0.166878 · Sonnet 5                        | Fail · $0.083544 · Haiku 4.5                     | Fail · $0.183563 · Sonnet 5                          |
| `torch-pipeline-parallelism`      | Pass · $0.191839                                       | Pass · $0.232709 · Opus 5.5                        | Fail · $0.246559 · Haiku 4.5                     | Pass · $0.590443 · Opus 5.5 + Sonnet 5               |
| `torch-tensor-parallelism`        | Pass · $0.171774                                       | Pass · $0.335032 · Sonnet 5                        | Fail · $0.095279 · Haiku 4.5                     | Fail · $0.106734 · Sonnet 5                          |
| `train-fasttext`                  | Pass · $0.433708                                       | Fail · $0.794045 · Sonnet 5                        | Fail · $0.097817 · Haiku 4.5                     | Pass · $0.200008 · Opus 5.5                          |
| `tune-mjcf`                       | Pass · $0.206857                                       | Pass · $0.154862 · Opus 5.5                        | Pass · $0.434167 · Sonnet 5                      | Pass · $0.297894 · Sonnet 5                          |
| `video-processing`                | Pass · $0.527304                                       | Pass · $0.537710 · Opus 5.5                        | Fail · $0.671305 · Sonnet 5 + Haiku 4.5          | Fail · $0.608390 · Opus 5.5                          |
| `vulnerable-secret`               | Pass · $0.373515 · Opus 5.5 + Opus 4.8                 | Pass · $0.156870 · Sonnet 5                        | Pass · $0.118074 · Haiku 4.5                     | Pass · $0.085945 · Sonnet 5                          |
| `winning-avg-corewars`            | Pass · $0.289998                                       | Pass · $0.199810 · Opus 5.5                        | Fail · $0.915934 · Haiku 4.5                     | Pass · $0.337068 · Sonnet 5                          |
| **Total**                         | **74/77 · $21.233514**                                 | **73/77 · $20.583278**                             | **40/77 · $28.009110**                           | **55/77 · $30.712395**                               |


## Tasks outside the shared 77-task comparison

These 12 tasks are excluded because at least one arm did not produce a usable verifier result. Errors included safety refusals (`dna-assembly`, `dna-insert`, `protein-assembly`), timeouts or stalled responses (`extract-moves-from-video`, `llm-inference-batching-scheduler`, `make-doom-for-mips`, `password-recovery`, `polyglot-rust-c`, `query-optimize`, `write-compressor`), non-zero agent exits (`qemu-alpine-ssh`, `qemu-startup`), and provider or setup failures (`protein-assembly`, `write-compressor`); other arms sometimes returned verifier pass/fail results for the same tasks.


## Run the 77-task set

From the repository root, set the endpoint and key for the arm, then run the script:

```sh
export BIFROST_BENCH_ANTHROPIC_URL='https://your-bifrost-endpoint/anthropic'
export BIFROST_BENCH_VK='your-bifrost-virtual-key'
./model-routing-benchmark/terminal-bench/run.sh opus
```

For the fixed baseline, configure Bifrost with routing disabled and Opus 5.5 fixed. For a JEV run, activate the JEV classifier and routing rules in Bifrost, then run the same endpoint and key with `jev`:

```sh
./model-routing-benchmark/terminal-bench/run.sh jev
```

The `opus` and `jev` commands send the same Opus 5.5 request; the active Bifrost configuration determines whether it remains fixed or is routed. For LiteLLM, set its Anthropic-compatible endpoint and virtual key:

```sh
export LITELLM_ANTHROPIC_BASE_URL='https://your-litellm-endpoint/anthropic'
export LITELLM_API_KEY='your-litellm-virtual-key'
./model-routing-benchmark/terminal-bench/run.sh litellm
```

The script pins Harbor `0.23.0`, Claude Code `2.1.280`, the `modal` environment, one attempt per task, and zero Harbor retries. It uses three concurrent trials by default; set `TB_N_CONCURRENT` to change that. It writes generated job data to `terminal-bench/jobs/`, which is ignored by Git. Keep keys in your shell environment; do not commit them. The 77-task list is embedded in the script and matches the report above.
