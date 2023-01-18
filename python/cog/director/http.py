import multiprocessing
import os
import signal
import threading
from typing import Any

import structlog
from fastapi import FastAPI
from fastapi.responses import JSONResponse

from .. import schema
from ..json import make_encodeable
from ..server.probes import ProbeHelper
from ..server.webhook import webhook_caller
from .queue_worker import QueueWorker
from .redis import RedisConsumer

log = structlog.get_logger(__name__)


def run_queue_worker(**kwargs: Any) -> None:
    worker = QueueWorker(**kwargs)
    worker.start()


def create_app(
    redis_consumer: RedisConsumer, predict_timeout: int, max_failure_count: int
) -> FastAPI:
    app = FastAPI(title="Director")

    # Used to signal between webserver and queue worker when a prediction is
    # running or not.
    app.state.prediction_event = threading.Event()

    # Used to signal when the queue worker should shut down.
    app.state.shutdown_event = threading.Event()

    # Used to send the original prediction request from the queue worker to the
    # webserver for constructing outgoing webhooks.
    (
        app.state.prediction_request_pipe,
        worker_prediction_request_pipe,
    ) = multiprocessing.Pipe()
    app.state.prediction_request = None

    # Number of consecutive failures seen
    app.state.failure_count = 0

    worker = threading.Thread(
        target=run_queue_worker,
        kwargs=dict(
            redis_consumer=redis_consumer,
            predict_timeout=predict_timeout,
            prediction_event=app.state.prediction_event,
            shutdown_event=app.state.shutdown_event,
            prediction_request_pipe=worker_prediction_request_pipe,
        ),
    )

    def check_failure_count() -> None:
        if max_failure_count is None:
            return
        if app.state.failure_count <= max_failure_count:
            return

        log.error(
            "saw too many failures in a row, exiting...",
            failure_count=app.state.failure_count,
        )

        # TODO: find a better way to shut down uvicorn
        os.kill(os.getpid(), signal.SIGTERM)

    @app.on_event("startup")
    def startup() -> None:
        # Signal pod readiness (when in k8s)
        probes = ProbeHelper()
        probes.ready()

        worker.start()

    @app.on_event("shutdown")
    def shutdown() -> None:
        app.state.shutdown_event.set()

    @app.post("/webhook")
    def webhook(payload: schema.PredictionResponse) -> Any:
        # TODO the logic here seems weird, might need to invert the variable
        # name to something like `prediction_running`?
        if app.state.prediction_event.is_set():
            return JSONResponse(
                {"detail": "cannot receive webhooks when no prediction is running"},
                status_code=409,
            )

        if payload.status is None:
            return JSONResponse(
                {"detail": "webhook payload must have a status"}, status_code=400
            )

        if app.state.prediction_request is None:
            log.info("Getting updated prediction_request from pipe")
            # TODO how defensive do we need to be reading from this pipe?
            app.state.prediction_request = app.state.prediction_request_pipe.recv()
            app.state.send_webhook = webhook_caller(
                app.state.prediction_request["webhook"]
            )

        # only permit a limited set of keys from the payload, to prevent
        # untrusted code from setting things like IDs and internal data
        outgoing_response = make_encodeable(
            {
                **app.state.prediction_request,
                **allowed_fields(payload.dict()),
            }
        )
        log.info("Sending outgoing webhook", payload=outgoing_response)
        app.state.send_webhook(outgoing_response)

        if schema.Status.is_terminal(payload.status):
            app.state.prediction_request = None
            app.state.prediction_event.set()

        if payload.status == schema.Status.FAILED:
            app.state.failure_count += 1
            check_failure_count()
        else:
            app.state.failure_count = 0

        return JSONResponse({"status": "ok"}, status_code=200)

    return app


ALLOWED_FIELDS_FROM_UNTRUSTED_CONTAINER = (
    # TODO: we shouldn't trust the timings (or derived metrics) either
    "completed_at",
    "started_at",
    "metrics",
    # Prediction output and output metadata
    "error",
    "logs",
    "output",
    "status",
)


def allowed_fields(payload: dict):
    return {
        k: v for k, v in payload.items() if k in ALLOWED_FIELDS_FROM_UNTRUSTED_CONTAINER
    }
