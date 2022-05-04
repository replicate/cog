import logging
import os
import types
from typing import Any, Optional

from fastapi import Body, FastAPI, HTTPException
from fastapi.responses import JSONResponse
from pydantic import BaseModel, ValidationError

# https://github.com/encode/uvicorn/issues/998
import uvicorn  # type: ignore


from ..files import upload_file
from ..json import encode_json
from ..predictor import BasePredictor, get_input_type, get_output_type, load_predictor
from ..response import Status, get_response_type

logger = logging.getLogger("cog")


def create_app(predictor: BasePredictor) -> FastAPI:
    app = FastAPI(
        title="Cog",  # TODO: mention model name?
        # version=None # TODO
    )
    app.on_event("startup")(predictor.setup)

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

        encoded_response = encode_json(
            response, upload_file=lambda fh: upload_file(fh, output_file_prefix)
        )
        # TODO: clean up output files
        return JSONResponse(content=encoded_response)

    return app


if __name__ == "__main__":
    predictor = load_predictor()
    app = create_app(predictor)
    uvicorn.run(
        app,
        host="0.0.0.0",
        port=5000,
        # log level is configurable so we can make it quiet or verbose for `cog predict`
        # cog predict --debug       # -> debug
        # cog predict               # -> warning
        # docker run <image-name>   # -> info (default)
        log_level=os.environ.get("COG_LOG_LEVEL", "info"),
        # Single worker to safely run on GPUs.
        workers=1,
    )
