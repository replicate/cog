"""Core test runner: clone, patch, build, predict, validate."""

from __future__ import annotations

import json
import logging
import os
import re
import shutil
import subprocess
import tempfile
import time
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any

from .patcher import patch_cog_yaml
from .validators import ValidationResult, validate

logger = logging.getLogger(__name__)

# Docker label key where the OpenAPI schema is stored
OPENAPI_SCHEMA_LABEL = "run.cog.openapi_schema"

# ── Data types ─────────────────────────────────────────────────────────


@dataclass
class TestCaseResult:
    description: str
    passed: bool
    message: str
    duration_s: float = 0.0


@dataclass
class ModelResult:
    name: str
    passed: bool
    build_duration_s: float = 0.0
    test_results: list[TestCaseResult] = field(default_factory=list)
    train_results: list[TestCaseResult] = field(default_factory=list)
    error: str | None = None
    skipped: bool = False
    skip_reason: str | None = None
    gpu: bool = False


@dataclass
class SchemaCompareResult:
    """Result of comparing static vs runtime schema generation for one model."""

    name: str
    passed: bool
    static_build_s: float = 0.0
    runtime_build_s: float = 0.0
    error: str | None = None
    diff: str | None = None  # Human-readable diff on mismatch
    static_schema: dict[str, Any] | None = None
    runtime_schema: dict[str, Any] | None = None


# ── Runner ─────────────────────────────────────────────────────────────


class Runner:
    """Orchestrates the clone -> patch -> build -> predict -> validate cycle."""

    def __init__(
        self,
        *,
        cog_binary: str = "cog",
        sdk_version: str | None = None,
        sdk_wheel: str | None = None,
        fixtures_dir: Path | None = None,
        work_dir: Path | None = None,
        keep_images: bool = False,
        default_timeout: int = 300,
    ) -> None:
        self.cog_binary = cog_binary
        self.sdk_version = sdk_version
        self.sdk_wheel = sdk_wheel
        self.fixtures_dir = fixtures_dir or Path(__file__).parent.parent / "fixtures"
        self.work_dir = work_dir or Path(tempfile.mkdtemp(prefix="cog-harness-"))
        self.keep_images = keep_images
        self.default_timeout = default_timeout
        self._cloned_repos: dict[str, Path] = {}

    def prepare_model(self, model: dict[str, Any]) -> Path:
        """Public wrapper around model preparation (clone + patch)."""
        return self._prepare_model(model)

    def build_model(self, model_dir: Path, model: dict[str, Any]) -> None:
        """Public wrapper around ``cog build``."""
        self._cog_build(model_dir, model)

    def run_model(self, model: dict[str, Any]) -> ModelResult:
        """Run all tests for a single model definition from the manifest."""
        name = model["name"]
        gpu = model.get("gpu", False)
        result = ModelResult(name=name, passed=True, gpu=gpu)

        # Check required env vars
        required_env = model.get("requires_env", [])
        missing = [v for v in required_env if not os.environ.get(v)]
        if missing:
            result.passed = True  # not a failure, just skipped
            result.skipped = True
            result.skip_reason = f"Missing env vars: {', '.join(missing)}"
            logger.info("SKIP %s: %s", name, result.skip_reason)
            return result

        try:
            model_dir = self._prepare_model(model)
        except Exception as exc:
            result.passed = False
            result.error = f"Preparation failed: {exc}"
            logger.error("FAIL %s: %s", name, result.error)
            return result

        # Build
        build_start = time.monotonic()
        try:
            self._cog_build(model_dir, model)
            result.build_duration_s = time.monotonic() - build_start
            logger.info("BUILD OK %s (%.1fs)", name, result.build_duration_s)
        except subprocess.CalledProcessError as exc:
            result.passed = False
            result.build_duration_s = time.monotonic() - build_start
            stderr = exc.stderr or ""
            result.error = f"Build failed:\n{stderr[-2000:]}"
            logger.error("BUILD FAIL %s:\n%s", name, stderr[-500:])
            return result

        # Train tests
        for tc in model.get("train_tests", []):
            tc_result = self._run_train_test(model_dir, model, tc)
            result.train_results.append(tc_result)
            if not tc_result.passed:
                result.passed = False

        # Predict tests
        for tc in model.get("tests", []):
            tc_result = self._run_predict_test(model_dir, model, tc)
            result.test_results.append(tc_result)
            if not tc_result.passed:
                result.passed = False

        return result

    def compare_schema(self, model: dict[str, Any]) -> SchemaCompareResult:
        """Build a model twice (static + runtime) and compare the OpenAPI schemas.

        1. Build with COG_STATIC_SCHEMA=1 → extract schema from Docker label
        2. Build without COG_STATIC_SCHEMA → extract schema from Docker label
        3. Parse both as JSON and compare for exact equality
        """
        name = model["name"]
        result = SchemaCompareResult(name=name, passed=True)

        try:
            model_dir = self._prepare_model(model)
        except Exception as exc:
            result.passed = False
            result.error = f"Preparation failed: {exc}"
            logger.error("FAIL %s: %s", name, result.error)
            return result

        image_tag = f"cog-harness-{name}:test"

        # ── Build 1: Static (Go tree-sitter) ──────────────────────────
        logger.info("  Building %s with static schema gen...", name)
        start = time.monotonic()
        try:
            self._cog_build_with_env(
                model_dir, model, image_tag, extra_env={"COG_STATIC_SCHEMA": "1"}
            )
            result.static_build_s = time.monotonic() - start
        except subprocess.CalledProcessError as exc:
            result.passed = False
            result.static_build_s = time.monotonic() - start
            stderr = exc.stderr or ""
            result.error = f"Static build failed:\n{stderr[-2000:]}"
            logger.error("FAIL %s static build:\n%s", name, stderr[-500:])
            return result

        static_schema_raw = self._extract_schema_label(image_tag)
        if static_schema_raw is None:
            result.passed = False
            result.error = "Static build produced no schema label"
            return result

        try:
            result.static_schema = json.loads(static_schema_raw)
        except json.JSONDecodeError as exc:
            result.passed = False
            result.error = f"Static schema is not valid JSON: {exc}"
            return result

        # Remove the image before rebuilding
        self._remove_image(image_tag)

        # ── Build 2: Runtime (Python) ─────────────────────────────────
        logger.info("  Building %s with runtime schema gen...", name)
        start = time.monotonic()
        try:
            self._cog_build_with_env(model_dir, model, image_tag, extra_env={})
            result.runtime_build_s = time.monotonic() - start
        except subprocess.CalledProcessError as exc:
            result.passed = False
            result.runtime_build_s = time.monotonic() - start
            stderr = exc.stderr or ""
            result.error = f"Runtime build failed:\n{stderr[-2000:]}"
            logger.error("FAIL %s runtime build:\n%s", name, stderr[-500:])
            return result

        runtime_schema_raw = self._extract_schema_label(image_tag)
        if runtime_schema_raw is None:
            result.passed = False
            result.error = "Runtime build produced no schema label"
            return result

        try:
            result.runtime_schema = json.loads(runtime_schema_raw)
        except json.JSONDecodeError as exc:
            result.passed = False
            result.error = f"Runtime schema is not valid JSON: {exc}"
            return result

        # Clean up
        self._remove_image(image_tag)

        # ── Compare ───────────────────────────────────────────────────
        if result.static_schema != result.runtime_schema:
            result.passed = False
            result.diff = _json_diff(result.static_schema, result.runtime_schema)
            logger.error("FAIL %s: schemas differ\n%s", name, result.diff)
        else:
            logger.info(
                "PASS %s (static %.1fs, runtime %.1fs)",
                name,
                result.static_build_s,
                result.runtime_build_s,
            )

        return result

    # ── Internal helpers ───────────────────────────────────────────────

    def _prepare_model(self, model: dict[str, Any]) -> Path:
        """Clone the repo (if needed) and patch cog.yaml. Returns model dir."""
        # Local fixture models use "local" as repo and "path" as absolute/relative path
        if model.get("repo") == "local":
            subpath = model.get("path", ".")
            # Resolve relative to the fixtures/models directory
            fixtures_models = self.fixtures_dir / "models"
            model_dir = fixtures_models / subpath
            if not model_dir.is_absolute():
                model_dir = fixtures_models / subpath
            if not (model_dir / "cog.yaml").exists():
                raise FileNotFoundError(f"No cog.yaml in {model_dir}")

            # Copy to work dir so we can patch without modifying the source
            dest = self.work_dir / f"local-{model['name']}"
            if dest.exists():
                shutil.rmtree(dest)
            shutil.copytree(model_dir, dest)

            sdk_version = model.get("sdk_version", self.sdk_version)
            overrides = model.get("cog_yaml_overrides")
            patch_cog_yaml(
                dest / "cog.yaml", sdk_version=sdk_version, overrides=overrides
            )
            return dest

        repo = model["repo"]
        subpath = model.get("path", ".")

        repo_dir = self._clone_repo(repo)
        model_dir = repo_dir / subpath

        if not (model_dir / "cog.yaml").exists():
            raise FileNotFoundError(f"No cog.yaml in {model_dir}")

        sdk_version = model.get("sdk_version", self.sdk_version)
        overrides = model.get("cog_yaml_overrides")

        patch_cog_yaml(
            model_dir / "cog.yaml",
            sdk_version=sdk_version,
            overrides=overrides,
        )

        return model_dir

    def _clone_repo(self, repo: str) -> Path:
        """Shallow-clone a GitHub repo into the work dir, caching by repo name."""
        if repo in self._cloned_repos:
            return self._cloned_repos[repo]

        dest = self.work_dir / repo.replace("/", "--")
        if dest.exists():
            shutil.rmtree(dest)

        url = f"https://github.com/{repo}.git"
        logger.info("Cloning %s ...", url)
        subprocess.run(
            ["git", "clone", "--depth=1", url, str(dest)],
            check=True,
            capture_output=True,
            text=True,
        )
        self._cloned_repos[repo] = dest
        return dest

    def _cog_build(self, model_dir: Path, model: dict[str, Any]) -> None:
        """Run ``cog build`` in the model directory."""
        image_tag = f"cog-harness-{model['name']}:test"
        cmd = [self.cog_binary, "build", "-t", image_tag]

        env = self._build_env(model)
        timeout = model.get("timeout", self.default_timeout)

        subprocess.run(
            cmd,
            cwd=model_dir,
            check=True,
            capture_output=True,
            text=True,
            env=env,
            timeout=timeout,
        )

    def _cog_build_with_env(
        self,
        model_dir: Path,
        model: dict[str, Any],
        image_tag: str,
        extra_env: dict[str, str] | None = None,
    ) -> None:
        """Run ``cog build`` with a specific image tag and optional extra env vars."""
        cmd = [self.cog_binary, "build", "-t", image_tag]
        env = self._build_env(model)
        if extra_env:
            env.update(extra_env)
        timeout = model.get("timeout", self.default_timeout)

        subprocess.run(
            cmd,
            cwd=model_dir,
            check=True,
            capture_output=True,
            text=True,
            env=env,
            timeout=timeout,
        )

    @staticmethod
    def _extract_schema_label(image_tag: str) -> str | None:
        """Extract the OpenAPI schema JSON from a Docker image label."""
        try:
            proc = subprocess.run(
                [
                    "docker",
                    "inspect",
                    image_tag,
                    "--format",
                    '{{index .Config.Labels "' + OPENAPI_SCHEMA_LABEL + '"}}',
                ],
                capture_output=True,
                text=True,
                check=True,
            )
            schema = proc.stdout.strip()
            return schema if schema else None
        except subprocess.CalledProcessError:
            return None

    @staticmethod
    def _remove_image(image_tag: str) -> None:
        """Remove a Docker image, ignoring errors."""
        subprocess.run(
            ["docker", "rmi", "-f", image_tag],
            capture_output=True,
            text=True,
        )

    def _run_predict_test(
        self, model_dir: Path, model: dict[str, Any], tc: dict[str, Any]
    ) -> TestCaseResult:
        description = tc.get("description", "predict")
        start = time.monotonic()

        cmd = [self.cog_binary, "predict"]
        for key, value in tc.get("inputs", {}).items():
            resolved = self._resolve_input(value)
            cmd.extend(["-i", f"{key}={resolved}"])

        env = self._build_env(model)
        timeout = model.get("timeout", self.default_timeout)

        try:
            proc = subprocess.run(
                cmd,
                cwd=model_dir,
                capture_output=True,
                text=True,
                env=env,
                timeout=timeout,
            )
            duration = time.monotonic() - start

            if proc.returncode != 0:
                return TestCaseResult(
                    description=description,
                    passed=False,
                    message=f"cog predict exited {proc.returncode}:\n{proc.stderr[-1000:]}",
                    duration_s=duration,
                )

            output = self._extract_output(proc, model_dir)
            vr: ValidationResult = validate(output, tc.get("expect", {}))
            logger.info(
                "  %s %s: %s (%.1fs)",
                "PASS" if vr.passed else "FAIL",
                description,
                vr.message[:80],
                duration,
            )
            return TestCaseResult(
                description=description,
                passed=vr.passed,
                message=vr.message,
                duration_s=duration,
            )

        except subprocess.TimeoutExpired:
            duration = time.monotonic() - start
            return TestCaseResult(
                description=description,
                passed=False,
                message=f"Timed out after {timeout}s",
                duration_s=duration,
            )
        except Exception as exc:
            duration = time.monotonic() - start
            return TestCaseResult(
                description=description,
                passed=False,
                message=f"Unexpected error: {exc}",
                duration_s=duration,
            )

    def _run_train_test(
        self, model_dir: Path, model: dict[str, Any], tc: dict[str, Any]
    ) -> TestCaseResult:
        description = tc.get("description", "train")
        start = time.monotonic()

        cmd = [self.cog_binary, "train"]
        for key, value in tc.get("inputs", {}).items():
            resolved = self._resolve_input(value)
            cmd.extend(["-i", f"{key}={resolved}"])

        env = self._build_env(model)
        timeout = model.get("timeout", self.default_timeout)

        try:
            proc = subprocess.run(
                cmd,
                cwd=model_dir,
                capture_output=True,
                text=True,
                env=env,
                timeout=timeout,
            )
            duration = time.monotonic() - start

            if proc.returncode != 0:
                return TestCaseResult(
                    description=description,
                    passed=False,
                    message=f"cog train exited {proc.returncode}:\n{proc.stderr[-1000:]}",
                    duration_s=duration,
                )

            output = self._extract_output(proc, model_dir)
            vr: ValidationResult = validate(output, tc.get("expect", {}))
            logger.info(
                "  %s %s: %s (%.1fs)",
                "PASS" if vr.passed else "FAIL",
                description,
                vr.message[:80],
                duration,
            )
            return TestCaseResult(
                description=description,
                passed=vr.passed,
                message=vr.message,
                duration_s=duration,
            )

        except subprocess.TimeoutExpired:
            duration = time.monotonic() - start
            return TestCaseResult(
                description=description,
                passed=False,
                message=f"Timed out after {timeout}s",
                duration_s=duration,
            )
        except Exception as exc:
            duration = time.monotonic() - start
            return TestCaseResult(
                description=description,
                passed=False,
                message=f"Unexpected error: {exc}",
                duration_s=duration,
            )

    @staticmethod
    def _extract_output(proc: subprocess.CompletedProcess[str], model_dir: Path) -> str:
        """Extract the prediction output from cog's stdout/stderr.

        ``cog predict`` prints text/JSON output to **stdout**.  For file
        outputs (e.g. images) it writes the file to the CWD and prints
        ``Written output to: <filename>`` on **stderr**.  We detect the
        latter pattern and return the absolute path to the file so that
        the ``file_exists`` validator can verify it.
        """

        # If there's meaningful stdout, prefer that
        stdout = proc.stdout.strip()
        if stdout:
            return proc.stdout

        # Check stderr for "Written output to: <path>"
        m = re.search(r"Written output to:\s*(.+)", proc.stderr)
        if m:
            rel_path = m.group(1).strip()
            abs_path = model_dir / rel_path
            return str(abs_path)

        # Fallback: return whatever stdout had (possibly empty)
        return proc.stdout

    def _resolve_input(self, value: Any) -> str:
        """Resolve input values — ``@filename`` becomes an absolute fixture path.

        The path is resolved to an absolute, canonical path (no symlinks or
        ``..`` components) so that ``cog predict -i image=@/abs/path`` works
        correctly when cog mounts the file into the container.
        """
        s = str(value)
        if s.startswith("@"):
            fixture_path = (self.fixtures_dir / s[1:]).resolve()
            if not fixture_path.exists():
                raise FileNotFoundError(
                    f"Fixture not found: {fixture_path} (referenced as {s!r})"
                )
            return f"@{fixture_path}"
        return s

    def _build_env(self, model: dict[str, Any]) -> dict[str, str]:
        """Build environment dict, expanding ${VAR} references from host env."""
        env = os.environ.copy()
        if self.sdk_wheel:
            env["COG_SDK_WHEEL"] = self.sdk_wheel
        for key, value in model.get("env", {}).items():
            resolved = os.path.expandvars(value)
            env[key] = resolved
        return env

    def cleanup(self) -> None:
        """Remove work directory and optionally docker images."""
        if not self.keep_images:
            # Clean up docker images we created
            try:
                proc = subprocess.run(
                    [
                        "docker",
                        "images",
                        "--filter",
                        "reference=cog-harness-*",
                        "--format",
                        "{{.Repository}}:{{.Tag}}",
                    ],
                    capture_output=True,
                    text=True,
                )
                images = [
                    line.strip() for line in proc.stdout.splitlines() if line.strip()
                ]
                if images:
                    subprocess.run(
                        ["docker", "rmi", "--force"] + images,
                        capture_output=True,
                        text=True,
                    )
            except Exception as exc:
                logger.warning("Failed to clean up Docker images in cleanup(): %s", exc)

        if self.work_dir.exists():
            shutil.rmtree(self.work_dir, ignore_errors=True)


# ── Module-level helpers ──────────────────────────────────────────────


def _json_diff(a: Any, b: Any, path: str = "") -> str:
    """Produce a human-readable diff between two JSON-like structures.

    Returns a string describing all differences found. No normalization
    is applied — the comparison is strict structural equality.
    """
    lines: list[str] = []
    _diff_recursive(a, b, path or "$", lines)
    return "\n".join(lines) if lines else "(no differences)"


def _diff_recursive(a: Any, b: Any, path: str, lines: list[str]) -> None:
    if type(a) is not type(b):
        lines.append(
            f"  {path}: type mismatch: {type(a).__name__} vs {type(b).__name__}"
        )
        lines.append(f"    static:  {json.dumps(a, sort_keys=True)[:200]}")
        lines.append(f"    runtime: {json.dumps(b, sort_keys=True)[:200]}")
        return

    if isinstance(a, dict):
        all_keys = sorted(set(list(a.keys()) + list(b.keys())))
        for key in all_keys:
            child_path = f"{path}.{key}"
            if key not in a:
                lines.append(f"  {child_path}: missing in static schema")
                lines.append(f"    runtime: {json.dumps(b[key], sort_keys=True)[:200]}")
            elif key not in b:
                lines.append(f"  {child_path}: missing in runtime schema")
                lines.append(f"    static: {json.dumps(a[key], sort_keys=True)[:200]}")
            else:
                _diff_recursive(a[key], b[key], child_path, lines)
    elif isinstance(a, list):
        if len(a) != len(b):
            lines.append(f"  {path}: array length mismatch: {len(a)} vs {len(b)}")
            lines.append(f"    static:  {json.dumps(a, sort_keys=True)[:200]}")
            lines.append(f"    runtime: {json.dumps(b, sort_keys=True)[:200]}")
            return
        for i, (ai, bi) in enumerate(zip(a, b, strict=True)):
            _diff_recursive(ai, bi, f"{path}[{i}]", lines)
    elif a != b:
        lines.append(f"  {path}: value mismatch")
        lines.append(f"    static:  {json.dumps(a, sort_keys=True)[:200]}")
        lines.append(f"    runtime: {json.dumps(b, sort_keys=True)[:200]}")
