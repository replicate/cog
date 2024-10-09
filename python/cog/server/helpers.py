from __future__ import annotations

import contextlib
import ctypes
import io
import os
import re
import selectors
import sys
import threading
import uuid
from types import TracebackType
from typing import Any, Callable, Dict, List, Sequence, TextIO, Union

import pydantic
from typing_extensions import Self

from ..types import PYDANTIC_V2
from .errors import CogRuntimeError, CogTimeoutError


class _StreamWrapper:
    def __init__(self, name: str, stream: TextIO) -> None:
        self.name = name
        self._stream = stream
        self._original_fp: TextIO | None = None
        self._wrapped_fp: TextIO | None = None

    def wrap(self) -> None:
        if self._wrapped_fp or self._original_fp:
            raise CogRuntimeError("stream is already wrapped")

        r, w = os.pipe()

        # Save a copy of the stream file descriptor.
        original_fd = self._stream.fileno()
        original_fd_copy = os.dup(original_fd)

        # Overwrite the stream file descriptor with the write end of the pipe.
        os.dup2(w, self._stream.fileno())
        os.close(w)

        # Create a writeable file object with the original FD. This can be used
        # to write to the original destination of the passed stream.
        self._original_fp = os.fdopen(original_fd_copy, "w")

        # Create a readable file object with the read end of the pipe. This can
        # be used to read any writes to the passed stream.
        #
        # We set the FD to be non-blocking so that we can select/poll/epoll
        # over multiple wrapped streams.
        os.set_blocking(r, False)
        self._wrapped_fp = os.fdopen(r, "r")

    def unwrap(self) -> None:
        if not self._wrapped_fp or not self._original_fp:
            raise CogRuntimeError("stream is not wrapped (call wrap first)")

        # Put the original file descriptor back.
        os.dup2(self._original_fp.fileno(), self._stream.fileno())

        # Close the write end of the pipe.
        self._original_fp.close()
        self._original_fp = None

        # Close the read end of the pipe.
        self._wrapped_fp.close()
        self._wrapped_fp = None

    def write(self, data: str) -> int:
        return self._stream.write(data)

    def flush(self) -> None:
        return self._stream.flush()

    @property
    def wrapped(self) -> TextIO:
        if not self._wrapped_fp:
            raise CogRuntimeError("stream is not wrapped (call wrap first)")
        return self._wrapped_fp

    @property
    def original(self) -> TextIO:
        if not self._original_fp:
            raise CogRuntimeError("stream is not wrapped (call wrap first)")
        return self._original_fp


if sys.version_info < (3, 9):

    class _StreamRedirectorBase(contextlib.AbstractContextManager):
        pass
else:

    class _StreamRedirectorBase(contextlib.AbstractContextManager["StreamRedirector"]):
        pass


class StreamRedirector(_StreamRedirectorBase):
    """
    StreamRedirector is a context manager that redirects I/O streams to a
    callback function. If `tee` is True, it also writes output to the original
    streams.

    If `streams` is not provided, it defaults to redirecting the process's
    STDOUT and STDERR file descriptors.
    """

    def __init__(
        self,
        callback: Callable[[str, str], None],
        tee: bool = False,
        streams: Sequence[TextIO] = None,
    ) -> None:
        self._callback = callback
        self._tee = tee

        self._depth = 0
        self._drain_token = uuid.uuid4().hex
        self._drain_event = threading.Event()
        self._terminate_token = uuid.uuid4().hex

        if not streams:
            streams = [sys.stdout, sys.stderr]
        self._streams = [_StreamWrapper(s.name, s) for s in streams]

    def __enter__(self) -> Self:
        self._depth += 1

        if self._depth == 1:
            for s in self._streams:
                s.wrap()

            self._thread = threading.Thread(target=self._start)
            self._thread.start()

        return self

    def __exit__(
        self,
        exc_type: type[BaseException] | None,
        exc_value: BaseException | None,
        traceback: TracebackType | None,
    ) -> None:
        self._depth -= 1

        if self._depth == 0:
            self._stop()
            self._thread.join()

            for s in self._streams:
                s.unwrap()

    def drain(self, timeout: float = 1) -> None:
        self._drain_event.clear()
        for stream in self._streams:
            stream.write(self._drain_token + "\n")
            stream.flush()
        if not self._drain_event.wait(timeout=timeout):
            raise CogTimeoutError("output streams failed to drain")

    def _start(self) -> None:
        selector = selectors.DefaultSelector()

        should_exit = False
        drain_tokens_seen = 0
        drain_tokens_needed = 0
        buffers = {}

        for stream in self._streams:
            selector.register(stream.wrapped, selectors.EVENT_READ, stream)
            buffers[stream.name] = io.StringIO()
            drain_tokens_needed += 1

        while not should_exit:
            for key, _ in selector.select():
                stream = key.data

                for line in stream.wrapped:
                    if not line.endswith("\n"):
                        # TODO: limit how much we're prepared to buffer on a
                        # single line
                        buffers[stream.name].write(line)
                        continue

                    full_line = buffers[stream.name].getvalue() + line.strip()

                    # Reset buffer (this is quicker and easier than resetting
                    # the existing buffer, but may generate more garbage).
                    buffers[stream.name] = io.StringIO()

                    if full_line.endswith(self._terminate_token):
                        should_exit = True
                        full_line = full_line[: -len(self._terminate_token)]

                    if full_line.endswith(self._drain_token):
                        drain_tokens_seen += 1
                        full_line = full_line[: -len(self._drain_token)]

                    # If full_line is empty at this point it means the only
                    # thing in the line was a drain token (or a terminate
                    # token).
                    if full_line:
                        self._callback(stream.name, full_line + "\n")
                        if self._tee:
                            stream.original.write(full_line + "\n")
                            stream.original.flush()

                    if drain_tokens_seen >= drain_tokens_needed:
                        self._drain_event.set()
                        drain_tokens_seen = 0

    def _stop(self) -> None:
        for s in self._streams:
            s.write(self._terminate_token + "\n")
            s.flush()
            break  # we only need to send the terminate token to one stream


# Precompile the regular expression
_ADDRESS_PATTERN = re.compile(r"0x[0-9A-Fa-f]+")


if PYDANTIC_V2:

    def _unwrap_pydantic_serialization_iterator(obj: Any) -> Any:
        # SerializationIterator doesn't expose the object it wraps
        # but does give us a pointer in the `__repr__` string
        match = _ADDRESS_PATTERN.search(repr(obj))
        if match:
            address = int(match.group(), 16)
            # Cast the memory address to a Python object
            return ctypes.cast(address, ctypes.py_object).value

        return obj

    def unwrap_pydantic_serialization_iterators(obj: Any) -> Any:
        """
        Unwraps instances of `pydantic_core._pydantic_core.SerializationIterator`,
        returning their underlying object so that it can be pickled when passed
        between multiprocessing workers.

        This is a temporary workaround until the following issues are fixed:
        - https://github.com/pydantic/pydantic/issues/8907
        - https://github.com/pydantic/pydantic-core/pull/1399
        - https://github.com/pydantic/pydantic-core/pull/1401
        """

        if type(obj).__name__ == "SerializationIterator":
            return _unwrap_pydantic_serialization_iterator(obj)
        if type(obj) == str:  # noqa: E721 # pylint: disable=unidiomatic-typecheck
            return obj
        if isinstance(obj, pydantic.BaseModel):
            return unwrap_pydantic_serialization_iterators(
                obj.model_dump(exclude_unset=True)
            )
        if isinstance(obj, dict):
            return {
                key: unwrap_pydantic_serialization_iterators(value)
                for key, value in obj.items()
            }
        if isinstance(obj, list):
            return [unwrap_pydantic_serialization_iterators(value) for value in obj]
        return obj


def update_openapi_schema_for_pydantic_2(
    openapi_schema: Dict[str, Any],
) -> None:
    _remove_webhook_events_filter_title(openapi_schema)
    _remove_empty_or_nullable_anyof(openapi_schema)
    _flatten_selected_allof_refs(openapi_schema)
    _extract_enum_properties(openapi_schema)
    _set_default_enumeration_description(openapi_schema)
    _restore_allof_for_prediction_id_put(openapi_schema)


def _remove_webhook_events_filter_title(
    openapi_schema: Dict[str, Any],
) -> None:
    try:
        del openapi_schema["components"]["schemas"]["PredictionRequest"]["properties"][
            "webhook_events_filter"
        ]["title"]
    except KeyError:
        pass


def _remove_empty_or_nullable_anyof(
    openapi_schema: Union[Dict[str, Any], List[Dict[str, Any]]],
) -> None:
    if isinstance(openapi_schema, dict):
        for key, value in list(openapi_schema.items()):
            if key == "anyOf" and isinstance(value, list):
                non_null_types = [item for item in value if item.get("type") != "null"]
                if len(non_null_types) == 0:
                    del openapi_schema[key]
                elif len(non_null_types) == 1:
                    openapi_schema.update(non_null_types[0])
                    del openapi_schema[key]

                    # FIXME: Update tests to expect nullable
                    # openapi_schema["nullable"] = True

            else:
                _remove_empty_or_nullable_anyof(value)
    elif isinstance(openapi_schema, list):  # pyright: ignore
        for item in openapi_schema:
            _remove_empty_or_nullable_anyof(item)


def _flatten_selected_allof_refs(
    openapi_schema: Dict[str, Any],
) -> None:
    try:
        response = openapi_schema["components"]["schemas"]["PredictionResponse"]
        response["properties"]["output"] = {"$ref": "#/components/schemas/Output"}
    except KeyError:
        pass

    try:
        path = openapi_schema["paths"]["/predictions"]["post"]
        body = path["requestBody"]
        body["content"]["application/json"]["schema"] = {
            "$ref": "#/components/schemas/PredictionRequest"
        }
    except KeyError:
        pass


def _extract_enum_properties(
    openapi_schema: Dict[str, Any],
) -> None:
    schemas = openapi_schema.get("components", {}).get("schemas", {})
    if "Input" in schemas and "properties" in schemas["Input"]:
        input_properties = schemas["Input"]["properties"]
        for prop_name, prop_value in input_properties.items():
            if "enum" in prop_value:
                # Create a new schema for the enum
                schemas[prop_name] = {
                    "type": prop_value["type"],
                    "enum": prop_value["enum"],
                    "title": prop_name,
                    "description": prop_value.get("description", "An enumeration."),
                }

                # Replace the original property with an allOf reference
                input_properties[prop_name] = {
                    "allOf": [{"$ref": f"#/components/schemas/{prop_name}"}]
                }

                # Preserve x-order if it exists
                if "x-order" in prop_value:
                    input_properties[prop_name]["x-order"] = prop_value["x-order"]


def _set_default_enumeration_description(
    openapi_schema: Union[Dict[str, Any], List[Dict[str, Any]]],
) -> None:
    if isinstance(openapi_schema, dict):
        schemas = openapi_schema.get("components", {}).get("schemas", {})
        for _key, value in list(schemas.items()):
            if isinstance(value, dict) and value.get("enum"):
                value["description"] = value.get("description", "An enumeration.")
            else:
                _set_default_enumeration_description(value)
    elif isinstance(openapi_schema, list):  # pyright: ignore
        for item in openapi_schema:
            _set_default_enumeration_description(item)


def _restore_allof_for_prediction_id_put(
    openapi_schema: Dict[str, Any],
) -> None:
    try:
        put_operation = openapi_schema["paths"]["/predictions/{prediction_id}"]["put"]
        request_body = put_operation["requestBody"]
        json_schema = request_body["content"]["application/json"]["schema"]

        if "$ref" in json_schema:
            ref = json_schema["$ref"]
            json_schema.clear()
            json_schema["allOf"] = [{"$ref": ref}]
            json_schema["title"] = "Prediction Request"
    except KeyError:
        pass

    for _key, value in (
        openapi_schema.get("components", {})
        .get("schemas", {})
        .get("Input", {})
        .get("properties", {})
        .items()
    ):
        if "$ref" in value:
            ref = value["$ref"]
            del value["$ref"]
            value["allOf"] = [{"$ref": ref}]
