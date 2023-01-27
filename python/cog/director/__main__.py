import logging
import os
import queue
import signal
import sys
from argparse import ArgumentParser
from typing import Any

import structlog
import uvicorn

from ..logging import setup_logging
from .director import Director
from .healthchecker import Healthchecker, http_fetcher
from .http import Server, create_app
from .redis import RedisConsumer

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

events: queue.Queue = queue.Queue(maxsize=128)

config = uvicorn.Config(create_app(events=events), port=4900, log_config=None)
server = Server(config)
server.start()

healthchecker = Healthchecker(
    events=events, fetcher=http_fetcher("http://localhost:5000/health-check")
)
healthchecker.start()

redis_consumer = RedisConsumer(
    redis_url=args.redis_url,
    redis_input_queue=args.redis_input_queue,
    redis_consumer_id=args.redis_consumer_id,
    predict_timeout=args.predict_timeout,
)

director = Director(
    events=events,
    healthchecker=healthchecker,
    redis_consumer=redis_consumer,
    predict_timeout=args.predict_timeout,
    max_failure_count=args.max_failure_count,
    report_setup_run_url=args.report_setup_run_url,
)
director.register_shutdown_hook(healthchecker.stop)
director.register_shutdown_hook(server.stop)
director.start()

healthchecker.join()
server.join()
