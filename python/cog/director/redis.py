from typing import Callable, List, Optional, Tuple

import redis
import structlog
import threading
import time

log = structlog.get_logger(__name__)


class EmptyRedisStream(Exception):
    pass


class RedisConsumer:
    def __init__(
        self,
        redis_url: str,
        redis_input_queue: str,
        redis_consumer_id: str,
        predict_timeout: Optional[int] = None,
    ):
        self.redis_url = redis_url
        self.redis_input_queue = redis_input_queue
        self.redis_consumer_id = redis_consumer_id
        self.message_id = None

        self.redis = redis.from_url(self.redis_url)
        log.info(
            "connected to redis",
            host=self.redis.get_connection_kwargs().get("host"),
        )

    def get(self) -> str:
        assert self.message_id is None

        # Redis streams are reliable queues: messages must be acked once
        # handled, otherwise they will remain in the queue where they can be
        # claimed by other consumers after a visibility timeout.
        #
        # At the moment, we try to ack every message we receive, even if we
        # encounter an exception while handling it. We do this because the
        # semantics of restarting a prediction are not well defined, and
        # because in truly exceptional cases where the messages go unacked, the
        # predictions will be failed by `terminate_stuck_predictions` in
        # replicate-web, and the unacked messages will be purged by
        # `pruneStuckRequests` in autoscaler.
        #
        # None of this is ideal, and in future we likely want to improve
        # director's ability to recover and retry predictions that failed
        # exceptionally.
        try:
            raw_messages = self.redis.xreadgroup(
                groupname=self.redis_input_queue,
                consumername=self.redis_consumer_id,
                streams={self.redis_input_queue: ">"},
                count=1,
                block=1000,
            )
        except redis.exceptions.ResponseError as e:
            # treat a missing queue the same as an empty queue
            if str(e).startswith("NOGROUP No such key"):
                raise EmptyRedisStream()
            else:
                raise

        if not raw_messages:
            raise EmptyRedisStream()

        # format: [[b'mystream', [(b'1619395583065-0', {b'mykey': b'myval6'})]]]
        key, raw_message = raw_messages[0][1][0]
        self.message_id = key.decode()
        message = raw_message[b"value"].decode()

        self._claimer = RedisMessageClaimer(
            claim_message=self.claim_message(self.message_id)
        )
        self._claimer.start()

        return message

    def ack(self) -> None:
        assert self.message_id is not None

        self.redis.xack(self.redis_input_queue, self.redis_input_queue, self.message_id)
        self.redis.xdel(self.redis_input_queue, self.message_id)
        self._claimer.stop()

        self.message_id = None
        self._claimer = None

    def checker(self, redis_key: str) -> Callable:
        def checker_() -> bool:
            return redis_key is not None and self.redis.exists(redis_key) > 0

        return checker_

    def claim_message(self, message_id: str) -> Callable:
        def claim_message_() -> None:
            self.redis.xclaim(
                name=self.redis_input_queue,
                groupname=self.redis_input_queue,
                consumername=self.redis_consumer_id,
                min_idle_time=0,
                message_ids=[self.message_id],
                force=True,
                justid=True,
            )

        return claim_message_


class RedisConsumerRotator:
    def __init__(self, consumers: List[RedisConsumer]):
        if len(consumers) == 0:
            raise ValueError("Must provide at least one RedisConsumer")

        self.consumers = consumers
        self._current_consumer_index = 0

    def get_current(self) -> RedisConsumer:
        return self.consumers[self._current_consumer_index]

    def rotate(self) -> None:
        consumer_count = len(self.consumers)
        self._current_consumer_index = (
            self._current_consumer_index + 1
        ) % consumer_count


class RedisMessageClaimer:
    def __init__(self, claim_message: Callable, claim_interval: int = 2):
        self._thread = None
        self._should_exit = threading.Event()
        self._claim_message = claim_message
        self._claim_interval = claim_interval

    def start(self) -> None:
        self._thread = threading.Thread(target=self._run)
        self._thread.start()

    def stop(self) -> None:
        self._should_exit.set()
        if self._thread is not None:
            self._thread.join()

    def _run(self) -> None:
        last_claimed_at = time.perf_counter()

        while not self._should_exit.is_set():
            now = time.perf_counter()
            if now >= last_claimed_at + self._claim_interval:
                self._claim_message()
                last_claimed_at = now

            # Only sleep for a short time to respect _should_exit
            time.sleep(0.01)
