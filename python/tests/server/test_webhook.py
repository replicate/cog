import json

import httpx
import pytest
import respx
from cog.schema import PredictionResponse, Status, WebhookEvent
from cog.server.clients import ClientManager


@pytest.fixture
def client_manager():
    return ClientManager()


@pytest.mark.asyncio
@respx.mock
async def test_webhook_caller_basic(client_manager):
    url = "https://example.com/webhook/123"
    sender = client_manager.make_webhook_sender(url, WebhookEvent.default_events())

    payload = {
        "status": Status.PROCESSING,
        "output": {"animal": "giraffe"},
        "input": {},
    }
    response = PredictionResponse(**payload)

    route = respx.post(url).mock(return_value=httpx.Response(200))

    await sender(response, WebhookEvent.COMPLETED)

    assert route.called
    assert json.loads(route.calls.last.request.content) == payload


@pytest.mark.asyncio
@respx.mock
async def test_webhook_caller_non_terminal_does_not_retry(client_manager):
    url = "https://example.com/webhook/123"
    sender = client_manager.make_webhook_sender(url, WebhookEvent.default_events())

    payload = {
        "status": Status.PROCESSING,
        "output": {"animal": "giraffe"},
        "input": {},
    }
    response = PredictionResponse(**payload)

    route = respx.post(url).mock(return_value=httpx.Response(429))

    await sender(response, WebhookEvent.COMPLETED)

    assert route.call_count == 1


@pytest.mark.asyncio
@respx.mock
async def test_webhook_caller_terminal_retries(client_manager):
    url = "https://example.com/webhook/123"
    sender = client_manager.make_webhook_sender(url, WebhookEvent.default_events())

    payload = {"status": Status.SUCCEEDED, "output": {"animal": "giraffe"}, "input": {}}
    response = PredictionResponse(**payload)

    route = respx.post(url).mock(
        side_effect=[httpx.Response(429), httpx.Response(429), httpx.Response(200)]
    )

    await sender(response, WebhookEvent.COMPLETED)

    assert route.call_count == 3


@pytest.mark.asyncio
@respx.mock
async def test_webhook_caller_filtered_basic(client_manager):
    url = "https://example.com/webhook/123"
    events = WebhookEvent.default_events()
    sender = client_manager.make_webhook_sender(url, events)

    payload = {
        "status": Status.PROCESSING,
        "output": {"animal": "giraffe"},
        "input": {},
    }
    response = PredictionResponse(**payload)

    route = respx.post(url).mock(return_value=httpx.Response(200))

    await sender(response, WebhookEvent.LOGS)

    assert route.called
    assert json.loads(route.calls.last.request.content) == payload


@pytest.mark.asyncio
@respx.mock
async def test_webhook_caller_filtered_omits_filtered_events(client_manager):
    url = "https://example.com/webhook/123"
    events = {WebhookEvent.COMPLETED}
    sender = client_manager.make_webhook_sender(url, events)

    payload = {
        "status": Status.PROCESSING,
        "output": {"animal": "giraffe"},
        "input": {},
    }
    response = PredictionResponse(**payload)

    route = respx.post(url).mock(return_value=httpx.Response(200))

    await sender(response, WebhookEvent.LOGS)

    assert not route.called


@pytest.mark.asyncio
@respx.mock
async def test_webhook_caller_connection_errors(client_manager):
    url = "https://example.com/webhook/123"
    sender = client_manager.make_webhook_sender(url, WebhookEvent.default_events())

    payload = {
        "status": Status.PROCESSING,
        "output": {"animal": "giraffe"},
        "input": {},
    }
    response = PredictionResponse(**payload)

    route = respx.post(url).mock(side_effect=httpx.RequestError("Connection error"))

    # this should not raise an error
    await sender(response, WebhookEvent.COMPLETED)

    assert route.called
