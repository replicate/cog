"""Resolve and download specific cog CLI and SDK versions."""

from __future__ import annotations

import glob as _glob
import json
import logging
import os
import platform
import re
import shutil
import stat
import subprocess
import tempfile
import urllib.request
from dataclasses import dataclass
from pathlib import Path

logger = logging.getLogger(__name__)

GITHUB_API = "https://api.github.com/repos/replicate/cog/releases"
GITHUB_REPO = "https://github.com/replicate/cog.git"
DOWNLOAD_BASE = (
    "https://github.com/replicate/cog/releases/download/{tag}/cog_{os}_{arch}"
)
PYPI_API = "https://pypi.org/pypi/cog/json"

# Pre-release patterns to skip when resolving "latest"
_PRERELEASE_RE = re.compile(r"-(alpha|beta|rc|dev)", re.IGNORECASE)


@dataclass
class RefBuildResult:
    """Result of building cog from a git ref."""

    cog_binary: str
    sdk_wheel: str
    label: str


def resolve_latest_stable_version() -> str:
    """Query GitHub releases and return the tag of the latest stable release.

    Skips any release marked as a prerelease or whose tag contains
    alpha/beta/rc/dev suffixes.
    """
    url = f"{GITHUB_API}?per_page=50"
    headers = {"Accept": "application/vnd.github+json"}

    # Use a token if available to avoid rate limits
    token = os.environ.get("GITHUB_TOKEN") or os.environ.get("GH_TOKEN")
    if token:
        headers["Authorization"] = f"Bearer {token}"

    req = urllib.request.Request(url, headers=headers)
    with urllib.request.urlopen(req, timeout=30) as resp:
        releases = json.loads(resp.read().decode())

    for release in releases:
        tag = release.get("tag_name", "")
        if release.get("prerelease") or release.get("draft"):
            continue
        if _PRERELEASE_RE.search(tag):
            continue
        return tag

    raise RuntimeError(
        "Could not find a stable cog release. "
        "Check https://github.com/replicate/cog/releases"
    )


def _platform_asset_name() -> str:
    """Return the cog binary asset name for the current platform."""
    system = platform.system()  # Darwin, Linux
    machine = platform.machine()  # arm64, x86_64, aarch64

    if system not in ("Darwin", "Linux"):
        raise RuntimeError(f"Unsupported OS: {system}")

    # Normalise architecture names
    arch_map = {
        "arm64": "arm64",
        "aarch64": "arm64",
        "x86_64": "x86_64",
        "amd64": "x86_64",
    }
    arch = arch_map.get(machine)
    if not arch:
        raise RuntimeError(f"Unsupported architecture: {machine}")

    return f"cog_{system}_{arch}"


def download_cog_binary(tag: str, dest_dir: Path | None = None) -> Path:
    """Download the cog binary for *tag* and return the path to it.

    The binary is placed in *dest_dir* (default: a new temp directory) and
    made executable.
    """
    asset = _platform_asset_name()
    url = DOWNLOAD_BASE.format(tag=tag, os=platform.system(), arch=asset.split("_")[-1])

    if dest_dir is None:
        dest_dir = Path(tempfile.mkdtemp(prefix="cog-bin-"))
    dest_dir.mkdir(parents=True, exist_ok=True)

    dest = dest_dir / "cog"

    logger.info("Downloading cog %s from %s ...", tag, url)

    req = urllib.request.Request(url)
    token = os.environ.get("GITHUB_TOKEN") or os.environ.get("GH_TOKEN")
    if token:
        req.add_header("Authorization", f"Bearer {token}")

    with urllib.request.urlopen(req, timeout=120) as resp, open(dest, "wb") as f:
        while True:
            chunk = resp.read(1 << 16)
            if not chunk:
                break
            f.write(chunk)

    # Make executable
    dest.chmod(dest.stat().st_mode | stat.S_IXUSR | stat.S_IXGRP | stat.S_IXOTH)

    # Verify it works
    logger.info("Downloaded cog %s -> %s", tag, dest)
    return dest


def build_cog_from_ref(ref: str, dest_dir: Path | None = None) -> RefBuildResult:
    """Clone the cog repo at *ref*, build the CLI binary and SDK wheel.

    Returns a :class:`RefBuildResult` with paths to both artifacts.
    Requires ``go`` and ``uv`` on PATH.
    """
    if dest_dir is None:
        dest_dir = Path(tempfile.mkdtemp(prefix="cog-ref-"))
    dest_dir.mkdir(parents=True, exist_ok=True)

    # Verify prerequisites
    for tool in ("go", "uv"):
        if not shutil.which(tool):
            raise RuntimeError(
                f"'{tool}' not found on PATH. "
                f"Building from a git ref requires go and uv. "
                f"Run 'mise install' to set up the development environment."
            )

    clone_dir = dest_dir / "cog-src"
    logger.info("Cloning cog repo at ref '%s' ...", ref)
    subprocess.run(
        [
            "git",
            "clone",
            "--single-branch",
            "--depth=1",
            "--branch",
            ref,
            GITHUB_REPO,
            str(clone_dir),
        ],
        check=False,
        capture_output=True,
        text=True,
    )
    # If --branch clone failed (e.g. commit SHA), do a full clone + checkout
    if not clone_dir.exists():
        logger.info("Shallow clone failed for ref '%s', trying full clone ...", ref)
        subprocess.run(
            ["git", "clone", GITHUB_REPO, str(clone_dir)],
            check=True,
            capture_output=True,
            text=True,
        )
        subprocess.run(
            ["git", "checkout", ref],
            cwd=clone_dir,
            check=True,
            capture_output=True,
            text=True,
        )

    # Build the CLI binary
    cog_binary = dest_dir / "cog"
    logger.info("Building cog CLI from ref '%s' ...", ref)
    subprocess.run(
        ["go", "build", "-o", str(cog_binary), "./cmd/cog"],
        cwd=clone_dir,
        check=True,
        capture_output=True,
        text=True,
        timeout=300,
    )
    cog_binary.chmod(
        cog_binary.stat().st_mode | stat.S_IXUSR | stat.S_IXGRP | stat.S_IXOTH
    )
    logger.info("Built cog CLI -> %s", cog_binary)

    # Build the SDK wheel
    wheel_dir = dest_dir / "dist"
    wheel_dir.mkdir(exist_ok=True)

    # Read VERSION.txt and convert to PEP 440
    version_file = clone_dir / "VERSION.txt"
    if version_file.exists():
        raw_version = version_file.read_text().strip()
        pep440_version = re.sub(
            r"-alpha",
            "a",
            re.sub(
                r"-beta",
                "b",
                re.sub(r"-rc", "rc", re.sub(r"-dev", ".dev", raw_version)),
            ),
        )
    else:
        pep440_version = "0.0.0.dev0"

    logger.info(
        "Building SDK wheel from ref '%s' (version %s) ...", ref, pep440_version
    )
    env = os.environ.copy()
    env["SETUPTOOLS_SCM_PRETEND_VERSION"] = pep440_version
    subprocess.run(
        ["uv", "build", "--out-dir", str(wheel_dir), "."],
        cwd=clone_dir,
        check=True,
        capture_output=True,
        text=True,
        env=env,
        timeout=120,
    )

    # Find the built wheel
    wheels = _glob.glob(str(wheel_dir / "cog-*.whl"))
    if not wheels:
        raise RuntimeError(f"SDK wheel build produced no .whl files in {wheel_dir}")
    sdk_wheel = wheels[0]
    logger.info("Built SDK wheel -> %s", sdk_wheel)

    return RefBuildResult(
        cog_binary=str(cog_binary),
        sdk_wheel=sdk_wheel,
        label=f"ref:{ref}",
    )


def resolve_cog_binary(
    cog_version: str | None,
    cog_binary: str | None,
    cog_ref: str | None = None,
    manifest_defaults: dict | None = None,
) -> tuple[str, str, str | None]:
    """Resolve which cog binary to use.

    Returns ``(binary_path, version_label, sdk_wheel_or_none)``.

    The third element is non-None only when ``--cog-ref`` is used, in which
    case it points to the SDK wheel built from the same ref.

    Priority:
    1. ``--cog-binary`` (explicit path) — use as-is, version label = "custom"
    2. ``--cog-ref`` — clone repo, build CLI + SDK from that ref
    3. ``--cog-version`` — download that specific tag
    4. ``defaults.cog_version`` from manifest — download that tag
    5. No version specified — resolve latest stable, download it

    If *cog_binary* is provided and is not the default ``"cog"``, it takes
    top priority (the user wants their own binary).
    """
    defaults = manifest_defaults or {}

    # 1. Explicit --cog-binary (non-default)
    if cog_binary and cog_binary != "cog":
        return cog_binary, "custom", None

    # 2. Explicit --cog-ref (build from source)
    if cog_ref:
        ref_result = build_cog_from_ref(cog_ref)
        return ref_result.cog_binary, ref_result.label, ref_result.sdk_wheel

    # 3. Explicit --cog-version
    if cog_version:
        tag = cog_version if cog_version.startswith("v") else f"v{cog_version}"
        path = download_cog_binary(tag)
        return str(path), tag, None

    # 4. Manifest default
    manifest_version = defaults.get("cog_version")
    if manifest_version and manifest_version != "latest":
        tag = (
            manifest_version
            if manifest_version.startswith("v")
            else f"v{manifest_version}"
        )
        path = download_cog_binary(tag)
        return str(path), tag, None

    # 5. Resolve latest stable
    tag = resolve_latest_stable_version()
    logger.info("Resolved latest stable cog version: %s", tag)
    path = download_cog_binary(tag)
    return str(path), tag, None


# ── SDK version resolution ─────────────────────────────────────────────


def resolve_latest_sdk_version() -> str:
    """Query PyPI and return the latest stable version of the ``cog`` package.

    PyPI's ``info.version`` field always returns the latest non-prerelease
    version, so no extra filtering is needed.
    """
    req = urllib.request.Request(PYPI_API, headers={"Accept": "application/json"})
    with urllib.request.urlopen(req, timeout=30) as resp:
        data = json.loads(resp.read().decode())
    version = data["info"]["version"]
    logger.info("Resolved latest stable SDK version from PyPI: %s", version)
    return version


def resolve_sdk_version(
    cli_sdk_version: str | None,
    manifest_defaults: dict | None = None,
) -> tuple[str, bool]:
    """Resolve which SDK version to use. Returns ``(version, was_resolved)``.

    Priority:
    1. ``--sdk-version`` CLI flag — use as-is
    2. ``defaults.sdk_version`` from manifest (if not ``"latest"``)
    3. Resolve latest stable from PyPI

    *was_resolved* is ``True`` when the version was auto-resolved from PyPI.
    """
    defaults = manifest_defaults or {}

    # 1. Explicit --sdk-version
    if cli_sdk_version:
        return cli_sdk_version, False

    # 2. Manifest default
    manifest_version = defaults.get("sdk_version")
    if manifest_version and manifest_version != "latest":
        return manifest_version, False

    # 3. Resolve latest stable from PyPI
    version = resolve_latest_sdk_version()
    return version, True
