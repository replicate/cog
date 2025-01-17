import inspect
from typing import Callable, Any
import replicate


def _find_api_token() -> str:
    frame = inspect.currentframe()
    while frame:
        if "self" in frame.f_locals:
            self = frame.f_locals["self"]
            if hasattr(self, "_current_run_token"):
                token = self._current_run_token()
                return token
        frame = frame.f_back
    raise ValueError("No run token found in call stack")


def include(model_path: str) -> Callable[..., Any]:
    def run(**inputs: dict[str, Any]) -> Any:
        client = replicate.Client(api_token=_find_api_token())
        model_owner, model_name = model_path.split("/")
        model_name, model_version = (
            model_name.split(":") if ":" in model_name else (model_name, None)
        )

        model = client.models.get(f"{model_owner}/{model_name}")
        version = (
            model.versions.get(model_version) if model_version else model.latest_version
        )

        prediction = client.predictions.create(version=version, input=inputs)
        print(f"Running prediction {prediction.id}")

        prediction.wait()
        return prediction.output

    return run
