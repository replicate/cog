# SPDX-License-Identifier: MIT OR Apache-2.0
# This file is dual licensed under the terms of the Apache License, Version
# 2.0, and the MIT License.  See the LICENSE file in the root of this
# repository for complete details.

"""
Logger wrapper and helper class.
"""

from __future__ import annotations

from typing import Any, Iterable, Mapping, Sequence

from cog._vendor.structlog.exceptions import DropEvent

from .typing import BindableLogger, Context, Processor, WrappedLogger


class BoundLoggerBase:
    """
    Immutable context carrier.

    Doesn't do any actual logging; examples for useful subclasses are:

    - the generic `BoundLogger` that can wrap anything,
    - `structlog.stdlib.BoundLogger`.
    - `structlog.twisted.BoundLogger`,

    See also `custom-wrappers`.
    """

    _logger: WrappedLogger
    """
    Wrapped logger.

    .. note::

        Despite underscore available **read-only** to custom wrapper classes.

        See also `custom-wrappers`.
    """

    def __init__(
        self,
        logger: WrappedLogger,
        processors: Iterable[Processor],
        context: Context,
    ):
        self._logger = logger
        self._processors = processors
        self._context = context

    def __repr__(self) -> str:
        return "<{}(context={!r}, processors={!r})>".format(
            self.__class__.__name__, self._context, self._processors
        )

    def __eq__(self, other: Any) -> bool:
        try:
            return self._context == other._context
        except AttributeError:
            return False

    def __ne__(self, other: Any) -> bool:
        return not self.__eq__(other)

    def bind(self, **new_values: Any) -> BoundLoggerBase:
        """
        Return a new logger with *new_values* added to the existing ones.
        """
        return self.__class__(
            self._logger,
            self._processors,
            self._context.__class__(self._context, **new_values),
        )

    def unbind(self, *keys: str) -> BoundLoggerBase:
        """
        Return a new logger with *keys* removed from the context.

        :raises KeyError: If the key is not part of the context.
        """
        bl = self.bind()
        for key in keys:
            del bl._context[key]

        return bl

    def try_unbind(self, *keys: str) -> BoundLoggerBase:
        """
        Like :meth:`unbind`, but best effort: missing keys are ignored.

        .. versionadded:: 18.2.0
        """
        bl = self.bind()
        for key in keys:
            bl._context.pop(key, None)

        return bl

    def new(self, **new_values: Any) -> BoundLoggerBase:
        """
        Clear context and binds *initial_values* using `bind`.

        Only necessary with dict implementations that keep global state like
        those wrapped by `structlog.threadlocal.wrap_dict` when threads
        are re-used.
        """
        self._context.clear()

        return self.bind(**new_values)

    # Helper methods for sub-classing concrete BoundLoggers.

    def _process_event(
        self, method_name: str, event: str | None, event_kw: dict[str, Any]
    ) -> tuple[Sequence[Any], Mapping[str, Any]]:
        """
        Combines creates an ``event_dict`` and runs the chain.

        Call it to combine your *event* and *context* into an event_dict and
        process using the processor chain.

        :param method_name: The name of the logger method.  Is passed into
            the processors.
        :param event: The event -- usually the first positional argument to a
            logger.
        :param event_kw: Additional event keywords.  For example if someone
            calls ``log.info("foo", bar=42)``, *event* would to be ``"foo"``
            and *event_kw* ``{"bar": 42}``.

        :raises: `structlog.DropEvent` if log entry should be dropped.
        :raises: `ValueError` if the final processor doesn't return a
            str, bytes, bytearray, tuple, or a dict.

        :returns: `tuple` of ``(*args, **kw)``

        .. note::

            Despite underscore available to custom wrapper classes.

            See also `custom-wrappers`.

        .. versionchanged:: 14.0.0
            Allow final processor to return a `dict`.
        .. versionchanged:: 20.2.0
            Allow final processor to return `bytes`.
        .. versionchanged:: 21.2.0
            Allow final processor to return a `bytearray`.
        """
        # We're typing it as Any, because processors can return more than an
        # EventDict.
        event_dict: Any = self._context.copy()
        event_dict.update(**event_kw)

        if event is not None:
            event_dict["event"] = event
        for proc in self._processors:
            event_dict = proc(self._logger, method_name, event_dict)

        if isinstance(event_dict, (str, bytes, bytearray)):
            return (event_dict,), {}

        if isinstance(event_dict, tuple):
            # In this case we assume that the last processor returned a tuple
            # of ``(args, kwargs)`` and pass it right through.
            return event_dict  # type: ignore[return-value]

        if isinstance(event_dict, dict):
            return (), event_dict

        raise ValueError(
            "Last processor didn't return an appropriate value.  Valid "
            "return values are a dict, a tuple of (args, kwargs), bytes, "
            "or a str."
        )

    def _proxy_to_logger(
        self, method_name: str, event: str | None = None, **event_kw: Any
    ) -> Any:
        """
        Run processor chain on event & call *method_name* on wrapped logger.

        DRY convenience method that runs :func:`_process_event`, takes care of
        handling :exc:`structlog.DropEvent`, and finally calls *method_name* on
        :attr:`_logger` with the result.

        :param method_name: The name of the method that's going to get
            called.  Technically it should be identical to the method the
            user called because it also get passed into processors.
        :param event: The event -- usually the first positional argument to a
            logger.
        :param event_kw: Additional event keywords.  For example if someone
            calls ``log.info("foo", bar=42)``, *event* would to be ``"foo"``
            and *event_kw* ``{"bar": 42}``.

        .. note::

            Despite underscore available to custom wrapper classes.

            See also `custom-wrappers`.
        """
        try:
            args, kw = self._process_event(method_name, event, event_kw)
            return getattr(self._logger, method_name)(*args, **kw)
        except DropEvent:
            return None


def get_context(bound_logger: BindableLogger) -> Context:
    """
    Return *bound_logger*'s context.

    The type of *bound_logger* and the type returned depend on your
    configuration.

    :param bound_logger: The bound logger whose context you want.

    :returns: The *actual* context from *bound_logger*. It is *not* copied
        first.

    .. versionadded:: 20.2
    """
    # This probably will get more complicated in the future.
    return bound_logger._context
