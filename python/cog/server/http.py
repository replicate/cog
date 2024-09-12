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
from datetime import datetime, timezone
from enum import Enum, auto, unique
from typing import TYPE_CHECKING, Any, Awaitable, Callable, Optional

import structlog
import uvicorn
from fastapi import Body, FastAPI, Header, HTTPException, Path, Response
from fastapi.encoders import jsonable_encoder
from fastapi.exceptions import RequestValidationError
from fastapi.responses import JSONResponse
from pydantic import ValidationError
from pydantic.error_wrappers import ErrorWrapper

from .. import schema
from ..errors import PredictorNotSet
from ..files import upload_file
from ..json import upload_files
from ..logging import setup_logging
from ..predictor import (
    get_input_type,
    get_output_type,
    get_predictor_ref,
    get_training_input_type,
    get_training_output_type,
    load_config,
    load_slim_predictor_from_ref,
)
from ..types import CogConfig
from .probes import ProbeHelper
from .runner import (
    PredictionRunner,
    RunnerBusyError,
    SetupResult,
    UnknownPredictionError,
)
from .telemetry import make_trace_context, trace_context
from .worker import make_worker

if TYPE_CHECKING:
    from typing import ParamSpec, TypeVar  # pylint: disable=import-outside-toplevel

    P = ParamSpec("P")  # pylint: disable=invalid-name
    T = TypeVar("T")  # pylint: disable=invalid-name

log = structlog.get_logger("cog.server.http")


@unique
class Health(Enum):
    UNKNOWN = auto()
    STARTING = auto()
    READY = auto()
    BUSY = auto()
    SETUP_FAILED = auto()
    DEFUNCT = auto()


class MyState:
    health: Health
    setup_result: Optional[SetupResult]


class MyFastAPI(FastAPI):
    # TODO: not, strictly speaking, legal
    # https://github.com/microsoft/pyright/issues/5933
    # but it'd need a FastAPI patch to fix
    state: MyState  # type: ignore


def add_setup_failed_routes(
    app: MyFastAPI,  # pylint: disable=redefined-outer-name
    started_at: datetime,
    msg: str,
) -> None:
    print(msg)
    result = SetupResult(
        started_at=started_at,
        completed_at=datetime.now(tz=timezone.utc),
        logs=[msg],
        status=schema.Status.FAILED,
    )
    app.state.setup_result = result
    app.state.health = Health.SETUP_FAILED

    @app.get("/health-check")
    async def healthcheck_startup_failed() -> Any:
        assert app.state.setup_result
        return jsonable_encoder(
            {
                "status": app.state.health.name,
                "setup": app.state.setup_result.to_dict(),
            }
        )


def create_app(  # pylint: disable=too-many-arguments,too-many-locals,too-many-statements
    config: CogConfig,  # pylint: disable=redefined-outer-name
    shutdown_event: Optional[threading.Event],  # pylint: disable=redefined-outer-name
    threads: int = 1,  # pylint: disable=redefined-outer-name
    upload_url: Optional[str] = None,
    mode: str = "predict",
    is_build: bool = False,
    await_explicit_shutdown: bool = False,  # pylint: disable=redefined-outer-name
) -> MyFastAPI:
    app = MyFastAPI(  # pylint: disable=redefined-outer-name
        title="Cog",  # TODO: mention model name?
        # version=None # TODO
    )

    app.state.health = Health.STARTING
    app.state.setup_result = None
    started_at = datetime.now(tz=timezone.utc)

    # shutdown is needed no matter what happens
    @app.post("/shutdown")
    async def start_shutdown() -> Any:
        log.info("shutdown requested via http")
        if shutdown_event:
            shutdown_event.set()
        return JSONResponse({}, status_code=200)

    try:
        predictor_ref = get_predictor_ref(config, mode)
        predictor = load_slim_predictor_from_ref(predictor_ref, "predict")
        InputType = get_input_type(predictor)  # pylint: disable=invalid-name
        OutputType = get_output_type(predictor)  # pylint: disable=invalid-name
    except Exception:  # pylint: disable=broad-exception-caught
        msg = "Error while loading predictor:\n\n" + traceback.format_exc()
        add_setup_failed_routes(app, started_at, msg)
        return app

    worker = make_worker(predictor_ref=predictor_ref)
    runner = PredictionRunner(worker=worker)

    class PredictionRequest(schema.PredictionRequest.with_types(input_type=InputType)):
        pass

    PredictionResponse = schema.PredictionResponse.with_types(  # pylint: disable=invalid-name
        input_type=InputType, output_type=OutputType
    )

    http_semaphore = asyncio.Semaphore(threads)

    def limited(f: "Callable[P, Awaitable[T]]") -> "Callable[P, Awaitable[T]]":
        @functools.wraps(f)
        async def wrapped(*args: "P.args", **kwargs: "P.kwargs") -> "T":  # pylint: disable=redefined-outer-name
            async with http_semaphore:
                return await f(*args, **kwargs)

        return wrapped

    if "train" in config:
        try:
            trainer_ref = get_predictor_ref(config, "train")
            trainer = load_slim_predictor_from_ref(trainer_ref, "train")
            TrainingInputType = get_training_input_type(trainer)  # pylint: disable=invalid-name
            TrainingOutputType = get_training_output_type(trainer)  # pylint: disable=invalid-name

            class TrainingRequest(
                schema.TrainingRequest.with_types(input_type=TrainingInputType)
            ):
                pass

            TrainingResponse = schema.TrainingResponse.with_types(  # pylint: disable=invalid-name
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
                traceparent: Optional[str] = Header(
                    default=None, include_in_schema=False
                ),
                tracestate: Optional[str] = Header(
                    default=None, include_in_schema=False
                ),
            ) -> Any:  # type: ignore
                with trace_context(make_trace_context(traceparent, tracestate)):
                    return predict(request, prefer)

            @app.put(
                "/trainings/{training_id}",
                response_model=TrainingResponse,
                response_model_exclude_unset=True,
            )
            def train_idempotent(
                training_id: str = Path(..., title="Training ID"),
                request: TrainingRequest = Body(..., title="Training Request"),
                prefer: Optional[str] = Header(default=None),
                traceparent: Optional[str] = Header(
                    default=None, include_in_schema=False
                ),
                tracestate: Optional[str] = Header(
                    default=None, include_in_schema=False
                ),
            ) -> Any:
                with trace_context(make_trace_context(traceparent, tracestate)):
                    return predict_idempotent(training_id, request, prefer)

            @app.post("/trainings/{training_id}/cancel")
            def cancel_training(
                training_id: str = Path(..., title="Training ID"),
            ) -> Any:
                return cancel(training_id)

        except Exception as e:  # pylint: disable=broad-exception-caught
            if isinstance(e, (PredictorNotSet, FileNotFoundError)) and not is_build:
                pass  # ignore missing train.py for backward compatibility with existing "bad" models in use
            else:
                app.state.health = Health.SETUP_FAILED
                msg = "Error while loading trainer:\n\n" + traceback.format_exc()
                add_setup_failed_routes(app, started_at, msg)
                return app

    @app.on_event("startup")
    def startup() -> None:
        # check for early setup failures
        if (
            app.state.setup_result
            and app.state.setup_result.status == schema.Status.FAILED
        ):
            # signal shutdown if interactive run
            if shutdown_event and not await_explicit_shutdown:
                shutdown_event.set()
        else:
            setup_task = runner.setup()
            setup_task.add_done_callback(_handle_setup_done)

    @app.on_event("shutdown")
    def shutdown() -> None:
        worker.terminate()

    @app.get("/")
    async def root() -> Any:
        return {
            # "cog_version": "", # TODO
            "docs_url": "/docs",
            "openapi_url": "/openapi.json",
        }

    @app.get("/health-check")
    async def healthcheck() -> Any:
        if app.state.health == Health.READY:
            health = Health.BUSY if runner.is_busy() else Health.READY
        else:
            health = app.state.health
        setup = app.state.setup_result.to_dict() if app.state.setup_result else {}
        return jsonable_encoder({"status": health.name, "setup": setup})

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
        # TODO: spec-compliant parsing of Prefer header.
        respond_async = prefer == "respond-async"

        with trace_context(make_trace_context(traceparent, tracestate)):
            return _predict(
                request=request,
                respond_async=respond_async,
            )

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
        if request.id is not None and request.id != prediction_id:
            raise RequestValidationError(
                [
                    ErrorWrapper(
                        ValueError(
                            "prediction ID must match the ID supplied in the URL"
                        ),
                        ("body", "id"),
                    )
                ]
            )

        # We've already checked that the IDs match, now ensure that an ID is
        # set on the prediction object
        request.id = prediction_id

        # If the prediction service is already running a prediction with a
        # matching ID, return its current state.
        if runner.is_busy():
            task = runner.get_predict_task(request.id)
            if task:
                return JSONResponse(
                    jsonable_encoder(task.result),
                    status_code=202,
                )

        # TODO: spec-compliant parsing of Prefer header.
        respond_async = prefer == "respond-async"

        with trace_context(make_trace_context(traceparent, tracestate)):
            return _predict(
                request=request,
                respond_async=respond_async,
            )

    def _predict(
        *,
        request: Optional[PredictionRequest],
        respond_async: bool = False,
    ) -> Response:
        # [compat] If no body is supplied, assume that this model can be run
        # with empty input. This will throw a ValidationError if that's not
        # possible.
        if request is None:
            request = PredictionRequest(input={})
        # [compat] If body is supplied but input is None, set it to an empty
        # dictionary so that later code can be simpler.
        if request.input is None:
            request.input = {}  # pylint: disable=attribute-defined-outside-init

        task_kwargs = {}
        if respond_async:
            # For now, we only ask PredictionService to handle file uploads for
            # async predictions. This is unfortunate but required to ensure
            # backwards-compatible behaviour for synchronous predictions.
            task_kwargs["upload_url"] = upload_url

        try:
            predict_task = runner.predict(request, task_kwargs=task_kwargs)
        except RunnerBusyError:
            return JSONResponse(
                {"detail": "Already running a prediction"}, status_code=409
            )

        if hasattr(request.input, "cleanup"):
            predict_task.add_done_callback(lambda _: request.input.cleanup())

        predict_task.add_done_callback(_handle_predict_done)

        if respond_async:
            return JSONResponse(
                jsonable_encoder(predict_task.result),
                status_code=202,
            )

        # Otherwise, wait for the prediction to complete...
        predict_task.wait()

        # ...and return the result.
        try:
            response = PredictionResponse(**predict_task.result.dict())
        except ValidationError as e:
            _log_invalid_output(e)
            raise HTTPException(status_code=500, detail=str(e)) from e

        response_object = response.dict()
        response_object["output"] = upload_files(
            response_object["output"],
            upload_file=lambda fh: upload_file(fh, request.output_file_prefix),  # type: ignore
        )

        # FIXME: clean up output files
        encoded_response = jsonable_encoder(response_object)
        return JSONResponse(content=encoded_response)

    @app.post("/predictions/{prediction_id}/cancel")
    async def cancel(prediction_id: str = Path(..., title="Prediction ID")) -> Any:
        """
        Cancel a running prediction
        """
        if not runner.is_busy():
            return JSONResponse({}, status_code=404)
        try:
            runner.cancel(prediction_id)
        except UnknownPredictionError:
            return JSONResponse({}, status_code=404)
        return JSONResponse({}, status_code=200)

    def _handle_predict_done(response: schema.PredictionResponse) -> None:
        if response._fatal_exception:
            _maybe_shutdown(response._fatal_exception)

    def _handle_setup_done(setup_result: SetupResult) -> None:
        app.state.setup_result = setup_result

        if app.state.setup_result.status == schema.Status.SUCCEEDED:
            app.state.health = Health.READY

            # In kubernetes, mark the pod as ready now setup has completed.
            probes = ProbeHelper()
            probes.ready()
        else:
            _maybe_shutdown(Exception("setup failed"), status=Health.SETUP_FAILED)

    def _maybe_shutdown(exc: BaseException, *, status: Health = Health.DEFUNCT) -> None:
        log.error("encountered fatal error", exc_info=exc)
        app.state.health = status
        if shutdown_event and not await_explicit_shutdown:
            log.error("shutting down immediately")
            shutdown_event.set()
        else:
            log.error("awaiting explicit shutdown")

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
        self._thread = threading.Thread(target=self.run)  # pylint: disable=attribute-defined-outside-init
        self._thread.start()

    def stop(self) -> None:
        log.info("stopping server")
        self.should_exit = True  # pylint: disable=attribute-defined-outside-init

        self._thread.join(timeout=5)
        if not self._thread.is_alive():
            return

        log.warn("failed to exit after 5 seconds, setting force_exit")
        self.force_exit = True  # pylint: disable=attribute-defined-outside-init
        self._thread.join(timeout=5)
        if not self._thread.is_alive():
            return

        log.warn("failed to exit after another 5 seconds, sending SIGKILL")
        os.kill(os.getpid(), signal.SIGKILL)


def is_port_in_use(port: int) -> bool:  # pylint: disable=redefined-outer-name
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as sock:
        return sock.connect_ex(("localhost", port)) == 0


def signal_ignore(signum: Any, frame: Any) -> None:  # pylint: disable=unused-argument
    log.warn("Got a signal to exit, ignoring it...", signal=signal.Signals(signum).name)


def signal_set_event(event: threading.Event) -> Callable[[Any, Any], None]:
    def _signal_set_event(signum: Any, frame: Any) -> None:  # pylint: disable=unused-argument
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
        "--host",
        dest="host",
        type=str,
        default="0.0.0.0",
        help="Host to bind to",
    )
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

    threads = args.threads
    if threads is None:
        gpu_enabled = config.get("build", {}).get("gpu", False)
        threads = 1 if gpu_enabled else _cpu_count()

    shutdown_event = threading.Event()

    await_explicit_shutdown = args.await_explicit_shutdown
    if await_explicit_shutdown:
        signal.signal(signal.SIGTERM, signal_ignore)
    else:
        signal.signal(signal.SIGTERM, signal_set_event(shutdown_event))

    app = create_app(
        config=config,
        shutdown_event=shutdown_event,
        threads=threads,
        upload_url=args.upload_url,
        mode=args.mode,
        await_explicit_shutdown=await_explicit_shutdown,
    )

    host: str = args.host

    port = int(os.getenv("PORT", "5000"))
    if is_port_in_use(port):
        log.error(f"Port {port} is already in use")
        sys.exit(1)

    server_config = uvicorn.Config(
        app,
        host=host,
        port=port,
        log_config=None,
        # This is the default, but to be explicit: only run a single worker
        workers=1,
    )

    s = Server(config=server_config)
    s.start()

    try:
        shutdown_event.wait()
    except KeyboardInterrupt:
        pass

    s.stop()

    # return error exit code when setup failed and cog is running in interactive mode (not k8s)
    if (
        app.state.setup_result
        and app.state.setup_result.status == schema.Status.FAILED
        and not await_explicit_shutdown
    ):
        sys.exit(-1)
