import queue
import threading
from typing import Callable, Optional

import requests
import structlog
from attrs import define
from requests.adapters import HTTPAdapter
from requests.packages.urllib3.util.retry import Retry  # type: ignore

from .eventtypes import Health, HealthcheckStatus

log = structlog.get_logger(__name__)

# How often to healthcheck initially
DEFAULT_POLL_INTERVAL = 0.1


class Healthchecker:
    def __init__(
        self,
        *,
        events: queue.Queue,
        fetcher: Callable[[], HealthcheckStatus],
        interval: float = DEFAULT_POLL_INTERVAL,
    ):
        self._events = events
        self._fetch = fetcher

        self._thread: Optional[threading.Thread] = None
        self._interval = interval
        self._control: queue.Queue = queue.Queue()

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
        self._control.put(_Stop())

    def set_interval(self, interval: float) -> None:
        """
        Change the polling interval.
        """
        self._control.put(_SetInterval(value=interval))

    def request_status(self) -> None:
        """
        Request an unconditional status update.
        """
        self._control.put(_RequestStatus())

    def join(self) -> None:
        if self._thread is not None:
            self._thread.join()

    def _run(self) -> None:
        while True:
            try:
                msg = self._control.get(timeout=self._interval)
            except queue.Empty:
                self._check()
                continue

            if isinstance(msg, _Stop):
                break
            elif isinstance(msg, _SetInterval):
                self._interval = msg.value
            elif isinstance(msg, _RequestStatus):
                self._check(force_update=True)
            else:
                log.warn("unknown message on control queue", msg=msg)

    def _check(self, force_update: bool = False) -> None:
        state = self._fetch()

        if self._state != state:
            log.debug("healthchecker status changed", state=state)
        elif not force_update:
            return

        self._state = state

        try:
            self._events.put(self._state, timeout=0.1)
        except queue.Full:
            log.warn("failed to enqueue healthcheck status change: queue full")


def http_fetcher(url: str) -> Callable:
    c = _make_http_client()

    def _fetch() -> HealthcheckStatus:
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


@define
class _Stop:
    pass


@define
class _RequestStatus:
    pass


@define
class _SetInterval:
    value: float
