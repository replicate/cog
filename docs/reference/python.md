# Prediction interface reference

This document defines the API of the `cog` Python module, which is used to define the interface for running predictions on your model.

## Contents

- [Contents](#contents)
- [`BasePredictor`](#basepredictor)
  - [`Predictor.setup()`](#predictorsetup)
  - [`Predictor.predict(**kwargs)`](#predictorpredictkwargs)
- [`async` predictors and concurrency](#async-predictors-and-concurrency)
- [`Input(**kwargs)`](#inputkwargs)
  - [Deprecating inputs](#deprecating-inputs)
- [Output](#output)
  - [Returning an object](#returning-an-object)
  - [Returning a list](#returning-a-list)
  - [Optional properties](#optional-properties)
  - [Streaming output](#streaming-output)
- [Metrics](#metrics)
  - [Recording metrics](#recording-metrics)
  - [Accumulation modes](#accumulation-modes)
  - [Dot-path keys](#dot-path-keys)
  - [Type safety](#type-safety)
- [Cancellation](#cancellation)
  - [`CancelationException`](#cancelationexception)
- [Input and output types](#input-and-output-types)
- [`File()`](#file)
- [`Path()`](#path)
- [`Secret`](#secret)
- [`Optional`](#optional)
- [`List`](#list)

## `BasePredictor`

A class that defines how Cog runs predictions on your model. Subclass `BasePredictor` and implement `setup()` and `predict()`:

```python
from cog import BasePredictor, Path, Input
import torch

class Predictor(BasePredictor):
    def setup(self):
        """Load the model into memory to make running multiple predictions efficient"""
        self.model = torch.load("weights.pth")

    def predict(self,
            image: Path = Input(description="Image to enlarge"),
            scale: float = Input(description="Factor to scale image by", default=1.5)
    ) -> Path:
        """Run a single prediction on the model"""
        # ... pre-processing ...
        output = self.model(image)
        # ... post-processing ...
        return output
```

### `Predictor.setup()`

Optional method. Runs once when the model container starts. Use it for expensive one-off operations such as loading trained models or instantiating data transformations.

Models may download weights in `setup()` (e.g. using [`pget`](https://github.com/replicate/pget)) or store weights directly in the image. See [cog.yaml `build` reference](yaml.md#build) for image configuration options.

### `Predictor.predict(**kwargs)`

Required method. Runs a single prediction.

Takes an arbitrary list of named arguments, where each argument name must correspond to an [`Input()`](#inputkwargs) annotation.

Returns strings, numbers, [`cog.Path`](#path) objects representing files on disk, or lists or dicts of those types. You can also define a custom [`Output()`](#output) for complex return types.

## `async` predictors and concurrency

> Added in cog 0.14.0.

The `predict()` method may be declared as `async def predict(...)`. When async, `setup()` may also be async:

```py
class Predictor(BasePredictor):
    async def setup(self) -> None:
        ...

    async def predict(self) -> str:
        return "hello world"
```

Models with an async `predict()` function can run predictions concurrently, up to the limit specified by [`concurrency.max`](yaml.md#max) in `cog.yaml`. Exceeding this limit returns a `409 Conflict` response.

For a practical guide to concurrency, see [How to configure concurrency](../how-to/concurrency.md).

## `Input(**kwargs)`

Defines a parameter for the `predict()` method:

```py
class Predictor(BasePredictor):
    def predict(self,
            image: Path = Input(description="Image to enlarge"),
            scale: float = Input(description="Factor to scale image by", default=1.5, ge=1.0, le=10.0)
    ) -> Path:
```

The `Input()` function takes these keyword arguments:

- `description`: A description of what to pass to this input for users of the model.
- `default`: A default value to set the input to. If this argument is not passed, the input is required. If it is explicitly set to `None`, the input is optional.
- `ge`: For `int` or `float` types, the value must be greater than or equal to this number.
- `le`: For `int` or `float` types, the value must be less than or equal to this number.
- `min_length`: For `str` types, the minimum length of the string.
- `max_length`: For `str` types, the maximum length of the string.
- `regex`: For `str` types, the string must match this regular expression.
- `choices`: For `str` or `int` types, a list of possible values for this input.
- `deprecated`: (optional) If set to `True`, marks this input as deprecated. Deprecated inputs will still be accepted, but tools and UIs may warn users that the input is deprecated and may be removed in the future. See [Deprecating inputs](#deprecating-inputs).

Each parameter of the `predict()` method must be annotated with a type like `str`, `int`, `float`, `bool`, etc. See [Input and output types](#input-and-output-types) for the full list of supported types.

Using the `Input` function provides better documentation and validation constraints to the users of your model, but it is not strictly required. Default values may be specified using plain Python:

```py
class Predictor(BasePredictor):
    def predict(self,
        prompt: str = "default prompt", # this is valid
        iterations: int                 # also valid
    ) -> str:
        # ...
```

## Deprecating inputs

Mark an input as deprecated by passing `deprecated=True` to the `Input()` function. Deprecated inputs are still accepted, but tools and UIs may warn users that the input may be removed in a future version.

```py
from cog import BasePredictor, Input

class Predictor(BasePredictor):
    def predict(self,
        text: str = Input(description="Some deprecated text", deprecated=True),
        prompt: str = Input(description="Prompt for the model")
    ) -> str:
        return prompt
```

## Output

Cog predictors can return a simple data type like a string, number, float, or boolean. Use Python's `-> <type>` syntax to annotate the return type.

```py
from cog import BasePredictor

class Predictor(BasePredictor):
    def predict(self) -> str:
        return "hello"
```

### Returning an object

To return a complex object with multiple values, define an `Output` object with multiple fields:

```py
from cog import BasePredictor, BaseModel, File

class Output(BaseModel):
    file: File
    text: str

class Predictor(BasePredictor):
    def predict(self) -> Output:
        return Output(text="hello", file=io.StringIO("hello"))
```

Each of the output object's properties must be one of the supported output types. See [Input and output types](#input-and-output-types). The output class must be named `Output`.

### Returning a list

The `predict()` method can return a list of any supported output type:

```py
from cog import BasePredictor, Path

class Predictor(BasePredictor):
    def predict(self) -> list[Path]:
        predictions = ["foo", "bar", "baz"]
        output = []
        for i, prediction in enumerate(predictions):
            out_path = Path(f"/tmp/out-{i}.txt")
            with out_path.open("w") as f:
                f.write(prediction)
            output.append(out_path)
        return output
```

Files are named in the format `output.<index>.<extension>`, e.g. `output.0.txt`, `output.1.txt`, `output.2.txt`.

### Optional properties

To conditionally omit properties from the Output object, define them using `typing.Optional`:

```py
from cog import BaseModel, BasePredictor, Path
from typing import Optional

class Output(BaseModel):
    score: Optional[float]
    file: Optional[Path]

class Predictor(BasePredictor):
    def predict(self) -> Output:
        if condition:
            return Output(score=1.5)
        else:
            return Output(file=io.StringIO("hello"))
```

### Streaming output

Cog models can stream output as `predict()` runs. The return type annotation takes the form `-> Iterator[<type>]` where `<type>` can be `str`, `int`, `float`, `bool`, or `cog.Path`. Use `yield` to emit each chunk.

| Predictor type | Return type | Iterator type |
|----------------|-------------|---------------|
| Sync | `Iterator[T]` | `typing.Iterator` |
| Async | `AsyncIterator[T]` | `cog.AsyncIterator` |
| Sync (concatenated text) | `ConcatenateIterator[str]` | `cog.ConcatenateIterator` |
| Async (concatenated text) | `AsyncConcatenateIterator[str]` | `cog.AsyncConcatenateIterator` |

`ConcatenateIterator` and `AsyncConcatenateIterator` hint that output chunks should be concatenated into a single string.

```py
from cog import BasePredictor, ConcatenateIterator

class Predictor(BasePredictor):
    def predict(self) -> ConcatenateIterator[str]:
        for token in ["The", "quick", "brown", "fox"]:
            yield token + " "
```

For a practical guide to streaming, see [How to stream output](../how-to/streaming.md).

## Metrics

Custom metrics can be recorded from `predict()` to track model-specific data like token counts, timing breakdowns, or confidence scores. Metrics are included in the prediction response.

### Recording metrics

Use `self.record_metric()` inside `predict()`:

```python
from cog import BasePredictor

class Predictor(BasePredictor):
    def predict(self, prompt: str) -> str:
        self.record_metric("temperature", 0.7)
        self.record_metric("token_count", 42)

        result = self.model.generate(prompt)
        return result
```

For dict-style access, use `self.scope`:

```python
self.scope.metrics["token_count"] = 42
del self.scope.metrics["token_count"]
```

Metrics appear in the prediction response `metrics` field:

```json
{
  "status": "succeeded",
  "output": "...",
  "metrics": {
    "temperature": 0.7,
    "token_count": 42,
    "predict_time": 1.23
  }
}
```

The `predict_time` metric is always added automatically by the runtime. If you set `predict_time` yourself, the runtime value takes precedence.

Supported value types are `bool`, `int`, `float`, `str`, `list`, and `dict`. Setting a metric to `None` deletes it.

### Accumulation modes

By default, recording a metric replaces any previous value for that key. The `mode` parameter accepts `"replace"` (default), `"incr"`, or `"append"`:

```python
# Increment a counter (adds to existing numeric value)
self.record_metric("token_count", 1, mode="incr")
self.record_metric("token_count", 1, mode="incr")
# Result: {"token_count": 2}

# Append to an array
self.record_metric("steps", "preprocessing", mode="append")
self.record_metric("steps", "inference", mode="append")
# Result: {"steps": ["preprocessing", "inference"]}

# Replace (default)
self.record_metric("status", "running", mode="replace")
self.record_metric("status", "done", mode="replace")
# Result: {"status": "done"}
```

### Dot-path keys

Use dot-separated keys to create nested objects in the metrics output:

```python
self.record_metric("timing.preprocess", 0.12)
self.record_metric("timing.inference", 0.85)
```

Produces:

```json
{
  "metrics": {
    "timing": {
      "preprocess": 0.12,
      "inference": 0.85
    },
    "predict_time": 1.23
  }
}
```

### Type safety

Once a metric key has been assigned a value of a certain type, it cannot be changed to a different type without deleting it first:

```python
self.record_metric("count", 1)

# This would raise an error -- "count" is an int, not a string:
# self.record_metric("count", "oops")

# Delete first, then set with new type:
del self.scope.metrics["count"]
self.record_metric("count", "now a string")
```

Outside an active prediction, `self.record_metric()` and `self.scope` are silent no-ops.

## Cancellation

When a prediction is canceled (via the [cancel HTTP endpoint](http.md#post-predictionsprediction_idcancel) or a dropped connection), the Cog runtime interrupts the running `predict()` function. The exception raised depends on the predictor type:

| Predictor type              | Exception raised         |
| --------------------------- | ------------------------ |
| Sync (`def predict`)        | `CancelationException`   |
| Async (`async def predict`) | `asyncio.CancelledError` |

### `CancelationException`

```python
from cog import CancelationException
```

`CancelationException` is raised in **sync** predictors when a prediction is cancelled. It is a `BaseException` subclass (not an `Exception` subclass), meaning bare `except Exception` blocks will not catch it -- matching the behavior of `KeyboardInterrupt` and `asyncio.CancelledError`.

The runtime manages cancellation automatically. If cleanup logic is needed on cancellation, catch and re-raise:

```python
from cog import BasePredictor, CancelationException, Path

class Predictor(BasePredictor):
    def predict(self, image: Path) -> Path:
        try:
            return self.process(image)
        except CancelationException:
            self.cleanup()
            raise  # always re-raise
```

> [!WARNING]
> You **must** re-raise `CancelationException` after cleanup. Swallowing it will prevent the runtime from marking the prediction as canceled, and may result in the termination of the container.

`CancelationException` is available as:

- `cog.CancelationException` (recommended)
- `cog.exceptions.CancelationException`

For **async** predictors, cancellation follows standard Python async conventions and raises `asyncio.CancelledError` instead.

## Input and output types

Each parameter of the `predict()` method must be annotated with a type. The method's return type must also be annotated. The supported types are:

- `str`: a string
- `int`: an integer
- `float`: a floating point number
- `bool`: a boolean
- [`cog.File`](#file): a file-like object representing a file
- [`cog.Path`](#path): a path to a file on disk
- [`cog.Secret`](#secret): a string containing sensitive information

## `File()`

> [!WARNING]
> `cog.File` is deprecated and will be removed in a future version of Cog. Use [`cog.Path`](#path) instead.

The `cog.File` object is used to get files in and out of models. It represents a _file handle_.

For models that return a `cog.File` object, the prediction output returned by Cog's built-in HTTP server will be a URL.

```python
from cog import BasePredictor, File, Input, Path
from PIL import Image

class Predictor(BasePredictor):
    def predict(self, source_image: File = Input(description="Image to enlarge")) -> File:
        pillow_img = Image.open(source_image)
        upscaled_image = do_some_processing(pillow_img)
        return File(upscaled_image)
```

## `Path()`

The `cog.Path` object is used to get files in and out of models. It represents a _path to a file on disk_.

`cog.Path` is a subclass of Python's [`pathlib.Path`](https://docs.python.org/3/library/pathlib.html#basic-use) and can be used as a drop-in replacement.

For models that return a `cog.Path` object, the prediction output returned by Cog's built-in HTTP server will be a URL.

```python
import tempfile
from cog import BasePredictor, Input, Path

class Predictor(BasePredictor):
    def predict(self, image: Path = Input(description="Image to enlarge")) -> Path:
        upscaled_image = do_some_processing(image)
        output_path = Path(tempfile.mkdtemp()) / "upscaled.png"
        upscaled_image.save(output_path)
        return Path(output_path)
```

## `Secret`

The `cog.Secret` type signifies that an input holds sensitive information, like a password or API token.

`cog.Secret` redacts its contents in string representations to prevent accidental disclosure. Access the value with `get_secret_value()`.

```python
from cog import BasePredictor, Secret

class Predictor(BasePredictor):
    def predict(self, api_token: Secret) -> None:
        print(api_token)                    # Prints '**********'
        print(api_token.get_secret_value()) # Prints the actual value
```

OpenAPI schema representation:

```json
{
  "type": "string",
  "format": "password",
  "x-cog-secret": true
}
```

Models uploaded to Replicate treat secret inputs differently: any value passed to a `Secret` input is redacted after being sent to the model.

> [!WARNING]
> Passing secret values to untrusted models can result in
> unintended disclosure, exfiltration, or misuse of sensitive data.

## `Optional`

Optional inputs should be explicitly defined as `Optional[T]` so that type checkers can warn about error-prone `None` values.

```python
class Predictor(BasePredictor):
    def predict(self, prompt: Optional[str]=Input(description="prompt")) -> str:
        if prompt is None:
            return "hello"
        else:
            return "hello" + prompt
```

Note: `default=None` is redundant when using `Optional`, as `Optional` implies it. The error-prone usage of `prompt: str=Input(default=None)` may raise an error in a future release of Cog.

## `List`

The List type is supported in inputs. It can hold any supported type.

```py
class Predictor(BasePredictor):
   def predict(self, paths: list[Path]) -> str:
       output_parts = []
       for path in paths:
           with open(path) as f:
             output_parts.append(f.read())
       return "".join(output_parts)
```

Multiple values for a list input are passed by repeating the input name:

```bash
$ cog predict -i paths=@1.txt -i paths=@2.txt
```
