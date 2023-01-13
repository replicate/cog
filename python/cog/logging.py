import logging

import structlog


def setup_logging(*, log_level: int = logging.NOTSET) -> None:
    """
    Configure stdlib logger to use structlog processors and formatters so that
    uvicorn and application logs are consistent.
    """

    processors: list[structlog.types.Processor] = [
        structlog.contextvars.merge_contextvars,
        structlog.stdlib.add_logger_name,
        structlog.stdlib.add_log_level,
        structlog.processors.StackInfoRenderer(),
        structlog.dev.set_exc_info,
        structlog.processors.TimeStamper(fmt="iso"),
    ]

    structlog.configure(
        processors=processors
        + [structlog.stdlib.ProcessorFormatter.wrap_for_formatter],
        logger_factory=structlog.stdlib.LoggerFactory(),
        cache_logger_on_first_use=True,
    )

    # TODO: emit JSON logs in production
    log_renderer = structlog.dev.ConsoleRenderer()

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
    for l in ["uvicorn", "uvicorn.access", "uvicorn.error"]:
        logging.getLogger(l).handlers.clear()
        logging.getLogger(l).propagate = True
