"""Generate human-readable and machine-readable test reports."""

from __future__ import annotations

import json
import sys
from datetime import datetime, timezone
from typing import Any, TextIO

from .runner import ModelResult


def console_report(
    results: list[ModelResult],
    *,
    sdk_version: str = "",
    stream: TextIO = sys.stdout,
) -> None:
    """Print a coloured summary table to the terminal."""
    header = (
        f"Cog {sdk_version} Compatibility Report"
        if sdk_version
        else "Cog Compatibility Report"
    )
    stream.write(f"\n{'=' * len(header)}\n")
    stream.write(f"{header}\n")
    stream.write(f"{'=' * len(header)}\n\n")

    passed = 0
    failed = 0
    skipped = 0

    for r in results:
        if r.skipped:
            _write(stream, "SKIP", r.name, r.skip_reason or "", gpu=r.gpu)
            skipped += 1
            continue

        if r.error:
            _write(stream, "FAIL", r.name, r.error.splitlines()[0], gpu=r.gpu)
            failed += 1
            continue

        all_tests = r.test_results + r.train_results
        if r.passed:
            timing = _timing_str(r.build_duration_s, all_tests)
            _write(stream, "PASS", r.name, timing, gpu=r.gpu)
            passed += 1
        else:
            failures = [t for t in all_tests if not t.passed]
            msg = f"{len(failures)} test(s) failed"
            if failures:
                msg += f": {failures[0].message[:60]}"
            _write(stream, "FAIL", r.name, msg, gpu=r.gpu)
            failed += 1

            # Print individual test failures indented
            for t in failures:
                stream.write(f"    FAIL {t.description}: {t.message[:100]}\n")

    stream.write(f"\n{'-' * 40}\n")
    total = passed + failed + skipped
    stream.write(f"{passed}/{total} passed")
    if skipped:
        stream.write(f", {skipped} skipped")
    if failed:
        stream.write(f", {failed} FAILED")
    stream.write("\n\n")


def json_report(
    results: list[ModelResult],
    *,
    sdk_version: str = "",
) -> dict[str, Any]:
    """Return a JSON-serializable report dict."""
    models = []
    for r in results:
        entry: dict[str, Any] = {
            "name": r.name,
            "passed": r.passed,
            "skipped": r.skipped,
            "gpu": r.gpu,
            "build_duration_s": round(r.build_duration_s, 2),
        }
        if r.skipped:
            entry["skip_reason"] = r.skip_reason
        if r.error:
            entry["error"] = r.error
        if r.test_results:
            entry["tests"] = [
                {
                    "description": t.description,
                    "passed": t.passed,
                    "message": t.message,
                    "duration_s": round(t.duration_s, 2),
                }
                for t in r.test_results
            ]
        if r.train_results:
            entry["train_tests"] = [
                {
                    "description": t.description,
                    "passed": t.passed,
                    "message": t.message,
                    "duration_s": round(t.duration_s, 2),
                }
                for t in r.train_results
            ]
        models.append(entry)

    total = len(results)
    passed = sum(1 for r in results if r.passed and not r.skipped)
    failed = sum(1 for r in results if not r.passed)
    skipped_count = sum(1 for r in results if r.skipped)

    return {
        "timestamp": datetime.now(timezone.utc).isoformat(),
        "sdk_version": sdk_version,
        "summary": {
            "total": total,
            "passed": passed,
            "failed": failed,
            "skipped": skipped_count,
        },
        "models": models,
    }


def write_json_report(
    results: list[ModelResult],
    *,
    sdk_version: str = "",
    stream: TextIO = sys.stdout,
) -> None:
    """Write JSON report to a stream."""
    report = json_report(results, sdk_version=sdk_version)
    json.dump(report, stream, indent=2)
    stream.write("\n")


# ── Helpers ────────────────────────────────────────────────────────────


def _write(
    stream: TextIO, status: str, name: str, detail: str, *, gpu: bool = False
) -> None:
    icon = {"PASS": "+", "FAIL": "x", "SKIP": "-"}[status]
    gpu_tag = " [GPU]" if gpu else ""
    stream.write(f"  {icon} {name:<25} {detail}{gpu_tag}\n")


def _timing_str(build_s: float, tests: list[Any]) -> str:
    parts = [f"{build_s:.1f}s build"]
    if tests:
        total_predict = sum(t.duration_s for t in tests)
        parts.append(f"{total_predict:.1f}s predict")
    return f"({', '.join(parts)})"
