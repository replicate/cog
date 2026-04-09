# Prediction interface reference

This document defines the API of the `cog` Python module, which is used to define the interface for running predictions on your model.

> [!TIP]
> Run [`cog init`](getting-started-own-model.md#initialization) to generate an annotated `run.py` file that can be used as a starting point for setting up your model.

> [!TIP]
> Using a language model to help you write the code for your new Cog model?
>
> Feed it [https://cog.run/llms.txt](https://cog.run/llms.txt), which has all of Cog's documentation bundled into a single file. To learn more about this format, check out [llmstxt.org](https://llmstxt.org).

## Contents

- [Contents](#contents)
- [`BaseRunner`](#baserunner)
  - [`Runner.setup()`](#runnersetup)
  - [`Runner.run(**kwargs)`](#runnerrunkwargs)
- [`async` runners and concurrency](#async-runners-and-concurrency)
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
  - [Primitive types](#primitive-types)
  - [`cog.Path`](#cogpath)
  - [`cog.File` (deprecated)](#cogfile-deprecated)
  - [`cog.Secret`](#cogsecret)
  - [Wrapper types](#wrapper-types)
    - [`Optional`](#optional)
    - [`list`](#list)
    - [`dict`](#dict)
  - [Structured output with `BaseModel`](#structured-output-with-basemodel)
    - [Using `cog.BaseModel`](#using-cogbasemodel)
    - [Using Pydantic `BaseModel`](#using-pydantic-basemodel)
    - [`BaseModel` field types](#basemodel-field-types)
  - [Type limitations](#type-limitations)

## `BaseRunner`

You define how Cog runs predictions on your model by defining a class that inherits from `BaseRunner`. It looks something like this:

```python
from cog import BaseRunner, Path, Input
import torch

class Runner(BaseRunner):
    def setup(self):
        """Load the model into memory to make running multiple predictions efficient"""
        self.model = torch.load("weights.pth")

    def run(self,
            image: Path = Input(description="Image to enlarge"),
            scale: float = Input(description="Factor to scale image by", default=1.5)
    ) -> Path:
        """Run a single prediction on the model"""
        # ... pre-processing ...
        output = self.model(image)
        # ... post-processing ...
        return output
```

> [!NOTE]
> `BasePredictor` and `predict()` still work for backwards compatibility, but `BaseRunner` and `run()` are preferred for new models.

Your Runner class should define two methods: `setup()` and `run()`.

### `Runner.setup()`

Prepare the model so multiple predictions run efficiently.

Use this _optional_ method to include expensive one-off operations like loading trained models, instantiating data transformations, etc.

Many models use this method to download their weights (e.g. using [`pget`](https://github.com/replicate/pget)). This has some advantages:

- Smaller image sizes
- Faster build times
- Faster pushes and inference on [Replicate](https://replicate.com)

However, this may also significantly increase your `setup()` time.

As an alternative, some choose to store their weights directly in the image. You can simply leave your weights in the directory alongside your `cog.yaml` and ensure they are not excluded in your `.dockerignore` file.

While this will increase your image size and build time, it offers other advantages:

- Faster `setup()` time
- Ensures idempotency and reduces your model's reliance on external systems
- Preserves reproducibility as your model will be self-contained in the image

> When using this method, you should use the `--separate-weights` flag on `cog build` to store weights in a [separate layer](https://github.com/replicate/cog/blob/12ac02091d93beebebed037f38a0c99cd8749806/docs/getting-started.md?plain=1#L219).

### `Runner.run(**kwargs)`

Run a single prediction.

This _required_ method is where you call the model that was loaded during `setup()`, but you may also want to add pre- and post-processing code here.

The `run()` method takes an arbitrary list of named arguments, where each argument name must correspond to an [`Input()`](#inputkwargs) annotation.

`run()` can return strings, numbers, [`cog.Path`](#cogpath) objects representing files on disk, or lists or dicts of those types. You can also define a custom [`BaseModel`](#structured-output-with-basemodel) for structured return types. See [Input and output types](#input-and-output-types) for the full list of supported types.

## `async` runners and concurrency

> Added in cog 0.14.0.

You may specify your `run()` method as `async def run(...)`. In
addition, if you have an async `run()` function you may also have an async
`setup()` function:

```py
class Runner(BaseRunner):
    async def setup(self) -> None:
        print("async setup is also supported...")

    async def run(self) -> str:
        print("async run");
        return "hello world";
```

Models that have an async `run()` function can run predictions concurrently, up to the limit specified by [`concurrency.max`](yaml.md#max) in cog.yaml. Attempting to exceed this limit will return a 409 Conflict response.

## `Input(**kwargs)`

Use cog's `Input()` function to define each of the parameters in your `run()` method:

```py
class Runner(BaseRunner):
    def run(self,
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

Each parameter of the `run()` method must be annotated with a type like `str`, `int`, `float`, `bool`, etc. See [Input and output types](#input-and-output-types) for the full list of supported types.

Using the `Input` function provides better documentation and validation constraints to the users of your model, but it is not strictly required. You can also specify default values for your parameters using plain Python, or omit default assignment entirely:

```py
class Runner(BaseRunner):
    def run(self,
        prompt: str = "default prompt", # this is valid
        iterations: int                 # also valid
    ) -> str:
        # ...
```

## Deprecating inputs

You can mark an input as deprecated by passing `deprecated=True` to the `Input()` function. Deprecated inputs will still be accepted, but tools and UIs may warn users that the input is deprecated and may be removed in the future.

This is useful when you want to phase out an input without breaking existing clients immediately:

```py
from cog import BaseRunner, Input

class Runner(BaseRunner):
    def run(self,
        text: str = Input(description="Some deprecated text", deprecated=True),
        prompt: str = Input(description="Prompt for the model")
    ) -> str:
        # ...
        return prompt
```

## Output

Cog runners can return a simple data type like a string, number, float, or boolean. Use Python's `-> <type>` syntax to annotate the return type.

Here's an example of a runner that returns a string:

```py
from cog import BaseRunner

class Runner(BaseRunner):
    def run(self) -> str:
        return "hello"
```

### Returning an object

To return a complex object with multiple values, define an `Output` object with multiple fields to return from your `run()` method:

```py
from cog import BaseRunner, BaseModel, File

class Output(BaseModel):
    file: File
    text: str

class Runner(BaseRunner):
    def run(self) -> Output:
        return Output(text="hello", file=io.StringIO("hello"))
```

Each of the output object's properties must be one of the supported output types. For the full list, see [Input and output types](#input-and-output-types).

### Returning a list

The `run()` method can return a list of any of the supported output types. Here's an example that outputs multiple files:

```py
from cog import BaseRunner, Path

class Runner(BaseRunner):
    def run(self) -> list[Path]:
        predictions = ["foo", "bar", "baz"]
        output = []
        for i, prediction in enumerate(predictions):
            out_path = Path(f"/tmp/out-{i}.txt")
            with out_path.open("w") as f:
                f.write(prediction)
            output.append(out_path)
        return output
```

Files are named in the format `output.<index>.<extension>`, e.g. `output.0.txt`, `output.1.txt`, and `output.2.txt` from the example above.

### Optional properties

To conditionally omit properties from the Output object, define them using `typing.Optional`:

```py
from cog import BaseModel, BaseRunner, Path
from typing import Optional

class Output(BaseModel):
    score: Optional[float]
    file: Optional[Path]

class Runner(BaseRunner):
    def run(self) -> Output:
        if condition:
            return Output(score=1.5)
        else:
            return Output(file=io.StringIO("hello"))
```

### Streaming output

Cog models can stream output as the `run()` method is running. For example, a language model can output tokens as they're being generated and an image generation model can output images as they are being generated.

To support streaming output in your Cog model, add `from typing import Iterator` to your run.py file. The `typing` package is a part of Python's standard library so it doesn't need to be installed. Then add a return type annotation to the `run()` method in the form `-> Iterator[<type>]` where `<type>` can be one of `str`, `int`, `float`, `bool`, or `cog.Path`.

```py
from cog import BaseRunner, Path
from typing import Iterator

class Runner(BaseRunner):
    def run(self) -> Iterator[Path]:
        done = False
        while not done:
            output_path, done = do_stuff()
            yield Path(output_path)
```

If you have an [async `run()` method](#async-runners-and-concurrency), use `AsyncIterator` from the `typing` module:

```py
from typing import AsyncIterator
from cog import BaseRunner, Path

class Runner(BaseRunner):
    async def run(self) -> AsyncIterator[Path]:
        done = False
        while not done:
            output_path, done = do_stuff()
            yield Path(output_path)
```

If you're streaming text output, you can use `ConcatenateIterator` to hint that the output should be concatenated together into a single string. This is useful on Replicate to display the output as a string instead of a list of strings.

```py
from cog import BaseRunner, Path, ConcatenateIterator

class Runner(BaseRunner):
    def run(self) -> ConcatenateIterator[str]:
        tokens = ["The", "quick", "brown", "fox", "jumps", "over", "the", "lazy", "dog"]
        for token in tokens:
            yield token + " "
```

Or for async `run()` methods, use `AsyncConcatenateIterator`:

```py
from cog import BaseRunner, Path, AsyncConcatenateIterator

class Runner(BaseRunner):
    async def run(self) -> AsyncConcatenateIterator[str]:
        tokens = ["The", "quick", "brown", "fox", "jumps", "over", "the", "lazy", "dog"]
        for token in tokens:
            yield token + " "
```

## Metrics

You can record custom metrics from your `run()` function to track model-specific data like token counts, timing breakdowns, or confidence scores. Metrics are included in the prediction response alongside the output.

### Recording metrics

Use `self.record_metric()` inside your `run()` method:

```python
from cog import BaseRunner

class Runner(BaseRunner):
    def run(self, prompt: str) -> str:
        self.record_metric("temperature", 0.7)
        self.record_metric("token_count", 42)

        result = self.model.generate(prompt)
        return result
```

For advanced use (dict-style access, deleting metrics), use `self.scope`:

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

The `predict_time` metric is always added automatically by the runtime.

Supported value types are `bool`, `int`, `float`, `str`, `list`, and `dict`. Setting a metric to `None` deletes it.

### Naming rules

Metric names must follow these rules:

- Each segment must start with a letter (`a-z`, `A-Z`) and end with a letter or digit
- Segments can contain letters, digits, and underscores (`_`)
- Segments cannot start or end with underscores
- Segments cannot contain consecutive underscores (`__`)
- Use dots (`.`) to create nested objects (e.g., `timing.inference` produces `{"timing": {"inference": ...}}`)
- Maximum 128 characters total
- Maximum 4 dot-separated segments
- Cannot be `predict_time` (reserved by runtime)
- Cannot start with `cog.` (reserved for system metrics)

Valid examples: `temperature`, `token_count`, `TTFT`, `T2I_latency`, `timing.preprocess`

Invalid examples: `_token`, `token_`, `foo__bar`, `.foo`, `foo..bar`, `foo bar`, `cog.system`

### Accumulation modes

By default, recording a metric replaces any previous value for that key. You can use accumulation modes to build up values across multiple calls:

```python
# Increment a counter (adds to the existing numeric value)
self.record_metric("token_count", 1, mode="incr")
self.record_metric("token_count", 1, mode="incr")
# Result: {"token_count": 2}

# Append to an array
self.record_metric("steps", "preprocessing", mode="append")
self.record_metric("steps", "inference", mode="append")
# Result: {"steps": ["preprocessing", "inference"]}

# Replace (default behavior)
self.record_metric("status", "running", mode="replace")
self.record_metric("status", "done", mode="replace")
# Result: {"status": "done"}
```

The `mode` parameter accepts `"replace"` (default), `"incr"`, or `"append"`.

### Dot-path keys

Use dot-separated keys to create nested objects in the metrics output:

```python
self.record_metric("timing.preprocess", 0.12)
self.record_metric("timing.inference", 0.85)
```

This produces nested JSON:

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

Once a metric key has been assigned a value of a certain type, it cannot be changed to a different type without deleting it first. This prevents accidental type mismatches when using accumulation modes:

```python
self.record_metric("count", 1)

# This would raise an error — "count" is an int, not a string:
# self.record_metric("count", "oops")

# Delete first, then set with new type:
del self.scope.metrics["count"]
self.record_metric("count", "now a string")
```

Outside an active prediction, `self.record_metric()` and `self.scope` are silent no-ops — no need for `None` checks.

## Cancellation

When a prediction is canceled (via the [cancel HTTP endpoint](http.md#post-predictionsprediction_idcancel) or a dropped connection), the Cog runtime interrupts the running `run()` function. The exception raised depends on whether the runner is sync or async:

| Runner type                 | Exception raised         |
| --------------------------- | ------------------------ |
| Sync (`def run`)            | `CancelationException`   |
| Async (`async def run`)     | `asyncio.CancelledError` |

### `CancelationException`

```python
from cog import CancelationException
```

`CancelationException` is raised in **sync** runners when a prediction is cancelled. It is a `BaseException` subclass — **not** an `Exception` subclass. This means bare `except Exception` blocks in your run code will not accidentally catch it, matching the behavior of `KeyboardInterrupt` and `asyncio.CancelledError`.

You do **not** need to handle this exception in normal runner code — the runtime manages cancellation automatically. However, if you need to run cleanup logic when a prediction is cancelled, you can catch it explicitly:

```python
from cog import BaseRunner, CancelationException, Path

class Runner(BaseRunner):
    def run(self, image: Path) -> Path:
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

For **async** runners, cancellation follows standard Python async conventions and raises `asyncio.CancelledError` instead.

## Input and output types

Each parameter of the `run()` method must be annotated with a type. The method's return type must also be annotated.

### Primitive types

These types can be used directly as input parameter types and output return types:

| Type | Description | JSON Schema |
|------|-------------|-------------|
| `str` | A string | `string` |
| `int` | An integer | `integer` |
| `float` | A floating-point number | `number` |
| `bool` | A boolean | `boolean` |
| [`cog.Path`](#cogpath) | A path to a file on disk | `string` (format: `uri`) |
| [`cog.File`](#cogfile-deprecated) | A file-like object (deprecated) | `string` (format: `uri`) |
| [`cog.Secret`](#cogsecret) | A string containing sensitive information | `string` (format: `password`) |

### `cog.Path`

`cog.Path` is used to get files in and out of models. It represents a _path to a file on disk_.

`cog.Path` is a subclass of Python's [`pathlib.Path`](https://docs.python.org/3/library/pathlib.html#basic-use) and can be used as a drop-in replacement. Any `os.PathLike` subclass is also accepted as an input type and treated as `cog.Path`.

For models that return a `cog.Path` object, the prediction output returned by Cog's built-in HTTP server will be a URL.

This example takes an input file, resizes it, and returns the resized image:

```python
import tempfile
from cog import BaseRunner, Input, Path

class Runner(BaseRunner):
    def run(self, image: Path = Input(description="Image to enlarge")) -> Path:
        upscaled_image = do_some_processing(image)

        # To output cog.Path objects the file needs to exist, so create a temporary file first.
        # This file will automatically be deleted by Cog after it has been returned.
        output_path = Path(tempfile.mkdtemp()) / "upscaled.png"
        upscaled_image.save(output_path)
        return Path(output_path)
```

### `cog.File` (deprecated)

> [!WARNING]  
> `cog.File` is deprecated and will be removed in a future version of Cog. Use [`cog.Path`](#cogpath) instead.

`cog.File` represents a _file handle_. For models that return a `cog.File` object, the prediction output returned by Cog's built-in HTTP server will be a URL.

```python
from cog import BaseRunner, File, Input
from PIL import Image

class Runner(BaseRunner):
    def run(self, source_image: File = Input(description="Image to enlarge")) -> File:
        pillow_img = Image.open(source_image)
        upscaled_image = do_some_processing(pillow_img)
        return File(upscaled_image)
```

### `cog.Secret`

`cog.Secret` signifies that an input holds sensitive information like a password or API token.

`cog.Secret` redacts its contents in string representations to prevent accidental disclosure. Access the underlying value with `get_secret_value()`.

```python
from cog import BaseRunner, Secret

class Runner(BaseRunner):
    def run(self, api_token: Secret) -> None:
        # Prints '**********'
        print(api_token)

        # Use get_secret_value method to see the secret's content.
        print(api_token.get_secret_value())
```

A runner's `Secret` inputs are represented in OpenAPI with the following schema:

```json
{
  "type": "string",
  "format": "password",
  "x-cog-secret": true
}
```

Models uploaded to Replicate treat secret inputs differently throughout its system. When you create a prediction on Replicate, any value passed to a `Secret` input is redacted after being sent to the model.

> [!WARNING]  
> Passing secret values to untrusted models can result in
> unintended disclosure, exfiltration, or misuse of sensitive data.

### Wrapper types

Cog supports wrapper types that modify how a primitive type is treated.

#### `Optional`

Use `Optional[T]` or `T | None` (Python 3.10+) to mark an input as optional. Optional inputs default to `None` if not provided.

```python
from typing import Optional
from cog import BaseRunner, Input

class Runner(BaseRunner):
    def run(self,
        prompt: Optional[str] = Input(description="Input prompt"),
        seed: int | None = Input(description="Random seed", default=None),
    ) -> str:
        if prompt is None:
            return "hello"
        return "hello " + prompt
```

Prefer `Optional[T]` or `T | None` over `str = Input(default=None)` for inputs that can be `None`. This lets type checkers warn about error-prone `None` values:

```python
# Bad: type annotation says str but value can be None
def run(self, prompt: str = Input(default=None)) -> str:
    return "hello" + prompt  # TypeError at runtime if prompt is None

# Good: type annotation matches actual behavior
def run(self, prompt: Optional[str] = Input(description="prompt")) -> str:
    if prompt is None:
        return "hello"
    return "hello " + prompt
```

> [!NOTE]
> `Optional[T]` is supported in `BaseModel` output fields but **not** as a top-level return type. Use a `BaseModel` with optional fields instead.

#### `list`

Use `list[T]` or `List[T]` to accept or return a list of values. `T` can be a supported Cog type, but nested container types are not supported.

**As an input type:**

```py
from cog import BaseRunner, Path

class Runner(BaseRunner):
    def run(self, paths: list[Path]) -> str:
        output_parts = []
        for path in paths:
            with open(path) as f:
                output_parts.append(f.read())
        return "".join(output_parts)
```

With `cog run`, repeat the input name to pass multiple values:

```bash
$ echo test1 > 1.txt
$ echo test2 > 2.txt
$ cog run -i paths=@1.txt -i paths=@2.txt
```

**As an output type:**

```py
from cog import BaseRunner, Path

class Runner(BaseRunner):
    def run(self) -> list[Path]:
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

#### `dict`

Use `dict` to accept or return an opaque JSON object. The value is passed through as-is without type validation.

```python
from cog import BaseRunner, Input

class Runner(BaseRunner):
    def run(self,
        params: dict = Input(description="Arbitrary JSON parameters"),
    ) -> dict:
        return {"greeting": "hello", "params": params}
```

> [!NOTE]
> `dict` inputs and outputs are represented as `{"type": "object"}` in the OpenAPI schema with no additional structure. For structured data with validated fields, use a [`BaseModel`](#structured-output-with-basemodel) instead.

### Structured output with `BaseModel`

To return a complex object with multiple typed fields, define a class that inherits from `cog.BaseModel` or Pydantic's `BaseModel` and use it as your return type.

#### Using `cog.BaseModel`

`cog.BaseModel` subclasses are automatically converted to Python dataclasses. Define fields using standard type annotations:

```python
from typing import Optional
from cog import BaseRunner, BaseModel, Path

class Output(BaseModel):
    text: str
    confidence: float
    image: Optional[Path]

class Runner(BaseRunner):
    def run(self, prompt: str) -> Output:
        result = self.model.generate(prompt)
        return Output(
            text=result.text,
            confidence=result.score,
            image=None,
        )
```

The output class can have any name — it does not need to be called `Output`:

```python
from cog import BaseModel

class SegmentationResult(BaseModel):
    success: bool
    error: Optional[str]
    segmented_image: Optional[Path]
```

#### Using Pydantic `BaseModel`

If you already use Pydantic v2 in your model, you can use a Pydantic `BaseModel` subclass directly as the output type:

```python
from pydantic import BaseModel as PydanticBaseModel
from cog import BaseRunner

class Result(PydanticBaseModel):
    name: str
    score: float
    tags: list[str]

class Runner(BaseRunner):
    def run(self, prompt: str) -> Result:
        return Result(name="example", score=0.95, tags=["fast", "accurate"])
```

#### `BaseModel` field types

Fields in a `BaseModel` output support these types:

| Type | Example |
|------|---------|
| `str`, `int`, `float`, `bool` | `score: float` |
| `cog.Path` | `image: Path` |
| `cog.File` | `data: File` (deprecated) |
| `cog.Secret` | `token: Secret` |
| `Optional[T]` | `error: Optional[str]` |
| `list[T]` | `tags: list[str]` |

### Type limitations

The following type patterns are **not** supported:

- **Nested generics**: `list[list[str]]`, `list[Optional[str]]`, `Optional[list[str]]` are not supported.
- **Union types beyond Optional**: `str | int`, `Union[str, int, None]` — only `Optional[T]` (i.e. `T | None`) is supported.
- **`Optional` as a top-level return type**: `-> Optional[str]` is not allowed. Use a `BaseModel` with optional fields instead.
- **Nested `BaseModel` fields**: A `BaseModel` field typed as another `BaseModel` is not supported in Cog's type system for schema generation.
- **Tuple, Set, or other collection types**: Only `list` and `dict` are supported as collection types.
