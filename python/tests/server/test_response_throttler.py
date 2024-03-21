import time

from cog.schema import PredictionResponse, Status
from cog.server.response_throttler import ResponseThrottler

processing = PredictionResponse(input={}, status=Status.PROCESSING)
succeeded = PredictionResponse(input={}, status=Status.SUCCEEDED)


def test_zero_interval():
    throttler = ResponseThrottler(response_interval=0)

    assert throttler.should_send_response(processing)
    throttler.update_last_sent_response_time()
    assert throttler.should_send_response(succeeded)


def test_terminal_status():
    throttler = ResponseThrottler(response_interval=10)

    assert throttler.should_send_response(processing)
    throttler.update_last_sent_response_time()
    assert not throttler.should_send_response(processing)
    throttler.update_last_sent_response_time()
    assert throttler.should_send_response(succeeded)


def test_nonzero_internal():
    throttler = ResponseThrottler(response_interval=0.2)

    assert throttler.should_send_response(processing)
    throttler.update_last_sent_response_time()
    assert not throttler.should_send_response(processing)
    throttler.update_last_sent_response_time()

    time.sleep(0.3)

    assert throttler.should_send_response(processing)
