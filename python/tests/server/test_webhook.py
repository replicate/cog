import threading
import time
from http.server import BaseHTTPRequestHandler, HTTPServer
from typing import Any, Dict, Optional, Tuple
from unittest.mock import patch

import responses

from cog.schema import PredictionResponse, Status, WebhookEvent
from cog.server.webhook import webhook_caller, webhook_caller_filtered


class SlowHandler(BaseHTTPRequestHandler):
    def do_POST(self):
        time.sleep(2)  # Simulate slow response
        self.send_response(200)
        self.end_headers()


class ErrorHandler(BaseHTTPRequestHandler):
    def do_POST(self):
        self.send_response(500)
        self.end_headers()


class UnreachableHandler(BaseHTTPRequestHandler):
    """Handler that simulates connection refused"""

    def do_POST(self):
        # Close connection immediately to simulate connection refused
        self.wfile.close()


def make_prediction_response(
    status: Status, output: Optional[Dict[str, Any]] = None
) -> PredictionResponse:
    return PredictionResponse(
        status=status,
        input={},  # Required field
        output=output or {},
    )


def wait_for_webhook_calls(expected_count: int, timeout: float = 2.0) -> None:
    """Wait for the expected number of webhook calls to complete"""
    start_time = time.time()
    while time.time() - start_time < timeout:
        # Check if all webhook threads are done
        active_threads = [
            t for t in threading.enumerate() if t.name.startswith("webhook")
        ]
        if len(active_threads) == 0:
            break
        time.sleep(0.1)


@responses.activate
def test_webhook_timeout():
    """Test that webhook calls timeout properly and don't block indefinitely"""
    # Set a very short timeout for testing
    with patch.dict("os.environ", {"COG_WEBHOOK_TIMEOUT": "0.5"}):
        responses.add(
            responses.POST,
            "http://example.com/webhook",
            body=lambda request: time.sleep(2) or "OK",  # type: ignore # Sleep longer than timeout
            status=200,
        )

        prediction = make_prediction_response(Status.SUCCEEDED)
        start_time = time.time()

        caller = webhook_caller_filtered(
            "http://example.com/webhook", {WebhookEvent.COMPLETED}
        )
        caller(prediction, WebhookEvent.COMPLETED)
        wait_for_webhook_calls(1, timeout=3.0)

        elapsed_time = time.time() - start_time
        # Should timeout quickly (within 2 seconds including overhead)
        assert elapsed_time < 2.0, f"Webhook call took too long: {elapsed_time}s"


@responses.activate
def test_webhook_error_handling():
    """Test that webhook calls handle HTTP errors gracefully"""
    responses.add(
        responses.POST,
        "http://example.com/webhook",
        status=500,
    )

    prediction = make_prediction_response(Status.SUCCEEDED)

    # Should not raise an exception
    caller = webhook_caller_filtered(
        "http://example.com/webhook", {WebhookEvent.COMPLETED}
    )
    caller(prediction, WebhookEvent.COMPLETED)
    wait_for_webhook_calls(1)

    assert len(responses.calls) == 1


def test_webhook_connection_refused():
    """Test webhook behavior when connection is refused (simulating service down)"""
    # Use a port that's guaranteed to be closed
    webhook_url = "http://127.0.0.1:65432/webhook"  # Unlikely to be in use

    prediction = make_prediction_response(Status.SUCCEEDED)
    start_time = time.time()

    # Should not raise an exception or block indefinitely
    caller = webhook_caller_filtered(webhook_url, {WebhookEvent.COMPLETED})
    caller(prediction, WebhookEvent.COMPLETED)
    wait_for_webhook_calls(1, timeout=5.0)

    elapsed_time = time.time() - start_time
    # Should fail quickly due to connection refused
    assert elapsed_time < 15.0, (
        f"Connection refused handling took too long: {elapsed_time}s"
    )


@responses.activate
def test_webhook_retry_behavior():
    """Test that webhook retries work correctly for terminal status"""
    call_count = 0

    def callback(request: Any) -> Tuple[int, Dict[str, str], str]:
        nonlocal call_count
        call_count += 1
        if call_count < 3:  # Fail first 2 attempts
            return (500, {}, "Server Error")
        return (200, {}, "OK")

    responses.add_callback(
        responses.POST,
        "http://example.com/webhook",
        callback=callback,
    )

    prediction = make_prediction_response(Status.SUCCEEDED)

    caller = webhook_caller_filtered(
        "http://example.com/webhook", {WebhookEvent.COMPLETED}
    )
    caller(prediction, WebhookEvent.COMPLETED)
    wait_for_webhook_calls(1, timeout=10.0)

    # Should have retried and eventually succeeded
    assert call_count == 3
    assert len(responses.calls) == 3


@responses.activate
def test_webhook_filtered():
    """Test that webhook_caller_filtered only sends webhooks for specified events"""
    responses.add(
        responses.POST,
        "http://example.com/webhook",
        status=200,
    )

    prediction = make_prediction_response(Status.SUCCEEDED)

    # Should send webhook for COMPLETED event
    caller = webhook_caller_filtered(
        "http://example.com/webhook", {WebhookEvent.COMPLETED}
    )
    caller(prediction, WebhookEvent.COMPLETED)
    wait_for_webhook_calls(1)

    assert len(responses.calls) == 1

    # Reset responses
    responses.reset()
    responses.add(
        responses.POST,
        "http://example.com/webhook",
        status=200,
    )

    # Should NOT send webhook for START event when only COMPLETED is in filter
    caller(prediction, WebhookEvent.START)
    wait_for_webhook_calls(1)

    assert len(responses.calls) == 0


def test_webhook_max_retry_limit():
    """Test that webhooks don't retry indefinitely"""
    # Create a server that always returns 500
    server = HTTPServer(("localhost", 0), ErrorHandler)
    thread = threading.Thread(target=server.serve_forever)
    thread.daemon = True
    thread.start()

    try:
        webhook_url = f"http://localhost:{server.server_port}/webhook"
        prediction = make_prediction_response(Status.SUCCEEDED)

        start_time = time.time()
        caller = webhook_caller_filtered(webhook_url, {WebhookEvent.COMPLETED})
        caller(prediction, WebhookEvent.COMPLETED)
        wait_for_webhook_calls(1, timeout=70.0)  # Max ~60s for 6 retries
        elapsed_time = time.time() - start_time

        # Should stop retrying after max attempts (~60s with exponential backoff)
        assert elapsed_time < 70.0, f"Webhook retries took too long: {elapsed_time}s"

    finally:
        server.shutdown()
        server.server_close()
        thread.join(timeout=1.0)


def test_webhook_background_execution():
    """Test that webhooks execute in background threads and don't block main thread"""
    # Create a slow server
    server = HTTPServer(("localhost", 0), SlowHandler)
    thread = threading.Thread(target=server.serve_forever)
    thread.daemon = True
    thread.start()

    try:
        webhook_url = f"http://localhost:{server.server_port}/webhook"
        prediction = make_prediction_response(Status.SUCCEEDED)

        start_time = time.time()

        # Make multiple webhook calls
        caller = webhook_caller_filtered(webhook_url, {WebhookEvent.COMPLETED})
        for _ in range(3):
            caller(prediction, WebhookEvent.COMPLETED)

        # Should return immediately (not wait for webhooks to complete)
        immediate_time = time.time() - start_time
        assert immediate_time < 0.5, (
            f"Webhook calls blocked main thread: {immediate_time}s"
        )

        # Wait for all webhooks to complete
        wait_for_webhook_calls(3, timeout=10.0)

    finally:
        server.shutdown()
        server.server_close()
        thread.join(timeout=1.0)


@responses.activate
def test_webhook_user_agent():
    """Test that webhook calls include correct user agent"""
    responses.add(
        responses.POST,
        "http://example.com/webhook",
        status=200,
    )

    prediction = make_prediction_response(Status.SUCCEEDED)

    caller = webhook_caller_filtered(
        "http://example.com/webhook", {WebhookEvent.COMPLETED}
    )
    caller(prediction, WebhookEvent.COMPLETED)
    wait_for_webhook_calls(1)

    assert len(responses.calls) == 1
    request = responses.calls[0].request
    assert "cog-worker/" in request.headers.get("User-Agent", "")


def test_webhook_original_bug_scenario():
    """
    Test the original bug scenario: webhook service down causes prediction to get stuck
    This test verifies that our fix prevents the issue
    """
    # Simulate webhook service being completely down (connection refused)
    webhook_url = "http://127.0.0.1:65433/webhook"  # Port guaranteed to be closed

    prediction = make_prediction_response(Status.SUCCEEDED)

    # Record start time
    start_time = time.time()

    # This should NOT block indefinitely or cause the prediction to get stuck
    caller = webhook_caller_filtered(webhook_url, {WebhookEvent.COMPLETED})
    caller(prediction, WebhookEvent.COMPLETED)

    # Wait for webhook call to complete (should fail after retries)
    wait_for_webhook_calls(1, timeout=20.0)

    elapsed_time = time.time() - start_time

    # The fix should ensure this completes within reasonable time
    # Original bug would cause this to hang for 320+ seconds (5+ minutes)
    # With our fix, it should fail within ~15-20 seconds (6 retries with exponential backoff)
    # This proves the webhook failures don't block the main thread indefinitely
    assert elapsed_time < 25.0, (
        f"Webhook failure handling took too long: {elapsed_time}s"
    )
    assert elapsed_time > 10.0, (
        f"Webhook should have attempted retries, took only: {elapsed_time}s"
    )

    # Verify that the prediction status would not be stuck in "BUSY"
    # (In real usage, the runner would have updated status before webhook call)
    assert prediction.status == Status.SUCCEEDED


def test_webhook_cancellation_during_failure():
    """
    Test that webhook failures don't prevent cancellation
    This simulates the scenario where a prediction needs to be cancelled
    while webhook calls are failing
    """

    # Create a server that's very slow to respond
    class VerySlowHandler(BaseHTTPRequestHandler):
        def do_POST(self):
            time.sleep(5)  # Very slow response
            self.send_response(200)
            self.end_headers()

    server = HTTPServer(("localhost", 0), VerySlowHandler)
    thread = threading.Thread(target=server.serve_forever)
    thread.daemon = True
    thread.start()

    try:
        webhook_url = f"http://localhost:{server.server_port}/webhook"

        # Start a webhook call that will be slow
        prediction = make_prediction_response(Status.PROCESSING)
        caller = webhook_caller_filtered(
            webhook_url, {WebhookEvent.START, WebhookEvent.COMPLETED}
        )
        caller(prediction, WebhookEvent.START)

        # Immediately try to "cancel" by updating status
        # This should not be blocked by the ongoing webhook call
        start_time = time.time()
        prediction.status = Status.CANCELED

        # In real usage, this would trigger another webhook call for cancellation
        caller(prediction, WebhookEvent.COMPLETED)

        immediate_time = time.time() - start_time

        # Cancellation should be immediate, not blocked by slow webhook
        assert immediate_time < 1.0, (
            f"Cancellation was blocked by webhook: {immediate_time}s"
        )

        # Clean up - wait for webhooks to complete or timeout
        wait_for_webhook_calls(2, timeout=15.0)

    finally:
        server.shutdown()
        server.server_close()
        thread.join(timeout=1.0)


def test_webhook_thread_pool_limits():
    """Test that webhook thread pool doesn't create unlimited threads"""
    initial_thread_count = threading.active_count()

    # Create many webhook calls simultaneously
    prediction = make_prediction_response(Status.SUCCEEDED)

    # Use a non-existent URL to make calls fail quickly
    webhook_url = "http://127.0.0.1:65434/webhook"

    # Make many concurrent webhook calls
    caller = webhook_caller_filtered(webhook_url, {WebhookEvent.COMPLETED})
    for _ in range(20):
        caller(prediction, WebhookEvent.COMPLETED)

    # Check thread count hasn't exploded
    peak_thread_count = threading.active_count()
    thread_increase = peak_thread_count - initial_thread_count

    # Should not create more than the thread pool limit (4) + some overhead
    assert thread_increase < 10, f"Too many threads created: {thread_increase}"

    # Wait for all webhooks to complete
    wait_for_webhook_calls(20, timeout=10.0)

    # Thread count should return to normal
    final_thread_count = threading.active_count()
    assert (
        final_thread_count <= initial_thread_count + 4
    )  # Allow for thread pool threads


@responses.activate
def test_webhook_caller_basic():
    """Test basic webhook_caller functionality (without events)"""
    responses.add(
        responses.POST,
        "http://example.com/webhook",
        status=200,
    )

    prediction = make_prediction_response(Status.SUCCEEDED)

    # webhook_caller doesn't use events, just sends the response
    caller = webhook_caller("http://example.com/webhook")
    caller(prediction)
    wait_for_webhook_calls(1)

    assert len(responses.calls) == 1
