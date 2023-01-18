from typing import Callable, Optional, Tuple

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
        log.debug("waiting for message", queue=self.redis_input_queue)

        # first, try to autoclaim old messages from pending queue
        raw_messages = self.redis.execute_command(
            "XAUTOCLAIM",
            self.redis_input_queue,
            self.redis_input_queue,
            self.redis_consumer_id,
            str(self.autoclaim_messages_after * 1000),
            "0-0",
            "COUNT",
            1,
        )
        # format: [[b'1619393873567-0', [b'mykey', b'myval']]]
        # since redis==4.3.4 an empty response from xautoclaim is indicated by [[b'0-0', []]]
        if raw_messages and raw_messages[0] is not None and len(raw_messages[0]) == 2:
            key, raw_message = raw_messages[0]
            assert raw_message[0] == b"value"

            message_id = key.decode()
            message = raw_message[1].decode()
            log.info(
                "received message", message_id=message_id, queue=self.redis_input_queue
            )
            return message_id, message

        # if no old messages exist, get message from main queue
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
        log.info(
            "received message", message_id=message_id, queue=self.redis_input_queue
        )
        return message_id, message

    def ack(self, message_id: str) -> None:
        self.redis.xack(self.redis_input_queue, self.redis_input_queue, message_id)
        self.redis.xdel(self.redis_input_queue, message_id)
        log.info("acked message", message_id=message_id, queue=self.redis_input_queue)

    def checker(self, redis_key: str) -> Callable:
        def checker_() -> bool:
            return redis_key is not None and self.redis.exists(redis_key) > 0

        return checker_
