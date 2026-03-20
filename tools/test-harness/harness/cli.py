"""CLI entry point for the cog test harness."""

from __future__ import annotations

import argparse
import logging
import sys
from pathlib import Path

import yaml

from .report import console_report, write_json_report
from .runner import ModelResult, Runner


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

    if args.command == "list":
        _cmd_list(args)
    elif args.command == "build":
        _cmd_build(args)
    elif args.command == "run":
        _cmd_run(args)


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
    sdk_version = args.sdk_version or manifest.get("defaults", {}).get("sdk_version")

    runner = Runner(
        cog_binary=args.cog_binary,
        sdk_version=sdk_version,
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

    console_report(results, sdk_version=sdk_version or "")

    failed = any(not r.passed for r in results)
    sys.exit(1 if failed else 0)


def _cmd_run(args: argparse.Namespace) -> None:
    manifest = _load_manifest(args.manifest)
    models = _filter_models(manifest, args)
    sdk_version = args.sdk_version or manifest.get("defaults", {}).get("sdk_version")

    runner = Runner(
        cog_binary=args.cog_binary,
        sdk_version=sdk_version,
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
                write_json_report(results, sdk_version=sdk_version or "", stream=f)
        else:
            write_json_report(results, sdk_version=sdk_version or "")
    else:
        console_report(results, sdk_version=sdk_version or "")
        if args.output_file:
            with open(args.output_file, "w") as f:
                write_json_report(results, sdk_version=sdk_version or "", stream=f)

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
        help="Override sdk_version for all models",
    )
    parser.add_argument(
        "--cog-binary",
        type=str,
        default="cog",
        help="Path to cog binary (default: cog in PATH)",
    )
    parser.add_argument(
        "--keep-images",
        action="store_true",
        help="Don't clean up Docker images after run",
    )


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
