import queue
import time

import pytest
import requests
import responses
from responses.registries import OrderedRegistry


from cog.director.eventtypes import Health, HealthcheckStatus
from cog.director.healthchecker import Healthchecker, http_fetcher


def fake_fetcher(statuses):
    def _fetch():
        try:
            return statuses.pop()
        except IndexError:
            return HealthcheckStatus(health=Health.UNKNOWN)

    return _fetch


def test_healthchecker():
    events = queue.Queue(maxsize=64)
    fetcher = fake_fetcher(
        [
            HealthcheckStatus(health=Health.UNKNOWN),
            HealthcheckStatus(health=Health.UNKNOWN),
            HealthcheckStatus(health=Health.UNKNOWN),
            HealthcheckStatus(health=Health.UNKNOWN),
            HealthcheckStatus(health=Health.READY, metadata={"animal": "giraffe"}),
        ]
    )

    h = Healthchecker(events=events, fetcher=fetcher)
    h.start()

    result = events.get(timeout=1)

    assert result == HealthcheckStatus(
        health=Health.READY, metadata={"animal": "giraffe"}
    )

    h.stop()
    h.join()


def test_healthchecker_set_interval():
    events = queue.Queue(maxsize=64)
    fetcher = fake_fetcher(
        [
            HealthcheckStatus(health=Health.UNKNOWN),
            HealthcheckStatus(health=Health.READY, metadata={"animal": "giraffe"}),
        ]
    )

    h = Healthchecker(events=events, fetcher=fetcher, interval=5)
    h.start()

    with pytest.raises(queue.Empty):
        events.get(timeout=0.1)

    h.set_interval(0.1)

    result = events.get(timeout=1)

    assert result == HealthcheckStatus(
        health=Health.READY, metadata={"animal": "giraffe"}
    )

    h.stop()
    h.join()


def test_healthchecker_request_status():
    events = queue.Queue(maxsize=64)
    fetcher = fake_fetcher(
        [
            HealthcheckStatus(health=Health.UNKNOWN),
        ]
    )

    h = Healthchecker(events=events, fetcher=fetcher)
    h.start()

    with pytest.raises(queue.Empty):
        events.get(timeout=0.1)

    h.request_status()

    result = events.get(timeout=1)

    assert result == HealthcheckStatus(health=Health.UNKNOWN)

    h.stop()
    h.join()


@responses.activate
def test_http_fetcher_ok():
    responses.add(
        responses.GET,
        "https://example.com/health-check",
        json={
            "status": "READY",
            "setup": {"status": "succeeded", "logs": "hello there"},
        },
        status=200,
    )
    fetcher = http_fetcher("https://example.com/health-check")

    status = fetcher()

    assert status.health == Health.READY
    assert status.metadata == {"status": "succeeded", "logs": "hello there"}


@responses.activate
def test_http_fetcher_non_200_response():
    responses.add(
        responses.GET,
        "https://example.com/health-check",
        status=404,
    )
    fetcher = http_fetcher("https://example.com/health-check")

    status = fetcher()

    assert status.health == Health.UNKNOWN
    assert status.metadata is None


@responses.activate
def test_http_fetcher_invalid_json():
    responses.add(
        responses.GET,
        "https://example.com/health-check",
        headers={"content-type": "application/json"},
        body='{"malformed":"json"',
        status=200,
    )
    fetcher = http_fetcher("https://example.com/health-check")

    status = fetcher()

    assert status.health == Health.UNKNOWN
    assert status.metadata is None


@responses.activate
def test_http_fetcher_missing_fields():
    responses.add(
        responses.GET,
        "https://example.com/health-check",
        json='{"status":"READY"}',  # missing "setup" field
        status=200,
    )
    fetcher = http_fetcher("https://example.com/health-check")

    status = fetcher()

    assert status.health == Health.UNKNOWN
    assert status.metadata is None


@responses.activate
def test_http_fetcher_connection_errors():
    connerror_resp = responses.Response(
        responses.GET,
        "https://example.com/health-check",
        status=200,
    )
    connerror_exc = requests.ConnectionError("failed to connect")
    connerror_exc.response = connerror_resp
    connerror_resp.body = connerror_exc
    responses.add(connerror_resp)
    fetcher = http_fetcher("https://example.com/health-check")

    status = fetcher()

    assert status.health == Health.UNKNOWN
    assert status.metadata is None


@responses.activate
def test_http_fetcher_unrecognised_status():
    responses.add(
        responses.GET,
        "https://example.com/health-check",
        json={
            "status": "READY_FOR_ANYTHING",
            "setup": {"status": "succeeded", "logs": "hello there"},
        },
        status=200,
    )
    fetcher = http_fetcher("https://example.com/health-check")

    status = fetcher()

    assert status.health == Health.UNKNOWN
    assert status.metadata == {"status": "succeeded", "logs": "hello there"}


@responses.activate
def test_http_fetcher_setup_failed():
    responses.add(
        responses.GET,
        "https://example.com/health-check",
        json={"status": "SETUP_FAILED", "setup": {"status": "failed"}},
        status=200,
    )
    fetcher = http_fetcher("https://example.com/health-check")

    status = fetcher()

    assert status.health == Health.SETUP_FAILED
    assert status.metadata == {"status": "failed"}
