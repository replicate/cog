import os
import sys
from dataclasses import dataclass
from typing import Any, Callable, Dict, Optional, Tuple

import replicate
from replicate.exceptions import ModelError
from replicate.model import Model
from replicate.prediction import Prediction
from replicate.run import _has_output_iterator_array_type
from replicate.version import Version

from cog.server.scope import current_scope


def _find_api_token() -> str:
    token = os.environ.get("REPLICATE_API_TOKEN")
    if token:
        print("Using Replicate API token from environment", file=sys.stderr)
        return token

    token = current_scope()._run_token

    if not token:
        raise ValueError("No run token found")

    return token


@dataclass
class Run:
    prediction: Prediction
    version: Version

    def wait(self) -> Any:
        self.prediction.wait()

        if self.prediction.status == "failed":
            raise ModelError(self.prediction)

        if _has_output_iterator_array_type(self.version):
            return "".join(self.prediction.output)

        return self.prediction.output

    def logs(self) -> Optional[str]:
        self.prediction.reload()

        return self.prediction.logs


@dataclass
class Function:
    function_ref: str

    def _client(self) -> replicate.Client:
        return replicate.Client(api_token=_find_api_token())

    def _split_function_ref(self) -> Tuple[str, str, Optional[str]]:
        owner, name = self.function_ref.split("/")
        name, version = name.split(":") if ":" in name else (name, None)
        return owner, name, version

    def _model(self) -> Model:
        client = self._client()
        model_owner, model_name, _ = self._split_function_ref()
        return client.models.get(f"{model_owner}/{model_name}")

    def _version(self) -> Version:
        client = self._client()
        model_owner, model_name, model_version = self._split_function_ref()
        model = client.models.get(f"{model_owner}/{model_name}")
        version = (
            model.versions.get(model_version) if model_version else model.latest_version
        )
        return version

    def __call__(self, **inputs: Dict[str, Any]) -> Any:
        run = self.start(**inputs)
        return run.wait()

    def start(self, **inputs: Dict[str, Any]) -> Run:
        version = self._version()
        prediction = self._client().predictions.create(version=version, input=inputs)
        print(f"Running {self.function_ref}: https://replicate.com/p/{prediction.id}")

        return Run(prediction, version)

    @property
    def default_example(self) -> Optional[Prediction]:
        return self._model().default_example


def include(function_ref: str) -> Callable[..., Any]:
    return Function(function_ref)
