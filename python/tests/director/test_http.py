import queue

import pytest
from fastapi.testclient import TestClient

from cog.director.eventtypes import Webhook
from cog.director.http import create_app
from cog.schema import PredictionResponse


@pytest.fixture
def events():
    return queue.Queue(maxsize=64)


@pytest.fixture
def client(events):
    app = create_app(events=events)
    with TestClient(app) as c:
        yield c


def read_all_events(q):
    result = []
    while True:
        try:
            result.append(q.get_nowait())
        except queue.Empty:
            return result


def test_webhook_enqueues_payloads(client, events):
    resp = client.post(
        "/webhook",
        json={"input": {"prompt": "hello world"}, "logs": "running prediction"},
    )
    assert resp.status_code == 200
    assert resp.json() == {"status": "ok"}

    enqueued = read_all_events(events)

    assert enqueued == [
        Webhook(
            payload=PredictionResponse(
                input={"prompt": "hello world"}, logs="running prediction"
            )
        )
    ]


def test_webhook_returns_503s_with_full_queue(client, events):
    for _ in range(64):
        client.post(
            "/webhook",
            json={"input": {"prompt": "hello world"}, "logs": "running prediction"},
        )

    resp = client.post(
        "/webhook",
        json={"input": {"prompt": "hello world"}, "logs": "running prediction"},
    )
    assert resp.status_code == 503

    enqueued = read_all_events(events)
    assert len(enqueued) == 64
