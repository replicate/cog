# Infra verification predictor for the v1 managed-weights OCI pipeline.
# Validates weight files on disk against weights.lock at setup; predict()
# returns a per-weight status summary.

import hashlib
import json
import sys
from pathlib import Path
from typing import Any

from cog import BasePredictor

LOCK_PATH = Path("/src/weights.lock")


def _file_sha256(path: Path) -> str:
    h = hashlib.sha256()
    with open(path, "rb") as f:
        while chunk := f.read(8 * 1024 * 1024):
            h.update(chunk)
    return f"sha256:{h.hexdigest()}"


def _validate_weight(
    name: str, target: str, expected_files: list[dict[str, Any]]
) -> dict[str, Any]:
    """Validate a single weight entry from the lockfile.

    Checks presence and size first (cheap), then hashes only files whose
    size matches (expensive). This way missing or truncated files fail fast
    without reading gigabytes of data.
    """
    target_dir = Path(target)

    if not target_dir.is_dir():
        return {
            "name": name,
            "target": target,
            "errors": [f"weight directory {target} does not exist"],
            "warnings": [],
            "ok": [],
            "missing": [f["path"] for f in expected_files],
            "extra": [],
            "mismatch": [],
        }

    # Walk the directory once — just stat, no hashing yet.
    actual_by_path: dict[str, Path] = {}
    actual_sizes: dict[str, int] = {}
    for p in sorted(target_dir.rglob("*")):
        if not p.is_file():
            continue
        rel = str(p.relative_to(target_dir))
        actual_by_path[rel] = p
        actual_sizes[rel] = p.stat().st_size

    ok: list[str] = []
    missing: list[str] = []
    mismatch: list[str] = []
    errors: list[str] = []

    for entry in expected_files:
        path = entry["path"]

        if path not in actual_by_path:
            missing.append(path)
            errors.append(f"missing: {path}")
            continue

        disk_size = actual_sizes[path]
        if disk_size != entry["size"]:
            mismatch.append(path)
            errors.append(
                f"size mismatch: {path} (expected {entry['size']}, got {disk_size})"
            )
            actual_by_path.pop(path)
            continue

        # Size matches — hash to confirm content.
        digest = _file_sha256(actual_by_path.pop(path))
        if digest != entry["digest"]:
            mismatch.append(path)
            errors.append(f"digest mismatch: {path}")
        else:
            ok.append(path)

    extra = sorted(actual_by_path.keys())
    warnings = [f"extra file: {p}" for p in extra]

    return {
        "name": name,
        "target": target,
        "errors": errors,
        "warnings": warnings,
        "ok": ok,
        "missing": missing,
        "extra": extra,
        "mismatch": mismatch,
    }


class Predictor(BasePredictor):
    def setup(self) -> None:
        if not LOCK_PATH.exists():
            raise RuntimeError(f"{LOCK_PATH} not found — cannot validate weights")

        lock = json.loads(LOCK_PATH.read_text())

        self.results: list[dict[str, Any]] = []
        all_errors: list[str] = []

        for entry in lock["weights"]:
            name = entry["name"]
            target = entry["target"]
            expected_files = [
                {"path": f["path"], "size": f["size"], "digest": f["digest"]}
                for f in entry["files"]
            ]

            # Dump directory contents before validation for debugging.
            target_dir = Path(target)
            if target_dir.is_dir():
                print(f"--- find {target} ---", file=sys.stderr)
                for p in sorted(target_dir.rglob("*")):
                    suffix = "/" if p.is_dir() else f"  ({p.stat().st_size})"
                    print(f"  {p.relative_to(target_dir)}{suffix}", file=sys.stderr)
                print("---", file=sys.stderr)
            else:
                print(f"--- {target}: does not exist ---", file=sys.stderr)

            print(
                f"validating weight '{name}' at {target} ({len(expected_files)} files)",
                file=sys.stderr,
            )
            result = _validate_weight(name, target, expected_files)
            self.results.append(result)

            for w in result["warnings"]:
                print(f"  WARNING: {w}", file=sys.stderr)

            if result["errors"]:
                for e in result["errors"]:
                    all_errors.append(f"[{name}] {e}")
            else:
                print(f"  OK ({len(result['ok'])} files)", file=sys.stderr)

        if all_errors:
            msg = "weight validation failed:\n" + "\n".join(
                f"  - {e}" for e in all_errors
            )
            raise RuntimeError(msg)

        print("all weights validated", file=sys.stderr)

    def predict(self) -> str:
        summary = []
        for r in self.results:
            entry: dict[str, Any] = {
                "name": r["name"],
                "target": r["target"],
                "status": "ok" if not r["errors"] else "error",
                "ok": len(r["ok"]),
                "missing": r["missing"],
                "extra": r["extra"],
                "mismatch": r["mismatch"],
            }
            summary.append(entry)
        return json.dumps(summary)
