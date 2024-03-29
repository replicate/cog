import asyncio
import random
from datetime import datetime
from typing import Iterable, Mapping, Optional, Union

import httpx


# Adapted from https://github.com/encode/httpx/issues/108#issuecomment-1132753155
# via https://github.com/replicate/replicate-python/blob/main/replicate/client.py
class RetryTransport(httpx.AsyncBaseTransport):
    """A custom HTTP transport that automatically retries requests using an exponential backoff strategy
    for specific HTTP status codes and request methods.
    """

    RETRYABLE_METHODS = frozenset(["HEAD", "GET", "PUT", "DELETE", "OPTIONS", "TRACE"])
    RETRYABLE_STATUS_CODES = frozenset(
        [
            429,  # Too Many Requests
            503,  # Service Unavailable
            504,  # Gateway Timeout
        ]
    )
    MAX_BACKOFF_WAIT = 60

    def __init__(  # pylint: disable=too-many-arguments
        self,
        *,
        max_attempts: int = 10,
        max_backoff_wait: float = MAX_BACKOFF_WAIT,
        backoff_factor: float = 0.1,
        jitter_ratio: float = 0.1,
        retryable_methods: Optional[Iterable[str]] = None,
        retry_status_codes: Optional[Iterable[int]] = None,
        verify: httpx._types.VerifyTypes = True,
    ) -> None:
        self._wrapped_transport = httpx.AsyncHTTPTransport(verify=verify)

        if jitter_ratio < 0 or jitter_ratio > 0.5:
            raise ValueError(
                f"jitter ratio should be between 0 and 0.5, actual {jitter_ratio}"
            )

        self.max_attempts = max_attempts
        self.backoff_factor = backoff_factor
        self.retryable_methods = (
            frozenset(retryable_methods)
            if retryable_methods
            else self.RETRYABLE_METHODS
        )
        self.retry_status_codes = (
            frozenset(retry_status_codes)
            if retry_status_codes
            else self.RETRYABLE_STATUS_CODES
        )
        self.jitter_ratio = jitter_ratio
        self.max_backoff_wait = max_backoff_wait

    def _calculate_sleep(
        self, attempts_made: int, headers: Union[httpx.Headers, Mapping[str, str]]
    ) -> float:
        retry_after_header = (headers.get("Retry-After") or "").strip()
        if retry_after_header:
            if retry_after_header.isdigit():
                return float(retry_after_header)

            try:
                parsed_date = datetime.fromisoformat(retry_after_header).astimezone()
                diff = (parsed_date - datetime.now().astimezone()).total_seconds()
                if diff > 0:
                    return min(diff, self.max_backoff_wait)
            except ValueError:
                pass

        backoff = self.backoff_factor * (2 ** (attempts_made - 1))
        jitter = (backoff * self.jitter_ratio) * random.choice([1, -1])  # noqa: S311
        total_backoff = backoff + jitter
        return min(total_backoff, self.max_backoff_wait)

    async def handle_async_request(self, request: httpx.Request) -> httpx.Response:
        response = await self._wrapped_transport.handle_async_request(request)  # type: ignore

        if request.method not in self.retryable_methods:
            return response

        remaining_attempts = self.max_attempts - 1
        attempts_made = 1

        while True:
            if (
                remaining_attempts < 1
                or response.status_code not in self.retry_status_codes
            ):
                return response

            await response.aclose()

            sleep_for = self._calculate_sleep(attempts_made, response.headers)
            await asyncio.sleep(sleep_for)

            response = await self._wrapped_transport.handle_async_request(request)  # type: ignore

            attempts_made += 1
            remaining_attempts -= 1

    async def aclose(self) -> None:
        await self._wrapped_transport.aclose()  # type: ignore
