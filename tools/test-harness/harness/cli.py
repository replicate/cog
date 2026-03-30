"""CLI entry point for the cog test harness."""

from __future__ import annotations

import argparse
import json
import logging
import sys
from pathlib import Path

import yaml

from .cog_resolver import resolve_cog_binary, resolve_sdk_version
from .report import (
    console_report,
    schema_compare_console_report,
    schema_compare_json_report,
    write_json_report,
)
from .runner import ModelResult, Runner, SchemaCompareResult


def main(argv: list[str] | None = None) -> None:
    parser = argparse.ArgumentParser(
        prog="cog-test",
        description="Test harness for validating cog models against new SDK versions",
    )
    subparsers = parser.add_subparsers(dest="command")

    # ── run ─────────────────────────────────────────────────────────
    run_parser = subparsers.add_parser("run", help="Build and test models")
    _add_common_args(run_parser)
    run_parser.add_argument(
        "--output",
        choices=["console", "json"],
        default="console",
        help="Output format (default: console)",
    )
    run_parser.add_argument(
        "--output-file",
        type=str,
        default=None,
        help="Write report to file instead of stdout",
    )

    # ── build ───────────────────────────────────────────────────────
    build_parser = subparsers.add_parser(
        "build", help="Build model images only (no predict)"
    )
    _add_common_args(build_parser)

    # ── schema-compare ────────────────────────────────────────────────
    schema_parser = subparsers.add_parser(
        "schema-compare",
        help="Compare static (Go) vs runtime (Python) schema generation",
    )
    _add_common_args(schema_parser)
    schema_parser.add_argument(
        "--output",
        choices=["console", "json"],
        default="console",
        help="Output format (default: console)",
    )
    schema_parser.add_argument(
        "--output-file",
        type=str,
        default=None,
        help="Write report to file",
    )

    # ── list ────────────────────────────────────────────────────────
    list_parser = subparsers.add_parser("list", help="List models in manifest")
    list_parser.add_argument(
        "--manifest",
        type=str,
        default=None,
        help="Path to manifest.yaml",
    )

    args = parser.parse_args(argv)

    if args.command is None:
        parser.print_help()
        sys.exit(1)

    logging.basicConfig(
        format="%(asctime)s %(levelname)-8s %(message)s",
        level=logging.INFO,
        datefmt="%H:%M:%S",
    )

    # Resolve relative paths to absolute before any cwd changes
    _resolve_paths(args)

    if args.command == "list":
        _cmd_list(args)
    elif args.command == "build":
        _cmd_build(args)
    elif args.command == "run":
        _cmd_run(args)
    elif args.command == "schema-compare":
        _cmd_schema_compare(args)


def _cmd_list(args: argparse.Namespace) -> None:
    manifest = _load_manifest(args.manifest)
    models = manifest.get("models", [])

    for m in models:
        gpu_tag = " [GPU]" if m.get("gpu") else ""
        req_env = m.get("requires_env", [])
        env_tag = f" (requires: {', '.join(req_env)})" if req_env else ""
        print(f"  {m['name']:<25} {m['repo']}/{m.get('path', '.')}{gpu_tag}{env_tag}")

    print(f"\n{len(models)} models total")


def _cmd_build(args: argparse.Namespace) -> None:
    manifest = _load_manifest(args.manifest)
    models = _filter_models(manifest, args)
    defaults = manifest.get("defaults", {})

    sdk_version, _ = resolve_sdk_version(
        cli_sdk_version=args.sdk_version,
        manifest_defaults=defaults,
    )
    cog_binary, cog_version_label = resolve_cog_binary(
        cog_version=args.cog_version,
        cog_binary=args.cog_binary,
        manifest_defaults=defaults,
    )
    log = logging.getLogger(__name__)
    log.info("Using cog CLI: %s (%s)", cog_binary, cog_version_label)
    log.info("Using SDK version: %s", sdk_version)

    sdk_wheel = getattr(args, "sdk_wheel", None)
    if sdk_wheel:
        log.info("Using SDK wheel: %s (overrides sdk_version)", sdk_wheel)

    runner = Runner(
        cog_binary=cog_binary,
        sdk_version=sdk_version,
        sdk_wheel=sdk_wheel,
        keep_images=True,
    )

    results: list[ModelResult] = []
    for model in models:
        result = ModelResult(
            name=model["name"], passed=True, gpu=model.get("gpu", False)
        )
        try:
            model_dir = runner.prepare_model(model)
            import time

            start = time.monotonic()
            runner.build_model(model_dir, model)
            result.build_duration_s = time.monotonic() - start
            logging.getLogger(__name__).info(
                "BUILD OK %s (%.1fs)", model["name"], result.build_duration_s
            )
        except Exception as exc:
            result.passed = False
            result.error = str(exc)

        results.append(result)

    console_report(
        results, sdk_version=sdk_version or "", cog_version=cog_version_label
    )

    failed = any(not r.passed for r in results)
    sys.exit(1 if failed else 0)


def _cmd_run(args: argparse.Namespace) -> None:
    manifest = _load_manifest(args.manifest)
    models = _filter_models(manifest, args)
    defaults = manifest.get("defaults", {})

    sdk_version, _ = resolve_sdk_version(
        cli_sdk_version=args.sdk_version,
        manifest_defaults=defaults,
    )
    cog_binary, cog_version_label = resolve_cog_binary(
        cog_version=args.cog_version,
        cog_binary=args.cog_binary,
        manifest_defaults=defaults,
    )
    log = logging.getLogger(__name__)
    sdk_wheel = getattr(args, "sdk_wheel", None)
    log.info("Using cog CLI: %s (%s)", cog_binary, cog_version_label)
    log.info("Using SDK version: %s", sdk_version)
    if sdk_wheel:
        log.info("Using SDK wheel: %s (overrides sdk_version)", sdk_wheel)

    runner = Runner(
        cog_binary=cog_binary,
        sdk_version=sdk_version,
        sdk_wheel=sdk_wheel,
        keep_images=args.keep_images,
    )

    results: list[ModelResult] = []
    try:
        for model in models:
            result = runner.run_model(model)
            results.append(result)
    finally:
        if not args.keep_images:
            runner.cleanup()

    # Output
    if args.output == "json":
        if args.output_file:
            with open(args.output_file, "w") as f:
                write_json_report(
                    results,
                    sdk_version=sdk_version or "",
                    cog_version=cog_version_label,
                    stream=f,
                )
        else:
            write_json_report(
                results,
                sdk_version=sdk_version or "",
                cog_version=cog_version_label,
            )
    else:
        console_report(
            results,
            sdk_version=sdk_version or "",
            cog_version=cog_version_label,
        )
        if args.output_file:
            with open(args.output_file, "w") as f:
                write_json_report(
                    results,
                    sdk_version=sdk_version or "",
                    cog_version=cog_version_label,
                    stream=f,
                )

    failed = any(not r.passed for r in results)
    sys.exit(1 if failed else 0)


def _cmd_schema_compare(args: argparse.Namespace) -> None:
    manifest = _load_manifest(args.manifest)
    models = _filter_models(manifest, args)
    defaults = manifest.get("defaults", {})

    sdk_version, _ = resolve_sdk_version(
        cli_sdk_version=args.sdk_version,
        manifest_defaults=defaults,
    )
    cog_binary, cog_version_label = resolve_cog_binary(
        cog_version=args.cog_version,
        cog_binary=args.cog_binary,
        manifest_defaults=defaults,
    )
    sdk_wheel = getattr(args, "sdk_wheel", None)
    log = logging.getLogger(__name__)
    log.info("Using cog CLI: %s (%s)", cog_binary, cog_version_label)
    log.info("Using SDK version: %s", sdk_version)
    if sdk_wheel:
        log.info("Using SDK wheel: %s (overrides sdk_version)", sdk_wheel)

    runner = Runner(
        cog_binary=cog_binary,
        sdk_version=sdk_version,
        sdk_wheel=sdk_wheel,
        keep_images=args.keep_images,
    )

    results: list[SchemaCompareResult] = []
    try:
        for model in models:
            log.info("Comparing schemas for %s ...", model["name"])
            result = runner.compare_schema(model)
            results.append(result)
    finally:
        if not args.keep_images:
            runner.cleanup()

    # Output
    if args.output == "json":
        report = schema_compare_json_report(results, cog_version=cog_version_label)
        if args.output_file:
            with open(args.output_file, "w") as f:
                json.dump(report, f, indent=2)
                f.write("\n")
        else:
            json.dump(report, sys.stdout, indent=2)
            print()
    else:
        schema_compare_console_report(results, cog_version=cog_version_label)
        if args.output_file:
            report = schema_compare_json_report(results, cog_version=cog_version_label)
            with open(args.output_file, "w") as f:
                json.dump(report, f, indent=2)
                f.write("\n")

    failed = any(not r.passed for r in results)
    sys.exit(1 if failed else 0)


# ── Helpers ────────────────────────────────────────────────────────────


def _add_common_args(parser: argparse.ArgumentParser) -> None:
    parser.add_argument(
        "--manifest",
        type=str,
        default=None,
        help="Path to manifest.yaml (default: auto-detect)",
    )
    parser.add_argument(
        "--model",
        type=str,
        action="append",
        default=None,
        help="Run only specific model(s) by name (repeatable)",
    )
    parser.add_argument(
        "--no-gpu",
        action="store_true",
        help="Skip models that require a GPU",
    )
    parser.add_argument(
        "--gpu-only",
        action="store_true",
        help="Only run models that require a GPU",
    )
    parser.add_argument(
        "--sdk-version",
        type=str,
        default=None,
        help=(
            "SDK version to inject into cog.yaml (e.g. 0.16.12). "
            "Default: latest stable release from PyPI."
        ),
    )
    parser.add_argument(
        "--cog-version",
        type=str,
        default=None,
        help=(
            "Cog CLI version to download and use (e.g. v0.16.12). "
            "Default: latest stable release. Ignored if --cog-binary is set."
        ),
    )
    parser.add_argument(
        "--cog-binary",
        type=str,
        default="cog",
        help="Path to a local cog binary (overrides --cog-version)",
    )
    parser.add_argument(
        "--sdk-wheel",
        type=str,
        default=None,
        help=(
            "Path to a local SDK wheel, a URL, or 'pypi[:version]'. "
            "Sets COG_SDK_WHEEL during builds, overriding --sdk-version. "
            "Use with --cog-binary to test fully from source."
        ),
    )
    parser.add_argument(
        "--keep-images",
        action="store_true",
        help="Don't clean up Docker images after run",
    )


def _resolve_paths(args: argparse.Namespace) -> None:
    """Resolve relative file paths to absolute so they survive cwd changes."""
    if hasattr(args, "cog_binary") and args.cog_binary and args.cog_binary != "cog":
        args.cog_binary = str(Path(args.cog_binary).resolve())
    if hasattr(args, "sdk_wheel") and args.sdk_wheel:
        # Only resolve if it looks like a file path (not a URL or pypi: shorthand)
        wheel = args.sdk_wheel
        if not wheel.startswith(("http://", "https://", "pypi")):
            args.sdk_wheel = str(Path(wheel).resolve())


def _load_manifest(manifest_path: str | None) -> dict:
    if manifest_path:
        path = Path(manifest_path)
    else:
        # Search up from CWD, then fall back to the default location
        path = Path(__file__).parent.parent / "manifest.yaml"

    if not path.exists():
        print(f"Error: manifest not found at {path}", file=sys.stderr)
        sys.exit(1)

    with open(path) as f:
        return yaml.safe_load(f)


def _filter_models(manifest: dict, args: argparse.Namespace) -> list[dict]:
    models = manifest.get("models", [])

    if args.model:
        names = set(args.model)
        models = [m for m in models if m["name"] in names]
        found = {m["name"] for m in models}
        missing = names - found
        if missing:
            print(f"Warning: models not found in manifest: {missing}", file=sys.stderr)

    if getattr(args, "no_gpu", False):
        models = [m for m in models if not m.get("gpu")]

    if getattr(args, "gpu_only", False):
        models = [m for m in models if m.get("gpu")]

    return models


if __name__ == "__main__":
    main()
