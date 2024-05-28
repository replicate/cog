import os
from unittest import mock

import requests
from cog.server.telemetry import requests_session
from opentelemetry.instrumentation.requests import RequestsInstrumentor


def test_otel_requests_session():
    os.environ["OTEL_SERVICE_NAME"] = "cog"
    session = requests_session()
    assert hasattr(session.send, "opentelemetry_instrumentation_requests_applied")
    assert session.send.opentelemetry_instrumentation_requests_applied
    os.environ.pop("OTEL_SERVICE_NAME")

    RequestsInstrumentor.uninstrument_session(session)
    assert not hasattr(
        requests.Session.send, "opentelemetry_instrumentation_requests_applied"
    )

    RequestsInstrumentor.uninstrument_session(session)
    assert not hasattr(session.send, "opentelemetry_instrumentation_requests_applied")


def test_otel_requests_session_without_environment():
    session = requests_session()
    assert not hasattr(session, "opentelemetry_instrumentation_requests_applied")


def test_otel_requests_session_default_client():
    os.environ["OTEL_SERVICE_NAME"] = "cog"
    _ = requests_session()
    assert not hasattr(
        requests.Session.send, "opentelemetry_instrumentation_requests_applied"
    )
    assert not hasattr(
        requests.Session().send, "opentelemetry_instrumentation_requests_applied"
    )
    os.unsetenv("OTEL_SERVICE_NAME")


@mock.patch(
    "requests.Session.send",
)
def test_header_propagation(mock_send: mock.Mock):
    os.environ["OTEL_SERVICE_NAME"] = "cog"
    session = requests_session()

    session.get("http://example.com")
    assert mock_send.call_count == 1
    assert "traceparent" in mock_send.call_args.args[-1].headers

    os.environ.pop("OTEL_SERVICE_NAME")
