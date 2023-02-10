import os
import threading
import time
from queue import Empty, Full, Queue
from typing import Optional

import structlog
from opentelemetry import trace

from .. import schema

log = structlog.get_logger(__name__)


def span_attributes_from_env():
    return {
        "hostname": os.environ.get("HOSTNAME", ""),
        "model.id": os.environ.get("COG_MODEL_ID", ""),
        "model.name": os.environ.get("COG_MODEL_NAME", ""),
        "model.username": os.environ.get("COG_USERNAME", ""),
        "model.version": os.environ.get("COG_MODEL_VERSION", ""),
        "hardware": os.environ.get("COG_HARDWARE", ""),
        "docker_image_uri": os.environ.get("COG_DOCKER_IMAGE_URI", ""),
    }


class Monitor:
    def __init__(self, utilization_interval=15):
        self._thread = None
        self._should_exit = threading.Event()
        self._tracer = trace.get_tracer("cog-director")
        self._prediction_events = Queue()
        self.utilization_interval = utilization_interval

    def start(self) -> None:
        self._thread = threading.Thread(target=self._run)
        self._thread.start()

    def stop(self) -> None:
        self._should_exit.set()

    def join(self) -> None:
        if self._thread is not None:
            self._thread.join()

    def set_current_prediction(self, prediction: Optional[schema.PredictionResponse]):
        try:
            self._prediction_events.put_nowait(prediction)
        except Full:
            log.info("prediction event queue is full, dropping event")

    def _run(self) -> None:
        last_span_at = time.perf_counter() - self.utilization_interval
        active_sec = 0.0
        current_prediction: Optional[schema.PredictionResponse] = None
        current_prediction_started_at: Optional[float] = None

        while not self._should_exit.is_set():
            try:
                current_prediction = self._prediction_events.get_nowait()
                # Set to None or schema.PredictionResponse
                # Will raise Empty when there is nothing in the queue in the timeout

                if current_prediction_started_at:  # count utilization in window
                    active_sec += time.perf_counter() - current_prediction_started_at

                if current_prediction:  # new prediction
                    current_prediction_started_at = time.perf_counter()
                else:  # switched to idling
                    current_prediction_started_at = None
            except Empty:
                pass

            if time.perf_counter() >= last_span_at + self.utilization_interval:
                with self._tracer.start_as_current_span(
                    name="cog.director.utilization",
                    attributes=span_attributes_from_env(),
                ) as span:
                    if current_prediction_started_at:  # event didn't finish in window
                        active_sec += (
                            time.perf_counter() - current_prediction_started_at
                        )

                    utilization_for_window = min(
                        active_sec / self.utilization_interval, 1.0
                    )
                    span.set_attributes(
                        {
                            "utilization": utilization_for_window,
                            "metric_duration": time.perf_counter() - last_span_at,
                        }
                    )
                    if current_prediction and current_prediction.version:
                        span.set_attribute("model.version", current_prediction.version)

                last_span_at = last_span_at + self.utilization_interval

                active_sec = 0.0
                if current_prediction:
                    current_prediction_started_at = last_span_at

            # Can't just sleep; we need to check for events, emit spans at the
            # right time, and whether to exit. We can't block on only one of
            # those things.
            time.sleep(0.01)

        log.info("shutting down monitor")
