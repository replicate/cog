# SPDX-License-Identifier: MIT OR Apache-2.0
# This file is dual licensed under the terms of the Apache License, Version
# 2.0, and the MIT License.  See the LICENSE file in the root of this
# repository for complete details.

"""
Processors useful regardless of the logging framework.
"""

from __future__ import annotations

import datetime
import enum
import inspect
import json
import logging
import operator
import os
import sys
import threading
import time

from typing import (
    Any,
    Callable,
    ClassVar,
    Collection,
    NamedTuple,
    Sequence,
    TextIO,
)

from ._frames import (
    _find_first_app_frame_and_name,
    _format_exception,
    _format_stack,
)
from ._log_levels import _NAME_TO_LEVEL, add_log_level
from ._utils import get_processname
from .tracebacks import ExceptionDictTransformer
from .typing import EventDict, ExceptionTransformer, ExcInfo, WrappedLogger


__all__ = [
    "_NAME_TO_LEVEL",  # some people rely on it being here
    "add_log_level",
    "CallsiteParameter",
    "CallsiteParameterAdder",
    "dict_tracebacks",
    "EventRenamer",
    "ExceptionPrettyPrinter",
    "format_exc_info",
    "JSONRenderer",
    "KeyValueRenderer",
    "StackInfoRenderer",
    "TimeStamper",
    "UnicodeDecoder",
    "UnicodeEncoder",
]


class KeyValueRenderer:
    """
    Render ``event_dict`` as a list of ``Key=repr(Value)`` pairs.

    :param sort_keys: Whether to sort keys when formatting.
    :param key_order: List of keys that should be rendered in this exact
        order.  Missing keys will be rendered as ``None``, extra keys depending
        on *sort_keys* and the dict class.
    :param drop_missing: When ``True``, extra keys in *key_order* will be
        dropped rather than rendered as ``None``.
    :param repr_native_str: When ``True``, :func:`repr()` is also applied
        to native strings.
        Setting this to ``False`` is useful if you want to have human-readable
        non-ASCII output on Python 2.

    .. versionadded:: 0.2.0 *key_order*
    .. versionadded:: 16.1.0 *drop_missing*
    .. versionadded:: 17.1.0 *repr_native_str*
    """

    def __init__(
        self,
        sort_keys: bool = False,
        key_order: Sequence[str] | None = None,
        drop_missing: bool = False,
        repr_native_str: bool = True,
    ):
        self._ordered_items = _items_sorter(sort_keys, key_order, drop_missing)

        if repr_native_str is True:
            self._repr = repr
        else:

            def _repr(inst: Any) -> str:
                if isinstance(inst, str):
                    return inst

                return repr(inst)

            self._repr = _repr

    def __call__(
        self, _: WrappedLogger, __: str, event_dict: EventDict
    ) -> str:
        return " ".join(
            k + "=" + self._repr(v) for k, v in self._ordered_items(event_dict)
        )


class LogfmtRenderer:
    """
    Render ``event_dict`` using the logfmt_ format.

    .. _logfmt: https://brandur.org/logfmt

    :param sort_keys: Whether to sort keys when formatting.
    :param key_order: List of keys that should be rendered in this exact
        order. Missing keys are rendered with empty values, extra keys
        depending on *sort_keys* and the dict class.
    :param drop_missing: When ``True``, extra keys in *key_order* will be
        dropped rather than rendered with empty values.
    :param bool_as_flag: When ``True``, render ``{"flag": True}`` as
        ``flag``, instead of ``flag=true``. ``{"flag": False}`` is
        always rendered as ``flag=false``.

    :raises ValueError: If a key contains non printable or space characters.

    .. versionadded:: 21.5.0
    """

    def __init__(
        self,
        sort_keys: bool = False,
        key_order: Sequence[str] | None = None,
        drop_missing: bool = False,
        bool_as_flag: bool = True,
    ):
        self._ordered_items = _items_sorter(sort_keys, key_order, drop_missing)
        self.bool_as_flag = bool_as_flag

    def __call__(
        self, _: WrappedLogger, __: str, event_dict: EventDict
    ) -> str:
        elements: list[str] = []
        for key, value in self._ordered_items(event_dict):
            if any(c <= " " for c in key):
                raise ValueError(f'Invalid key: "{key}"')

            if value is None:
                elements.append(f"{key}=")
                continue

            if isinstance(value, bool):
                if self.bool_as_flag and value:
                    elements.append(f"{key}")
                    continue
                value = "true" if value else "false"

            value = f"{value}".replace('"', '\\"')

            if " " in value or "=" in value:
                value = f'"{value}"'

            elements.append(f"{key}={value}")

        return " ".join(elements)


def _items_sorter(
    sort_keys: bool,
    key_order: Sequence[str] | None,
    drop_missing: bool,
) -> Callable[[EventDict], list[tuple[str, Any]]]:
    """
    Return a function to sort items from an ``event_dict``.

    See `KeyValueRenderer` for an explanation of the parameters.
    """
    # Use an optimized version for each case.
    if key_order and sort_keys:

        def ordered_items(event_dict: EventDict) -> list[tuple[str, Any]]:
            items = []
            for key in key_order:  # type: ignore[union-attr]
                value = event_dict.pop(key, None)
                if value is not None or not drop_missing:
                    items.append((key, value))

            items += sorted(event_dict.items())

            return items

    elif key_order:

        def ordered_items(event_dict: EventDict) -> list[tuple[str, Any]]:
            items = []
            for key in key_order:  # type: ignore[union-attr]
                value = event_dict.pop(key, None)
                if value is not None or not drop_missing:
                    items.append((key, value))

            items += event_dict.items()

            return items

    elif sort_keys:

        def ordered_items(event_dict: EventDict) -> list[tuple[str, Any]]:
            return sorted(event_dict.items())

    else:
        ordered_items = operator.methodcaller(  # type: ignore[assignment]
            "items"
        )

    return ordered_items


class UnicodeEncoder:
    """
    Encode unicode values in ``event_dict``.

    :param encoding: Encoding to encode to (default: ``"utf-8"``).
    :param errors: How to cope with encoding errors (default
        ``"backslashreplace"``).

    Useful if you're running Python 2 as otherwise ``u"abc"`` will be rendered
    as ``'u"abc"'``.

    Just put it in the processor chain before the renderer.
    """

    _encoding: str
    _errors: str

    def __init__(
        self, encoding: str = "utf-8", errors: str = "backslashreplace"
    ) -> None:
        self._encoding = encoding
        self._errors = errors

    def __call__(
        self, logger: WrappedLogger, name: str, event_dict: EventDict
    ) -> EventDict:
        for key, value in event_dict.items():
            if isinstance(value, str):
                event_dict[key] = value.encode(self._encoding, self._errors)

        return event_dict


class UnicodeDecoder:
    """
    Decode byte string values in ``event_dict``.

    :param encoding: Encoding to decode from (default: ``"utf-8"``).
    :param errors: How to cope with encoding errors (default:
        ``"replace"``).

    Useful if you're running Python 3 as otherwise ``b"abc"`` will be rendered
    as ``'b"abc"'``.

    Just put it in the processor chain before the renderer.

    .. versionadded:: 15.4.0
    """

    _encoding: str
    _errors: str

    def __init__(
        self, encoding: str = "utf-8", errors: str = "replace"
    ) -> None:
        self._encoding = encoding
        self._errors = errors

    def __call__(
        self, logger: WrappedLogger, name: str, event_dict: EventDict
    ) -> EventDict:
        for key, value in event_dict.items():
            if isinstance(value, bytes):
                event_dict[key] = value.decode(self._encoding, self._errors)

        return event_dict


class JSONRenderer:
    """
    Render the ``event_dict`` using ``serializer(event_dict, **dumps_kw)``.

    :param dumps_kw: Are passed unmodified to *serializer*.  If *default*
        is passed, it will disable support for ``__structlog__``-based
        serialization.
    :param serializer: A :func:`json.dumps`-compatible callable that
        will be used to format the string.  This can be used to use alternative
        JSON encoders like `orjson <https://pypi.org/project/orjson/>`__ or
        `RapidJSON <https://pypi.org/project/python-rapidjson/>`_  (default:
        :func:`json.dumps`).

    .. versionadded:: 0.2.0
        Support for ``__structlog__`` serialization method.

    .. versionadded:: 15.4.0
        *serializer* parameter.

    .. versionadded:: 18.2.0
       Serializer's *default* parameter can be overwritten now.

    """

    def __init__(
        self,
        serializer: Callable[..., str | bytes] = json.dumps,
        **dumps_kw: Any,
    ) -> None:
        dumps_kw.setdefault("default", _json_fallback_handler)
        self._dumps_kw = dumps_kw
        self._dumps = serializer

    def __call__(
        self, logger: WrappedLogger, name: str, event_dict: EventDict
    ) -> str | bytes:
        """
        The return type of this depends on the return type of self._dumps.
        """
        return self._dumps(event_dict, **self._dumps_kw)


def _json_fallback_handler(obj: Any) -> Any:
    """
    Serialize custom datatypes and pass the rest to __structlog__ & repr().
    """
    # circular imports :(
    from cog._vendor.structlog.threadlocal import _ThreadLocalDictWrapper

    if isinstance(obj, _ThreadLocalDictWrapper):
        return obj._dict

    try:
        return obj.__structlog__()
    except AttributeError:
        return repr(obj)


class ExceptionRenderer:
    """
    Replace an ``exc_info`` field with an ``exception`` field which is rendered
    by *exception_formatter*.

    The contents of the ``exception`` field depends on the return value of the
    :class:`.ExceptionTransformer` that is used:

    - The default produces a formatted string via Python's built-in traceback
      formatting.
    - The :class:`~structlog.tracebacks.ExceptionDictTransformer` a list of
      stack dicts that can be serialized to JSON.

    If *event_dict* contains the key ``exc_info``, there are three possible
    behaviors:

    1. If the value is a tuple, render it into the key ``exception``.
    2. If the value is an Exception render it into the key ``exception``.
    3. If the value true but no tuple, obtain exc_info ourselves and render
       that.

    If there is no ``exc_info`` key, the *event_dict* is not touched.
    This behavior is analogue to the one of the stdlib's logging.

    :param exception_formatter: A callable that is used to format the exception
        from the ``exc_info`` field.

    .. versionadded:: 22.1
    """

    def __init__(
        self,
        exception_formatter: ExceptionTransformer = _format_exception,
    ) -> None:
        self.format_exception = exception_formatter

    def __call__(
        self, logger: WrappedLogger, name: str, event_dict: EventDict
    ) -> EventDict:
        exc_info = event_dict.pop("exc_info", None)
        if exc_info:
            event_dict["exception"] = self.format_exception(
                _figure_out_exc_info(exc_info)
            )

        return event_dict


format_exc_info = ExceptionRenderer()
"""
Replace an ``exc_info`` field with an ``exception`` string field using
Python's built-in traceback formatting.

If *event_dict* contains the key ``exc_info``, there are three possible
behaviors:

1. If the value is a tuple, render it into the key ``exception``.
2. If the value is an Exception render it into the key ``exception``.
3. If the value is true but no tuple, obtain exc_info ourselves and render
   that.

If there is no ``exc_info`` key, the *event_dict* is not touched.
This behavior is analogue to the one of the stdlib's logging.
"""

dict_tracebacks = ExceptionRenderer(ExceptionDictTransformer())
"""
Replace an ``exc_info`` field with an ``exception`` field containing structured
tracebacks suitable for, e.g., JSON output.

It is a shortcut for :class:`ExceptionRenderer` with a
:class:`~structlog.tracebacks.ExceptionDictTransformer`.

The treatment of the ``exc_info`` key is identical to `format_exc_info`.

.. versionadded:: 22.1
"""


class TimeStamper:
    """
    Add a timestamp to ``event_dict``.

    :param fmt: strftime format string, or ``"iso"`` for `ISO 8601
        <https://en.wikipedia.org/wiki/ISO_8601>`_, or `None` for a `UNIX
        timestamp <https://en.wikipedia.org/wiki/Unix_time>`_.
    :param utc: Whether timestamp should be in UTC or local time.
    :param key: Target key in *event_dict* for added timestamps.

    .. versionchanged:: 19.2 Can be pickled now.
    """

    __slots__ = ("_stamper", "fmt", "utc", "key")

    def __init__(
        self,
        fmt: str | None = None,
        utc: bool = True,
        key: str = "timestamp",
    ) -> None:
        self.fmt, self.utc, self.key = fmt, utc, key

        self._stamper = _make_stamper(fmt, utc, key)

    def __call__(
        self, logger: WrappedLogger, name: str, event_dict: EventDict
    ) -> EventDict:
        return self._stamper(event_dict)

    def __getstate__(self) -> dict[str, Any]:
        return {"fmt": self.fmt, "utc": self.utc, "key": self.key}

    def __setstate__(self, state: dict[str, Any]) -> None:
        self.fmt = state["fmt"]
        self.utc = state["utc"]
        self.key = state["key"]

        self._stamper = _make_stamper(**state)


def _make_stamper(
    fmt: str | None, utc: bool, key: str
) -> Callable[[EventDict], EventDict]:
    """
    Create a stamper function.
    """
    if fmt is None and not utc:
        raise ValueError("UNIX timestamps are always UTC.")

    now: Callable[[], datetime.datetime]

    if utc:

        def now() -> datetime.datetime:
            return datetime.datetime.utcnow()

    else:

        def now() -> datetime.datetime:
            return datetime.datetime.now()

    if fmt is None:

        def stamper_unix(event_dict: EventDict) -> EventDict:
            event_dict[key] = time.time()

            return event_dict

        return stamper_unix

    if fmt.upper() == "ISO":

        def stamper_iso_local(event_dict: EventDict) -> EventDict:
            event_dict[key] = now().isoformat()
            return event_dict

        def stamper_iso_utc(event_dict: EventDict) -> EventDict:
            event_dict[key] = now().isoformat() + "Z"
            return event_dict

        if utc:
            return stamper_iso_utc

        return stamper_iso_local

    def stamper_fmt(event_dict: EventDict) -> EventDict:
        event_dict[key] = now().strftime(fmt)  # type: ignore[arg-type]

        return event_dict

    return stamper_fmt


def _figure_out_exc_info(v: Any) -> ExcInfo:
    """
    Depending on the Python version will try to do the smartest thing possible
    to transform *v* into an ``exc_info`` tuple.
    """
    if isinstance(v, BaseException):
        return (v.__class__, v, v.__traceback__)

    if isinstance(v, tuple):
        return v  # type: ignore[return-value]

    if v:
        return sys.exc_info()  # type: ignore[return-value]

    return v


class ExceptionPrettyPrinter:
    """
    Pretty print exceptions and remove them from the ``event_dict``.

    :param file: Target file for output (default: ``sys.stdout``).

    This processor is mostly for development and testing so you can read
    exceptions properly formatted.

    It behaves like `format_exc_info` except it removes the exception
    data from the event dictionary after printing it.

    It's tolerant to having `format_exc_info` in front of itself in the
    processor chain but doesn't require it.  In other words, it handles both
    ``exception`` as well as ``exc_info`` keys.

    .. versionadded:: 0.4.0

    .. versionchanged:: 16.0.0
       Added support for passing exceptions as ``exc_info`` on Python 3.
    """

    def __init__(
        self,
        file: TextIO | None = None,
        exception_formatter: ExceptionTransformer = _format_exception,
    ) -> None:
        if file is not None:
            self._file = file
        else:
            self._file = sys.stdout

    def __call__(
        self, logger: WrappedLogger, name: str, event_dict: EventDict
    ) -> EventDict:
        exc = event_dict.pop("exception", None)
        if exc is None:
            exc_info = _figure_out_exc_info(event_dict.pop("exc_info", None))
            if exc_info:
                exc = _format_exception(exc_info)

        if exc:
            print(exc, file=self._file)

        return event_dict


class StackInfoRenderer:
    """
    Add stack information with key ``stack`` if ``stack_info`` is `True`.

    Useful when you want to attach a stack dump to a log entry without
    involving an exception and works analogously to the *stack_info* argument
    of the Python standard library logging.

    :param additional_ignores: By default, stack frames coming from
        *structlog* are ignored. With this argument you can add additional
        names that are ignored, before the stack starts being rendered. They
        are matched using ``startswith()``, so they don't have to match
        exactly. The names are used to find the first relevant name, therefore
        once a frame is found that doesn't start with *structlog* or one of
        *additional_ignores*, **no filtering** is applied to subsequent frames.

    .. versionadded:: 0.4.0
    .. versionadded:: 22.1.0  *additional_ignores*
    """

    __slots__ = ["_additional_ignores"]

    def __init__(self, additional_ignores: list[str] | None = None) -> None:
        self._additional_ignores = additional_ignores

    def __call__(
        self, logger: WrappedLogger, name: str, event_dict: EventDict
    ) -> EventDict:
        if event_dict.pop("stack_info", None):
            event_dict["stack"] = _format_stack(
                _find_first_app_frame_and_name(self._additional_ignores)[0]
            )

        return event_dict


class CallsiteParameter(enum.Enum):
    """
    Callsite parameters that can be added to an event dictionary with the
    `structlog.processors.CallsiteParameterAdder` processor class.

    The string values of the members of this enum will be used as the keys for
    the callsite parameters in the event dictionary.

    .. versionadded:: 21.5.0
    """

    #: The full path to the python source file of the callsite.
    PATHNAME = "pathname"
    #: The basename part of the full path to the python source file of the
    #: callsite.
    FILENAME = "filename"
    #: The python module the callsite was in. This mimics the module attribute
    #: of `logging.LogRecord` objects and will be the basename, without
    #: extension, of the full path to the python source file of the callsite.
    MODULE = "module"
    #: The name of the function that the callsite was in.
    FUNC_NAME = "func_name"
    #: The line number of the callsite.
    LINENO = "lineno"
    #: The ID of the thread the callsite was executed in.
    THREAD = "thread"
    #: The name of the thread the callsite was executed in.
    THREAD_NAME = "thread_name"
    #: The ID of the process the callsite was executed in.
    PROCESS = "process"
    #: The name of the process the callsite was executed in.
    PROCESS_NAME = "process_name"


class CallsiteParameterAdder:
    """
    Adds parameters of the callsite that an event dictionary originated from to
    the event dictionary. This processor can be used to enrich events
    dictionaries with information such as the function name, line number and
    filename that an event dictionary originated from.

    .. warning::
        This processor cannot detect the correct callsite for invocation of
        async functions.

    If the event dictionary has an embedded `logging.LogRecord` object and did
    not originate from *structlog* then the callsite information will be
    determined from the `logging.LogRecord` object. For event dictionaries
    without an embedded `logging.LogRecord` object the callsite will be
    determined from the stack trace, ignoring all intra-structlog calls, calls
    from the `logging` module, and stack frames from modules with names that
    start with values in ``additional_ignores``, if it is specified.

    The keys used for callsite parameters in the event dictionary are the
    string values of `CallsiteParameter` enum members.

    :param parameters:
        A collection of `CallsiteParameter` values that should be added to the
        event dictionary.

    :param additional_ignores:
        Additional names with which a stack frame's module name must not
        start for it to be considered when determening the callsite.

    .. note::
        When used with `structlog.stdlib.ProcessorFormatter` the most efficient
        configuration is to either use this processor in ``foreign_pre_chain``
        of `structlog.stdlib.ProcessorFormatter` and in ``processors`` of
        `structlog.configure`, or to use it in ``processors`` of
        `structlog.stdlib.ProcessorFormatter` without using it in
        ``processors`` of `structlog.configure` and ``foreign_pre_chain`` of
        `structlog.stdlib.ProcessorFormatter`.

    .. versionadded:: 21.5.0
    """

    _handlers: ClassVar[
        dict[CallsiteParameter, Callable[[str, inspect.Traceback], Any]]
    ] = {
        CallsiteParameter.PATHNAME: (
            lambda module, frame_info: frame_info.filename
        ),
        CallsiteParameter.FILENAME: (
            lambda module, frame_info: os.path.basename(frame_info.filename)
        ),
        CallsiteParameter.MODULE: (
            lambda module, frame_info: os.path.splitext(
                os.path.basename(frame_info.filename)
            )[0]
        ),
        CallsiteParameter.FUNC_NAME: (
            lambda module, frame_info: frame_info.function
        ),
        CallsiteParameter.LINENO: (
            lambda module, frame_info: frame_info.lineno
        ),
        CallsiteParameter.THREAD: (
            lambda module, frame_info: threading.get_ident()
        ),
        CallsiteParameter.THREAD_NAME: (
            lambda module, frame_info: threading.current_thread().name
        ),
        CallsiteParameter.PROCESS: (lambda module, frame_info: os.getpid()),
        CallsiteParameter.PROCESS_NAME: (
            lambda module, frame_info: get_processname()
        ),
    }
    _record_attribute_map: ClassVar[dict[CallsiteParameter, str]] = {
        CallsiteParameter.PATHNAME: "pathname",
        CallsiteParameter.FILENAME: "filename",
        CallsiteParameter.MODULE: "module",
        CallsiteParameter.FUNC_NAME: "funcName",
        CallsiteParameter.LINENO: "lineno",
        CallsiteParameter.THREAD: "thread",
        CallsiteParameter.THREAD_NAME: "threadName",
        CallsiteParameter.PROCESS: "process",
        CallsiteParameter.PROCESS_NAME: "processName",
    }

    _all_parameters: ClassVar[set[CallsiteParameter]] = set(CallsiteParameter)

    class _RecordMapping(NamedTuple):
        event_dict_key: str
        record_attribute: str

    __slots__ = [
        "_active_handlers",
        "_additional_ignores",
        "_record_mappings",
    ]

    def __init__(
        self,
        parameters: Collection[CallsiteParameter] = _all_parameters,
        additional_ignores: list[str] | None = None,
    ) -> None:
        if additional_ignores is None:
            additional_ignores = []
        # Ignore stack frames from the logging module. They will occur if this
        # processor is used in ProcessorFormatter, and additionally the logging
        # module should not be logging using structlog.
        self._additional_ignores = ["logging", *additional_ignores]
        self._active_handlers: list[
            tuple[CallsiteParameter, Callable[[str, inspect.Traceback], Any]]
        ] = []
        self._record_mappings: list[CallsiteParameterAdder._RecordMapping] = []
        for parameter in parameters:
            self._active_handlers.append(
                (parameter, self._handlers[parameter])
            )
            self._record_mappings.append(
                self._RecordMapping(
                    parameter.value,
                    self._record_attribute_map[parameter],
                )
            )

    def __call__(
        self, logger: logging.Logger, name: str, event_dict: EventDict
    ) -> EventDict:
        record: logging.LogRecord | None = event_dict.get("_record")
        from_structlog: bool | None = event_dict.get("_from_structlog")
        # If the event dictionary has a record, but it comes from structlog,
        # then the callsite parameters of the record will not be correct.
        if record is not None and not from_structlog:
            for mapping in self._record_mappings:
                event_dict[mapping.event_dict_key] = record.__dict__[
                    mapping.record_attribute
                ]
        else:
            frame, module = _find_first_app_frame_and_name(
                additional_ignores=self._additional_ignores
            )
            frame_info = inspect.getframeinfo(frame)
            for parameter, handler in self._active_handlers:
                event_dict[parameter.value] = handler(module, frame_info)
        return event_dict


class EventRenamer:
    r"""
    Rename the ``event`` key in event dicts.

    This is useful if you want to use consistent log message keys across
    platforms and/or use the ``event`` key for something custom.

    .. warning::

       It's recommended to put this processor right before the renderer, since
       some processors may rely on the presence and meaning of the ``event``
       key.

    :param to: Rename ``event_dict["event"]`` to ``event_dict[to]``
    :param replace_by: Rename ``event_dict[replace_by]`` to
        ``event_dict["event"]``. *replace_by* missing from ``event_dict`` is
        handled gracefully.

    .. versionadded:: 22.1

    See also the :ref:`rename-event` recipe.
    """

    def __init__(self, to: str, replace_by: str | None = None):
        self.to = to
        self.replace_by = replace_by

    def __call__(
        self, logger: logging.Logger, name: str, event_dict: EventDict
    ) -> EventDict:
        event = event_dict.pop("event")
        event_dict[self.to] = event

        if self.replace_by is not None:
            replace_by = event_dict.pop(self.replace_by, None)
            if replace_by is not None:
                event_dict["event"] = replace_by

        return event_dict
