#!/usr/bin/env python3
"""Run paired RouterArena answer-quality and cost measurements through gateways."""

from __future__ import annotations

import argparse
import concurrent.futures
import json
import math
import os
import random
import statistics
import sys
import threading
import time
import urllib.error
import urllib.request
from collections import Counter
from pathlib import Path
from typing import Any


HERE = Path(__file__).resolve().parent
DEFAULT_ARMS = ("opus-baseline", "bifrost-jev", "litellm-v2")
OPTIONAL_ARMS = ("bifrost-semantic",)
SAVE_LOCK = threading.Lock()


def load_formatted_prompts(routerarena_root: Path, rows: list[dict[str, Any]]) -> dict[str, str]:
    """Load RouterArena's prepared, dataset-specific inference prompts by global index."""
    path = routerarena_root / "dataset" / "router_data.json"
    entries = json.loads(path.read_text(encoding="utf-8"))
    prompts: dict[str, str] = {}
    for entry in entries:
        global_index = entry.get("global index") or entry.get("global_index")
        prompt = entry.get("prompt_formatted") or entry.get("prompt")
        if global_index is None or not isinstance(prompt, str) or not prompt.strip():
            raise ValueError(f"Invalid formatted RouterArena prompt entry in {path}")
        key = str(global_index)
        if key in prompts:
            raise ValueError(f"Duplicate formatted RouterArena prompt for {key}")
        prompts[key] = prompt
    expected = {str(row["Global Index"]) for row in rows}
    missing = expected - prompts.keys()
    if missing:
        raise ValueError(f"{len(missing)} full-split rows have no prepared RouterArena prompt")
    return prompts


def read_jsonl(path: Path) -> dict[str, dict[str, Any]]:
    """Load JSONL rows keyed by ID, keeping the latest appended result for resume."""
    rows: dict[str, dict[str, Any]] = {}
    if path.exists():
        with path.open(encoding="utf-8") as stream:
            for line in stream:
                if line.strip():
                    row = json.loads(line)
                    rows[str(row["id"])] = row
    return rows


def append_jsonl(path: Path, row: dict[str, Any]) -> None:
    """Append one completed request result atomically with respect to peer workers."""
    with SAVE_LOCK:
        with path.open("a", encoding="utf-8") as stream:
            stream.write(json.dumps(row, ensure_ascii=False) + "\n")
            stream.flush()


def response_content(body: dict[str, Any]) -> str:
    """Extract text from an OpenAI-compatible chat completion response."""
    choices = body.get("choices") or []
    if not choices:
        return ""
    content = (choices[0].get("message") or {}).get("content", "")
    if isinstance(content, str):
        return content
    if isinstance(content, list):
        return "\n".join(
            str(part.get("text", "")) for part in content if isinstance(part, dict)
        )
    return ""


def usage_tokens(usage: Any) -> dict[str, int]:
    """Normalize provider usage into RouterArena's input/output token fields."""
    usage = usage if isinstance(usage, dict) else {}
    return {
        "input_tokens": int(usage.get("prompt_tokens", usage.get("input_tokens", 0)) or 0),
        "output_tokens": int(usage.get("completion_tokens", usage.get("output_tokens", 0)) or 0),
        "total_tokens": int(usage.get("total_tokens", 0) or 0),
    }


def usage_cost(model: str, usage: dict[str, int], prices: dict[str, Any]) -> float | None:
    """Price a completion from an explicit model-cost map when the gateway gives no cost header."""
    normalized = model.lower()
    price = prices.get(model)
    if price is None:
        matches = [value for name, value in prices.items() if str(name).lower() in normalized]
        if len(matches) == 1:
            price = matches[0]
    if not isinstance(price, dict):
        return None
    input_rate = price.get("input_token_price_per_million")
    output_rate = price.get("output_token_price_per_million")
    if input_rate is None or output_rate is None:
        return None
    input_rate = float(input_rate)
    output_rate = float(output_rate)
    reasoning_rate = float(price.get("reasoning_token_price_per_million", output_rate))
    input_tokens = usage.get("input_tokens", 0)
    output_tokens = usage.get("output_tokens", 0)
    reasoning_tokens = max(0, usage.get("total_tokens", 0) - input_tokens - output_tokens)
    return (
        input_tokens * input_rate
        + output_tokens * output_rate
        + reasoning_tokens * reasoning_rate
    ) / 1_000_000


def make_request(url: str, payload: dict[str, Any], headers: dict[str, str], timeout: int):
    """POST JSON and return parsed body, normalized headers, and elapsed seconds."""
    request = urllib.request.Request(
        url,
        data=json.dumps(payload, ensure_ascii=False).encode("utf-8"),
        headers={"Content-Type": "application/json", **headers},
        method="POST",
    )
    started = time.perf_counter()
    try:
        with urllib.request.urlopen(request, timeout=timeout) as response:
            body = json.loads(response.read())
            response_headers = {k.lower(): v for k, v in response.headers.items()}
        return body, response_headers, time.perf_counter() - started, None
    except urllib.error.HTTPError as error:
        message = error.read().decode("utf-8", "replace")[:1200]
        return {}, {k.lower(): v for k, v in error.headers.items()}, time.perf_counter() - started, f"HTTP {error.code}: {message}"
    except (urllib.error.URLError, TimeoutError, json.JSONDecodeError, OSError) as error:
        return {}, {}, time.perf_counter() - started, str(error)[:1200]


def chat(
    base_url: str,
    model: str,
    prompt: str,
    headers: dict[str, str],
    args: argparse.Namespace,
) -> dict[str, Any]:
    """Run one chat completion and capture answer, model, cost header, and usage."""
    payload = {
        "model": model,
        "messages": [{"role": "user", "content": prompt}],
        "max_tokens": args.max_tokens,
    }
    body, response_headers, elapsed, error = make_request(
        base_url.rstrip("/") + "/v1/chat/completions", payload, headers, args.timeout
    )
    chosen_model = (
        response_headers.get("x-litellm-model-name")
        or response_headers.get("x-bf-model-used")
        or body.get("model")
        or model
    )
    routed_tier = (
        response_headers.get("x-litellm-complexity-router-tier")
        or response_headers.get("x-bf-complexity-router-tier")
    )
    cost = None
    cost_source = None
    for header in ("x-litellm-response-cost", "x-bf-response-cost", "x-bf-cost"):
        try:
            if response_headers.get(header) is not None:
                cost = float(response_headers[header])
                cost_source = "gateway_header"
                break
        except ValueError:
            pass
    usage = usage_tokens(body.get("usage"))
    if cost is None:
        cost = usage_cost(chosen_model, usage, args.model_prices)
        if cost is not None:
            cost_source = "routerarena_model_cost"
    answer = response_content(body)
    if not error and not answer:
        error = "successful HTTP response contained no text answer"
    if not error and usage["output_tokens"] <= 0:
        error = "successful HTTP response contained no usable output-token usage"
    return {
        "model": chosen_model,
        "router_tier": routed_tier,
        "answer": answer,
        "usage": usage,
        "cost_usd": cost,
        "cost_source": cost_source,
        "latency_seconds": elapsed,
        "request_id": response_headers.get("x-bf-request-id")
        or response_headers.get("x-litellm-call-id")
        or response_headers.get("x-request-id"),
        "error": error,
    }


def run_one(row: dict[str, Any], arm: str, args: argparse.Namespace) -> dict[str, Any]:
    """Execute one router arm for one input, recording routing and answer work."""
    prompt = args.formatted_prompts[str(row["Global Index"])]
    if not prompt:
        return {"id": str(row["Global Index"]), "status": "error", "error": "empty prompt"}
    bifrost = os.environ.get("BIFROST_BASE_URL", "http://127.0.0.1:8080")
    bifrost_key = os.environ.get("BIFROST_VK", "")
    direct_key = os.environ.get("BIFROST_DIRECT_VK", bifrost_key)
    semantic_key = os.environ.get("BIFROST_SEMANTIC_VK", bifrost_key)
    litellm = os.environ.get("LITELLM_BASE_URL", "http://127.0.0.1:4000")
    litellm_key = os.environ.get("LITELLM_API_KEY", "")
    result: dict[str, Any] = {
        "id": str(row["Global Index"]),
        "arm": arm,
        "prompt": prompt,
        "status": "error",
        "router_tier": None,
        "route_latency_seconds": 0.0,
        "answer_latency_seconds": 0.0,
        "route_cost_usd": 0.0,
        "answer_cost_usd": None,
        "answer_cost_source": None,
        "generation_valid": False,
    }

    if arm == "opus-baseline":
        completion = chat(bifrost, args.opus_model, prompt, {"x-bf-vk": direct_key}, args)
    elif arm == "bifrost-jev":
        # Normal chat completions invoke the configured in-gateway classifier and
        # then follow its complexity_tier routing rules. The request model matches
        # the fixed Opus baseline; Bifrost's configured Jev classifier selects the
        # actual answer model. Its internal decision cost is reconciled from logs.
        completion = chat(bifrost, args.opus_model, prompt, {"x-bf-vk": direct_key}, args)
        result["router_tier"] = completion["router_tier"]
    elif arm == "litellm-v2":
        completion = chat(
            litellm,
            args.litellm_router_model,
            prompt,
            {"Authorization": f"Bearer {litellm_key}"} if litellm_key else {},
            args,
        )
        result["router_tier"] = completion["router_tier"]
    elif arm == "bifrost-semantic":
        completion = chat(
            bifrost,
            args.semantic_input_model,
            prompt,
            {"x-bf-vk": semantic_key},
            args,
        )
        result["router_tier"] = completion["router_tier"]
        # Estimate Gemini Embedding 2 request cost at ~4 characters/token.
        result["route_cost_usd"] = math.ceil(len(prompt) / 4) * 0.20 / 1_000_000
    else:
        raise ValueError(f"unknown arm: {arm}")

    result.update(
        status="ok" if completion["error"] is None else "error",
        generation_valid=completion["error"] is None,
        selected_model=completion["model"],
        answer=completion["answer"],
        answer_usage=completion["usage"],
        answer_cost_usd=completion["cost_usd"],
        answer_cost_source=completion["cost_source"],
        answer_latency_seconds=completion["latency_seconds"],
        answer_request_id=completion["request_id"],
        error=completion["error"],
    )
    return result


def scorer_setup(routerarena_root: Path):
    """Load RouterArena's own per-dataset scorers from a prepared checkout."""
    if not (routerarena_root / "dataset" / "routerarena").exists():
        raise FileNotFoundError(
            f"Missing {routerarena_root / 'dataset' / 'routerarena'}; clone RouterArena and run its dataset preparation first."
        )
    os.chdir(routerarena_root)
    sys.path.insert(0, str(routerarena_root))
    sys.path.insert(0, str(routerarena_root / "llm_evaluation"))
    from eval_reasoning import get_scorers_for_dataset
    from evaluate_models import ModelEvaluator, load_eval_config_for_dataset
    from datasets import load_from_disk

    dataset = load_from_disk(str(routerarena_root / "dataset" / "routerarena"))
    rows = dataset.to_list() if hasattr(dataset, "to_list") else list(dataset)
    evaluator = ModelEvaluator(num_workers=1)
    return rows, evaluator, get_scorers_for_dataset, load_eval_config_for_dataset


def score_answer(
    row: dict[str, Any],
    answer: str,
    evaluator: Any,
    get_scorers: Any,
    load_config: Any,
) -> float:
    """Apply the first configured RouterArena scorer to one generated answer."""
    global_index = str(row["Global Index"])
    dataset_name = evaluator.determine_dataset_from_global_index(global_index)
    scorers = get_scorers(dataset_name, load_config(dataset_name))
    if not scorers:
        raise ValueError(f"no RouterArena scorer configured for {dataset_name}")
    ground_truth = evaluator._get_ground_truth(global_index, dataset_name)
    if ground_truth is None:
        raise ValueError(f"RouterArena ground truth unavailable for {global_index}")
    score, metric_name = evaluator._evaluate_single_entry(
        answer, ground_truth, scorers[0][0], dataset_name
    )
    if metric_name == "error":
        raise ValueError(f"RouterArena {dataset_name} scorer failed")
    return float(score)


def percentile(values: list[float], fraction: float) -> float | None:
    """Return a nearest-rank percentile, or None when no values were recorded."""
    if not values:
        return None
    values.sort()
    return values[max(0, math.ceil(fraction * len(values)) - 1)]


def compute_arena_score(cost_per_1000: float, accuracy: float) -> float:
    """Apply RouterArena's published beta=0.1 cost-quality score formula."""
    beta, c_max, c_min = 0.1, 200.0, 0.0044
    cost = max(c_min, min(cost_per_1000, c_max))
    normalized_cost = (math.log2(c_max) - math.log2(cost)) / (
        math.log2(c_max) - math.log2(c_min)
    )
    denominator = beta * accuracy + normalized_cost
    return ((1 + beta) * accuracy * normalized_cost / denominator) if denominator else 0.0


def format_duration(seconds: float) -> str:
    """Format a nonnegative duration as a compact hours/minutes/seconds string."""
    seconds = max(0, int(seconds))
    hours, remainder = divmod(seconds, 3600)
    minutes, seconds = divmod(remainder, 60)
    if hours:
        return f"{hours}h{minutes:02d}m"
    if minutes:
        return f"{minutes}m{seconds:02d}s"
    return f"{seconds}s"


def summarize(
    arms: list[str], rows: list[dict[str, Any]], saved: dict[str, dict[str, dict[str, Any]]]
) -> dict[str, Any]:
    """Build per-arm metrics with failures counted as incorrect and unknown costs explicit."""
    output: dict[str, Any] = {"sample_size": len(rows), "arms": {}}
    for arm in arms:
        records = [saved[arm].get(str(row["Global Index"])) for row in rows]
        scores = [
            float(record.get("score", 0.0))
            if record is not None and record.get("generation_valid")
            else 0.0
            for record in records
        ]
        passed = sum(score >= 1.0 for score in scores)
        latency = [
            float(record.get("route_latency_seconds", 0.0))
            + float(record.get("answer_latency_seconds", 0.0))
            for record in records
            if record is not None and record.get("generation_valid")
        ]
        valid_records = [record for record in records if record is not None and record.get("generation_valid")]
        answer_costs = [record.get("answer_cost_usd") for record in valid_records]
        completed_count = sum(record is not None for record in records)
        cost_known = bool(valid_records) and completed_count == len(rows) and all(
            isinstance(cost, (int, float)) and cost > 0 for cost in answer_costs
        )
        answer_total = sum(float(cost) for cost in answer_costs if isinstance(cost, (int, float)))
        route_total = sum(
            float(record.get("route_cost_usd", 0.0) or 0.0)
            for record in records if record is not None
        )
        arena_cost = answer_total / len(rows) * 1000 if cost_known and rows else None
        accuracy = sum(scores) / len(rows) if rows else None
        output["arms"][arm] = {
            "completed": completed_count,
            "successful": sum(record is not None and record.get("status") == "ok" for record in records),
            "valid_generations": len(valid_records),
            "routerarena_accuracy": accuracy,
            "score_mean_all_items": accuracy,
            "fully_correct": passed,
            "cost_per_fully_correct_task_usd": (answer_total + route_total) / passed if cost_known and passed else None,
            "answer_cost_usd": answer_total if cost_known else None,
            "answer_cost_per_1000_queries_usd": arena_cost,
            "routerarena_score_beta_0_1": compute_arena_score(arena_cost, accuracy) if arena_cost and accuracy is not None else None,
            "routing_cost_usd": route_total,
            "cost_complete": cost_known,
            "latency_mean_success_seconds": statistics.mean(latency) if latency else None,
            "latency_p95_success_seconds": percentile(latency, 0.95),
            "answer_cost_sources": Counter(record.get("answer_cost_source") or "unknown" for record in valid_records),
            "failures": Counter(record.get("error", "unknown") for record in records if record is not None and record.get("status") != "ok"),
        }
    return output


def main() -> int:
    """Run selected gateway arms, apply RouterArena scorers, and save resumable outputs."""
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--routerarena-root", type=Path, required=True, help="Prepared RouterArena checkout")
    parser.add_argument("--output-dir", type=Path, default=HERE / "runs" / "latest")
    parser.add_argument("--model-prices", type=Path, help="Optional JSON map using RouterArena model_cost.json pricing fields")
    parser.add_argument("--arms", nargs="+", choices=[*DEFAULT_ARMS, *OPTIONAL_ARMS, "all"], default=["all"])
    parser.add_argument("--limit", type=int, help="Run a reproducible random N-row preflight")
    parser.add_argument("--task-ids-file", type=Path, help="Run only Global Index IDs listed one per line")
    parser.add_argument("--seed", type=int, default=42, help="Seed for reproducible --limit sampling")
    parser.add_argument("--concurrency", type=int, default=8)
    parser.add_argument("--timeout", type=int, default=1200)
    parser.add_argument("--max-tokens", type=int, default=2048, help="Answer token limit (RouterArena Anthropic adapter uses 2048)")
    parser.add_argument("--retry-failures", action="store_true")
    parser.add_argument("--opus-model", default="anthropic/claude-opus-5-5")
    parser.add_argument("--haiku-model", default="anthropic/claude-haiku-4-5")
    parser.add_argument("--sonnet-model", default="anthropic/claude-sonnet-5")
    parser.add_argument("--litellm-router-model", default="benchmarking-heuristic-v2")
    parser.add_argument("--semantic-input-model", default="azure/DeepSeek-V3")
    parser.add_argument("--dry-run", action="store_true", help="Validate dataset and print planned request counts without making calls")
    args = parser.parse_args()
    args.routerarena_root = args.routerarena_root.resolve()
    args.output_dir = args.output_dir.resolve()
    if args.concurrency < 1 or args.max_tokens < 1 or args.timeout < 1:
        parser.error("concurrency, timeout, and max-tokens must be positive")
    if "all" in args.arms:
        arms = list(DEFAULT_ARMS)
    else:
        arms = list(dict.fromkeys(args.arms))
    if "bifrost-jev" in arms and "bifrost-semantic" in arms:
        parser.error(
            "bifrost-jev and bifrost-semantic use mutually exclusive global Bifrost classifier configs; run them separately after switching the configured classifier"
        )

    if not args.routerarena_root.exists():
        parser.error("--routerarena-root must point to a cloned, prepared RouterArena checkout")
    try:
        rows, evaluator, get_scorers, load_config = scorer_setup(args.routerarena_root.resolve())
    except Exception as error:
        parser.error(str(error))
    if len(rows) != 8400:
        parser.error(f"expected RouterArena full split to contain 8,400 rows; found {len(rows)}")
    try:
        args.formatted_prompts = load_formatted_prompts(args.routerarena_root, rows)
    except Exception as error:
        parser.error(str(error))
    if args.limit is not None:
        if args.task_ids_file is not None:
            parser.error("--limit and --task-ids-file cannot be used together")
        if args.limit < 1 or args.limit > len(rows):
            parser.error(f"--limit must be between 1 and {len(rows)}")
        rows = sorted(random.Random(args.seed).sample(rows, args.limit), key=lambda row: str(row["Global Index"]))
    elif args.task_ids_file is not None:
        try:
            requested_ids = {
                line.strip()
                for line in args.task_ids_file.read_text(encoding="utf-8").splitlines()
                if line.strip() and not line.lstrip().startswith("#")
            }
        except OSError as error:
            parser.error(f"cannot read --task-ids-file: {error}")
        rows_by_id = {str(row["Global Index"]): row for row in rows}
        unknown_ids = requested_ids - rows_by_id.keys()
        if unknown_ids:
            parser.error(f"--task-ids-file contains {len(unknown_ids)} IDs absent from the prepared RouterArena split")
        if not requested_ids:
            parser.error("--task-ids-file must contain at least one task ID")
        rows = [row for row in rows if str(row["Global Index"]) in requested_ids]
    if args.dry_run:
        completions = len(rows) * len(arms)
        internal_jev = len(rows) if "bifrost-jev" in arms else 0
        internal_embeddings = len(rows) if "bifrost-semantic" in arms else 0
        print(json.dumps({
            "rows": len(rows),
            "arms": arms,
            "chat_completions": completions,
            "bifrost_internal_jev_decisions": internal_jev,
            "bifrost_internal_embedding_requests_estimate": internal_embeddings,
            "estimated_total_upstream_calls": completions + internal_jev + internal_embeddings,
        }, indent=2))
        return 0

    prices_path = args.model_prices or (args.routerarena_root / "model_cost" / "model_cost.json")
    args.model_prices = json.loads(prices_path.read_text(encoding="utf-8")) if prices_path.exists() else {}
    if not args.model_prices:
        print("Warning: no model prices loaded; per-answer and per-solved-task costs will be incomplete unless gateways return cost headers.", file=sys.stderr)

    fallback_key = os.environ.get("BIFROST_VK", "")
    required_bifrost_keys = []
    if "opus-baseline" in arms or "bifrost-jev" in arms:
        required_bifrost_keys.append("BIFROST_DIRECT_VK")
    if "bifrost-semantic" in arms:
        required_bifrost_keys.append("BIFROST_SEMANTIC_VK")
    missing_bifrost_keys = [
        name for name in required_bifrost_keys
        if not (os.environ.get(name) or fallback_key)
    ]
    if missing_bifrost_keys:
        parser.error(
            f"set {' or '.join(missing_bifrost_keys)} (or BIFROST_VK fallback); keys are never read from or written to a file"
        )
    if "litellm-v2" in arms and not os.environ.get("LITELLM_API_KEY"):
        parser.error("set LITELLM_API_KEY for LiteLLM calls")
    args.output_dir.mkdir(parents=True, exist_ok=True)
    saved = {arm: read_jsonl(args.output_dir / f"{arm}.jsonl") for arm in arms}
    for arm in arms:
        pending = [
            row for row in rows
            if not (
                saved[arm].get(str(row["Global Index"]), {}).get("status") == "ok"
                or (
                    not args.retry_failures
                    and saved[arm].get(str(row["Global Index"]), {}).get("status")
                    in {"error", "score_error"}
                )
            )
        ]
        total_pending = len(pending)
        print(f"Starting {arm}: {total_pending}/{len(rows)} rows pending", flush=True)
        if total_pending == 0:
            print(f"{arm}: nothing pending; using saved results", flush=True)
            continue
        interactive_progress = sys.stdout.isatty()
        started = time.monotonic()
        succeeded = 0
        failed = 0
        with concurrent.futures.ThreadPoolExecutor(max_workers=args.concurrency) as pool:
            futures = {pool.submit(run_one, row, arm, args): row for row in pending}
            for count, future in enumerate(concurrent.futures.as_completed(futures), 1):
                row = futures[future]
                try:
                    result = future.result()
                except Exception as error:  # Keep a failed task visible and resumable.
                    result = {"id": str(row["Global Index"]), "arm": arm, "status": "error", "error": str(error)[:1200]}
                source = row
                if result.get("status") == "ok":
                    try:
                        result["score"] = score_answer(source, result["answer"], evaluator, get_scorers, load_config)
                    except Exception as error:
                        result["status"] = "score_error"
                        result["error"] = f"RouterArena scoring failed: {error}"[:1200]
                result["source_dataset"] = source.get("Dataset name")
                result["ground_truth_label"] = source.get("Difficulty")
                saved[arm][result["id"]] = result
                append_jsonl(args.output_dir / f"{arm}.jsonl", result)
                if result.get("status") == "ok":
                    succeeded += 1
                else:
                    failed += 1
                elapsed = time.monotonic() - started
                rate = count / elapsed if elapsed > 0 else 0.0
                eta = (total_pending - count) / rate if rate > 0 else 0.0
                width = 24
                filled = int(width * count / total_pending)
                bar = "█" * filled + "░" * (width - filled)
                progress = (
                    f"{arm} [{bar}] {count}/{total_pending} "
                    f"({100 * count / total_pending:5.1f}%) "
                    f"ok={succeeded} err={failed} "
                    f"elapsed={format_duration(elapsed)} eta={format_duration(eta)}"
                )
                if interactive_progress:
                    sys.stdout.write("\r" + progress + "\033[K")
                    sys.stdout.flush()
                    if count == total_pending:
                        sys.stdout.write("\n")
                        sys.stdout.flush()
                elif count % 100 == 0 or count == total_pending:
                    print(progress, flush=True)

    summary = summarize(arms, rows, saved)
    summary["routerarena_rows"] = len(rows)
    summary["sample_split"] = (
        f"task-ids-{len(rows)}" if args.task_ids_file is not None
        else "full" if len(rows) == 8400
        else f"random-{len(rows)}-seed-{args.seed}"
    )
    if args.task_ids_file is not None:
        summary["task_ids_file"] = str(args.task_ids_file.resolve())
    summary["seed"] = args.seed
    summary["concurrency"] = args.concurrency
    summary["max_tokens"] = args.max_tokens
    summary["temperature"] = "provider default (omitted, matching RouterArena Anthropic adapter)"
    summary["prompt_source"] = "RouterArena dataset/router_data.json prompt_formatted"
    summary["answer_scoring"] = "RouterArena per-dataset scorers; source labels and answers are not sent to routers"
    summary["routerarena_score_cost_basis"] = "positive answer costs only, divided by all queries; requires complete answer costs; formula matches RouterArena beta=0.1"
    summary["routerarena_score_excludes_routing_cost"] = True
    summary["all_in_cost_includes_routing_cost"] = True
    summary["latency_scope"] = "end-to-end gateway request; in-gateway Jev classification is included in chat-completion latency"
    summary_path = args.output_dir / "summary.json"
    summary_path.write_text(json.dumps(summary, indent=2, default=dict) + "\n", encoding="utf-8")
    print(json.dumps(summary, indent=2, default=dict))
    print(f"Saved summary: {summary_path}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
