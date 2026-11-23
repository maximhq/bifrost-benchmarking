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
import re
import shutil
import sqlite3
import subprocess
import sys
import time
from concurrent.futures import ThreadPoolExecutor
from datetime import datetime, timezone

import requests

# ─── Configuration ───────────────────────────────────────────────────────────

BIFROST_BASE_URL = os.environ.get("BIFROST_BASE_URL", "http://localhost:8080/openai")
BIFROST_COMPLETIONS_URL = f"{BIFROST_BASE_URL}/v1/chat/completions"
API_KEY = os.environ.get("BIFROST_API_KEY", "dummy-key")
# fetch-logs reads Bifrost's management API (/api/logs), which needs the setup token before an
# admin account exists, and the admin's credentials (Basic auth) once dashboard auth is on.
SETUP_TOKEN = os.environ.get("BIFROST_SETUP_TOKEN")
ADMIN_USER = os.environ.get("BIFROST_ADMIN_USER")
ADMIN_PASSWORD = os.environ.get("BIFROST_ADMIN_PASSWORD")

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

# Names no server, so a run measures what the gateway itself tells the model about its tools.
# The default above lists every server by hand, which hides whether the model could find them.
NEUTRAL_SYSTEM_MESSAGE = (
    "You are a helpful AI agent. "
    "Use the available tools to fulfill the user's request completely. "
    "Be concise in your responses — focus on results, not process narration."
)
SYSTEM_MESSAGES = {"default": SYSTEM_MESSAGE, "neutral": NEUTRAL_SYSTEM_MESSAGE}

# Sent on every request; Bifrost maps it to each provider's reasoning setting (and drops it for
# models without reasoning).
REASONING_EFFORT = "medium"

CODE_MODE_DISCOVERY_TOOLS = ("listToolFiles", "readToolFile", "getToolDocs")
CODE_MODE_EXECUTE_TOOL = "executeToolCode"
# The line Bifrost adds to the system message naming the code mode servers the caller can reach:
# "MCP servers available through code mode: a, b. Each one is a server: ..." (and the earlier
# "MCP servers available through code mode (start with listToolFiles to use them): a, b.").
CODE_MODE_POINTER = re.compile(r"MCP servers available through code mode(?: \(start with listToolFiles to use them\))?: ([^\n]*?)\.(?: |$)", re.MULTILINE)
# What Bifrost answers a tool call with when the tool is not served to the caller (core/mcp/exec.go).
NOT_PERMITTED_TOOL_ERROR = "not permitted"
# What executeToolCode reports for a name the sandbox does not bind, e.g. a server the caller cannot
# reach, or one spelled in the wrong case (modules are named exactly as the Bifrost client).
UNDEFINED_NAME_ERROR = re.compile(r"undefined: (\w+)")

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


def run_query(query, model, system_message=SYSTEM_MESSAGE, virtual_key=None):
    """Send a single query to Bifrost and capture all metrics."""
    start_time = time.time()
    headers = {
        "Authorization": f"Bearer {API_KEY}",
        "Content-Type": "application/json",
    }
    if virtual_key:
        headers["x-bf-vk"] = virtual_key
    body = {
        "model": model,
        "messages": [
            {"role": "system", "content": system_message},
            {"role": "user", "content": query},
        ],
        "reasoning_effort": REASONING_EFFORT,
    }

    try:
        resp = requests.post(BIFROST_COMPLETIONS_URL, headers=headers, json=body)
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


def run_benchmark(name, model, difficulty_filter=None, start_from=None, query_ids=None, system_message="default"):
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
        "system_message": system_message,
        "reasoning_effort": REASONING_EFFORT,
        "total_tasks": len(tasks),
    }
    with open(os.path.join(run_dir, "config.json"), "w") as f:
        json.dump(config, f, indent=2)

    results = list(existing_results)

    print(f"\n{'='*70}")
    print(f"  Benchmark Run: {name}")
    print(f"  Model: {model}  |  Reasoning effort: {REASONING_EFFORT}")
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

        # A task can run under its own virtual key (e.g. one that cannot reach a server). Without
        # it the task would silently run with full access, so a missing key fails the task.
        vk_env = task.get("virtual_key_env")
        if vk_env and not os.environ.get(vk_env):
            result = {"status": "error", "error": f"{vk_env} is not set; this task runs under that virtual key", "wall_clock_seconds": 0}
        else:
            result = run_query(task["query"], model, SYSTEM_MESSAGES[system_message], os.environ.get(vk_env) if vk_env else None)
        result["query_id"] = task["id"]
        result["difficulty"] = task["difficulty"]
        result["query"] = task["query"]
        result["expected_servers"] = task["expected_servers"]
        if task.get("unavailable_server"):
            result["unavailable_server"] = task["unavailable_server"]
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


def matrix_runs(name):
    """The per-model runs a multi-model `run` wrote under runs/<name>/, or [] for a single run."""
    run_dir = os.path.join(RUNS_DIR, name)
    if os.path.exists(os.path.join(run_dir, "results.json")) or not os.path.isdir(run_dir):
        return []
    return sorted(
        f"{name}/{entry}" for entry in os.listdir(run_dir)
        if os.path.exists(os.path.join(run_dir, entry, "results.json"))
    )


def print_matrix_summary(name, runs):
    """Print one row per model run, so models can be compared on the same queries."""
    print(f"\n  Run: {name}  |  {len(runs)} model runs\n")
    print(f"┌{'─'*34}┬{'─'*9}┬{'─'*12}┬{'─'*11}┬{'─'*10}┬{'─'*11}┬{'─'*11}┐")
    print(f"│ {'Model':<32} │ {'OK':>7} │ {'Discovered':>10} │ {'Avg turns':>9} │ {'Errors':>8} │ {'Tokens':>9} │ {'Cost':>9} │")
    print(f"├{'─'*34}┼{'─'*9}┼{'─'*12}┼{'─'*11}┼{'─'*10}┼{'─'*11}┼{'─'*11}┤")
    for run in runs:
        run_dir = os.path.join(RUNS_DIR, run)
        with open(os.path.join(run_dir, "results.json")) as f:
            results = json.load(f)
        rows = score_rows(run_dir, results)
        scored = [row for row in rows if row["scored"]]
        ok = f"{sum(1 for r in results if r['status'] == 'success')}/{len(results)}"
        discovered = f"{sum(1 for row in scored if row['discovered'])}/{len(scored)}" if scored else "-"
        turns = f"{sum(row['turns'] for row in scored) / len(scored):.1f}" if scored else "-"
        errors = str(sum(1 for row in scored if row["unavailable_errors"])) if scored else "-"
        tokens = _fmt_tokens(sum(r.get("total_tokens", 0) or 0 for r in results))
        cost = _fmt_cost(sum(r.get("total_cost", 0) or 0 for r in results))
        label = run.split("/", 1)[1]
        if len(label) > 32:
            label = label[:31] + "…"
        print(f"│ {label:<32} │ {ok:>7} │ {discovered:>10} │ {turns:>9} │ {errors:>8} │ {tokens:>9} │ {cost:>9} │")
    print(f"└{'─'*34}┴{'─'*9}┴{'─'*12}┴{'─'*11}┴{'─'*10}┴{'─'*11}┴{'─'*11}┘")
    print(f"  Discovered and Errors need fetched logs ('-' = none yet). Per run: python benchmark.py summary --name {runs[0]}")


def print_summary(name):
    """Print a summary table for a completed run."""
    runs = matrix_runs(name)
    if runs:
        print_matrix_summary(name, runs)
        return
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

    # Whether each query found its servers through code mode (needs fetch-logs)
    print_discovered_breakdown(run_dir, results)


# ─── Fetch logs ─────────────────────────────────────────────────────────────


def fetch_logs(name):
    """Fetch full session logs from Bifrost API for a completed run."""
    runs = matrix_runs(name)
    if runs:
        for run in runs:
            fetch_logs(run)
        return
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

    management_auth = {}
    if ADMIN_USER and ADMIN_PASSWORD:
        management_auth["auth"] = (ADMIN_USER, ADMIN_PASSWORD)
    elif SETUP_TOKEN:
        management_auth["headers"] = {"X-Bifrost-Setup-Token": SETUP_TOKEN}

    print(f"Fetching logs for run '{name}' ({started} → {completed})...")

    all_logs = []
    offset = 0
    while True:
        params = {**base_params, "offset": offset}
        resp = requests.get(f"{BIFROST_BASE_URL.replace('/openai', '')}/api/logs", params=params, **management_auth)
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
            detail_resp = requests.get(f"{logs_base}/api/logs/{log_id}", **management_auth)
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


# ─── Discovered ──────────────────────────────────────────────────────────────
#
# A run's "success" only means the request returned 200: a model that answers "I can't reach
# Linear" without trying passes it. summary also reads the agent loop from the fetched logs and
# asks the question code mode discovery is about: did the model find the server it needed through
# code mode, without being told it was there?
#   discovered - every expected server was found through code mode: its file was read
#                (readToolFile / getToolDocs on servers/<name>...) or executeToolCode called it.
#                What happens after that does not count against the model: a tool that fails on a
#                bad key, an auth error or a provider error still means the server was found. A
#                task that expects no server (a control, a service that is not connected, or a
#                server the key cannot reach) is discovered when no server was used in its place
#                and the model answered.
#   infra      - the request failed before the model did anything (an upstream timeout, a gateway
#                error); there is nothing to score, so it is left out rather than passed or failed.
#   errors     - the "server is not connected" failures: a tool name Bifrost does not serve, such as
#                ListToolFiles for listToolFiles or the server name called as a tool (left
#                unexecuted on the response), a call answered "not permitted", or code naming an
#                expected server the sandbox does not bind ("undefined: linear" when the client is
#                Linear). Shown for diagnosis; they fail a task only when the server was never found.
#   turns      - model calls in the agent loop; the intended flow (listToolFiles, readToolFile,
#                executeToolCode, answer) takes 3-4.
#   discovery  - listToolFiles / readToolFile / getToolDocs calls before the first executeToolCode.
#   no tool    - the model called no tool at all.
# A server counts as called when executeToolCode code calls one of its tools (code mode binds each
# server as a module, `linear.get_teams()`), or when a tool named "<server>-<tool>" is called
# directly (code mode off). Server names are compared case-insensitively.


def _message_text(content):
    if isinstance(content, str):
        return content
    if isinstance(content, list):
        return "".join(part.get("text") or "" for part in content if isinstance(part, dict))
    return ""


def _tool_calls(log):
    """Every tool call in the agent loop, in order, as (name, arguments) pairs."""
    messages = list(log.get("input_history") or [])
    if log.get("output_message"):
        messages.append(log["output_message"])
    calls = []
    for message in messages:
        for call in message.get("tool_calls") or []:
            function = call.get("function") or {}
            calls.append((function.get("name") or "", function.get("arguments") or ""))
    return calls


def _executed_code(arguments):
    if isinstance(arguments, dict):
        return str(arguments.get("code") or "")
    try:
        return str(json.loads(arguments).get("code") or "")
    except (ValueError, AttributeError):
        return str(arguments)


def _servers_called(calls, servers):
    called = set()
    for name, arguments in calls:
        if name == CODE_MODE_EXECUTE_TOOL:
            code = _executed_code(arguments)
            for server in servers:
                if re.search(r"\b" + re.escape(server) + r"\s*\.\s*\w+\s*\(", code, re.IGNORECASE):
                    called.add(server)
        elif name not in CODE_MODE_DISCOVERY_TOOLS:
            for server in servers:
                if re.match(re.escape(server) + r"[-_]", name, re.IGNORECASE):
                    called.add(server)
    return called


def _servers_found(calls, servers):
    """Servers the model found through code mode: called from code, or their file read."""
    found = _servers_called(calls, servers)
    for name, arguments in calls:
        if name in ("readToolFile", "getToolDocs"):
            text = arguments if isinstance(arguments, str) else json.dumps(arguments)
            for server in servers:
                if re.search(r"servers/" + re.escape(server) + r"(\.pyi|/|\b)", text, re.IGNORECASE):
                    found.add(server)
    return found


def _offered_servers(history):
    """The code mode servers the gateway offered: the pointer line Bifrost adds to the system
    message, and any servers/<name>.pyi file the model read."""
    names = set()
    for message in history:
        text = _message_text(message.get("content"))
        if message.get("role") == "system":
            for listed in CODE_MODE_POINTER.findall(text):
                names.update(n.strip() for n in re.split(r",| and \d+ more", listed) if n.strip())
        names.update(re.findall(r"servers/(\w+)\.pyi", text))
    return names


def score_task(result, log):
    """Score one task from its result row and its fetched log (None when no log matched)."""
    row = {
        "query_id": result["query_id"],
        "completed": result.get("status") == "success",
        "scored": log is not None,
        "infra": False,
        "discovered": False,
        "turns": None,
        "discovery_calls": None,
        "tool_calls": None,
        "unavailable_errors": None,
        "missing_servers": [],
    }
    if log is None:
        return row
    calls = _tool_calls(log)
    history = log.get("input_history") or []
    # The answer is what the client got back; the log's final message stands in only when it is missing.
    answer = result.get("response")
    if answer is None:
        answer = _message_text((log.get("output_message") or {}).get("content"))
    expected = result.get("expected_servers") or []

    row["turns"] = sum(1 for m in history if m.get("role") == "assistant") + (1 if log.get("output_message") else 0)
    # A tool name Bifrost does not serve (ListToolFiles for listToolFiles) is never run: the agent
    # loop stops and hands the call back, so it shows up as a pending call on the response.
    errors = len(result.get("pending_tool_calls") or [])
    expected_names = {server.lower() for server in expected}
    for message in history:
        if message.get("role") != "tool":
            continue
        text = _message_text(message.get("content"))
        if NOT_PERMITTED_TOOL_ERROR in text.lower():
            errors += 1
        elif any(name.lower() in expected_names for name in UNDEFINED_NAME_ERROR.findall(text)):
            errors += 1
    row["unavailable_errors"] = errors

    first_execute = next((i for i, (name, _) in enumerate(calls) if name == CODE_MODE_EXECUTE_TOOL), len(calls))
    row["discovery_calls"] = sum(1 for name, _ in calls[:first_execute] if name in CODE_MODE_DISCOVERY_TOOLS)
    row["tool_calls"] = len(calls)

    # The request failed before the model acted: nothing to judge.
    if not row["completed"] and not calls and not result.get("pending_tool_calls"):
        row["scored"] = False
        row["infra"] = True
        return row

    if expected:
        found = _servers_found(calls, expected)
        row["missing_servers"] = [server for server in expected if server not in found]
        row["discovered"] = not row["missing_servers"]
    elif result.get("unavailable_server"):
        # A service that is not connected (or that the key cannot reach): the model must know it is
        # not there, so it passes when it never tries to use it and answers. Looking for related
        # information in a server that is connected (a runbook in the team wiki) is fine.
        missing = result["unavailable_server"].lower()
        named = [n for n, _ in calls] + [p.get("name") or "" for p in result.get("pending_tool_calls") or []]
        tried = any(missing in n.lower() for n in named if n not in CODE_MODE_DISCOVERY_TOOLS + (CODE_MODE_EXECUTE_TOOL,))
        tried = tried or bool(_servers_found(calls, [missing]))
        tried = tried or any(
            missing in (n.lower() for n in UNDEFINED_NAME_ERROR.findall(_message_text(m.get("content"))))
            for m in history if m.get("role") == "tool"
        )
        row["discovered"] = not tried and bool((answer or "").strip())
    else:
        # No server should be used at all (a control). executeToolCode on its own is not a server
        # call (a model may do arithmetic in the sandbox); it counts only when its code calls a server
        # the gateway offered, and any other tool the model calls directly is a server tool.
        direct = any(name != CODE_MODE_EXECUTE_TOOL and name not in CODE_MODE_DISCOVERY_TOOLS for name, _ in calls)
        used = direct or _servers_called(calls, _offered_servers(history))
        row["discovered"] = not used and bool((answer or "").strip())
    return row


def score_rows(run_dir, results):
    """Score every query of a run from runs/<run>/logs/."""
    rows = []
    for result in results:
        log_file = os.path.join(run_dir, "logs", f"{result['query_id']}.json")
        log = None
        if os.path.exists(log_file):
            with open(log_file) as f:
                log = json.load(f)
        rows.append(score_task(result, log))
    return rows


def print_discovered_breakdown(run_dir, results):
    """Print, from the fetched logs, whether each query found its servers through code mode."""
    rows = score_rows(run_dir, results)
    scored = [row for row in rows if row["scored"]]
    if not scored:
        print("\n  No fetched logs: run fetch-logs to see which queries found their servers.")
        return

    def dash(value):
        return "-" if value is None else str(value)

    print(f"\n┌{'─'*84}┐")
    print(f"│ {'DISCOVERED (from fetched logs)':^82} │")
    print(f"├{'─'*8}┬{'─'*10}┬{'─'*7}┬{'─'*11}┬{'─'*12}┬{'─'*8}┬{'─'*22}┤")
    print(f"│ {'ID':<6} │ {'Found':<8} │ {'Turns':>5} │ {'Discovery':>9} │ {'Tool calls':>10} │ {'Errors':>6} │ {'Missing servers':<20} │")
    print(f"├{'─'*8}┼{'─'*10}┼{'─'*7}┼{'─'*11}┼{'─'*12}┼{'─'*8}┼{'─'*22}┤")
    for row in rows:
        reached = "infra" if row["infra"] else ("-" if not row["scored"] else ("yes" if row["discovered"] else "no"))
        missing = ", ".join(row["missing_servers"])
        if len(missing) > 20:
            missing = missing[:19] + "…"
        print(
            f"│ {row['query_id']:<6} │ {reached:<8} │ {dash(row['turns']):>5} │ {dash(row['discovery_calls']):>9} │ "
            f"{dash(row['tool_calls']):>10} │ {dash(row['unavailable_errors']):>6} │ {missing:<20} │"
        )
    reached_str = f"{sum(1 for row in scored if row['discovered'])}/{len(scored)}"
    avg_turns = f"{sum(row['turns'] for row in scored) / len(scored):.1f}"
    avg_discovery = f"{sum(row['discovery_calls'] for row in scored) / len(scored):.1f} avg"
    no_tool = f"{sum(1 for row in scored if row['tool_calls'] == 0)} none"
    errors = str(sum(1 for row in scored if row["unavailable_errors"]))
    print(f"├{'─'*8}┼{'─'*10}┼{'─'*7}┼{'─'*11}┼{'─'*12}┼{'─'*8}┼{'─'*22}┤")
    print(f"│ {'TOTAL':<6} │ {reached_str:<8} │ {avg_turns:>5} │ {avg_discovery:>9} │ {no_tool:>10} │ {errors:>6} │ {'':<20} │")
    print(f"└{'─'*8}┴{'─'*10}┴{'─'*7}┴{'─'*11}┴{'─'*12}┴{'─'*8}┴{'─'*22}┘")
    print("  Errors: calls that failed as unavailable (tool name Bifrost does not serve, 'not permitted', or an undefined server).")

    infra = [row["query_id"] for row in rows if row["infra"]]
    if infra:
        print(f"  Left out, the request failed before the model acted (timeout or gateway error): {', '.join(infra)}")
    unscored = [row["query_id"] for row in rows if not row["scored"] and not row["infra"]]
    if unscored:
        print(f"  Unscored (no log matched, run fetch-logs again): {', '.join(unscored)}")


# ─── Parallel runs ───────────────────────────────────────────────────────────


def run_parallel(args, combos):
    """Run each model in its own process, all models at once. Each run's live output goes to
    runs/<run>/run.log."""
    by_model = {}
    for model, name in combos:
        by_model.setdefault(model, []).append(name)

    def run_model(model):
        for name in by_model[model]:
            log_path = os.path.join(RUNS_DIR, name, "run.log")
            os.makedirs(os.path.dirname(log_path), exist_ok=True)
            cmd = [sys.executable, os.path.abspath(__file__), "run", "--name", name, "--model", model,
                   "--eval-file", EVAL_TASKS_FILE, "--system-message", args.system_message]
            if args.difficulty:
                cmd += ["--difficulty", args.difficulty]
            if args.start_from:
                cmd += ["--start-from", args.start_from]
            if args.queries:
                cmd += ["--queries", *args.queries]
            with open(log_path, "w") as out:
                code = subprocess.run(cmd, stdout=out, stderr=subprocess.STDOUT).returncode
            print(f"  {name}: {'finished' if code == 0 else f'failed (exit {code})'}, log: {log_path}", flush=True)

    print(f"\n  Running {len(by_model)} models in parallel ({len(combos)} runs)...\n", flush=True)
    with ThreadPoolExecutor(max_workers=len(by_model)) as pool:
        list(pool.map(run_model, by_model))


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
  python benchmark.py run --name disc --model anthropic/claude-sonnet-4-6 openai/gpt-5.4 --parallel --eval-file eval_tasks/eval_tasks_discovery.json --system-message neutral
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
        nargs="+",
        help="Model(s) in provider/model format (e.g. anthropic/claude-sonnet-4-6); several run one after another",
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
    run_parser.add_argument(
        "--system-message",
        choices=sorted(SYSTEM_MESSAGES),
        default="default",
        help="default lists every server (R1-R3); neutral names none, for discoverability runs",
    )
    run_parser.add_argument(
        "--parallel",
        action="store_true",
        help="Run each --model at the same time, one process per model",
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
        combos = []
        for model in args.model:
            # One model keeps the plain run layout; several get runs/<name>/<model>.
            name = args.name if len(args.model) == 1 else f"{args.name}/{model.split('/')[-1]}"
            combos.append((model, name))
        if args.parallel and len(args.model) > 1:
            run_parallel(args, combos)
            # Each run fetched its logs as it finished, possibly before Bifrost flushed the last ones.
            time.sleep(10)
            fetch_logs(args.name)
        else:
            for model, name in combos:
                run_benchmark(name, model, args.difficulty, args.start_from, args.queries, args.system_message)
        if len(combos) > 1:
            print_matrix_summary(args.name, matrix_runs(args.name))
    elif args.command == "summary":
        print_summary(args.name)
    elif args.command == "fetch-logs":
        fetch_logs(args.name)


if __name__ == "__main__":
    main()
