# SPDX-License-Identifier: MIT OR Apache-2.0
# This file is dual licensed under the terms of the Apache License, Version
# 2.0, and the MIT License.  See the LICENSE file in the root of this
# repository for complete details.


from __future__ import annotations

from cog._vendor.structlog import (
    contextvars,
    dev,
    processors,
    stdlib,
    testing,
    threadlocal,
    tracebacks,
    types,
    typing,
)
from cog._vendor.structlog._base import BoundLoggerBase, get_context
from cog._vendor.structlog._config import (
    configure,
    configure_once,
    get_config,
    get_logger,
    getLogger,
    is_configured,
    reset_defaults,
    wrap_logger,
)
from cog._vendor.structlog._generic import BoundLogger
from cog._vendor.structlog._log_levels import make_filtering_bound_logger
from cog._vendor.structlog._output import (
    BytesLogger,
    BytesLoggerFactory,
    PrintLogger,
    PrintLoggerFactory,
    WriteLogger,
    WriteLoggerFactory,
)
from cog._vendor.structlog.exceptions import DropEvent
from cog._vendor.structlog.testing import ReturnLogger, ReturnLoggerFactory


try:
    from cog._vendor.structlog import twisted
except ImportError:
    twisted = None  # type: ignore[assignment]


__title__ = "structlog"

__author__ = "Hynek Schlawack"

__license__ = "MIT or Apache License, Version 2.0"
__copyright__ = "Copyright (c) 2013 " + __author__


__all__ = [
    "BoundLogger",
    "BoundLoggerBase",
    "BytesLogger",
    "BytesLoggerFactory",
    "configure_once",
    "configure",
    "contextvars",
    "dev",
    "DropEvent",
    "get_config",
    "get_context",
    "get_logger",
    "getLogger",
    "is_configured",
    "make_filtering_bound_logger",
    "PrintLogger",
    "PrintLoggerFactory",
    "processors",
    "reset_defaults",
    "ReturnLogger",
    "ReturnLoggerFactory",
    "stdlib",
    "testing",
    "threadlocal",
    "tracebacks",
    "twisted",
    "types",
    "typing",
    "wrap_logger",
    "WriteLogger",
    "WriteLoggerFactory",
]


def __getattr__(name: str) -> str:
    dunder_to_metadata = {
        "__version__": "version",
        "__description__": "summary",
        "__uri__": "",
        "__email__": "",
    }
    if name not in dunder_to_metadata.keys():
        raise AttributeError(f"module {__name__} has no attribute {name}")

    import sys
    import warnings

    if sys.version_info < (3, 8):
        from importlib_metadata import metadata
    else:
        from importlib.metadata import metadata

    warnings.warn(
        f"Accessing structlog.{name} is deprecated and will be "
        "removed in a future release. Use importlib.metadata directly "
        "to query for structlog's packaging metadata.",
        DeprecationWarning,
        stacklevel=2,
    )

    meta = metadata("structlog")

    if name == "__uri__":
        return meta["Project-URL"].split(" ", 1)[-1]

    if name == "__email__":
        return meta["Author-email"].split("<", 1)[1].rstrip(">")

    return meta[dunder_to_metadata[name]]
