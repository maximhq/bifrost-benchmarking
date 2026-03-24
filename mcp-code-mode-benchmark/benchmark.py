#!/usr/bin/env python3
"""
MCP Code Mode Benchmark Runner for Bifrost

Usage:
    python benchmark.py cleanup                          # Reset workspace to clean state
    python benchmark.py cleanup --eval-file eval_tasks/eval_tasks_r2.json  # Cleanup using R2 side effects
    python benchmark.py run --name <run_name> --model <model>   # Run all eval tasks
    python benchmark.py run --name <run_name> --model <model> --eval-file eval_tasks/eval_tasks_r2.json  # Round 2
    python benchmark.py run --name <run_name> --model <model> --difficulty simple  # Run subset
    python benchmark.py run --name <run_name> --model <model> --start-from M10    # Resume from task
    python benchmark.py summary --name <run_name>        # Print summary of a run
    python benchmark.py fetch-logs --name <run_name>     # Fetch full session logs from Bifrost API

Models (use provider/model format):
    anthropic/claude-sonnet-4-6
    anthropic/claude-opus-4-6
    openai/gpt-5.4
    gemini/gemini-3.1-pro

Before each run:
    1. Toggle Code Mode ON/OFF in Bifrost UI (MCP server catalog → toggle per server)
    2. Run: python benchmark.py cleanup
    3. Run: python benchmark.py run --name <descriptive_name> --model <model>
"""

import argparse
import json
import os
import shutil
import sqlite3
import sys
import time
from datetime import datetime, timezone

import requests

# ─── Configuration ───────────────────────────────────────────────────────────

BIFROST_BASE_URL = os.environ.get("BIFROST_BASE_URL", "http://localhost:8080/openai")
BIFROST_COMPLETIONS_URL = f"{BIFROST_BASE_URL}/v1/chat/completions"
API_KEY = os.environ.get("BIFROST_API_KEY", "dummy-key")

WORKSPACE = os.environ.get("BENCH_WORKSPACE", os.path.dirname(os.path.abspath(__file__)))
SQLITE_DB = os.environ.get("BENCH_SQLITE_DB", os.path.join(os.path.dirname(WORKSPACE), "mcp-bifrost-sqlite", "sqlite.db"))
DEFAULT_EVAL_FILE = os.path.join("eval_tasks", "eval_tasks.json")
EVAL_TASKS_FILE = os.path.join(WORKSPACE, DEFAULT_EVAL_FILE)  # overridden by --eval-file
RUNS_DIR = os.path.join(WORKSPACE, "runs")

SYSTEM_MESSAGE = (
    "You are a helpful AI agent with access to tools for file operations, "
    "web search, web scraping, GitHub, Notion, SQLite, Linear, Apollo, "
    "HackerNews, Google Sheets, Google Calendar, Google Drive, Google Docs, "
    "Calendly, and Figma. "
    "Use the available tools to fulfill the user's request completely. "
    "Be concise in your responses — focus on results, not process narration."
)

# Files that belong to the base workspace (never deleted by cleanup)
BASE_FILES = {
    "benchmark.py",
    "benchmark_report.md",
    "README.md",
    "mcp_catalog.png",
    ".DS_Store",
    ".gitignore",
}
BASE_DIRS = {".claude", "runs", "__pycache__", ".git", "mcp_tools", "eval_tasks"}


def resolve_eval_file(eval_file):
    """Resolve eval task files from either the repo root or eval_tasks/."""
    if os.path.isabs(eval_file):
        return eval_file

    candidates = [os.path.join(WORKSPACE, eval_file)]
    if os.path.dirname(eval_file) == "":
        candidates.append(os.path.join(WORKSPACE, "eval_tasks", eval_file))

    for candidate in candidates:
        if os.path.exists(candidate):
            return candidate

    return candidates[0]


# ─── Cleanup ─────────────────────────────────────────────────────────────────


def cleanup():
    """Reset workspace to pristine state before a benchmark run."""
    print("=== Cleaning up workspace ===\n")

    # Load eval tasks to find all side effects
    with open(EVAL_TASKS_FILE) as f:
        tasks = json.load(f)

    files_to_delete = set()
    dirs_to_delete = set()
    tables_to_drop = set()

    for task in tasks:
        se = task.get("side_effects", {})
        for fp in se.get("files", []):
            files_to_delete.add(fp)
        for dp in se.get("directories", []):
            dirs_to_delete.add(dp)
        for tbl in se.get("sqlite_tables", []):
            tables_to_drop.add(tbl)

    # Delete created files
    deleted_files = 0
    for f in sorted(files_to_delete):
        path = os.path.join(WORKSPACE, f)
        if os.path.exists(path):
            os.remove(path)
            print(f"  Deleted file: {f}")
            deleted_files += 1

    # Delete created directories
    deleted_dirs = 0
    for d in sorted(dirs_to_delete, key=len, reverse=True):  # deepest first
        path = os.path.join(WORKSPACE, d)
        if os.path.exists(path):
            shutil.rmtree(path)
            print(f"  Deleted directory: {d}/")
            deleted_dirs += 1

    # Reset SQLite database — drop all tables IN-PLACE (same inode)
    # Deleting + recreating the file breaks the MCP server's open connection
    db_dir = os.path.dirname(SQLITE_DB)
    os.makedirs(db_dir, exist_ok=True)
    if not os.path.exists(SQLITE_DB):
        # First time: create a valid empty SQLite DB
        conn = sqlite3.connect(SQLITE_DB)
        conn.close()
        print(f"  Created SQLite DB: {SQLITE_DB}")
    else:
        conn = sqlite3.connect(SQLITE_DB)
        cursor = conn.cursor()
        cursor.execute("SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'")
        tables = [row[0] for row in cursor.fetchall()]
        for table in tables:
            cursor.execute(f"DROP TABLE IF EXISTS [{table}]")
            print(f"  Dropped SQLite table: {table}")
        conn.execute("VACUUM")
        conn.commit()
        conn.close()
        # Clean up WAL/journal files
        for ext in ["-wal", "-shm", "-journal"]:
            path = SQLITE_DB + ext
            if os.path.exists(path):
                os.remove(path)
        print(f"  Reset SQLite DB: {SQLITE_DB} ({len(tables)} tables dropped)")

    # Scan for any unexpected files not in base set
    unexpected = []
    for item in os.listdir(WORKSPACE):
        if item in BASE_FILES or item in BASE_DIRS:
            continue
        if item.startswith("."):
            continue
        full = os.path.join(WORKSPACE, item)
        if os.path.isfile(full) and item not in files_to_delete:
            unexpected.append(item)

    if unexpected:
        print(f"\n  Warning: unexpected files found (not in base set):")
        for u in unexpected:
            print(f"    - {u}")
        print("  These were NOT deleted. Remove manually if needed.")

    # Reminder for manual cleanup of external resources
    manual_items = {
        "notion_pages": ("Notion", "delete page"),
        "notion_databases": ("Notion", "delete database"),
        "google_sheets": ("Google Sheets", "delete spreadsheet"),
        "google_docs": ("Google Docs", "delete document"),
        "google_drive_files": ("Google Drive", "delete file"),
        "google_drive_folders": ("Google Drive", "delete folder"),
        "calendar_events": ("Google Calendar", "delete event"),
        "linear_issues": ("Linear", "delete issue"),
    }
    collected = {key: set() for key in manual_items}
    for task in tasks:
        se = task.get("side_effects", {})
        for key in manual_items:
            for item in se.get(key, []):
                collected[key].add(item)

    has_manual = any(collected[k] for k in collected)
    if has_manual:
        print(f"\n  ⚠ Manual cleanup needed:")
        for key, (service, action) in manual_items.items():
            for item in sorted(collected[key]):
                print(f"    - {service}: {action} '{item}'")

    print(f"\nCleanup complete: {deleted_files} files, {deleted_dirs} dirs removed, SQLite DB reset.\n")


# ─── Table rendering ─────────────────────────────────────────────────────────

# Column widths
COL_W = {"num": 5, "id": 6, "diff": 8, "status": 8, "latency": 10, "in_tok": 9, "out_tok": 9, "tot_tok": 10, "cost": 10, "servers": 22}


def _fmt_tokens(val):
    if val is None:
        return "-"
    if val >= 1000:
        return f"{val/1000:.1f}k"
    return str(val)


def _fmt_cost(val):
    if val is None:
        return "-"
    if val == 0:
        return "$0"
    if val < 0.001:
        return f"${val:.5f}"
    return f"${val:.4f}"


def _fmt_latency(val):
    if val is None:
        return "-"
    if val >= 60:
        return f"{val/60:.1f}m"
    return f"{val:.1f}s"


def _table_sep(char="─", left="├", right="┤", mid="┼"):
    cols = COL_W
    parts = [
        char * (cols["num"] + 2),
        char * (cols["id"] + 2),
        char * (cols["diff"] + 2),
        char * (cols["status"] + 2),
        char * (cols["latency"] + 2),
        char * (cols["in_tok"] + 2),
        char * (cols["out_tok"] + 2),
        char * (cols["tot_tok"] + 2),
        char * (cols["cost"] + 2),
        char * (cols["servers"] + 2),
    ]
    return f"{left}{mid.join(parts)}{right}"


def print_table_header():
    c = COL_W
    top = _table_sep("─", "┌", "┐", "┬")
    hdr = (
        f"│ {'#':^{c['num']}} "
        f"│ {'ID':^{c['id']}} "
        f"│ {'Diff':^{c['diff']}} "
        f"│ {'Status':^{c['status']}} "
        f"│ {'Latency':^{c['latency']}} "
        f"│ {'In Tok':^{c['in_tok']}} "
        f"│ {'Out Tok':^{c['out_tok']}} "
        f"│ {'Tot Tok':^{c['tot_tok']}} "
        f"│ {'Cost':^{c['cost']}} "
        f"│ {'Servers':^{c['servers']}} │"
    )
    sep = _table_sep()
    print(top)
    print(hdr)
    print(sep)


def print_table_row(num, total, r):
    c = COL_W
    status = "OK" if r["status"] == "success" else "FAIL"
    latency = _fmt_latency(r.get("wall_clock_seconds"))
    in_tok = _fmt_tokens(r.get("prompt_tokens"))
    out_tok = _fmt_tokens(r.get("completion_tokens"))
    tot_tok = _fmt_tokens(r.get("total_tokens"))
    cost = _fmt_cost(r.get("total_cost"))
    servers = ", ".join(r.get("expected_servers", []))
    if len(servers) > c["servers"]:
        servers = servers[: c["servers"] - 1] + "…"

    row = (
        f"│ {num:>{c['num']}} "
        f"│ {r['query_id']:<{c['id']}} "
        f"│ {r['difficulty']:<{c['diff']}} "
        f"│ {status:<{c['status']}} "
        f"│ {latency:>{c['latency']}} "
        f"│ {in_tok:>{c['in_tok']}} "
        f"│ {out_tok:>{c['out_tok']}} "
        f"│ {tot_tok:>{c['tot_tok']}} "
        f"│ {cost:>{c['cost']}} "
        f"│ {servers:<{c['servers']}} │"
    )
    print(row)

    # Print error detail on next line if failed
    if r["status"] == "error" and r.get("error"):
        err_msg = str(r["error"])[:90]
        print(f"│ {' ' * c['num']} │ {'':>{c['id']}}   ERR: {err_msg}")


def print_table_footer(results):
    c = COL_W
    sep = _table_sep()
    bottom = _table_sep("─", "└", "┘", "┴")

    total_success = sum(1 for r in results if r["status"] == "success")
    total_latency = sum(r.get("wall_clock_seconds", 0) or 0 for r in results)
    total_in = sum(r.get("prompt_tokens", 0) or 0 for r in results)
    total_out = sum(r.get("completion_tokens", 0) or 0 for r in results)
    total_tok = sum(r.get("total_tokens", 0) or 0 for r in results)
    total_cost = sum(r.get("total_cost", 0) or 0 for r in results)

    status_str = f"{total_success}/{len(results)}"

    print(sep)
    totals_row = (
        f"│ {'':>{c['num']}} "
        f"│ {'TOTAL':<{c['id']}} "
        f"│ {'':<{c['diff']}} "
        f"│ {status_str:<{c['status']}} "
        f"│ {_fmt_latency(total_latency):>{c['latency']}} "
        f"│ {_fmt_tokens(total_in):>{c['in_tok']}} "
        f"│ {_fmt_tokens(total_out):>{c['out_tok']}} "
        f"│ {_fmt_tokens(total_tok):>{c['tot_tok']}} "
        f"│ {_fmt_cost(total_cost):>{c['cost']}} "
        f"│ {'':<{c['servers']}} │"
    )
    print(totals_row)
    print(bottom)


def print_difficulty_breakdown(results):
    """Print a summary breakdown by difficulty level."""
    c = COL_W
    print(f"\n┌{'─'*72}┐")
    print(f"│ {'BREAKDOWN BY DIFFICULTY':^70} │")
    print(f"├{'─'*12}┬{'─'*10}┬{'─'*12}┬{'─'*12}┬{'─'*12}┬{'─'*12}┤")
    print(f"│ {'Difficulty':<10} │ {'Pass':>8} │ {'Avg Lat.':>10} │ {'Avg Tok':>10} │ {'Tot Tok':>10} │ {'Tot Cost':>10} │")
    print(f"├{'─'*12}┼{'─'*10}┼{'─'*12}┼{'─'*12}┼{'─'*12}┼{'─'*12}┤")

    for diff in ["simple", "medium", "hard", "edge"]:
        group = [r for r in results if r.get("difficulty") == diff]
        if not group:
            continue
        s = sum(1 for r in group if r["status"] == "success")
        pass_str = f"{s}/{len(group)}"
        latencies = [r.get("wall_clock_seconds", 0) or 0 for r in group]
        avg_lat = sum(latencies) / len(latencies) if latencies else 0
        toks = [r.get("total_tokens", 0) or 0 for r in group]
        avg_tok = sum(toks) / len(toks) if toks else 0
        tot_tok = sum(toks)
        tot_cost = sum(r.get("total_cost", 0) or 0 for r in group)
        print(
            f"│ {diff:<10} │ {pass_str:>8} │ {_fmt_latency(avg_lat):>10} "
            f"│ {_fmt_tokens(int(avg_tok)):>10} │ {_fmt_tokens(tot_tok):>10} │ {_fmt_cost(tot_cost):>10} │"
        )

    print(f"└{'─'*12}┴{'─'*10}┴{'─'*12}┴{'─'*12}┴{'─'*12}┴{'─'*12}┘")


# ─── Run a single query ─────────────────────────────────────────────────────


def run_query(query, model):
    """Send a single query to Bifrost and capture all metrics."""
    start_time = time.time()

    try:
        resp = requests.post(
            BIFROST_COMPLETIONS_URL,
            headers={
                "Authorization": f"Bearer {API_KEY}",
                "Content-Type": "application/json",
            },
            json={
                "model": model,
                "messages": [
                    {"role": "system", "content": SYSTEM_MESSAGE},
                    {"role": "user", "content": query},
                ],
            },
        )
        elapsed = time.time() - start_time
        data = resp.json()
    except requests.exceptions.ConnectionError:
        return {
            "status": "error",
            "error": "Connection refused — is Bifrost running on localhost:8080?",
            "wall_clock_seconds": round(time.time() - start_time, 2),
        }
    except Exception as e:
        return {
            "status": "error",
            "error": str(e),
            "wall_clock_seconds": round(time.time() - start_time, 2),
        }

    # Handle HTTP errors
    if resp.status_code != 200:
        return {
            "status": "error",
            "error": data.get("error", {}).get("message", resp.text),
            "http_status": resp.status_code,
            "wall_clock_seconds": round(elapsed, 2),
            "response": None,
            "bifrost_latency_ms": None,
            "provider": data.get("extra_fields", {}).get("provider"),
            "model_requested": data.get("extra_fields", {}).get("model_requested"),
            "model_deployment": data.get("extra_fields", {}).get("model_deployment"),
            "prompt_tokens": None,
            "completion_tokens": None,
            "total_tokens": None,
            "input_cost": None,
            "output_cost": None,
            "total_cost": None,
            "pending_tool_calls": [],
        }

    # Extract metrics from successful response
    usage = data.get("usage", {})
    extra = data.get("extra_fields", {})
    cost = usage.get("cost", {})

    # Get response content
    choices = data.get("choices", [])
    message = choices[0].get("message", {}) if choices else {}
    content = message.get("content", "")

    # Check for pending tool calls (non-auto-executed)
    pending = []
    if message.get("tool_calls"):
        for tc in message["tool_calls"]:
            fn = tc.get("function", {})
            pending.append({"name": fn.get("name"), "arguments": fn.get("arguments")})

    return {
        "status": "success",
        "response": content,
        "wall_clock_seconds": round(elapsed, 2),
        "bifrost_latency_ms": extra.get("latency"),
        "provider": extra.get("provider"),
        "model_requested": extra.get("model_requested"),
        "model_deployment": extra.get("model_deployment"),
        "prompt_tokens": usage.get("prompt_tokens"),
        "completion_tokens": usage.get("completion_tokens"),
        "total_tokens": usage.get("total_tokens"),
        "input_cost": cost.get("input_tokens_cost"),
        "output_cost": cost.get("output_tokens_cost"),
        "total_cost": cost.get("total_cost"),
        "pending_tool_calls": pending,
        "error": None,
    }


# ─── Run benchmark ───────────────────────────────────────────────────────────


def run_benchmark(name, model, difficulty_filter=None, start_from=None, query_ids=None):
    """Run all (or filtered) eval tasks and save results."""

    # Load tasks
    with open(EVAL_TASKS_FILE) as f:
        tasks = json.load(f)

    # Filter to specific query IDs if specified
    if query_ids:
        id_set = set(query_ids)
        tasks = [t for t in tasks if t["id"] in id_set]
        missing = id_set - {t["id"] for t in tasks}
        if missing:
            print(f"Warning: query IDs not found: {missing}")
        print(f"Running {len(tasks)} specific queries: {[t['id'] for t in tasks]}")

    # Filter by difficulty if specified
    if difficulty_filter:
        tasks = [t for t in tasks if t["difficulty"] == difficulty_filter]
        print(f"Filtered to {len(tasks)} tasks with difficulty='{difficulty_filter}'")

    # Skip to start_from if specified
    if start_from:
        idx = next((i for i, t in enumerate(tasks) if t["id"] == start_from), None)
        if idx is None:
            print(f"Error: task ID '{start_from}' not found")
            sys.exit(1)
        tasks = tasks[idx:]
        print(f"Resuming from task {start_from} ({len(tasks)} tasks remaining)")

    # Create run directory
    run_dir = os.path.join(RUNS_DIR, name)
    os.makedirs(run_dir, exist_ok=True)

    # Check for existing results (to support resumption)
    results_file = os.path.join(run_dir, "results.json")
    existing_results = []
    completed_ids = set()
    rerun_ids = set(query_ids) if query_ids else set()
    if os.path.exists(results_file):
        with open(results_file) as f:
            existing_results = json.load(f)
        if rerun_ids:
            # Remove old results for queries we're re-running
            existing_results = [r for r in existing_results if r["query_id"] not in rerun_ids]
            print(f"Re-running {len(rerun_ids)} queries, keeping {len(existing_results)} existing results")
        completed_ids = {r["query_id"] for r in existing_results}
        if not rerun_ids:
            print(f"Found {len(existing_results)} existing results, will skip those")

    # Save run config
    config = {
        "run_name": name,
        "model": model,
        "started_at": datetime.now(timezone.utc).isoformat(),
        "difficulty_filter": difficulty_filter,
        "start_from": start_from,
        "total_tasks": len(tasks),
    }
    with open(os.path.join(run_dir, "config.json"), "w") as f:
        json.dump(config, f, indent=2)

    results = list(existing_results)

    print(f"\n{'='*70}")
    print(f"  Benchmark Run: {name}")
    print(f"  Model: {model}")
    print(f"  Tasks: {len(tasks)}")
    print(f"  Started: {config['started_at']}")
    print(f"{'='*70}\n")

    # Print live table header
    print_table_header()

    success_count = 0
    error_count = 0
    total_cost = 0.0
    total_tokens = 0

    for i, task in enumerate(tasks):
        # Skip already completed tasks
        if task["id"] in completed_ids:
            continue

        # Show which query is running
        print(f"\r  Running {task['id']}...", end="", flush=True)
        print(f"\r{' '*40}\r", end="", flush=True)

        result = run_query(task["query"], model)
        result["query_id"] = task["id"]
        result["difficulty"] = task["difficulty"]
        result["query"] = task["query"]
        result["expected_servers"] = task["expected_servers"]
        result["expected_tools"] = task["expected_tools"]
        result["success_criteria"] = task["success_criteria"]
        result["timestamp"] = datetime.now(timezone.utc).isoformat()

        results.append(result)

        # Track aggregates
        if result["status"] == "success":
            success_count += 1
            if result.get("total_cost"):
                total_cost += result["total_cost"]
            if result.get("total_tokens"):
                total_tokens += result["total_tokens"]
        else:
            error_count += 1

        # Print table row
        print_table_row(i + 1, len(tasks), result)

        # Save incrementally after each query
        with open(results_file, "w") as f:
            json.dump(results, f, indent=2)

    # Print table footer with totals
    print_table_footer(results)

    # Difficulty breakdown
    print_difficulty_breakdown(results)

    # Final banner
    completed_in_run = success_count + error_count
    print(f"\n  Results saved to: {results_file}")
    print(f"  View anytime:     python benchmark.py summary --name {name}\n")

    # Save final config with end time
    config["completed_at"] = datetime.now(timezone.utc).isoformat()
    config["summary"] = {
        "success": success_count,
        "errors": error_count,
        "total_tokens": total_tokens,
        "total_cost": round(total_cost, 6),
    }
    with open(os.path.join(run_dir, "config.json"), "w") as f:
        json.dump(config, f, indent=2)

    # Auto-fetch full session logs (useful for debugging)
    print("\n  Fetching session logs from Bifrost...")
    try:
        fetch_logs(name)
    except Exception as e:
        print(f"  Warning: failed to fetch logs: {e}")


# ─── Summary ─────────────────────────────────────────────────────────────────


def print_summary(name):
    """Print a summary table for a completed run."""
    run_dir = os.path.join(RUNS_DIR, name)
    results_file = os.path.join(run_dir, "results.json")
    config_file = os.path.join(run_dir, "config.json")

    if not os.path.exists(results_file):
        print(f"No results found for run '{name}'")
        sys.exit(1)

    with open(results_file) as f:
        results = json.load(f)
    with open(config_file) as f:
        config = json.load(f)

    model = config.get("model", "unknown")
    started = config.get("started_at", "?")
    completed = config.get("completed_at", "?")

    print(f"\n  Run: {name}  |  Model: {model}")
    print(f"  Started: {started}  |  Completed: {completed}\n")

    # Full results table
    print_table_header()
    for i, r in enumerate(results):
        print_table_row(i + 1, len(results), r)
    print_table_footer(results)

    # Difficulty breakdown
    print_difficulty_breakdown(results)


# ─── Fetch logs ─────────────────────────────────────────────────────────────


def fetch_logs(name):
    """Fetch full session logs from Bifrost API for a completed run."""
    run_dir = os.path.join(RUNS_DIR, name)
    results_file = os.path.join(run_dir, "results.json")
    config_file = os.path.join(run_dir, "config.json")

    if not os.path.exists(results_file):
        print(f"No results found for run '{name}'")
        sys.exit(1)

    with open(results_file) as f:
        results = json.load(f)
    with open(config_file) as f:
        config = json.load(f)

    # Use run timestamps to scope the log query
    started = config.get("started_at")
    completed = config.get("completed_at")
    model = config.get("model", "").split("/")[-1]  # e.g. claude-sonnet-4-6

    if not started or not completed:
        print("Run is missing started_at/completed_at in config.json")
        sys.exit(1)

    logs_dir = os.path.join(run_dir, "logs")
    os.makedirs(logs_dir, exist_ok=True)

    # Fetch all logs within the run's time window (paginated)
    base_params = {
        "start_time": started,
        "end_time": completed,
        "status": "success,error",
        "limit": 500,
    }
    if model:
        base_params["models"] = model

    print(f"Fetching logs for run '{name}' ({started} → {completed})...")

    all_logs = []
    offset = 0
    while True:
        params = {**base_params, "offset": offset}
        resp = requests.get(f"{BIFROST_BASE_URL.replace('/openai', '')}/api/logs", params=params)
        if resp.status_code != 200:
            print(f"Error fetching logs: {resp.status_code} {resp.text[:200]}")
            sys.exit(1)
        batch = resp.json().get("logs", [])
        all_logs.extend(batch)
        if len(batch) < 500:
            break
        offset += 500

    print(f"  Found {len(all_logs)} log entries from Bifrost (agent loop turns)")

    # The list endpoint returns only the last message per log entry (PR #2187).
    # To get the full conversation we fetch each entry's full log via
    # GET /api/logs/:id, match to queries by user message, and keep the
    # entry with the most messages per query (the final agent loop turn).

    logs_base = BIFROST_BASE_URL.replace("/openai", "")
    best_per_query = {}  # query_id -> (msg_count, log_data)

    for idx, log_entry in enumerate(all_logs):
        log_id = log_entry.get("id")
        if not log_id:
            continue

        # Fetch full log with complete input_history
        try:
            detail_resp = requests.get(f"{logs_base}/api/logs/{log_id}")
            if detail_resp.status_code != 200:
                continue
            full_log = detail_resp.json()
            if isinstance(full_log, dict) and "log" in full_log:
                full_log = full_log["log"]
        except Exception:
            continue

        input_history = full_log.get("input_history", [])

        # Find user message in the full conversation
        user_msg = ""
        for msg in input_history:
            if msg.get("role") == "user":
                user_msg = msg.get("content", "")
                break

        # Match to a result by query text
        matched_result = None
        for r in results:
            if r.get("query", "").strip() == user_msg.strip():
                matched_result = r
                break

        if not matched_result:
            continue

        query_id = matched_result["query_id"]
        msg_count = len(input_history)

        # Keep the log with the most messages (final agent loop turn)
        if query_id not in best_per_query or msg_count > best_per_query[query_id][0]:
            best_per_query[query_id] = (msg_count, {
                "query_id": query_id,
                "difficulty": matched_result.get("difficulty"),
                "query": matched_result.get("query"),
                "bifrost_log_id": log_id,
                "timestamp": full_log.get("timestamp"),
                "latency_ms": full_log.get("latency"),
                "cost": full_log.get("cost"),
                "status": full_log.get("status"),
                "token_usage": full_log.get("token_usage"),
                "input_history": input_history,
                "output_message": full_log.get("output_message"),
            })

        print(f"\r  Fetching full logs... {idx + 1}/{len(all_logs)}", end="", flush=True)

    print()

    # Save best log per query
    for query_id, (msg_count, log_data) in sorted(best_per_query.items()):
        log_file = os.path.join(logs_dir, f"{query_id}.json")
        with open(log_file, "w") as f:
            json.dump(log_data, f, indent=2)
        print(f"  {query_id}: {msg_count} messages")

    print(f"\nDone: {len(best_per_query)}/{len(results)} queries matched to logs")
    unmatched = [r["query_id"] for r in results if r["query_id"] not in best_per_query]
    if unmatched:
        print(f"  Unmatched queries: {', '.join(unmatched)}")
    print(f"  Logs saved to: {logs_dir}/")


# ─── CLI ─────────────────────────────────────────────────────────────────────


def main():
    parser = argparse.ArgumentParser(
        description="MCP Code Mode Benchmark Runner",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
Examples:
  python benchmark.py cleanup
  python benchmark.py run --name cm_off_sonnet_r1 --model anthropic/claude-sonnet-4-6
  python benchmark.py run --name cm_on_opus_r1 --model anthropic/claude-opus-4-6 --difficulty simple
  python benchmark.py run --name cm_off_gpt_r1 --model openai/gpt-5.4 --eval-file eval_tasks/eval_tasks_r3.json --start-from H1
  python benchmark.py summary --name cm_off_sonnet_r1
        """,
    )
    subparsers = parser.add_subparsers(dest="command", required=True)

    # cleanup
    cleanup_parser = subparsers.add_parser("cleanup", help="Reset workspace to clean state")
    cleanup_parser.add_argument(
        "--eval-file",
        default=DEFAULT_EVAL_FILE,
        help=f"Eval tasks file (default: {DEFAULT_EVAL_FILE})",
    )

    # run
    run_parser = subparsers.add_parser("run", help="Run benchmark tasks")
    run_parser.add_argument("--name", required=True, help="Unique name for this run")
    run_parser.add_argument(
        "--model",
        required=True,
        help="Model in provider/model format (e.g. anthropic/claude-sonnet-4-6)",
    )
    run_parser.add_argument(
        "--difficulty",
        choices=["simple", "medium", "hard", "edge"],
        help="Only run tasks of this difficulty",
    )
    run_parser.add_argument(
        "--start-from",
        help="Resume from this task ID (e.g. M10, H1)",
    )
    run_parser.add_argument(
        "--queries",
        nargs="+",
        help="Run only these specific query IDs (e.g. --queries S9 H1 M5)",
    )
    run_parser.add_argument(
        "--eval-file",
        default=DEFAULT_EVAL_FILE,
        help=f"Eval tasks file (default: {DEFAULT_EVAL_FILE})",
    )

    # summary
    summary_parser = subparsers.add_parser("summary", help="Print run summary")
    summary_parser.add_argument("--name", required=True, help="Run name to summarize")

    # fetch-logs
    logs_parser = subparsers.add_parser("fetch-logs", help="Fetch full session logs from Bifrost")
    logs_parser.add_argument("--name", required=True, help="Run name to fetch logs for")
    logs_parser.add_argument(
        "--eval-file",
        default=DEFAULT_EVAL_FILE,
        help=f"Eval tasks file (default: {DEFAULT_EVAL_FILE})",
    )

    args = parser.parse_args()

    # Override eval tasks file if specified
    global EVAL_TASKS_FILE
    eval_file = getattr(args, "eval_file", None)
    if eval_file:
        EVAL_TASKS_FILE = resolve_eval_file(eval_file)

    if args.command == "cleanup":
        cleanup()
    elif args.command == "run":
        run_benchmark(args.name, args.model, args.difficulty, args.start_from, args.queries)
    elif args.command == "summary":
        print_summary(args.name)
    elif args.command == "fetch-logs":
        fetch_logs(args.name)


if __name__ == "__main__":
    main()
