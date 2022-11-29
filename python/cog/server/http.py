import argparse
import logging
import os
from typing import Any, Optional

import pydantic

# https://github.com/encode/uvicorn/issues/998
import uvicorn  # type: ignore
from anyio import CapacityLimiter
from anyio.lowlevel import RunVar
from fastapi import Body, FastAPI, HTTPException
from fastapi.encoders import jsonable_encoder
from fastapi.responses import JSONResponse
from pydantic import BaseModel, ValidationError

from ..files import upload_file
from ..json import upload_files
from ..predictor import (
    BasePredictor,
    get_input_type,
    get_output_type,
    get_predictor_ref,
    load_config,
    load_predictor_from_ref,
)
from .. import schema
from .runner import PredictionRunner

logger = logging.getLogger("cog")


def create_app(predictor_ref: str, threads: int = 1) -> FastAPI:
    app = FastAPI(
        title="Cog",  # TODO: mention model name?
        # version=None # TODO
    )
    runner = PredictionRunner(predictor_ref)
    # TODO: avoid loading predictor code in this process
    predictor = load_predictor_from_ref(predictor_ref)

    InputType = get_input_type(predictor)
    OutputType = get_output_type(predictor)

    PredictionRequest = schema.PredictionRequest.with_types(input_type=InputType)
    PredictionResponse = schema.PredictionResponse.with_types(
        input_type=InputType, output_type=OutputType
    )

    @app.on_event("startup")
    def startup() -> None:
        # https://github.com/tiangolo/fastapi/issues/4221
        RunVar("_default_thread_limiter").set(CapacityLimiter(threads))  # type: ignore
        runner.setup()

    @app.get("/")
    def root() -> Any:
        return {
            # "cog_version": "", # TODO
            "docs_url": "/docs",
            "openapi_url": "/openapi.json",
        }

    @app.post(
        "/predictions",
        response_model=PredictionResponse,
        response_model_exclude_unset=True,
    )
    def predict(request: PredictionRequest = Body(default=None)) -> Any:  # type: ignore
        """
        Run a single prediction on the model
        """
        result = runner.predict(request)

        try:
            response = result.get()
        except ValidationError as e:
            logger.error(
                f"""The return value of predict() was not valid:

    {e}

    Check that your predict function is in this form, where `output_type` is the same as the type you are returning (e.g. `str`):

        def predict(...) -> output_type:
            ...
    """
            )
            raise HTTPException(status_code=500)
        finally:
            if request is not None and request.input is not None:
                request.input.cleanup()

        encoded_response = jsonable_encoder(response)

        if hasattr(request, "output_file_prefix"):
            encoded_response = upload_files(
                encoded_response,
                upload_file=lambda fh: upload_file(fh, request.output_file_prefix),  # type: ignore
            )

        # TODO: clean up output files
        return JSONResponse(content=encoded_response)

    return app


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="Cog HTTP server")
    parser.add_argument(
        "--threads",
        dest="threads",
        type=int,
        default=None,
        help="Number of worker processes. Defaults to number of CPUs, or 1 if using a GPU.",
    )
    args = parser.parse_args()

    config = load_config()

    threads = args.threads
    if threads is None:
        if config.get("build", {}).get("gpu", False):
            threads = 1
        else:
            threads = os.cpu_count()

    predictor_ref = get_predictor_ref(config)
    app = create_app(predictor_ref, threads=threads)
    port = int(os.getenv("PORT", 5000))
    uvicorn.run(
        app,
        host="0.0.0.0",
        port=port,
        # log level is configurable so we can make it quiet or verbose for `cog predict`
        # cog predict --debug       # -> debug
        # cog predict               # -> warning
        # docker run <image-name>   # -> info (default)
        log_level=os.environ.get("COG_LOG_LEVEL", "info"),
        # This is the default, but to be explicit: only run a single worker
        workers=1,
    )
