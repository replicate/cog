import argparse
import logging
import os
import textwrap
from datetime import datetime, timezone
from typing import Any, Optional, Union

import pydantic
import structlog

# https://github.com/encode/uvicorn/issues/998
import uvicorn  # type: ignore
from anyio import CapacityLimiter
from anyio.lowlevel import RunVar
from fastapi import Body, FastAPI, Header, HTTPException, Path
from fastapi.encoders import jsonable_encoder
from fastapi.responses import JSONResponse
from pydantic import BaseModel, ValidationError

from ..files import upload_file
from ..json import upload_files
from ..logging import setup_logging
from ..predictor import (
    BasePredictor,
    get_input_type,
    get_output_type,
    get_predictor_ref,
    load_config,
    load_predictor_from_ref,
)
from .. import schema
from .probes import ProbeHelper
from .runner import PredictionRunner

log = structlog.get_logger("cog.server.http")


def create_app(predictor_ref: str, threads: int = 1) -> FastAPI:
    app = FastAPI(
        title="Cog",  # TODO: mention model name?
        # version=None # TODO
    )

    app.state.health = {
        "status": "initializing",
        "setup": None,
    }

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

        setup_start = datetime.now(timezone.utc)
        status, logs = runner.setup()
        setup_complete = datetime.now(timezone.utc)

        app.state.health["setup"] = {
            "logs": logs,
            "status": status,
            "started_at": setup_start,
            "completed_at": setup_complete,
        }

        # TODO: this process should die if setup fails!
        probes = ProbeHelper()
        probes.ready()

        app.state.health["status"] = "healthy"

    @app.on_event("shutdown")
    def shutdown() -> None:
        runner.shutdown()

    @app.get("/")
    def root() -> Any:
        return {
            # "cog_version": "", # TODO
            "docs_url": "/docs",
            "openapi_url": "/openapi.json",
        }

    @app.get("/health-check")
    def healthcheck() -> Any:
        return JSONResponse(content=jsonable_encoder(app.state.health))

    @app.post(
        "/predictions",
        response_model=PredictionResponse,
        response_model_exclude_unset=True,
    )
    def predict(request: PredictionRequest = Body(default=None), prefer: Union[str, None] = Header(default=None)) -> Any:  # type: ignore
        """
        Run a single prediction on the model
        """
        # [compat] If no body is supplied, assume that this model can be run
        # with empty input. This will throw a ValidationError if that's not
        # possible.
        if request is None:
            request = PredictionRequest(input={})
        # [compat] If body is supplied but input is None, set it to an empty
        # dictionary so that later code can be simpler.
        if request.input is None:
            request.input = {}

        initial_response, async_result = runner.predict(request)

        # TODO: spec-compliant parsing of Prefer header.
        if prefer == "respond-async":
            return JSONResponse(jsonable_encoder(initial_response), status_code=202)

        try:
            response = PredictionResponse(**async_result.get().dict())
        except ValidationError as e:
            _log_invalid_output(e)
            raise HTTPException(status_code=500)

        response_object = response.dict()
        response_object["output"] = upload_files(
            response_object["output"],
            upload_file=lambda fh: upload_file(fh, request.output_file_prefix),  # type: ignore
        )

        # TODO: clean up output files
        encoded_response = jsonable_encoder(response_object)
        return JSONResponse(content=encoded_response)

    @app.post("/predictions/{prediction_id}/cancel")
    def cancel(prediction_id: str = Path(..., title="Prediction ID")):
        """
        Cancel a running prediction
        """
        if runner.current_prediction_id == prediction_id:
            runner.cancel()
            return JSONResponse({}, status_code=200)
        else:
            return JSONResponse({}, status_code=404)

    return app


def _log_invalid_output(error):
    log.error(
        textwrap.dedent(
            f"""\
            The return value of predict() was not valid:

            {error}

            Check that your predict function is in this form, where `output_type` is the same as the type you are returning (e.g. `str`):

                def predict(...) -> output_type:
                    ...
           """
        )
    )


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

    # log level is configurable so we can make it quiet or verbose for `cog predict`
    # cog predict --debug       # -> debug
    # cog predict               # -> warning
    # docker run <image-name>   # -> info (default)
    log_level = logging.getLevelName(os.environ.get("COG_LOG_LEVEL", "INFO").upper())
    setup_logging(log_level=log_level)

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
        log_config=None,
        # This is the default, but to be explicit: only run a single worker
        workers=1,
    )
