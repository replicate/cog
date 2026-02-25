import argparse
import logging
import os
import sys
from enum import Enum

import coglet


class Mode(Enum):
    PREDICT = "predict"
    TRAIN = "train"

    def __str__(self) -> str:
        return str(self.value)


if __name__ == "__main__":
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

    # Resolve predictor ref from env vars (set by Dockerfile at build time)
    if is_train:
        predictor_ref = os.environ.get("COG_TRAIN_TYPE_STUB")
    else:
        predictor_ref = os.environ.get("COG_PREDICT_TYPE_STUB")

    if not predictor_ref:
        env_var = "COG_TRAIN_TYPE_STUB" if is_train else "COG_PREDICT_TYPE_STUB"
        print(
            f"ERROR: {env_var} environment variable is not set.\n"
            f"This should be set automatically by 'cog build'. If running manually,\n"
            f"set it to your predictor reference (e.g. {env_var}=predict.py:Predictor).",
            file=sys.stderr,
        )
        sys.exit(1)

    coglet.server.serve(  # type: ignore[attr-defined]
        predictor_ref=predictor_ref,
        host=args.host,
        port=port,
        await_explicit_shutdown=args.await_explicit_shutdown,
        is_train=is_train,
        upload_url=args.upload_url,
    )
    sys.exit(0)
