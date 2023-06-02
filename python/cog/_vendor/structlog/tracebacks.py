# SPDX-License-Identifier: MIT OR Apache-2.0
# This file is dual licensed under the terms of the Apache License, Version
# 2.0, and the MIT License.  See the LICENSE file in the root of this
# repository for complete details.

"""
Extract a structured traceback from an exception.

Contributed by Will McGugan (see
https://github.com/hynek/structlog/pull/407#issuecomment-1150926246) from Rich:
https://github.com/Textualize/rich/blob/972dedff/rich/traceback.py
"""

from __future__ import annotations

import os

from dataclasses import asdict, dataclass, field
from traceback import walk_tb
from types import TracebackType
from typing import Any, Tuple, Union

from .typing import ExcInfo


__all__ = [
    "ExceptionDictTransformer",
    "Frame",
    "Stack",
    "SyntaxError_",
    "Trace",
    "extract",
    "safe_str",
    "to_repr",
]


SHOW_LOCALS = True
LOCALS_MAX_STRING = 80
MAX_FRAMES = 50

OptExcInfo = Union[ExcInfo, Tuple[None, None, None]]


@dataclass
class Frame:
    """
    Represents a single stack frame.
    """

    filename: str
    lineno: int
    name: str
    line: str = ""
    locals: dict[str, str] | None = None


@dataclass
class SyntaxError_:
    """
    Contains detailed information about :exc:`SyntaxError` exceptions.
    """

    offset: int
    filename: str
    line: str
    lineno: int
    msg: str


@dataclass
class Stack:
    """
    Represents an exception and a list of stack frames.
    """

    exc_type: str
    exc_value: str
    syntax_error: SyntaxError_ | None = None
    is_cause: bool = False
    frames: list[Frame] = field(default_factory=list)


@dataclass
class Trace:
    """
    Container for a list of stack traces.
    """

    stacks: list[Stack]


def safe_str(_object: Any) -> str:
    """Don't allow exceptions from __str__ to propegate."""
    try:
        return str(_object)
    except Exception as error:
        return f"<str-error {str(error)!r}>"


def to_repr(obj: Any, max_string: int | None = None) -> str:
    """Get repr string for an object, but catch errors."""
    if isinstance(obj, str):
        obj_repr = obj
    else:
        try:
            obj_repr = repr(obj)
        except Exception as error:
            obj_repr = f"<repr-error {str(error)!r}>"

    if max_string is not None and len(obj_repr) > max_string:
        truncated = len(obj_repr) - max_string
        obj_repr = f"{obj_repr[:max_string]!r}+{truncated}"

    return obj_repr


def extract(
    exc_type: type[BaseException],
    exc_value: BaseException,
    traceback: TracebackType | None,
    *,
    show_locals: bool = False,
    locals_max_string: int = LOCALS_MAX_STRING,
) -> Trace:
    """
    Extract traceback information.

    :param exc_type: Exception type.
    :param exc_value: Exception value.
    :param traceback: Python Traceback object.
    :param show_locals: Enable display of local variables. Defaults to False.
    :param locals_max_string: Maximum length of string before truncating, or
        ``None`` to disable.
    :param max_frames: Maximum number of frames in each stack

    :returns: A Trace instance with structured information about all
        exceptions.

    .. versionadded:: 22.1
    """

    stacks: list[Stack] = []
    is_cause = False

    while True:
        stack = Stack(
            exc_type=safe_str(exc_type.__name__),
            exc_value=safe_str(exc_value),
            is_cause=is_cause,
        )

        if isinstance(exc_value, SyntaxError):
            stack.syntax_error = SyntaxError_(
                offset=exc_value.offset or 0,
                filename=exc_value.filename or "?",
                lineno=exc_value.lineno or 0,
                line=exc_value.text or "",
                msg=exc_value.msg,
            )

        stacks.append(stack)
        append = stack.frames.append  # pylint: disable=no-member

        for frame_summary, line_no in walk_tb(traceback):
            filename = frame_summary.f_code.co_filename
            if filename and not filename.startswith("<"):
                filename = os.path.abspath(filename)
            frame = Frame(
                filename=filename or "?",
                lineno=line_no,
                name=frame_summary.f_code.co_name,
                locals={
                    key: to_repr(value, max_string=locals_max_string)
                    for key, value in frame_summary.f_locals.items()
                }
                if show_locals
                else None,
            )
            append(frame)

        cause = getattr(exc_value, "__cause__", None)
        if cause and cause.__traceback__:
            exc_type = cause.__class__
            exc_value = cause
            traceback = cause.__traceback__
            is_cause = True
            continue

        cause = exc_value.__context__
        if (
            cause
            and cause.__traceback__
            and not getattr(exc_value, "__suppress_context__", False)
        ):
            exc_type = cause.__class__
            exc_value = cause
            traceback = cause.__traceback__
            is_cause = False
            continue

        # No cover, code is reached but coverage doesn't recognize it.
        break  # pragma: no cover

    return Trace(stacks=stacks)


class ExceptionDictTransformer:
    """
    Return a list of exception stack dictionaries for an exception.

    These dictionaries are based on :class:`Stack` instances generated by
    :func:`extract()` and can be dumped to JSON.

    :param show_locals: Whether or not to include the values of a stack frame's
        local variables.
    :param locals_max_string: The maximum length after which long string
        representations are truncated.
    :param max_frames: Maximum number of frames in each stack.  Frames are
        removed from the inside out.  The idea is, that the first frames
        represent your code responsible for the exception and last frames the
        code where the exception actually happened.  With larger web
        frameworks, this does not always work, so you should stick with the
        default.
    """

    def __init__(
        self,
        show_locals: bool = True,
        locals_max_string: int = LOCALS_MAX_STRING,
        max_frames: int = MAX_FRAMES,
    ) -> None:
        if locals_max_string < 0:
            raise ValueError(
                f'"locals_max_string" must be >= 0: {locals_max_string}'
            )
        if max_frames < 2:
            raise ValueError(f'"max_frames" must be >= 2: {max_frames}')
        self.show_locals = show_locals
        self.locals_max_string = locals_max_string
        self.max_frames = max_frames

    def __call__(self, exc_info: ExcInfo) -> list[dict[str, Any]]:
        trace = extract(
            *exc_info,
            show_locals=self.show_locals,
            locals_max_string=self.locals_max_string,
        )

        for stack in trace.stacks:
            if len(stack.frames) <= self.max_frames:
                continue

            half = (
                self.max_frames // 2
            )  # Force int division to handle odd numbers correctly
            fake_frame = Frame(
                filename="",
                lineno=-1,
                name=f"Skipped frames: {len(stack.frames) - (2 * half)}",
            )
            stack.frames[:] = [
                *stack.frames[:half],
                fake_frame,
                *stack.frames[-half:],
            ]

        return [asdict(stack) for stack in trace.stacks]
