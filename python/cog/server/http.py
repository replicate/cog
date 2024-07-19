import argparse
import asyncio
import functools
import logging
import os
import signal
import socket
import sys
import textwrap
import threading
import traceback
import warnings
from datetime import datetime, timezone
from enum import Enum, auto, unique
from typing import (
    TYPE_CHECKING,
    Any,
    Awaitable,
    Callable,
    Dict,
    List,
    Optional,
    TypeVar,
    Union,
)

if TYPE_CHECKING:
    from typing import ParamSpec


import attrs
import structlog
import uvicorn
from fastapi import Body, FastAPI, Header, Path, Response
from fastapi.encoders import jsonable_encoder
from fastapi.exceptions import HTTPException
from fastapi.responses import JSONResponse

from .. import schema
from ..logging import setup_logging
from ..predictor import (
    get_input_type,
    get_output_type,
    get_predictor_ref,
    get_training_input_type,
    get_training_output_type,
    load_config,
    load_predictor_from_ref,
)
from ..types import PYDANTIC_V2
from .runner import (
    PredictionRunner,
    RunnerBusyError,
    SetupResult,
    SetupTask,
    UnknownPredictionError,
)
from .telemetry import make_trace_context, trace_context

log = structlog.get_logger("cog.server.http")


@unique
class Health(Enum):
    UNKNOWN = auto()
    STARTING = auto()
    READY = auto()
    BUSY = auto()
    SETUP_FAILED = auto()
    SHUTTING_DOWN = auto()


class MyState:
    health: Health
    setup_task: Optional[SetupTask]
    setup_result: Optional[SetupResult]


class MyFastAPI(FastAPI):
    # TODO: not, strictly speaking, legal
    # https://github.com/microsoft/pyright/issues/5933
    # but it'd need a FastAPI patch to fix
    state: MyState  # type: ignore


def add_setup_failed_routes(app: MyFastAPI, started_at: datetime, msg: str) -> None:
    print(msg)
    result = SetupResult(
        started_at=started_at,
        completed_at=datetime.now(tz=timezone.utc),
        logs=msg,
        status=schema.Status.FAILED,
    )
    app.state.setup_result = result
    app.state.health = Health.SETUP_FAILED

    @app.get("/health-check")
    async def healthcheck_startup_failed() -> Any:
        setup = attrs.asdict(app.state.setup_result)
        return jsonable_encoder({"status": app.state.health.name, "setup": setup})


def create_app(
    config: Dict[str, Any],
    shutdown_event: Optional[threading.Event],
    threads: int = 1,
    upload_url: Optional[str] = None,
    mode: str = "predict",
) -> MyFastAPI:
    app = MyFastAPI(
        title="Cog",  # TODO: mention model name?
        # version=None # TODO
    )

    # Pydantic 2 changes how optional fields are represented in OpenAPI schema.
    # See: https://github.com/tiangolo/fastapi/pull/9873#issuecomment-1997105091
    if PYDANTIC_V2:
        from fastapi.openapi.utils import get_openapi

        def remove_nullable_anyof(
            openapi_schema: Union[Dict[str, Any], List[Dict[str, Any]]],
        ) -> None:
            if isinstance(openapi_schema, dict):
                for key, value in list(openapi_schema.items()):
                    if key == "anyOf" and isinstance(value, list):
                        non_null_types = [
                            item for item in value if item.get("type") != "null"
                        ]
                        if len(value) > len(non_null_types) == 1:
                            openapi_schema.update(non_null_types[0])
                            del openapi_schema[key]

                            # FIXME: Update tests to expect nullable
                            # openapi_schema["nullable"] = True

                    else:
                        remove_nullable_anyof(value)
            elif isinstance(openapi_schema, list):  # pyright: ignore
                for item in openapi_schema:
                    remove_nullable_anyof(item)

        def flatten_selected_allof_refs(
            openapi_schema: Dict[str, Any],
        ) -> None:
            try:
                response = openapi_schema["components"]["schemas"]["PredictionResponse"]
                response["properties"]["output"] = {
                    "$ref": "#/components/schemas/Output"
                }
            except KeyError:
                pass

            for _key, value in (
                openapi_schema.get("components", {}).get("schemas", {}).items()
            ):
                if (
                    value.get("allOf")
                    and len(value.get("allOf")) == 1
                    and value["allOf"][0].get("$ref")
                ):
                    value["$ref"] = value["allOf"][0]["$ref"]
                    del value["allOf"]

            try:
                path = openapi_schema["paths"]["/predictions"]["post"]
                body = path["requestBody"]
                body["content"]["application/json"]["schema"] = {
                    "$ref": "#/components/schemas/PredictionRequest"
                }
            except KeyError:
                pass

        def set_default_enumeration_description(
            openapi_schema: Union[Dict[str, Any], List[Dict[str, Any]]],
        ) -> None:
            if isinstance(openapi_schema, dict):
                for _key, value in list(openapi_schema.items()):
                    if isinstance(value, dict) and value.get("enum"):
                        value["description"] = value.get(
                            "description", "An enumeration."
                        )
                    else:
                        set_default_enumeration_description(value)
            elif isinstance(openapi_schema, list):  # pyright: ignore
                for item in openapi_schema:
                    set_default_enumeration_description(item)

        def custom_openapi() -> Dict[str, Any]:
            if not app.openapi_schema:
                openapi_schema = get_openapi(
                    title="Cog",
                    openapi_version="3.0.2",
                    version="0.1.0",
                    routes=app.routes,
                )

                remove_nullable_anyof(openapi_schema)
                flatten_selected_allof_refs(openapi_schema)
                set_default_enumeration_description(openapi_schema)

                app.openapi_schema = openapi_schema

            return app.openapi_schema

        app.openapi = custom_openapi

    app.state.health = Health.STARTING
    app.state.setup_task = None
    app.state.setup_result = None
    started_at = datetime.now(tz=timezone.utc)

    # shutdown is needed no matter what happens
    @app.post("/shutdown")
    async def start_shutdown() -> Any:
        log.info("shutdown requested via http")
        if shutdown_event is not None:
            shutdown_event.set()
        return JSONResponse({}, status_code=200)

    try:
        predictor_ref = get_predictor_ref(config, mode)
        # TODO: avoid loading predictor code in this process
        predictor = load_predictor_from_ref(predictor_ref)
        InputType = get_input_type(predictor)
        OutputType = get_output_type(predictor)
    except Exception:
        msg = "Error while loading predictor:\n\n" + traceback.format_exc()
        add_setup_failed_routes(app, started_at, msg)
        return app

    concurrency = config.get("concurrency", {}).get("max", "1")

    runner = PredictionRunner(
        predictor_ref=predictor_ref,
        shutdown_event=shutdown_event,
        upload_url=upload_url,
        concurrency=int(concurrency),
    )

    class PredictionRequest(schema.PredictionRequest.with_types(input_type=InputType)):
        pass

    PredictionResponse = schema.PredictionResponse.with_types(
        input_type=InputType, output_type=OutputType
    )

    http_semaphore = asyncio.Semaphore(threads)

    if TYPE_CHECKING:
        P = ParamSpec("P")
        T = TypeVar("T")

    def limited(f: "Callable[P, Awaitable[T]]") -> "Callable[P, Awaitable[T]]":
        @functools.wraps(f)
        async def wrapped(*args: "P.args", **kwargs: "P.kwargs") -> "T":
            async with http_semaphore:
                return await f(*args, **kwargs)

        return wrapped

    if "train" in config:
        try:
            # TODO: avoid loading trainer code in this process
            trainer = load_predictor_from_ref(config["train"])
            TrainingInputType = get_training_input_type(trainer)
            TrainingOutputType = get_training_output_type(trainer)
        except Exception:
            app.state.health = Health.SETUP_FAILED
            msg = "Error while loading trainer:\n\n" + traceback.format_exc()
            add_setup_failed_routes(app, started_at, msg)
            return app

        class TrainingRequest(
            schema.TrainingRequest.with_types(input_type=TrainingInputType)
        ):
            pass

        TrainingResponse = schema.TrainingResponse.with_types(
            input_type=TrainingInputType, output_type=TrainingOutputType
        )

        @app.post(
            "/trainings",
            response_model=TrainingResponse,
            response_model_exclude_unset=True,
        )
        def train(
            request: TrainingRequest = Body(default=None),
            prefer: Optional[str] = Header(default=None),
            traceparent: Optional[str] = Header(default=None, include_in_schema=False),
            tracestate: Optional[str] = Header(default=None, include_in_schema=False),
        ) -> Any:  # type: ignore
            return predict(
                request,
                prefer=prefer,
                traceparent=traceparent,
                tracestate=tracestate,
            )

        @app.put(
            "/trainings/{training_id}",
            response_model=PredictionResponse,
            response_model_exclude_unset=True,
        )
        def train_idempotent(
            training_id: str = Path(..., title="Training ID"),
            request: TrainingRequest = Body(..., title="Training Request"),
            prefer: Optional[str] = Header(default=None),
            traceparent: Optional[str] = Header(default=None, include_in_schema=False),
            tracestate: Optional[str] = Header(default=None, include_in_schema=False),
        ) -> Any:
            return predict_idempotent(
                prediction_id=training_id,
                request=request,
                prefer=prefer,
                traceparent=traceparent,
                tracestate=tracestate,
            )

        @app.post("/trainings/{training_id}/cancel")
        def cancel_training(training_id: str = Path(..., title="Training ID")) -> Any:
            return cancel(training_id)

    with warnings.catch_warnings():
        warnings.filterwarnings(
            "ignore",
            category=DeprecationWarning,
        )

        @app.on_event("startup")
        def startup() -> None:
            # check for early setup failures
            if (
                app.state.setup_result
                and app.state.setup_result.status == schema.Status.FAILED
            ):
                if (
                    not args.await_explicit_shutdown
                ):  # signal shutdown if interactive run
                    if shutdown_event is not None:
                        shutdown_event.set()
            else:
                app.state.setup_task = runner.setup()

        @app.on_event("shutdown")
        def shutdown() -> None:
            runner.shutdown()

    @app.get("/")
    async def root() -> Any:
        return {
            # "cog_version": "", # TODO
            "docs_url": "/docs",
            "openapi_url": "/openapi.json",
        }

    @app.get("/health-check")
    async def healthcheck() -> Any:
        await _check_setup_task()
        if shutdown_event is not None and shutdown_event.is_set():
            health = Health.SHUTTING_DOWN
        elif app.state.health == Health.READY:
            health = Health.BUSY if runner.is_busy() else Health.READY
        else:
            health = app.state.health
        setup = attrs.asdict(app.state.setup_result) if app.state.setup_result else {}
        activity = runner.activity_info()
        return jsonable_encoder(
            {"status": health.name, "setup": setup, "concurrency": activity}
        )

    # this is a readiness probe, it only returns 200 when work can be accepted
    @app.get("/ready")
    async def ready() -> Any:
        activity = runner.activity_info()
        if runner.is_busy():
            return JSONResponse(
                {"status": "ready", "activity": activity}, status_code=200
            )
        return JSONResponse(
            {"status": "not ready", "activity": activity}, status_code=503
        )

    @limited
    @app.post(
        "/predictions",
        response_model=PredictionResponse,
        response_model_exclude_unset=True,
    )
    async def predict(
        request: PredictionRequest = Body(default=None),
        prefer: Optional[str] = Header(default=None),
        traceparent: Optional[str] = Header(default=None, include_in_schema=False),
        tracestate: Optional[str] = Header(default=None, include_in_schema=False),
    ) -> Any:  # type: ignore
        """
        Run a single prediction on the model
        """
        if shutdown_event is not None and shutdown_event.is_set():
            return JSONResponse({"detail": "Model shutting down"}, status_code=409)
        if runner.is_busy():
            return JSONResponse(
                {"detail": "Already running a prediction"}, status_code=409
            )

        # TODO: spec-compliant parsing of Prefer header.
        respond_async = prefer == "respond-async"

        with trace_context(make_trace_context(traceparent, tracestate)):
            return await shared_predict(request=request, respond_async=respond_async)

    @limited
    @app.put(
        "/predictions/{prediction_id}",
        response_model=PredictionResponse,
        response_model_exclude_unset=True,
    )
    async def predict_idempotent(
        prediction_id: str = Path(..., title="Prediction ID"),
        request: PredictionRequest = Body(..., title="Prediction Request"),
        prefer: Optional[str] = Header(default=None),
        traceparent: Optional[str] = Header(default=None, include_in_schema=False),
        tracestate: Optional[str] = Header(default=None, include_in_schema=False),
    ) -> Any:
        """
        Run a single prediction on the model (idempotent creation).
        """
        if shutdown_event is not None and shutdown_event.is_set():
            return JSONResponse({"detail": "Model shutting down"}, status_code=409)
        if request.id is not None and request.id != prediction_id:
            body = {
                "loc": ("body", "id"),
                "msg": "prediction ID must match the ID supplied in the URL",
                "type": "value_error",
            }
            raise HTTPException(422, [body])

        # We've already checked that the IDs match, now ensure that an ID is
        # set on the prediction object
        request.id = prediction_id

        # TODO: spec-compliant parsing of Prefer header.
        respond_async = prefer == "respond-async"

        with trace_context(make_trace_context(traceparent, tracestate)):
            return await shared_predict(request=request, respond_async=respond_async)

    async def shared_predict(
        *, request: Optional[PredictionRequest], respond_async: bool = False
    ) -> Response:
        # [compat] If no body is supplied, assume that this model can be run
        # with empty input. This will throw a ValidationError if that's not
        # possible.
        if request is None:
            request = PredictionRequest(input={})
        # [compat] If body is supplied but input is None, set it to an empty
        # dictionary so that later code can be simpler.
        if request.input is None:
            request.input = {}

        try:
            # Previously, we only asked PredictionRunner to handle file uploads for
            # async predictions. However, PredictionRunner now handles data uris.
            # If we ever want to do output_file_prefix, runner also sees that
            initial_response, async_result = runner.predict(request)
        except RunnerBusyError:
            return JSONResponse(
                {"detail": "Already running a prediction"}, status_code=409
            )

        if respond_async:
            return JSONResponse(jsonable_encoder(initial_response), status_code=202)

        # # by now, output Path and File are already converted to str
        # # so when we validate the schema, those urls get cast back to Path and File
        # # in the previous implementation those would then get encoded as strings
        # # however the changes to Path and File break this and return the filename instead
        #
        # # moreover, validating outputs can be a bottleneck with enough volume
        # # since it's not strictly needed, we can comment it out
        # try:
        #     prediction = await async_result
        #     # we're only doing this to catch validation errors
        #     response = PredictionResponse(**prediction.dict())
        #     del response
        # except ValidationError as e:
        #     _log_invalid_output(e)
        #     raise HTTPException(status_code=500, detail=str(e)) from e

        prediction = await async_result

        if PYDANTIC_V2:
            encoded_response = jsonable_encoder(prediction.model_dump())
        else:
            encoded_response = jsonable_encoder(prediction.dict())
        return JSONResponse(content=encoded_response)

    @app.post("/predictions/{prediction_id}/cancel")
    async def cancel(prediction_id: str = Path(..., title="Prediction ID")) -> Any:
        """
        Cancel a running prediction
        """
        # no need to check whether or not we're busy
        try:
            runner.cancel(prediction_id)
        except UnknownPredictionError:
            return JSONResponse({}, status_code=404)
        else:
            return JSONResponse({}, status_code=200)

    async def _check_setup_task() -> Any:
        if app.state.setup_task is None:
            return

        if not app.state.setup_task.done():
            return

        # this can raise CancelledError
        result = app.state.setup_task.result()

        if result.status == schema.Status.SUCCEEDED:
            app.state.health = Health.READY
        else:
            app.state.health = Health.SETUP_FAILED

        app.state.setup_result = result

        # Reset app.state.setup_task so future calls are a no-op
        app.state.setup_task = None

    return app


def _log_invalid_output(error: Any) -> None:
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


class Server(uvicorn.Server):
    def start(self) -> None:
        self._thread = threading.Thread(target=self.run)
        self._thread.start()

    def stop(self) -> None:
        log.info("stopping server")
        self.should_exit = True

        self._thread.join(timeout=5)
        if not self._thread.is_alive():
            return

        log.warn("failed to exit after 5 seconds, setting force_exit")
        self.force_exit = True
        self._thread.join(timeout=5)
        if not self._thread.is_alive():
            return

        log.warn("failed to exit after another 5 seconds, sending SIGKILL")
        os.kill(os.getpid(), signal.SIGKILL)


def is_port_in_use(port: int) -> bool:
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as s:
        return s.connect_ex(("localhost", port)) == 0


def signal_ignore(signum: Any, frame: Any) -> None:
    log.warn("Got a signal to exit, ignoring it...", signal=signal.Signals(signum).name)


def signal_set_event(event: threading.Event) -> Callable[[Any, Any], None]:
    def _signal_set_event(signum: Any, frame: Any) -> None:
        event.set()

    return _signal_set_event


def _cpu_count() -> int:
    try:
        return len(os.sched_getaffinity(0)) or 1  # type: ignore
    except AttributeError:  # not available on every platform
        return os.cpu_count() or 1


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="Cog HTTP server")
    parser.add_argument(
        "--threads",
        dest="threads",
        type=int,
        default=None,
        help="Number of worker processes. Defaults to number of CPUs, or 1 if using a GPU.",
    )
    parser.add_argument(
        "--upload-url",
        dest="upload_url",
        type=str,
        default=None,
        help="An endpoint for Cog to PUT output files to",
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
        type=str,
        default="predict",
        choices=["predict", "train"],
        help="Experimental: Run in 'predict' or 'train' mode",
    )
    args = parser.parse_args()

    # log level is configurable so we can make it quiet or verbose for `cog predict`
    # cog predict --debug       # -> debug
    # cog predict               # -> warning
    # docker run <image-name>   # -> info (default)
    log_level = logging.getLevelName(os.environ.get("COG_LOG_LEVEL", "INFO").upper())
    setup_logging(log_level=log_level)

    config = load_config()

    threads: Optional[int] = args.threads
    if threads is None:
        if config.get("build", {}).get("gpu", False):
            threads = 1
        else:
            threads = _cpu_count()

    shutdown_event = threading.Event()
    app = create_app(
        config=config,
        shutdown_event=shutdown_event,
        threads=threads,
        upload_url=args.upload_url,
        mode=args.mode,
    )

    port = int(os.getenv("PORT", 5000))
    if is_port_in_use(port):
        log.error(f"Port {port} is already in use")
        sys.exit(1)

    server_config = uvicorn.Config(
        app,
        host="0.0.0.0",
        port=port,
        log_config=None,
        # This is the default, but to be explicit: only run a single worker
        workers=1,
    )

    if args.await_explicit_shutdown:
        signal.signal(signal.SIGTERM, signal_ignore)
    else:
        signal.signal(signal.SIGTERM, signal_set_event(shutdown_event))

    s = Server(config=server_config)
    s.start()

    try:
        shutdown_event.wait()
    except KeyboardInterrupt:
        pass

    s.stop()

    # return error exit code when setup failed and cog is running in interactive mode (not k8s)
    if app.state.setup_result and not args.await_explicit_shutdown:
        if app.state.setup_result.status == schema.Status.FAILED:
            exit(-1)
