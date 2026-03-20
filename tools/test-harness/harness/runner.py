"""Core test runner: clone, patch, build, predict, validate."""

from __future__ import annotations

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


# ── Runner ─────────────────────────────────────────────────────────────


class Runner:
    """Orchestrates the clone -> patch -> build -> predict -> validate cycle."""

    def __init__(
        self,
        *,
        cog_binary: str = "cog",
        sdk_version: str | None = None,
        fixtures_dir: Path | None = None,
        work_dir: Path | None = None,
        keep_images: bool = False,
        default_timeout: int = 300,
    ) -> None:
        self.cog_binary = cog_binary
        self.sdk_version = sdk_version
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

    # ── Internal helpers ───────────────────────────────────────────────

    def _prepare_model(self, model: dict[str, Any]) -> Path:
        """Clone the repo (if needed) and patch cog.yaml. Returns model dir."""
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
