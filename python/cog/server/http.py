import argparse
import logging
import os
import sys

import coglet
import structlog

from ..config import Config
from ..logging import setup_logging
from ..mode import Mode

log = structlog.get_logger("cog.server.http")

_COG_LOG_LEVELS = {
    "debug": logging.DEBUG,
    "info": logging.INFO,
    "warning": logging.WARNING,
    "warn": logging.WARNING,
    "error": logging.ERROR,
}

if __name__ == "__main__":
    log_level = _COG_LOG_LEVELS.get(
        os.environ.get("COG_LOG_LEVEL", "").lower(), logging.INFO
    )
    setup_logging(log_level=log_level)

    # Parse minimal args needed for Rust server
    parser = argparse.ArgumentParser(description="Cog HTTP server")
    parser.add_argument(
        "-v", "--version", action="store_true", help="Show version and exit"
    )
    parser.add_argument(
        "--host",
        dest="host",
        type=str,
        default="0.0.0.0",
        help="Host to bind to",
    )
    parser.add_argument(
        "--await-explicit-shutdown",
        dest="await_explicit_shutdown",
        type=bool,
        default=False,
        help="Ignore SIGTERM and wait for a request to /shutdown (or a SIGINT) before exiting",
    )
    parser.add_argument(
        "--x-mode",
        dest="mode",
        type=Mode,
        default=Mode.PREDICT,
        choices=list(Mode),
        help="Experimental: Run in 'predict' or 'train' mode",
    )
    # Accept but ignore other args for compatibility
    parser.add_argument("--threads", dest="threads", type=int, default=None)
    parser.add_argument("--upload-url", dest="upload_url", type=str, default=None)
    args = parser.parse_args()

    if args.version:
        print(f"coglet (Rust) {coglet.__version__}")  # type: ignore[attr-defined]
        sys.exit(0)

    port = int(os.getenv("PORT", "5000"))
    is_train = args.mode == Mode.TRAIN

    # Get runner/predictor ref from config
    config = Config()
    try:
        predictor_ref = config.get_predictor_ref(args.mode)
    except ValueError as e:
        log.error(f"Configuration error: {e}")
        if args.mode == Mode.PREDICT:
            log.error(
                "Please add 'run' to your cog.yaml file. "
                'Example: run: "run.py:Runner"'
            )
        else:
            log.error(
                f"Please add '{args.mode}' to your cog.yaml file. "
                f'Example: {args.mode}: "train.py:Train"'
            )
        sys.exit(1)

    log.debug("Starting Rust coglet server")
    coglet.server.serve(  # type: ignore[attr-defined]
        predictor_ref=predictor_ref,
        host=args.host,
        port=port,
        await_explicit_shutdown=args.await_explicit_shutdown,
        is_train=is_train,
        upload_url=args.upload_url,
    )
    sys.exit(0)
