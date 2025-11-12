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
from typing import (
    Any,
    BinaryIO,
    Callable,
    Dict,
    List,
    Sequence,
    TextIO,
    Union,
    get_args,
    get_type_hints,
)

import pydantic
from fastapi import FastAPI
from fastapi.routing import APIRoute
from pydantic import BaseModel
from typing_extensions import Self  # added to typing in python 3.11

from ..predictor import is_none, is_optional
from ..types import PYDANTIC_V2
from .errors import CogRuntimeError, CogTimeoutError


class _SimpleStreamWrapper(io.TextIOWrapper):
    """
    _SimpleStreamWrapper wraps a binary I/O buffer and provides a TextIOWrapper
    interface (primarily write and flush methods) which call a provided
    callback function instead of (or, if `tee` is True, in addition to) writing
    to the underlying buffer.
    """

    def __init__(
        self,
        buffer: BinaryIO,
        callback: Callable[[str, str], None],
        tee: bool = False,
    ) -> None:
        super().__init__(buffer)

        self._callback = callback
        self._tee = tee
        self._buffer = []

    def write(self, s: str) -> int:
        length = len(s)
        self._buffer.append(s)
        if self._tee:
            super().write(s)

        if "\n" in s or "\r" in s:
            self.flush()

        return length

    def flush(self) -> None:
        self._callback(self.name, "".join(self._buffer))
        self._buffer.clear()
        if self._tee:
            super().flush()


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

    class _SimpleStreamRedirectorBase(contextlib.AbstractContextManager):
        pass
else:

    class _SimpleStreamRedirectorBase(
        contextlib.AbstractContextManager["SimpleStreamRedirector"]
    ):
        pass


class SimpleStreamRedirector(_SimpleStreamRedirectorBase):
    """
    SimpleStreamRedirector is a context manager that redirects I/O streams to a
    callback function. If `tee` is True, it also writes output to the original
    streams.

    Unlike StreamRedirector, the underlying stream file descriptors are not
    modified, which means that only stream writes from Python code will be
    captured. Writes from native code will not be captured.

    Unlike StreamRedirector, the streams redirected cannot be configured. The
    context manager is only able to redirect STDOUT and STDERR.
    """

    def __init__(
        self,
        callback: Callable[[str, str], None],
        tee: bool = False,
    ) -> None:
        self._callback = callback
        self._tee = tee

        stdout_wrapper = _SimpleStreamWrapper(sys.stdout.buffer, callback, tee)
        stderr_wrapper = _SimpleStreamWrapper(sys.stderr.buffer, callback, tee)
        self._stdout_ctx = contextlib.redirect_stdout(stdout_wrapper)
        self._stderr_ctx = contextlib.redirect_stderr(stderr_wrapper)

    def __enter__(self) -> Self:
        self._stdout_ctx.__enter__()
        self._stderr_ctx.__enter__()
        return self

    def __exit__(
        self,
        exc_type: type[BaseException] | None,
        exc_value: BaseException | None,
        traceback: TracebackType | None,
    ) -> None:
        self._stdout_ctx.__exit__(exc_type, exc_value, traceback)
        self._stderr_ctx.__exit__(exc_type, exc_value, traceback)

    def drain(self, timeout: float = 0.0) -> None:
        # Draining isn't complicated for SimpleStreamRedirector, since we're not
        # moving data between threads. We just need to flush the streams.
        sys.stdout.flush()
        sys.stderr.flush()


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

else:

    def get_annotations(tp) -> dict[str, Any]:
        if sys.version_info >= (3, 10):
            return get_type_hints(tp)
        return tp.__annotations__

    def is_pydantic_model_type(tp) -> bool:
        try:
            return isinstance(tp, type) and issubclass(tp, BaseModel)
        except TypeError:
            return False

    def update_nullable_optional(openapi_schema: Dict[str, Any], app: FastAPI) -> None:
        def fetch_referenced_schema(schema: Dict[str, Any], ref: str) -> Dict[str, Any]:
            input_path = ref.replace("#/", "").split("/")
            referenced_schema = schema
            while input_path:
                referenced_schema = referenced_schema[input_path[0]]
                input_path = input_path[1:]
            return referenced_schema

        for route in app.routes:
            if not isinstance(route, APIRoute):
                continue

            for dep in route.dependant.body_params:
                model = getattr(dep, "type_", None)
                if not is_pydantic_model_type(model):
                    continue
                input_model_union = get_annotations(model).get("input")
                if input_model_union is None:
                    continue
                input_model = get_args(input_model_union)[0]
                schema_node = openapi_schema["components"]["schemas"].get(
                    model.__name__
                )
                referenced_schema = fetch_referenced_schema(
                    openapi_schema, schema_node["properties"]["input"]["$ref"]
                )
                for k, v in referenced_schema["properties"].items():
                    annotated_type = get_annotations(input_model)[k]
                    if is_optional(annotated_type):
                        v["nullable"] = True

            response_model = getattr(route, "response_model", None)
            if is_pydantic_model_type(response_model):
                output_model_union = get_annotations(response_model).get("output")
                if output_model_union is None:
                    continue
                output_model = get_args(output_model_union)[0]
                schema_node = openapi_schema["components"]["schemas"].get(
                    output_model.__name__
                )
                root = get_annotations(output_model).get("__root__")
                for type_arg in get_args(root):
                    if not is_none(type_arg):
                        continue
                    schema_node["nullable"] = True
                    break
                for count, type_node in enumerate(schema_node.get("anyOf", [])):
                    ref_node = type_node.get("$ref")
                    if ref_node is None:
                        continue
                    referenced_schema = fetch_referenced_schema(
                        openapi_schema, ref_node
                    )
                    output_model = get_args(root)[count]
                    for k, v in referenced_schema["properties"].items():
                        annotated_type = get_annotations(output_model)[k]
                        if is_optional(annotated_type):
                            v["nullable"] = True

        return None


def update_openapi_schema_for_pydantic_2(
    openapi_schema: Dict[str, Any],
) -> None:
    _remove_webhook_events_filter_title(openapi_schema)
    _update_nullable_anyof(openapi_schema)
    _flatten_selected_allof_refs(openapi_schema)
    _extract_enum_properties(openapi_schema)
    _set_default_enumeration_description(openapi_schema)
    _restore_allof_for_prediction_id_put(openapi_schema)
    _ensure_nullable_properties_not_required(openapi_schema)


def _remove_webhook_events_filter_title(
    openapi_schema: Dict[str, Any],
) -> None:
    try:
        del openapi_schema["components"]["schemas"]["PredictionRequest"]["properties"][
            "webhook_events_filter"
        ]["title"]
    except KeyError:
        pass


def _update_nullable_anyof(
    openapi_schema: Union[Dict[str, Any], List[Dict[str, Any]]],
    in_header: Union[bool, None] = None,
) -> None:
    # Version 3.0.X of OpenAPI doesn't support a `null` type, expecting
    # `nullable` to be set instead.
    if isinstance(openapi_schema, dict):
        if in_header is None:
            if "in" in openapi_schema:
                in_header = openapi_schema.get("in") == "header"
        for key, value in list(openapi_schema.items()):
            if key != "anyOf" or not isinstance(value, list):
                _update_nullable_anyof(value, in_header=in_header)
                continue

            non_null_items = [item for item in value if item.get("type") != "null"]
            if len(non_null_items) == 0:
                del openapi_schema[key]
            elif len(non_null_items) == 1:
                openapi_schema.update(non_null_items[0])
                del openapi_schema[key]
            else:
                openapi_schema[key] = non_null_items

            if len(non_null_items) < len(value) and not in_header:
                openapi_schema["nullable"] = True

    elif isinstance(openapi_schema, list):  # type: ignore
        for item in openapi_schema:
            _update_nullable_anyof(item, in_header=in_header)


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


def _ensure_nullable_properties_not_required(openapi_schema: Dict[str, Any]) -> None:
    schemas = openapi_schema["components"]["schemas"]
    for schema in schemas.values():
        properties = schema.get("properties", {})
        nullable = {k for k, v in properties.items() if v.get("nullable", False)}

        if "required" in schema and nullable:
            schema["required"] = [k for k in schema["required"] if k not in nullable]
