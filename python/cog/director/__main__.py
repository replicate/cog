import logging
import queue
import os
import signal
import sys
from argparse import ArgumentParser
from typing import Any

import structlog
import uvicorn

from ..logging import setup_logging
from .redis import RedisConsumer
from .director import Director

log = structlog.get_logger("cog.director")


def _die(signum: Any, frame: Any) -> None:
    log.warning("caught early SIGTERM: exiting immediately!")
    sys.exit(1)


# We are probably running as PID 1 so need to explicitly register a handler
# to die on SIGTERM. This will be overwritten once we start uvicorn.
signal.signal(signal.SIGINT, _die)
signal.signal(signal.SIGTERM, _die)

parser = ArgumentParser()

parser.add_argument("--redis-url", required=True)
parser.add_argument("--redis-input-queue", required=True)
parser.add_argument("--redis-consumer-id", required=True)
parser.add_argument("--predict-timeout", type=int, default=1800)
parser.add_argument(
    "--max-failure-count",
    type=int,
    default=5,
    help="Maximum number of consecutive failures before the worker should exit",
)
parser.add_argument("--report-setup-run-url")

log_level = logging.getLevelName(os.environ.get("LOG_LEVEL", "INFO").upper())
setup_logging(log_level=log_level)

args = parser.parse_args()

redis_consumer = RedisConsumer(
    redis_url=args.redis_url,
    redis_input_queue=args.redis_input_queue,
    redis_consumer_id=args.redis_consumer_id,
    predict_timeout=args.predict_timeout,
)
director = Director(
    events=queue.Queue(maxsize=128),
    redis_consumer=redis_consumer,
    predict_timeout=args.predict_timeout,
    max_failure_count=args.max_failure_count,
    report_setup_run_url=args.report_setup_run_url,
)
director.start()
