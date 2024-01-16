import typing as t
from datetime import datetime
from enum import Enum

import pydantic
from pathlib import Path
from tempfile import TemporaryDirectory
import importlib.util
import os.path
import os
import sys
import subprocess

from .files import get_site_packages_bin_path

OPENAPI_SCHEMA_FILE = "openapi_schema.json"

class Status(str, Enum):
    STARTING = "starting"
    PROCESSING = "processing"
    SUCCEEDED = "succeeded"
    CANCELED = "canceled"
    FAILED = "failed"

    @staticmethod
    def is_terminal(status: t.Optional["Status"]) -> bool:
        return status in {Status.SUCCEEDED, Status.CANCELED, Status.FAILED}


class WebhookEvent(str, Enum):
    START = "start"
    OUTPUT = "output"
    LOGS = "logs"
    COMPLETED = "completed"

    @classmethod
    def default_events(cls) -> t.List["WebhookEvent"]:
        # if this is a set, it gets serialized to an array with an unstable ordering
        # so even though it's logically a set, have it as a list for deterministic schemas
        # note: this change removes "uniqueItems":true
        return [cls.START, cls.OUTPUT, cls.LOGS, cls.COMPLETED]


class PredictionBaseModel(pydantic.BaseModel, extra=pydantic.Extra.allow):
    input: t.Dict[str, t.Any]


class PredictionRequest(PredictionBaseModel):
    id: t.Optional[str]
    created_at: t.Optional[datetime]

    # TODO: deprecate this
    output_file_prefix: t.Optional[str]

    webhook: t.Optional[pydantic.AnyHttpUrl]
    webhook_events_filter: t.Optional[
        t.List[WebhookEvent]
    ] = WebhookEvent.default_events()

    @classmethod
    def with_types(cls, input_type: t.Type[t.Any]) -> t.Any:
        # [compat] Input is implicitly optional -- previous versions of the
        # Cog HTTP API allowed input to be omitted (e.g. for models that don't
        # have any inputs). We should consider changing this in future.
        return pydantic.create_model(
            cls.__name__, __base__=cls, input=(t.Optional[input_type], None)
        )


class PredictionResponse(PredictionBaseModel):
    output: t.Any

    id: t.Optional[str]
    version: t.Optional[str]

    created_at: t.Optional[datetime]
    started_at: t.Optional[datetime]
    completed_at: t.Optional[datetime]

    logs: str = ""
    error: t.Optional[str]
    status: t.Optional[Status]

    metrics: t.Optional[t.Dict[str, t.Any]]

    @classmethod
    def with_types(cls, input_type: t.Type[t.Any], output_type: t.Type[t.Any]) -> t.Any:
        # [compat] Input is implicitly optional -- previous versions of the
        # Cog HTTP API allowed input to be omitted (e.g. for models that don't
        # have any inputs). We should consider changing this in future.
        return pydantic.create_model(
            cls.__name__,
            __base__=cls,
            input=(t.Optional[input_type], None),
            output=(output_type, None),
        )


def create_schema_model(openapi_schema_path="openapi_schema.json"):
    with TemporaryDirectory() as temporary_directory_name:
        temporary_directory = Path(temporary_directory_name)
        output = Path(temporary_directory / 'model.py')
        bin_path = get_site_packages_bin_path()
        command = [f"{bin_path}/datamodel-codegen", "--input-file-type", "openapi",
                   "--input", openapi_schema_path,
                   "--output", output]
        subprocess.run(command, capture_output=True, check=True, text=True)
        module_name = os.path.basename(output).rstrip('.py')
        spec = importlib.util.spec_from_file_location(module_name, output)
        module = importlib.util.module_from_spec(spec)
        # Execute the module in its own namespace
        sys.modules[module_name] = module
        spec.loader.exec_module(module)
        return module
