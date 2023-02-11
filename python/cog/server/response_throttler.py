import time

from ..schema import Status


class ResponseThrottler:
    def __init__(self, response_interval: float) -> None:
        self.last_sent_response_time = 0.0
        self.response_interval = response_interval

    def should_send_response(self, response: dict) -> bool:
        if Status.is_terminal(response["status"]):
            return True

        return self.seconds_since_last_response() >= self.response_interval

    def update_last_sent_response_time(self) -> None:
        self.last_sent_response_time = time.time()

    def seconds_since_last_response(self) -> float:
        return time.time() - self.last_sent_response_time
