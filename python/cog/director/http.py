import queue
import threading
from typing import Any

import structlog
import uvicorn
from fastapi import FastAPI
from fastapi.responses import JSONResponse

from .. import schema
from .eventtypes import Webhook

log = structlog.get_logger(__name__)


class Server(uvicorn.Server):
    def start(self) -> None:
        self._thread = threading.Thread(target=self.run)
        self._thread.start()

    def stop(self) -> None:
        self.should_exit = True

    def join(self) -> None:
        assert self._thread is not None, "cannot terminate unstarted server"
        self._thread.join()


def create_app(events: queue.Queue) -> FastAPI:
    app = FastAPI(title="Director")

    # The event queue is used to communicate with Director when webhook
    # events are received.
    app.state.events = events

    @app.post("/webhook")
    def webhook(payload: schema.PredictionResponse) -> Any:
        event = Webhook(payload=payload)
        try:
            app.state.events.put(event, timeout=0.1)
        except queue.Full:
            return JSONResponse(
                {"detail": "cannot receive webhooks: queue is full"},
                status_code=503,
            )

        return JSONResponse({"status": "ok"}, status_code=200)

    return app
