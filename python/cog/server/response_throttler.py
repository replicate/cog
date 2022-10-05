import time
import os

from ..response import Status


class ResponseThrottler:
    def __init__(self):
        self.last_sent_response_time = 0
        self.response_interval = float(
            os.environ.get("COG_THROTTLE_RESPONSE_INTERVAL", 0)
        )

    def should_send_response(self, response):
        if Status.is_terminal(response["status"]):
            return True

        seconds_since_last_response = time.time() - self.last_sent_response_time
        return seconds_since_last_response >= self.response_interval

    def update_last_sent_response_time(self):
        self.last_sent_response_time = time.time()
