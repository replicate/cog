# SPDX-License-Identifier: MIT OR Apache-2.0
# This file is dual licensed under the terms of the Apache License, Version
# 2.0, and the MIT License.  See the LICENSE file in the root of this
# repository for complete details.

"""
Generic utilities.
"""

from __future__ import annotations

import errno
import sys

from contextlib import suppress
from typing import Any, Callable


def until_not_interrupted(f: Callable[..., Any], *args: Any, **kw: Any) -> Any:
    """
    Retry until *f* succeeds or an exception that isn't caused by EINTR occurs.

    :param f: A callable like a function.
    :param *args: Positional arguments for *f*.
    :param **kw: Keyword arguments for *f*.
    """
    while True:
        try:
            return f(*args, **kw)
        except OSError as e:
            if e.args[0] == errno.EINTR:
                continue
            raise


def get_processname() -> str:
    # based on code from
    # https://github.com/python/cpython/blob/313f92a57bc3887026ec16adb536bb2b7580ce47/Lib/logging/__init__.py#L342-L352
    processname = "n/a"
    mp: Any = sys.modules.get("multiprocessing")
    if mp is not None:
        # Errors may occur if multiprocessing has not finished loading
        # yet - e.g. if a custom import hook causes third-party code
        # to run when multiprocessing calls import.
        with suppress(Exception):
            processname = mp.current_process().name

    return processname
