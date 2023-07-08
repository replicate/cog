import logging
import os

import structlog
from structlog.typing import EventDict


def replace_level_with_severity(
    _: logging.Logger, __: str, event_dict: EventDict
) -> EventDict:
    """
    Replace the level field with a severity field as understood by Stackdriver
    logs.
    """
    if "level" in event_dict:
        event_dict["severity"] = event_dict.pop("level").upper()
    return event_dict


def setup_logging(*, log_level: int = logging.NOTSET) -> None:
    """
    Configure stdlib logger to use structlog processors and formatters so that
    uvicorn and application logs are consistent.
    """

    # Switch to human-friendly log output if LOG_FORMAT environment variable is
    # set to "development".
    development_logs = os.environ.get("LOG_FORMAT", "") == "development"

    processors: list[structlog.types.Processor] = [
        structlog.contextvars.merge_contextvars,
        structlog.stdlib.add_logger_name,
        structlog.stdlib.add_log_level,
        structlog.processors.StackInfoRenderer(),
        structlog.processors.TimeStamper(fmt="iso"),
    ]

    if development_logs:
        # In development, set `exc_info` on the log event if the log method is
        # `exception` and `exc_info` is not already set.
        #
        # Rendering of `exc_info` is handled by ConsoleRenderer.
        processors.append(structlog.dev.set_exc_info)
    else:
        # Outside of development mode `exc_info` must be set explicitly when
        # needed, and is translated into a formatted `exception` field.
        processors.append(structlog.processors.format_exc_info)
        # Set `severity`, not `level`, for compatibility with Google
        # Stackdriver logging expectations.
        processors.append(replace_level_with_severity)

    # Stackdriver logging expects a "message" field, not "event"
    processors.append(structlog.processors.EventRenamer("message"))

    structlog.configure(
        processors=processors
        + [structlog.stdlib.ProcessorFormatter.wrap_for_formatter],
        logger_factory=structlog.stdlib.LoggerFactory(),
        cache_logger_on_first_use=True,
    )

    if development_logs:
        log_renderer = structlog.dev.ConsoleRenderer(event_key="message")  # type: ignore
    else:
        log_renderer = structlog.processors.JSONRenderer()  # type: ignore

    formatter = structlog.stdlib.ProcessorFormatter(
        foreign_pre_chain=processors,
        processors=[
            structlog.stdlib.ProcessorFormatter.remove_processors_meta,
            log_renderer,
        ],
    )

    handler = logging.StreamHandler()
    handler.setFormatter(formatter)

    root = logging.getLogger()
    root.addHandler(handler)
    root.setLevel(log_level)

    # Propagate uvicorn logs instead of letting uvicorn configure the format
    for name in ["uvicorn", "uvicorn.access", "uvicorn.error"]:
        logging.getLogger(name).handlers.clear()
        logging.getLogger(name).propagate = True

    # Reconfigure log levels for some overly chatty libraries
    logging.getLogger("uvicorn.access").setLevel(logging.WARNING)
    logging.getLogger("urllib3.connectionpool").setLevel(logging.ERROR)
