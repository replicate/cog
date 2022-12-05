import argparse
import logging
import os
import types
from typing import Any, Optional

# https://github.com/encode/uvicorn/issues/998
import uvicorn  # type: ignore
from anyio import CapacityLimiter
from anyio.lowlevel import RunVar
from fastapi import Body, FastAPI, HTTPException
from fastapi.responses import JSONResponse
from pydantic import BaseModel, ValidationError

from ..files import upload_file
from ..json import make_encodeable, upload_files
from ..predictor import (
    BasePredictor,
    get_input_type,
    get_output_type,
    load_config,
    load_predictor,
)
from ..response import Status, get_response_type

logger = logging.getLogger("cog")


def create_app(predictor: BasePredictor, threads: int = 1) -> FastAPI:
    app = FastAPI(
        title="Cog",  # TODO: mention model name?
        # version=None # TODO
    )

    @app.on_event("startup")
    def startup() -> None:
        # https://github.com/tiangolo/fastapi/issues/4221
        RunVar("_default_thread_limiter").set(CapacityLimiter(threads))  # type: ignore

        predictor.setup()

    @app.get("/")
    def root() -> Any:
        return {
            # "cog_version": "", # TODO
            "docs_url": "/docs",
            "openapi_url": "/openapi.json",
        }

    InputType = get_input_type(predictor)

    class Request(BaseModel):
        """The request body for a prediction"""

        input: Optional[InputType] = None  # type: ignore
        output_file_prefix: Optional[str] = None

    # response_model is purely for generating schema.
    # We generate Response again in the request so we can set file output paths correctly, etc.
    OutputType = get_output_type(predictor)
    Response = get_response_type(OutputType)

    @app.post(
        "/predictions",
        response_model=get_response_type(OutputType),
        response_model_exclude_unset=True,
    )

    # The signature of this function is used by FastAPI to generate the schema.
    # The function body is not used to generate the schema.
    def predict(request: Request = Body(default=None)) -> Any:
        """
        Run a single prediction on the model
        """
        try:
            if request is not None and request.input is not None:
                output = predictor.predict(**request.input.dict())
            else:
                output = predictor.predict()

            response = Response(status=Status.SUCCEEDED, output=output)

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

        output_file_prefix = None
        if request:
            output_file_prefix = request.output_file_prefix

        encoded_response = make_encodeable(response)
        encoded_response = upload_files(
            encoded_response, upload_file=lambda fh: upload_file(fh, output_file_prefix)
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

    predictor = load_predictor(config)
    app = create_app(predictor, threads=threads)
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
