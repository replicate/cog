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
    Set,
    TextIO,
    Type,
    Union,
    get_args,
    get_origin,
)

import pydantic
from fastapi import FastAPI
from fastapi.routing import APIRoute
from pydantic import BaseModel
from typing_extensions import Self  # added to typing in python 3.11

from ..predictor import is_optional
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

    def update_nullable_optional(openapi_schema: Dict[str, Any], app: FastAPI) -> None:
        def patch_nullable_parameters(openapi_schema: Dict[str, Any]) -> None:
            for _, methods in openapi_schema.get("paths", {}).items():
                for _, operation in methods.items():
                    for param in operation.get("parameters", []):
                        # If the parameter is optional (required: false), make it nullable
                        if not param.get("required", True):
                            schema = param.get("schema", {})
                            if "nullable" not in schema:
                                schema["nullable"] = True

        def patch_nullable_union_outputs(openapi_schema: Dict[str, Any]) -> None:
            for _, schema in (
                openapi_schema.get("components", {}).get("schemas", {}).items()
            ):
                # Look for anyOf with more than one entry
                if (
                    "anyOf" in schema
                    and isinstance(schema["anyOf"], list)
                    and len(schema["anyOf"]) > 1
                ):
                    # If it's missing nullable, and it's meant to represent an Optional/Union output
                    if "nullable" not in schema:
                        schema["nullable"] = True

        def is_pydantic_model_type(tp) -> bool:
            try:
                return isinstance(tp, type) and issubclass(tp, BaseModel)
            except TypeError:
                return False

        def extract_nullable_fields_recursive(
            model: BaseModel, prefix: str = "", is_output: bool = False
        ) -> Dict[str, bool]:
            nullable_map = {}
            for field_name, field in model.__fields__.items():
                full_field_name = f"{prefix}.{field_name}" if prefix else field_name
                type_hint = field.annotation

                if is_optional(type_hint) and (
                    full_field_name.startswith("input.") or is_output
                ):
                    nullable_map[full_field_name] = True

                inner_type = (
                    get_args(type_hint)[0] if is_optional(type_hint) else type_hint
                )
                if is_pydantic_model_type(inner_type):
                    nested = extract_nullable_fields_recursive(
                        inner_type, prefix=full_field_name, is_output=is_output
                    )
                    nullable_map.update(nested)
            return nullable_map

        def resolve_schema_ref(
            ref: str, openapi_schema: Dict[str, Any]
        ) -> Dict[str, Any]:
            parts = ref.lstrip("#/").split("/")
            node = openapi_schema
            for part in parts:
                node = node.get(part, {})
            return node

        def patch_nullable_fields_for_model(
            model: BaseModel,
            schema: Dict[str, Any],
            openapi_schema: Dict[str, Any],
            is_output: bool = False,
        ) -> None:
            nullable_fields = extract_nullable_fields_recursive(
                model, is_output=is_output
            )

            for field_path in nullable_fields:
                parts = field_path.split(".")
                node = schema

                for i, part in enumerate(parts):
                    if "properties" not in node:
                        break

                    prop = node["properties"].get(part)
                    if prop is None:
                        break

                    # Handle nested $ref
                    if "$ref" in prop:
                        node = resolve_schema_ref(prop["$ref"], openapi_schema)
                    else:
                        node = prop

                    if i == len(parts) - 1:
                        node["nullable"] = True

        def extract_pydantic_models_from_type(tp) -> Set[Type[BaseModel]]:
            """Recursively extract all Pydantic models from a response_model type."""
            models = set()

            origin = get_origin(tp)
            args = get_args(tp)

            if origin is Union or origin is list or origin is List:
                for arg in args:
                    models.update(extract_pydantic_models_from_type(arg))
            elif isinstance(tp, type) and issubclass(tp, BaseModel):
                models.add(tp)

            return models

        def collect_nested_models_from_pydantic_model(
            model: Type[BaseModel], visited=None
        ) -> Set[Type[BaseModel]]:
            """Recursively collect all nested Pydantic models inside a given model."""
            if visited is None:
                visited = set()

            if model in visited:
                return set()
            visited.add(model)

            models = {model}
            for field in model.__fields__.values():
                field_type = field.annotation
                origin = get_origin(field_type)

                if origin is Union:
                    args = get_args(field_type)
                else:
                    args = [field_type]

                for arg in args:
                    if is_pydantic_model_type(arg):
                        models.update(
                            collect_nested_models_from_pydantic_model(arg, visited)
                        )

            return models

        for route in app.routes:
            if not isinstance(route, APIRoute):
                continue

            for method in route.methods:
                method = method.lower()
                operation = openapi_schema["paths"].get(route.path, {}).get(method, {})
                if not operation:
                    continue

                response_model = getattr(route, "response_model", None)
                if response_model:
                    for model in extract_pydantic_models_from_type(response_model):
                        ref_name = model.__name__
                        schema_node = (
                            openapi_schema.get("components", {})
                            .get("schemas", {})
                            .get(ref_name)
                        )
                        if schema_node:
                            patch_nullable_fields_for_model(
                                model,
                                schema_node,
                                openapi_schema,
                            )
                            # Also patch any properties that reference other models
                            properties = schema_node.get("properties", {})

                            all_models = collect_nested_models_from_pydantic_model(
                                model
                            )

                            for field_schema in properties.values():
                                if "$ref" in field_schema:
                                    ref = field_schema["$ref"]
                                    nested_model_name = ref.split("/")[-1]
                                    nested_model = next(
                                        (
                                            m
                                            for m in all_models
                                            if m.__name__ == nested_model_name
                                        ),
                                        None,
                                    )
                                    if nested_model:
                                        nested_schema_node = resolve_schema_ref(
                                            ref, openapi_schema
                                        )
                                        if "anyOf" in nested_schema_node:
                                            for item in nested_schema_node["anyOf"]:
                                                if "$ref" in item:
                                                    inner_ref = item["$ref"]
                                                    inner_model_name = inner_ref.split(
                                                        "/"
                                                    )[-1]

                                                    inner_model = next(
                                                        (
                                                            m
                                                            for m in all_models
                                                            if m.__name__
                                                            == inner_model_name
                                                        ),
                                                        None,
                                                    )
                                                    if inner_model:
                                                        actual_schema = (
                                                            resolve_schema_ref(
                                                                inner_ref,
                                                                openapi_schema,
                                                            )
                                                        )
                                                        patch_nullable_fields_for_model(
                                                            inner_model,
                                                            actual_schema,
                                                            openapi_schema,
                                                            is_output=True,
                                                        )
                                        patch_nullable_fields_for_model(
                                            nested_model,
                                            nested_schema_node,
                                            openapi_schema,
                                        )

                request_body = (
                    operation.get("requestBody", {})
                    .get("content", {})
                    .get("application/json", {})
                )
                schema = request_body.get("schema", {})

                for dep in route.dependant.body_params:
                    model = getattr(dep, "type_", None)
                    if not model or not issubclass(model, BaseModel):
                        continue

                    # Resolve schema node for this model
                    if "$ref" in schema:
                        schema_node = resolve_schema_ref(schema["$ref"], openapi_schema)
                    elif "allOf" in schema:
                        for item in schema["allOf"]:
                            if "$ref" in item:
                                schema_node = resolve_schema_ref(
                                    item["$ref"], openapi_schema
                                )
                                break
                        else:
                            schema_node = schema
                    else:
                        schema_node = schema

                    patch_nullable_fields_for_model(model, schema_node, openapi_schema)

        patch_nullable_parameters(openapi_schema)
        patch_nullable_union_outputs(openapi_schema)


def update_openapi_schema_for_pydantic_2(
    openapi_schema: Dict[str, Any],
) -> None:
    _remove_webhook_events_filter_title(openapi_schema)
    _update_nullable_anyof(openapi_schema)
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


def _update_nullable_anyof(
    openapi_schema: Union[Dict[str, Any], List[Dict[str, Any]]],
) -> None:
    # Version 3.0.X of OpenAPI doesn't support a `null` type, expecting
    # `nullable` to be set instead.
    if isinstance(openapi_schema, dict):
        for key, value in list(openapi_schema.items()):
            if key != "anyOf" or not isinstance(value, list):
                _update_nullable_anyof(value)
                continue

            non_null_items = [item for item in value if item.get("type") != "null"]
            if len(non_null_items) == 0:
                del openapi_schema[key]
            elif len(non_null_items) == 1:
                openapi_schema.update(non_null_items[0])
                del openapi_schema[key]
            else:
                openapi_schema[key] = non_null_items

            if len(non_null_items) < len(value):
                openapi_schema["nullable"] = True

    elif isinstance(openapi_schema, list):
        for item in openapi_schema:
            _update_nullable_anyof(item)


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
