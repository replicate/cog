from typing import Callable, List, Optional, Tuple

import redis
import structlog

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

        if predict_timeout is not None:
            # 30s grace period allows final responses to be sent and job to be acked
            self.autoclaim_messages_after = predict_timeout + 30
        else:
            # retry after 10 minutes by default
            self.autoclaim_messages_after = 10 * 60

        self.redis = redis.from_url(self.redis_url)
        log.info("connected to redis", url=self.redis_url)

    def get(self) -> Tuple[str, str]:
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
        raw_messages = self.redis.xreadgroup(
            groupname=self.redis_input_queue,
            consumername=self.redis_consumer_id,
            streams={self.redis_input_queue: ">"},
            count=1,
            block=1000,
        )
        if not raw_messages:
            raise EmptyRedisStream()

        # format: [[b'mystream', [(b'1619395583065-0', {b'mykey': b'myval6'})]]]
        key, raw_message = raw_messages[0][1][0]
        message_id = key.decode()
        message = raw_message[b"value"].decode()
        return message_id, message

    def ack(self, message_id: str) -> None:
        self.redis.xack(self.redis_input_queue, self.redis_input_queue, message_id)
        self.redis.xdel(self.redis_input_queue, message_id)

    def checker(self, redis_key: str) -> Callable:
        def checker_() -> bool:
            return redis_key is not None and self.redis.exists(redis_key) > 0

        return checker_


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
