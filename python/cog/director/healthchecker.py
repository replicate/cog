import queue
import time
import threading
from typing import Any, Callable, Dict, Optional

import requests
import structlog
from requests.adapters import HTTPAdapter
from requests.packages.urllib3.util.retry import Retry

from .eventtypes import Health, HealthcheckStatus

log = structlog.get_logger(__name__)

# How often to healthcheck when state is unknown
UNKNOWN_POLL_INTERVAL = 0.1

# How often to healthcheck when state is known
POLL_INTERVAL = 5


class Healthchecker:
    def __init__(
        self, *, events: queue.Queue, fetcher: Callable[[], HealthcheckStatus]
    ):
        self._events = events
        self._fetch = fetcher

        self._thread = None
        self._should_exit = threading.Event()

        self._last_check = time.perf_counter()
        self._state = HealthcheckStatus(health=Health.UNKNOWN)

    def start(self) -> None:
        """
        Start the background healthchecker thread.
        """
        self._thread = threading.Thread(target=self._run)
        self._thread.start()

    def stop(self) -> None:
        """
        Trigger the termination of the healthcheck thread.
        """
        self._should_exit.set()

    def join(self) -> None:
        if self._thread is not None:
            self._thread.join()

    def _run(self) -> None:
        while not self._should_exit.is_set():
            if self._check_due():
                self._set_state(self._fetch())
                self._last_check = time.perf_counter()
            else:
                time.sleep(min(POLL_INTERVAL, UNKNOWN_POLL_INTERVAL))

    def _set_state(self, state: HealthcheckStatus) -> None:
        if self._state == state:
            return
        self._state = state
        log.debug("healthchecker status changed", state=state)
        try:
            self._events.put(self._state, timeout=0.1)
        except queue.Full:
            log.warn("failed to enqueue healthcheck status change: queue full")

    def _check_due(self) -> bool:
        last_check_delta = time.perf_counter() - self._last_check
        if self._state.health == Health.UNKNOWN:
            return last_check_delta > UNKNOWN_POLL_INTERVAL
        else:
            return last_check_delta > POLL_INTERVAL


def http_fetcher(url: str):
    c = _make_http_client()

    def _fetch():
        try:
            resp = c.get(url, timeout=1)
        except requests.exceptions.RequestException:
            return HealthcheckStatus(health=Health.UNKNOWN)
        else:
            return _state_from_response(resp)

    return _fetch


def _make_http_client() -> requests.Session:
    session = requests.Session()
    adapter = HTTPAdapter(
        max_retries=Retry(
            total=6,
            backoff_factor=0.2,
            status_forcelist=[429, 500, 502, 503, 504],
        ),
    )
    session.mount("http://", adapter)
    session.mount("https://", adapter)
    return session


def _state_from_response(resp: requests.Response) -> HealthcheckStatus:
    if resp.status_code != 200:
        return HealthcheckStatus(health=Health.UNKNOWN)

    try:
        body = resp.json()
    except requests.exceptions.JSONDecodeError:
        log.warn("received invalid JSON from healthcheck endpoint", response=resp.text)
        return HealthcheckStatus(health=Health.UNKNOWN)

    if "status" not in body or "setup" not in body:
        log.warn(
            "received response with invalid schema from healthcheck endpoint",
            response=resp.text,
        )
        return HealthcheckStatus(health=Health.UNKNOWN)

    if body["status"] != "healthy":
        return HealthcheckStatus(health=Health.UNKNOWN)

    if body["setup"] is None:
        return HealthcheckStatus(health=Health.UNKNOWN)

    if body["setup"].get("status") == "succeeded":
        return HealthcheckStatus(health=Health.HEALTHY, metadata=body["setup"])

    return HealthcheckStatus(health=Health.SETUP_FAILED, metadata=body["setup"])
