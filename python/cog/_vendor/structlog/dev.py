# SPDX-License-Identifier: MIT OR Apache-2.0
# This file is dual licensed under the terms of the Apache License, Version
# 2.0, and the MIT License.  See the LICENSE file in the root of this
# repository for complete details.

"""
Helpers that make development with *structlog* more pleasant.

See also the narrative documentation in `development`.
"""

from __future__ import annotations

import sys
import warnings

from io import StringIO
from typing import Any, Iterable, TextIO, Type, Union

from ._frames import _format_exception
from .processors import _figure_out_exc_info
from .typing import EventDict, ExceptionRenderer, ExcInfo, WrappedLogger


if sys.version_info >= (3, 8):
    from typing import Protocol
else:
    from cog._vendor.typing_extensions import Protocol


try:
    import colorama
except ImportError:
    colorama = None

try:
    import better_exceptions
except ImportError:
    better_exceptions = None

try:
    import rich

    from rich.console import Console
    from rich.traceback import Traceback
except ImportError:
    rich = None  # type: ignore[assignment]


__all__ = [
    "ConsoleRenderer",
    "plain_traceback",
    "rich_traceback",
    "better_traceback",
]

_IS_WINDOWS = sys.platform == "win32"

_MISSING = "{who} requires the {package} package installed.  "
_EVENT_WIDTH = 30  # pad the event name to so many characters


def _pad(s: str, length: int) -> str:
    """
    Pads *s* to length *length*.
    """
    missing = length - len(s)

    return s + " " * (missing if missing > 0 else 0)


if colorama is not None:
    RESET_ALL = colorama.Style.RESET_ALL
    BRIGHT = colorama.Style.BRIGHT
    DIM = colorama.Style.DIM
    RED = colorama.Fore.RED
    BLUE = colorama.Fore.BLUE
    CYAN = colorama.Fore.CYAN
    MAGENTA = colorama.Fore.MAGENTA
    YELLOW = colorama.Fore.YELLOW
    GREEN = colorama.Fore.GREEN
    RED_BACK = colorama.Back.RED
else:
    # These are the same values as the Colorama color codes. Redefining them
    # here allows users to specify that they want color without having to
    # install Colorama, which is only supposed to be necessary in Windows.
    RESET_ALL = "\033[0m"
    BRIGHT = "\033[1m"
    DIM = "\033[2m"
    RED = "\033[31m"
    BLUE = "\033[34m"
    CYAN = "\033[36m"
    MAGENTA = "\033[35m"
    YELLOW = "\033[33m"
    GREEN = "\033[32m"
    RED_BACK = "\033[41m"


if _IS_WINDOWS:  # pragma: no cover
    # On Windows, use colors by default only if Colorama is installed.
    _has_colors = colorama is not None
else:
    # On other OSes, use colors by default.
    _has_colors = True

# Prevent breakage of packages that used the old name of the variable.
_use_colors = _has_colors


class _Styles(Protocol):
    reset: str
    bright: str
    level_critical: str
    level_exception: str
    level_error: str
    level_warn: str
    level_info: str
    level_debug: str
    level_notset: str

    timestamp: str
    logger_name: str
    kv_key: str
    kv_value: str


Styles = Union[_Styles, Type[_Styles]]


class _ColorfulStyles:
    reset = RESET_ALL
    bright = BRIGHT

    level_critical = RED
    level_exception = RED
    level_error = RED
    level_warn = YELLOW
    level_info = GREEN
    level_debug = GREEN
    level_notset = RED_BACK

    timestamp = DIM
    logger_name = BLUE
    kv_key = CYAN
    kv_value = MAGENTA


class _PlainStyles:
    reset = ""
    bright = ""

    level_critical = ""
    level_exception = ""
    level_error = ""
    level_warn = ""
    level_info = ""
    level_debug = ""
    level_notset = ""

    timestamp = ""
    logger_name = ""
    kv_key = ""
    kv_value = ""


def plain_traceback(sio: TextIO, exc_info: ExcInfo) -> None:
    """
    "Pretty"-print *exc_info* to *sio* using our own plain formatter.

    To be passed into `ConsoleRenderer`'s ``exception_formatter`` argument.

    Used by default if neither *Rich* not *better-exceptions* are present.

    .. versionadded:: 21.2
    """
    sio.write("\n" + _format_exception(exc_info))


def rich_traceback(sio: TextIO, exc_info: ExcInfo) -> None:
    """
    Pretty-print *exc_info* to *sio* using the *Rich* package.

    To be passed into `ConsoleRenderer`'s ``exception_formatter`` argument.

    Used by default if *Rich* is installed.

    .. versionadded:: 21.2
    """
    sio.write("\n")
    Console(file=sio, color_system="truecolor").print(
        Traceback.from_exception(*exc_info, show_locals=True)
    )


def better_traceback(sio: TextIO, exc_info: ExcInfo) -> None:
    """
    Pretty-print *exc_info* to *sio* using the *better-exceptions* package.

    To be passed into `ConsoleRenderer`'s ``exception_formatter`` argument.

    Used by default if *better-exceptions* is installed and *Rich* is absent.

    .. versionadded:: 21.2
    """
    sio.write("\n" + "".join(better_exceptions.format_exception(*exc_info)))


if rich is not None:
    default_exception_formatter = rich_traceback
elif better_exceptions is not None:
    default_exception_formatter = better_traceback
else:
    default_exception_formatter = plain_traceback


class ConsoleRenderer:
    """
    Render ``event_dict`` nicely aligned, possibly in colors, and ordered.

    If ``event_dict`` contains a true-ish ``exc_info`` key, it will be
    rendered *after* the log line. If Rich_ or better-exceptions_ are present,
    in colors and with extra context.

    :param pad_event: Pad the event to this many characters.
    :param colors: Use colors for a nicer output. `True` by default. On
        Windows only if Colorama_ is installed.
    :param force_colors: Force colors even for non-tty destinations.
        Use this option if your logs are stored in a file that is meant
        to be streamed to the console. Only meaningful on Windows.
    :param repr_native_str: When `True`, `repr` is also applied
        to native strings (i.e. unicode on Python 3 and bytes on Python 2).
        Setting this to `False` is useful if you want to have human-readable
        non-ASCII output on Python 2.  The ``event`` key is *never*
        `repr` -ed.
    :param level_styles: When present, use these styles for colors. This
        must be a dict from level names (strings) to Colorama styles. The
        default can be obtained by calling
        `ConsoleRenderer.get_default_level_styles`
    :param exception_formatter: A callable to render ``exc_infos``. If rich_
        or better-exceptions_ are installed, they are used for pretty-printing
        by default (rich_ taking precedence). You can also manually set it to
        `plain_traceback`, `better_traceback`, `rich_traceback`, or implement
        your own.
    :param sort_keys: Whether to sort keys when formatting. `True` by default.
    :param event_key: The key to look for the main log message. Needed when
        you rename it e.g. using `structlog.processors.EventRenamer`.

    Requires the Colorama_ package if *colors* is `True` **on Windows**.

    .. _Colorama: https://pypi.org/project/colorama/
    .. _better-exceptions: https://pypi.org/project/better-exceptions/
    .. _Rich: https://pypi.org/project/rich/

    .. versionadded:: 16.0
    .. versionadded:: 16.1 *colors*
    .. versionadded:: 17.1 *repr_native_str*
    .. versionadded:: 18.1 *force_colors*
    .. versionadded:: 18.1 *level_styles*
    .. versionchanged:: 19.2
       *Colorama* now initializes lazily to avoid unwanted initializations as
       ``ConsoleRenderer`` is used by default.
    .. versionchanged:: 19.2 Can be pickled now.
    .. versionchanged:: 20.1 *Colorama* does not initialize lazily on Windows
       anymore because it breaks rendering.
    .. versionchanged:: 21.1 It is additionally possible to set the logger name
       using the ``logger_name`` key in the ``event_dict``.
    .. versionadded:: 21.2 *exception_formatter*
    .. versionchanged:: 21.2 `ConsoleRenderer` now handles the ``exc_info``
       event dict key itself. Do **not** use the
       `structlog.processors.format_exc_info` processor together with
       `ConsoleRenderer` anymore! It will keep working, but you can't have
       customize exception formatting and a warning will be raised if you ask
       for it.
    .. versionchanged:: 21.2 The colors keyword now defaults to True on
       non-Windows systems, and either True or False in Windows depending on
       whether *Colorama* is installed.
    .. versionadded:: 21.3.0 *sort_keys*
    .. versionadded:: 22.1 *event_key*
    """

    def __init__(
        self,
        pad_event: int = _EVENT_WIDTH,
        colors: bool = _has_colors,
        force_colors: bool = False,
        repr_native_str: bool = False,
        level_styles: Styles | None = None,
        exception_formatter: ExceptionRenderer = default_exception_formatter,
        sort_keys: bool = True,
        event_key: str = "event",
    ):
        styles: Styles
        if colors:
            if _IS_WINDOWS:  # pragma: no cover
                # On Windows, we can't do colorful output without colorama.
                if colorama is None:
                    classname = self.__class__.__name__
                    raise SystemError(
                        _MISSING.format(
                            who=classname + " with `colors=True`",
                            package="colorama",
                        )
                    )
                # Colorama must be init'd on Windows, but must NOT be
                # init'd on other OSes, because it can break colors.
                if force_colors:
                    colorama.deinit()
                    colorama.init(strip=False)
                else:
                    colorama.init()

            styles = _ColorfulStyles
        else:
            styles = _PlainStyles

        self._styles = styles
        self._pad_event = pad_event

        if level_styles is None:
            self._level_to_color = self.get_default_level_styles(colors)
        else:
            self._level_to_color = level_styles

        for key in self._level_to_color:
            self._level_to_color[key] += styles.bright
        self._longest_level = len(
            max(self._level_to_color.keys(), key=lambda e: len(e))
        )

        self._repr_native_str = repr_native_str
        self._exception_formatter = exception_formatter
        self._sort_keys = sort_keys
        self._event_key = event_key

    def _repr(self, val: Any) -> str:
        """
        Determine representation of *val* depending on its type &
        self._repr_native_str.
        """
        if self._repr_native_str is True:
            return repr(val)

        if isinstance(val, str):
            return val

        return repr(val)

    def __call__(
        self, logger: WrappedLogger, name: str, event_dict: EventDict
    ) -> str:
        sio = StringIO()

        ts = event_dict.pop("timestamp", None)
        if ts is not None:
            sio.write(
                # can be a number if timestamp is UNIXy
                self._styles.timestamp
                + str(ts)
                + self._styles.reset
                + " "
            )
        level = event_dict.pop("level", None)
        if level is not None:
            sio.write(
                "["
                + self._level_to_color.get(level, "")
                + _pad(level, self._longest_level)
                + self._styles.reset
                + "] "
            )

        # force event to str for compatibility with standard library
        event = event_dict.pop(self._event_key, None)
        if not isinstance(event, str):
            event = str(event)

        if event_dict:
            event = _pad(event, self._pad_event) + self._styles.reset + " "
        else:
            event += self._styles.reset
        sio.write(self._styles.bright + event)

        logger_name = event_dict.pop("logger", None)
        if logger_name is None:
            logger_name = event_dict.pop("logger_name", None)

        if logger_name is not None:
            sio.write(
                "["
                + self._styles.logger_name
                + self._styles.bright
                + logger_name
                + self._styles.reset
                + "] "
            )

        stack = event_dict.pop("stack", None)
        exc = event_dict.pop("exception", None)
        exc_info = event_dict.pop("exc_info", None)

        event_dict_keys: Iterable[str] = event_dict.keys()
        if self._sort_keys:
            event_dict_keys = sorted(event_dict_keys)

        sio.write(
            " ".join(
                self._styles.kv_key
                + key
                + self._styles.reset
                + "="
                + self._styles.kv_value
                + self._repr(event_dict[key])
                + self._styles.reset
                for key in event_dict_keys
            )
        )

        if stack is not None:
            sio.write("\n" + stack)
            if exc_info or exc is not None:
                sio.write("\n\n" + "=" * 79 + "\n")

        if exc_info:
            exc_info = _figure_out_exc_info(exc_info)

            self._exception_formatter(sio, exc_info)
        elif exc is not None:
            if self._exception_formatter is not plain_traceback:
                warnings.warn(
                    "Remove `format_exc_info` from your processor chain "
                    "if you want pretty exceptions.",
                    stacklevel=2,
                )
            sio.write("\n" + exc)

        return sio.getvalue()

    @staticmethod
    def get_default_level_styles(colors: bool = True) -> Any:
        """
        Get the default styles for log levels

        This is intended to be used with `ConsoleRenderer`'s ``level_styles``
        parameter.  For example, if you are adding custom levels in your
        home-grown :func:`~structlog.stdlib.add_log_level` you could do::

            my_styles = ConsoleRenderer.get_default_level_styles()
            my_styles["EVERYTHING_IS_ON_FIRE"] = my_styles["critical"]
            renderer = ConsoleRenderer(level_styles=my_styles)

        :param colors: Whether to use colorful styles. This must match the
            *colors* parameter to `ConsoleRenderer`. Default: `True`.
        """
        styles: Styles
        styles = _ColorfulStyles if colors else _PlainStyles
        return {
            "critical": styles.level_critical,
            "exception": styles.level_exception,
            "error": styles.level_error,
            "warn": styles.level_warn,
            "warning": styles.level_warn,
            "info": styles.level_info,
            "debug": styles.level_debug,
            "notset": styles.level_notset,
        }


_SENTINEL = object()


def set_exc_info(
    logger: WrappedLogger, method_name: str, event_dict: EventDict
) -> EventDict:
    """
    Set ``event_dict["exc_info"] = True`` if *method_name* is ``"exception"``.

    Do nothing if the name is different or ``exc_info`` is already set.
    """
    if (
        method_name != "exception"
        or event_dict.get("exc_info", _SENTINEL) is not _SENTINEL
    ):
        return event_dict

    event_dict["exc_info"] = True

    return event_dict
