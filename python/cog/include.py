import sys
import os
from dataclasses import dataclass
import inspect
from typing import Callable, Any
import replicate
from replicate.model import Model
from replicate.version import Version
from replicate.prediction import Prediction
from replicate.exceptions import ModelError


def _find_api_token() -> str:
    token = os.environ.get("REPLICATE_API_TOKEN")
    if token:
        print("Using Replicate API token from environment", file=sys.stderr)
        return token

    frame = inspect.currentframe()
    while frame:
        if "self" in frame.f_locals:
            self = frame.f_locals["self"]
            if hasattr(self, "_current_run_token"):
                token = self._current_run_token()
                return token
        frame = frame.f_back
    raise ValueError("No run token found in call stack")


@dataclass
class Run:
    function_ref: str

    def _client(self) -> replicate.Client:
        return replicate.Client(api_token=_find_api_token())

    def _split_function_ref(self) -> tuple[str, str, str | None]:
        owner, name = self.function_ref.split("/")
        name, version = name.split(":") if ":" in name else (name, None)
        return owner, name, version

    def __call__(self, **inputs: dict[str, Any]):
        version = self._version()
        prediction = self._client().predictions.create(version=version, input=inputs)
        print(f"Running prediction https://replicate.com/p/{prediction.id}")

        prediction.wait()

        if prediction.status == "failed":
            raise ModelError(prediction)

        return prediction.output

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

    @property
    def default_example(self) -> Prediction | None:
        return self._model().default_example


def include(model: str) -> Callable[..., Any]:
    return Run(model)
