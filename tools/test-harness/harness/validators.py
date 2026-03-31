"""Output validation strategies for cog model predictions."""

from __future__ import annotations

import json
import mimetypes
import re
from dataclasses import dataclass
from pathlib import Path
from typing import Any


@dataclass
class ValidationResult:
    passed: bool
    message: str


def validate(output: str, expect: dict[str, Any]) -> ValidationResult:
    """Dispatch to the appropriate validator based on ``expect["type"]``."""
    vtype = expect.get("type", "not_empty")
    validator = _VALIDATORS.get(vtype)
    if validator is None:
        return ValidationResult(
            passed=False,
            message=f"Unknown validation type: {vtype!r}",
        )
    return validator(output, expect)


# ── Individual validators ──────────────────────────────────────────────


def _validate_exact(output: str, expect: dict[str, Any]) -> ValidationResult:
    expected = str(expect["value"])
    clean = output.strip()
    if clean == expected:
        return ValidationResult(passed=True, message="Exact match")
    return ValidationResult(
        passed=False,
        message=f"Expected exact match:\n  expected: {expected!r}\n  got:      {clean!r}",
    )


def _validate_contains(output: str, expect: dict[str, Any]) -> ValidationResult:
    substring = str(expect["value"])
    if substring in output:
        return ValidationResult(passed=True, message=f"Contains {substring!r}")
    return ValidationResult(
        passed=False,
        message=f"Expected output to contain {substring!r}, got:\n  {output[:200]!r}",
    )


def _validate_regex(output: str, expect: dict[str, Any]) -> ValidationResult:
    pattern = expect["pattern"]
    if re.search(pattern, output):
        return ValidationResult(passed=True, message=f"Matches pattern {pattern!r}")
    return ValidationResult(
        passed=False,
        message=f"Output does not match regex {pattern!r}:\n  {output[:200]!r}",
    )


def _validate_file_exists(output: str, expect: dict[str, Any]) -> ValidationResult:
    """Validate that the output references an existing file.

    ``cog predict`` prints the output file path to stdout.  It may be an
    absolute path or a relative path.  We also handle the common case
    where cog wraps the path in quotes or prints extra whitespace.
    """
    path_str = output.strip().strip("'\"")

    # cog predict may output a URL or a path -- for local testing it's a path
    if path_str.startswith("http://") or path_str.startswith("https://"):
        # Can't verify remote files; treat as pass
        return ValidationResult(passed=True, message=f"Output is a URL: {path_str}")

    path = Path(path_str)
    if not path.exists():
        return ValidationResult(
            passed=False,
            message=f"Output file does not exist: {path}",
        )

    expected_mime = expect.get("mime")
    if expected_mime:
        guessed, _ = mimetypes.guess_type(str(path))
        if guessed != expected_mime:
            return ValidationResult(
                passed=False,
                message=f"Expected MIME {expected_mime}, got {guessed} for {path}",
            )

    return ValidationResult(passed=True, message=f"File exists: {path}")


def _validate_json_match(output: str, expect: dict[str, Any]) -> ValidationResult:
    """Parse output as JSON and verify that ``expect["match"]`` is a subset."""
    try:
        parsed = json.loads(output.strip())
    except json.JSONDecodeError as exc:
        return ValidationResult(
            passed=False,
            message=f"Output is not valid JSON: {exc}\n  {output[:200]!r}",
        )

    match = expect["match"]
    if not _is_subset(match, parsed):
        return ValidationResult(
            passed=False,
            message=f"JSON subset mismatch:\n  expected subset: {match}\n  got: {parsed}",
        )
    return ValidationResult(passed=True, message="JSON subset match")


def _validate_json_keys(output: str, expect: dict[str, Any]) -> ValidationResult:
    """Parse output as JSON dict and verify it has entries (non-empty)."""
    try:
        parsed = json.loads(output.strip())
    except json.JSONDecodeError as exc:
        return ValidationResult(
            passed=False,
            message=f"Output is not valid JSON: {exc}\n  {output[:200]!r}",
        )

    if not isinstance(parsed, dict):
        return ValidationResult(
            passed=False,
            message=f"Expected JSON object, got {type(parsed).__name__}",
        )

    required_keys = expect.get("keys", [])
    if required_keys:
        missing = [k for k in required_keys if k not in parsed]
        if missing:
            return ValidationResult(
                passed=False,
                message=f"Missing keys: {missing}. Got: {list(parsed.keys())}",
            )
    elif not parsed:
        return ValidationResult(
            passed=False,
            message="Expected non-empty JSON object, got empty dict",
        )

    return ValidationResult(
        passed=True,
        message=f"JSON dict with {len(parsed)} keys: {list(parsed.keys())[:5]}",
    )


def _validate_not_empty(output: str, _expect: dict[str, Any]) -> ValidationResult:
    if output.strip():
        return ValidationResult(passed=True, message="Output is non-empty")
    return ValidationResult(passed=False, message="Output is empty")


# ── Helpers ────────────────────────────────────────────────────────────


def _is_subset(subset: Any, superset: Any) -> bool:
    """Check that *subset* is recursively contained in *superset*."""
    if isinstance(subset, dict) and isinstance(superset, dict):
        return all(
            k in superset and _is_subset(v, superset[k]) for k, v in subset.items()
        )
    if isinstance(subset, list) and isinstance(superset, list):
        return all(
            any(_is_subset(s_item, p_item) for p_item in superset) for s_item in subset
        )
    return subset == superset


# ── Registry ───────────────────────────────────────────────────────────

_VALIDATORS = {
    "exact": _validate_exact,
    "contains": _validate_contains,
    "regex": _validate_regex,
    "file_exists": _validate_file_exists,
    "json_match": _validate_json_match,
    "json_keys": _validate_json_keys,
    "not_empty": _validate_not_empty,
}
