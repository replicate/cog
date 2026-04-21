# Predictor for examples/managed-weights — serves as an infra verification
# tool for the v1 managed-weights OCI pipeline.
#
# setup() validates weight files on disk against weights_manifest.json
# (baked into the image) and errors if anything is missing or mismatched.
# predict() returns a structured diff of expected vs actual files.
#
# The manifest check is a temporary hack until /.cog/weights.json is
# embedded in the model image. See generate_manifest.py.

import hashlib
import json
import sys
from pathlib import Path
from typing import Any

from cog import BasePredictor

WEIGHTS_DIR = Path("/src/weights")
MANIFEST_PATH = Path("/src/weights_manifest.json")


def _file_sha256(path: Path) -> str:
    h = hashlib.sha256()
    with open(path, "rb") as f:
        while chunk := f.read(8 * 1024 * 1024):
            h.update(chunk)
    return f"sha256:{h.hexdigest()}"


def _inventory(root: Path) -> list[dict[str, Any]]:
    """Return sorted list of {path, size, digest} for all files under root."""
    entries = []
    for p in sorted(root.rglob("*")):
        if not p.is_file():
            continue
        entries.append(
            {
                "path": str(p.relative_to(root)),
                "size": p.stat().st_size,
                "digest": _file_sha256(p),
            }
        )
    return entries


def _diff(
    expected: list[dict[str, Any]], actual: list[dict[str, Any]]
) -> dict[str, Any]:
    """Compare expected manifest against actual inventory.

    Returns a dict with:
      ok:       list of files that match exactly
      missing:  list of files expected but not on disk
      extra:    list of files on disk but not in manifest
      mismatch: list of files present but with wrong size/digest
      errors:   flat list of human-readable error strings (for setup)
    """
    actual_by_path = {e["path"]: e for e in actual}
    ok = []
    missing = []
    mismatch = []
    errors: list[str] = []

    for entry in expected:
        path = entry["path"]
        on_disk = actual_by_path.pop(path, None)
        if on_disk is None:
            missing.append(entry)
            errors.append(f"missing: {path}")
            continue

        diffs = {}
        if on_disk["size"] != entry["size"]:
            diffs["size"] = {"expected": entry["size"], "actual": on_disk["size"]}
            errors.append(
                f"size mismatch: {path} (expected {entry['size']}, got {on_disk['size']})"
            )
        if on_disk["digest"] != entry["digest"]:
            diffs["digest"] = {"expected": entry["digest"], "actual": on_disk["digest"]}
            errors.append(f"digest mismatch: {path}")

        if diffs:
            mismatch.append({"path": path, **diffs})
        else:
            ok.append(entry)

    extra = [actual_by_path[p] for p in sorted(actual_by_path)]
    for e in extra:
        errors.append(f"unexpected: {e['path']}")

    return {
        "ok": ok,
        "missing": missing,
        "extra": extra,
        "mismatch": mismatch,
        "errors": errors,
    }


class Predictor(BasePredictor):
    def setup(self) -> None:
        if not WEIGHTS_DIR.is_dir():
            raise RuntimeError(f"weight directory {WEIGHTS_DIR} does not exist")

        self.inventory = _inventory(WEIGHTS_DIR)
        for entry in self.inventory:
            print(
                f"weight file: {entry['path']}  size={entry['size']}  digest={entry['digest']}",
                file=sys.stderr,
            )
        print(f"total weight files: {len(self.inventory)}", file=sys.stderr)

        if not MANIFEST_PATH.exists():
            print(f"WARNING: {MANIFEST_PATH} not found, skipping validation", file=sys.stderr)
            self.manifest = None
            return

        self.manifest = json.loads(MANIFEST_PATH.read_text())
        result = _diff(self.manifest["files"], self.inventory)
        if result["errors"]:
            msg = "weight validation failed:\n" + "\n".join(f"  - {e}" for e in result["errors"])
            raise RuntimeError(msg)

        print("weight validation passed", file=sys.stderr)

    def predict(self) -> str:
        if self.manifest is None:
            return json.dumps({
                "target": str(WEIGHTS_DIR),
                "status": "no manifest",
                "files": self.inventory,
            })

        result = _diff(self.manifest["files"], self.inventory)
        return json.dumps({
            "target": str(WEIGHTS_DIR),
            "status": "ok" if not result["errors"] else "mismatch",
            "ok": result["ok"],
            "missing": result["missing"],
            "extra": result["extra"],
            "mismatch": result["mismatch"],
        })
