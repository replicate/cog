import requests
import responses
from cog.schema import WebhookEvent
from cog.server.webhook import webhook_caller, webhook_caller_filtered
from responses import registries


@responses.activate
def test_webhook_caller_basic():
    c = webhook_caller("https://example.com/webhook/123")

    responses.post(
        "https://example.com/webhook/123",
        json={"status": "processing", "animal": "giraffe"},
        status=200,
    )

    c({"status": "processing", "animal": "giraffe"})


@responses.activate
def test_webhook_caller_non_terminal_does_not_retry():
    c = webhook_caller("https://example.com/webhook/123")

    responses.post(
        "https://example.com/webhook/123",
        json={"status": "processing", "animal": "giraffe"},
        status=429,
    )

    c({"status": "processing", "animal": "giraffe"})


@responses.activate(registry=registries.OrderedRegistry)
def test_webhook_caller_terminal_retries():
    c = webhook_caller("https://example.com/webhook/123")
    resps = []

    for _ in range(2):
        resps.append(
            responses.post(
                "https://example.com/webhook/123",
                json={"status": "succeeded", "animal": "giraffe"},
                status=429,
            )
        )
    resps.append(
        responses.post(
            "https://example.com/webhook/123",
            json={"status": "succeeded", "animal": "giraffe"},
            status=200,
        )
    )

    c({"status": "succeeded", "animal": "giraffe"})

    assert all(r.call_count == 1 for r in resps)


@responses.activate
def test_webhook_includes_user_agent():
    c = webhook_caller("https://example.com/webhook/123")

    responses.post(
        "https://example.com/webhook/123",
        json={"status": "processing", "animal": "giraffe"},
        status=200,
    )

    c({"status": "processing", "animal": "giraffe"})

    assert len(responses.calls) == 1
    user_agent = responses.calls[0].request.headers["user-agent"]
    assert user_agent.startswith("cog-worker/")


@responses.activate
def test_webhook_caller_filtered_basic():
    events = WebhookEvent.default_events()
    c = webhook_caller_filtered("https://example.com/webhook/123", events)

    responses.post(
        "https://example.com/webhook/123",
        json={"status": "processing", "animal": "giraffe"},
        status=200,
    )

    c({"status": "processing", "animal": "giraffe"}, WebhookEvent.LOGS)


@responses.activate
def test_webhook_caller_filtered_omits_filtered_events():
    events = {WebhookEvent.COMPLETED}
    c = webhook_caller_filtered("https://example.com/webhook/123", events)

    c({"status": "processing", "animal": "giraffe"}, WebhookEvent.LOGS)


@responses.activate
def test_webhook_caller_connection_errors():
    connerror_resp = responses.Response(
        responses.POST,
        "https://example.com/webhook/123",
        status=200,
    )
    connerror_exc = requests.ConnectionError("failed to connect")
    connerror_exc.response = connerror_resp
    connerror_resp.body = connerror_exc
    responses.add(connerror_resp)

    c = webhook_caller("https://example.com/webhook/123")
    # this should not raise an error
    c({"status": "processing", "animal": "giraffe"})
