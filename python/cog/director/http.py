import multiprocessing
import os
import signal
import threading
from typing import Any

import structlog
import uvicorn
from fastapi import FastAPI
from fastapi.responses import JSONResponse

from .. import schema
from ..json import make_encodeable
from ..server.webhook import webhook_caller

log = structlog.get_logger(__name__)


class Server(uvicorn.Server):
    def start(self):
        self._thread = threading.Thread(target=self.run)
        self._thread.start()

    def stop(self):
        self.should_exit = True

    def join(self):
        assert self._thread is not None, "cannot terminate unstarted server"
        self._thread.join()


def create_app(
    prediction_event: threading.Event,
    prediction_timeout_event: threading.Event,
    prediction_request_pipe: multiprocessing.connection.Connection,
    max_failure_count: int,
) -> FastAPI:
    app = FastAPI(title="Director")

    # Used to signal between webserver and queue worker when a prediction is
    # running or not.
    app.state.prediction_event = prediction_event

    # Used to signal to the webserver that a prediction timed out and was
    # canceled automatically.
    app.state.prediction_timeout_event = prediction_timeout_event

    # Used to send the original prediction request from the queue worker to the
    # webserver for constructing outgoing webhooks.
    app.state.prediction_request_pipe = prediction_request_pipe
    app.state.prediction_request = None

    # Number of consecutive failures seen
    app.state.failure_count = 0

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

        # if the prediction was canceled, that might be because it timed out:
        # change the status to match that set by redis_queue if so...
        if (
            outgoing_response["status"] == schema.Status.CANCELED
            and app.state.prediction_timeout_event.is_set()
        ):
            outgoing_response["status"] = schema.Status.FAILED
            outgoing_response["error"] = "Prediction timed out"

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
