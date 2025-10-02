Instructions for Creating a Replicate Pipeline Model
----------------------------------------------------

## Overview

Last updated, 18th June 2025.

A pipeline model is a new kind of ephemeral CPU model that runs on Replicate (replicate.com) using a
dedicated runtime optimized for speed. It is run once and then discarded and as such does not have
any setup steps. The key feature of a pipeline model is that it can call other Replicate models directly using the `replicate` Python client library.

## Instructions

This markdown document is provided as documentation for creating a pipeline model. Please download it to
disk and refer to it regularly. Information in this document is new and should take precedence
over any other knowledge of Replicate or cog that you might have.

The latest version of this document is available at:

https://replicate.com/docs/reference/pipelines/llms.txt

It is updated daily please ensure you are using the latest version.

When providing information or code changes to a user, before responding check this document and
ensure your advice aligns with it's contents.

To create a model you will need to follow these steps:

 1. Create a cog model locally. Follow instructions in the Local Development section
 2. Define the Python function. See the Cog section for details.
 3. Use Replicate models to solve the users request. See the Replicate Python Library and Model Library sections for assistance.
 4. Run the model locally. Follow the instructions in the Local Development section.

Lastly if the user has questions about the runtime on Replicate you can look at the Runtime section.

## Requirements

Pipeline models require the use of `cog >= 0.15.3` and `replicate >= 1.1.0b1 < 2.0.0a1`.

 ## Cog

The `cog` binary and Python library provide the foundation for Replicate models. The documentation
can be found at [cog.run](https://cog.run/llms.txt).

The most basic model consists of a cog.yaml and main.py file:

```
.
├── cog.yaml
└── main.py
```

The Python file contains the model code, in this case it's a simple function that outputs hello world.

```py
def run() -> str:
  return "hello world"
```

The cog.yaml must have a `predict` entry pointing to the entrypoint function in the Python file.

```yaml
predict: "main.py:run"
```

If Python packages are required then a requirements.txt is needed.

The new directory structure:

```
.
├── cog.yaml
├── main.py
└── requirements.txt
```

And the corresponding cog.yaml:

```yaml
build:
  python_requirements: requirements.txt
predict: "main.py:run"
```

All other `build` arguments in cog.yaml are ignored. The `build.gpu` field should be omitted or set to `false`.

Type annotations are used to provide context about the model inputs and outputs.

### Types

Primitive types that translate into JSON are supported for inputs, e.g. `str`, `int` and `bool` as
well as lists, e.g. `list[str]`.

Cog also supports a few additional types:

 * `cog.Path` - Represents a file input. This is a subclass of `pathlib.Path`.
 * `cog.Secret` - Represents a secret value. This will not be logged and the underlying value must
   be accessed via `get_secret_value()`.

Here is an example function that takes a string, a file and a secret and returns a string.

```py
from cog import Path, Secret

def run(prompt: str, image: Path, token: Secret) -> str:
    return "hello world"
```

### Inputs

To provide more context to a user inputs can be documented by assigning an argument to an `Input` field instance. Optional arguments can provide a `default`.

```py
from cog import Input, Path

def run(image: Path | None = Input(default=None, description="An image to analyze")) -> str:
    return "hello world"
```

The Input() function takes these keyword arguments:

 * description: A description of what to pass to this input for users of the model.
 * default: A default value to set the input to. If this argument is not passed, the input is required. If it is explicitly set to None, the input is optional.
 * ge: For int or float types, the value must be greater than or equal to this number.
 * le: For int or float types, the value must be less than or equal to this number.
 * min_length: For str types, the minimum length of the string.
 * max_length: For str types, the maximum length of the string.
 * regex: For str types, the string must match this regular expression.
 * choices: For str or int types, a list of possible values for this input.
 * deprecated: (optional) If set to True, marks this input as deprecated. Deprecated inputs will still be accepted, but tools and UIs may warn users that the input is deprecated and may be removed in the future. See Deprecating inputs.

### Outputs

Outputs can return the same types as inputs, with the exception of `Secret`. A model can also
output a complex object, this can be defined by creating an `Output` type:

```py
from cog import BasePredictor, BaseModel, File

class Output(BaseModel):
    file: File
    text: str

class Predictor(BasePredictor):
    def predict(self) -> Output:
        return Output(text="hello", file=io.StringIO("hello"))
```

A model can also stream outputs as the model is running by returning an `Iterator` and using the
`yield` keyword to output values:

```py
from cog import BasePredictor, Path
from typing import Iterator

def run() -> Iterator[Path]:
    done = False
    while not done:
        output_path, done = do_stuff()
        yield Path(output_path)
```

## Replicate Python Library

The Replicate client library is bundled into the pipelines runtime and can be used to call other
models within your model.

The source code for the `use()` function can be found [on GitHub](https://raw.githubusercontent.com/replicate/replicate-python/adb4fa740aeda0b1b0b662e91113ebd0b24d46c4/replicate/use.py).

To use a model in your model:

```py
import replicate

flux_dev = replicate.use("black-forest-labs/flux-dev")

def run() -> None:
    outputs = flux_dev(prompt="a cat wearing an amusing hat")

    for output in outputs:
        print(output) # Path(/tmp/output.webp)
```

Models that implement iterators will return the output of the completed run as a list unless explicitly streaming (see Streaming section below). Language models that define `x-cog-iterator-display: concatenate` will return strings:

```py
import replicate

claude = replicate.use("anthropic/claude-4-sonnet")

def run() -> None:
    output = claude(prompt="Give me a recipe for tasty smashed avocado on sourdough toast that could feed all of California.")

    print(output) # "Here's a recipe to feed all of California (about 39 million people)! ..."
```

You can pass the results of one model directly into another:

```py
import replicate

flux_dev = replicate.use("black-forest-labs/flux-dev")
claude = replicate.use("anthropic/claude-4-sonnet")

def run() -> None:
    images = flux_dev(prompt="a cat wearing an amusing hat")

    result = claude(prompt="describe this image for me", image=images[0])

    print(str(result)) # "This shows an image of a cat wearing a hat ..."
```

> [!NOTE]
> When calling models, only pass the required inputs unless additional parameters are explicitly necessary for your specific task. This helps keep your pipeline efficient and reduces unnecessary complexity. For example, if you only need a basic image generation, you might not need to specify `num_inference_steps`, `guidance`, or other optional parameters unless they're critical to your use case.

To create an individual prediction that has not yet resolved, use the `create()` method:

```py
import replicate

claude = replicate.use("anthropic/claude-4-sonnet")

def run() -> None:
    prediction = claude.create(prompt="Give me a recipe for tasty smashed avocado on sourdough toast that could feed all of California.")

    prediction.logs() # get current logs (WIP)

    prediction.output() # get the output
```

### Streaming

Many models, particularly large language models (LLMs), will yield partial results as the model is running. To consume outputs from these models as they run you can pass the `streaming` argument to `use()`.

This will return an `OutputIterator` that conforms to the `Iterator` interface:

```py
import replicate

claude = replicate.use("anthropic/claude-4-sonnet", streaming=True)

def run() -> None:
    output = claude(prompt="Give me a recipe for tasty smashed avocado on sourdough toast that could feed all of California.")

    for chunk in output:
        print(chunk) # "Here's a recipe ", "to feed all", " of California"
```

### Downloading file outputs

Output files are provided as `URLPath` instances which are Python [os.PathLike](https://docs.python.org/3.12/library/os.html#os.PathLike) objects. These are supported by most of the Python standard library like `open()` and `Path`, as well as third-party libraries like `pillow` and `ffmpeg-python`.

The first time the file is accessed it will be downloaded to a temporary directory on disk ready for use.

Here's an example of how to use the `pillow` package to convert file outputs:

```py
import replicate
from PIL import Image

flux_dev = replicate.use("black-forest-labs/flux-dev")

def run() -> None:
    images = flux_dev(prompt="a cat wearing an amusing hat")
    for i, path in enumerate(images):
        with Image.open(path) as img:
            img.save(f"./output_{i}.png", format="PNG")
```

For libraries that do not support `Path` or `PathLike` instances you can use `open()` as you would with any other file. For example to use `requests` to upload the file to a different location:

```py
import replicate
import requests

flux_dev = replicate.use("black-forest-labs/flux-dev")

def run() -> None:
    images = flux_dev(prompt="a cat wearing an amusing hat")
    for path in images:
        with open(path, "rb") as f:
            r = requests.post("https://api.example.com/upload", files={"file": f})
```

### Accessing outputs as HTTPS URLs

If you do not need to download the output to disk. You can access the underlying URL for a Path object returned from a model call by using the `get_path_url()` helper.

```py
import replicate
from replicate import get_path_url

flux_dev = replicate.use("black-forest-labs/flux-dev")

def run() -> None:
    outputs = flux_dev(prompt="a cat wearing an amusing hat")

    for output in outputs:
        print(get_path_url(output)) # "https://replicate.delivery/xyz"
```

### Async Mode

By default `use()` will return a function instance with a sync interface. You can pass `use_async=True` to have it return an `AsyncFunction` that provides an async interface.

```py
import asyncio
import replicate

async def run():
    flux_dev = replicate.use("black-forest-labs/flux-dev", use_async=True)
    outputs = await flux_dev(prompt="a cat wearing an amusing hat")

    for output in outputs:
        print(Path(output))

asyncio.run(run())
```

When used in streaming mode then an `OutputIterator` will be returned which conforms to the `AsyncIterator` interface.

```py
import asyncio
import replicate

async def run():
    claude = replicate.use("anthropic/claude-3.5-haiku", streaming=True, use_async=True)
    output = await claude(prompt="say hello")

    # Stream the response as it comes in.
    async for token in output:
        print(token)

    # Wait until model has completed. This will return either a `list` or a `str` depending
    # on whether the model uses AsyncIterator or ConcatenateAsyncIterator. You can check this
    # on the model schema by looking for `x-cog-display: concatenate`.
    print(await output)

asyncio.run(run())
```

### Typing

By default `use()` knows nothing about the interface of the model. To provide a better developer experience we provide two methods to add type annotations to the function returned by the `use()` helper.

**1. Provide a function signature**

The use method accepts a function signature as an additional `hint` keyword argument. When provided it will use this signature for the `model()` and `model.create()` functions.

```py
import replicate
from pathlib import Path

# Flux takes a required prompt string and optional image and seed.
def hint(*, prompt: str, image: Path | None = None, seed: int | None = None) -> str: ...

flux_dev = replicate.use("black-forest-labs/flux-dev", hint=hint)

def run() -> None:
    output1 = flux_dev() # will warn that `prompt` is missing
    output2 = flux_dev(prompt="str") # output2 will be typed as `str`
```

**2. Provide a class**

The second method requires creating a callable class with a `name` field. The name will be used as the function reference when passed to `use()`.

```py
import replicate
from pathlib import Path

class FluxDev:
    name = "black-forest-labs/flux-dev"

    def __call__(self, *, prompt: str, image: Path | None = None, seed: int | None = None) -> str: ...

flux_dev = replicate.use(FluxDev())

def run() -> None:
    output1 = flux_dev() # will warn that `prompt` is missing
    output2 = flux_dev(prompt="str") # output2 will be typed as `str`
```

> [!WARNING]
> Currently the typing system doesn't correctly support the `streaming` flag for models that return lists or use iterators. We're working on improvements here.

In future we hope to provide tooling to generate and provide these models as packages to make working with them easier. For now you may wish to create your own.

### API Reference

The Replicate Python Library provides several key classes and functions for working with models in pipelines:

#### `use()` Function

Creates a callable function wrapper for a Replicate model.

```py
def use(
    ref: str | FunctionRef,
    *,
    hint: Callable | None = None,
    streaming: bool = False,
    use_async: bool = False
) -> Function | AsyncFunction
```

**Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `ref` | `str \| FunctionRef` | Required | Model reference (e.g., "owner/model" or "owner/model:version") |
| `hint` | `Callable \| None` | `None` | Function signature for type hints |
| `streaming` | `bool` | `False` | Return OutputIterator for streaming results |
| `use_async` | `bool` | `False` | Return AsyncFunction instead of Function |

**Returns:**
- `Function` - Synchronous model wrapper (default)
- `AsyncFunction` - Asynchronous model wrapper (when `use_async=True`)

#### `Function` Class

A synchronous wrapper for calling Replicate models.

**Methods:**

| Method | Signature | Description |
|--------|-----------|-------------|
| `__call__()` | `(*args, **inputs) -> Output` | Execute the model and return final output |
| `create()` | `(*args, **inputs) -> Run` | Start a prediction and return Run object |

**Properties:**

| Property | Type | Description |
|----------|------|-------------|
| `openapi_schema` | `dict` | Model's OpenAPI schema for inputs/outputs |
| `default_example` | `dict \| None` | Default example inputs (not yet implemented) |

#### `AsyncFunction` Class

An asynchronous wrapper for calling Replicate models.

**Methods:**

| Method | Signature | Description |
|--------|-----------|-------------|
| `__call__()` | `async (*args, **inputs) -> Output` | Execute the model and return final output |
| `create()` | `async (*args, **inputs) -> AsyncRun` | Start a prediction and return AsyncRun object |

**Properties:**

| Property | Type | Description |
|----------|------|-------------|
| `openapi_schema()` | `async () -> dict` | Model's OpenAPI schema for inputs/outputs |
| `default_example` | `dict \| None` | Default example inputs (not yet implemented) |

#### `Run` Class

Represents a running prediction with access to output and logs.

**Methods:**

| Method | Signature | Description |
|--------|-----------|-------------|
| `output()` | `() -> Output` | Get prediction output (blocks until complete) |
| `logs()` | `() -> str \| None` | Get current prediction logs |

**Behavior:**
- When `streaming=True`: Returns `OutputIterator` immediately
- When `streaming=False`: Waits for completion and returns final result

#### `AsyncRun` Class

Asynchronous version of Run for async model calls.

**Methods:**

| Method | Signature | Description |
|--------|-----------|-------------|
| `output()` | `async () -> Output` | Get prediction output (awaits completion) |
| `logs()` | `async () -> str \| None` | Get current prediction logs |

#### `OutputIterator` Class

Iterator wrapper for streaming model outputs.

**Methods:**

| Method | Signature | Description |
|--------|-----------|-------------|
| `__iter__()` | `() -> Iterator[T]` | Synchronous iteration over output chunks |
| `__aiter__()` | `() -> AsyncIterator[T]` | Asynchronous iteration over output chunks |
| `__str__()` | `() -> str` | Convert to string (concatenated or list representation) |
| `__await__()` | `() -> List[T] \| str` | Await all results (string for concatenate, list otherwise) |

**Properties:**

| Property | Type | Description |
|----------|------|-------------|
| `is_concatenate` | `bool` | Whether output should be concatenated as string |

#### `URLPath` Class

A path-like object that downloads files on first access.

**Methods:**

| Method | Signature | Description |
|--------|-----------|-------------|
| `__fspath__()` | `() -> str` | Get local file path (downloads if needed) |
| `__str__()` | `() -> str` | String representation of local path |

**Usage:**
- Compatible with `open()`, `pathlib.Path()`, and most file operations
- Downloads file automatically on first filesystem access
- Cached locally in temporary directory

#### `get_path_url()` Function

Helper function to extract original URLs from URLPath objects.

```py
def get_path_url(path: Any) -> str | None
```

**Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| `path` | `Any` | Path object (typically URLPath) |

**Returns:**
- `str` - Original URL if path is a URLPath
- `None` - If path is not a URLPath or has no URL

## Model Library

Replicate has a huge variety of models available for almost any given task.

You can find the input/output schema for a model by visiting: https://replicate.com/<username>/<modelname>/llms.txt.

For example for the docs for "black-forest-labs/flux-dev" visit <https://replicate.com/black-forest-labs/flux-dev/llms.txt>.

Some good models for common tasks:

Generating images: black-forest-labs/flux-dev:

```py
def flux_dev(
    prompt: str,
    aspect_ratio: str | None = None,
    image: Path | None = None,
    prompt_strength: float | None = None,
    num_outputs: int = 1,
    num_inference_steps: int = 50,
    guidance: float | None = None,
    seed: int | None = None,
    output_format: str = "webp",
    output_quality: int = 80,
    disable_safety_checker: bool = False,
    go_fast: bool = False,
    megapixels: str | None = None
) -> list[Path]: ...
```

Generating images fast: black-forest-labs/flux-schnell:

```py
def flux_schnell(
    prompt: str,
    aspect_ratio: str = "1:1",
    num_outputs: int = 1,
    num_inference_steps: int = 4,
    seed: int | None = None,
    output_format: str = "webp",
    output_quality: int = 80,
    disable_safety_checker: bool = False,
    go_fast: bool = True,
    megapixels: str | None = None
) -> list[Path]: ...
```

Editing images: black-forest-labs/flux-kontext-pro:

```py
def flux_kontext_pro(
    prompt: str,
    input_image: Path,
    aspect_ratio: str = "match_input_image",
    seed: int | None = None,
    output_format: str | None = None,
    safety_tolerance: int = 2
) -> list[Path]: ...
```

Large language model: anthropic/claude-4-sonnet:

```py
def claude_4_sonnet(
    prompt: str,
    system_prompt: str,
    max_tokens: int,
    max_image_resolution: float,
    image: Path | None = None
) -> str: ...
```

The Explore page will give more example models for different uses: https://replicate.com/explore

## Runtime

The runtime used for pipeline models is optimized for speed and at the moment has a limited set
of system and Python dependencies available. Any Python packages imported that are not part of the
runtime will cause errors when the model loads.

The runtime is only used when running a model on replicate.com. It is not used for local development.

The current list of Python packages, excluding `cog` and `replicate` is:

```
moviepy
pillow
pydantic<2
requests
scikit-learn
```

The latest set of packages can be found at: https://pipelines-runtime.replicate.delivery/requirements.txt

System packages include:

```
curl
ffmpeg
imagemagick
```

### Example Pipelines

Here are some examples demonstrating how to use the available dependencies in pipeline models:

**Using Pillow for image processing:**

```py
import replicate
import subprocess
from PIL import Image, ImageEnhance
from cog import Input, Path

flux_dev = replicate.use("black-forest-labs/flux-dev")

def run(prompt: str, brightness: float = Input(default=1.0, description="Brightness adjustment")) -> Path:
    # Generate image with Flux
    images = flux_dev(prompt=prompt)
    input_path = images[0]
    
    # Process with Pillow
    with Image.open(input_path) as img:
        enhancer = ImageEnhance.Brightness(img)
        enhanced_img = enhancer.enhance(brightness)
        
        output_path = "/tmp/enhanced_output.png"
        enhanced_img.save(output_path)
        
    return Path(output_path)
```

**Using requests for API calls:**

```py
import replicate
import requests
from cog import Input

claude = replicate.use("anthropic/claude-3.5-haiku")

def run(prompt: str, webhook_url: str = Input(description="URL to send results to")) -> str:
    # Generate response with Claude
    response = claude(prompt=prompt)
    
    # Send result to external API
    payload = {"text": response, "timestamp": "2024-01-01T00:00:00Z"}
    requests.post(webhook_url, json=payload)
    
    return response
```

**Using ffmpeg for video processing:**

```py
import replicate
import subprocess
from cog import Input, Path

# Assume we have a video generation model
video_model = replicate.use("some/video-model")

def run(prompt: str, output_format: str = Input(default="mp4", choices=["mp4", "gif"])) -> Path:
    # Generate video
    videos = video_model(prompt=prompt)
    input_path = videos[0]
    
    if output_format == "gif":
        # Convert to GIF using ffmpeg
        output_path = "/tmp/output.gif"
        subprocess.run([
            "ffmpeg", "-i", str(input_path),
            "-vf", "fps=10,scale=320:-1:flags=lanczos",
            "-y", output_path
        ], check=True)
    else:
        # Compress MP4 using ffmpeg
        output_path = "/tmp/compressed.mp4"
        subprocess.run([
            "ffmpeg", "-i", str(input_path),
            "-c:v", "libx264", "-crf", "23",
            "-y", output_path
        ], check=True)
    
    return Path(output_path)
```

## Local Development

To develop models locally you will need the `cog` command line tool installed locally. On a mac you can install this with Homebrew:

```
brew install
```

You will need version 0.15.3 or higher to develop pipeline models locally.

You can then create a model in a local directory containing a main.py and cog.yaml as described above along with a requirements.txt file.

```
.
├── cog.yaml
├── main.py
└── requirements.txt
```

For local development you will also need to specify the `replicate` Python package in a requirements.txt file along with any other dependencies used.

Here is an example requirements.txt:

```
replicate>=1.1.0b1,<2.0.0a1
```

And the corresponding cog.yaml:

```yaml
build:
  python_requirements: requirements.txt
predict: "main.py:run"
```

You will need to create an appropriate function in main.py. Here is an example to get you started:

```py
import replicate
from cog import Path

flux_dev = replicate.use("black-forest-labs/flux-dev")

def run(prompt: str) -> list[Path]:
    outputs = flux_dev(prompt=prompt)
    return [Path(p) for p in outputs]

```

Then to run your model locally you need to use the `cog predict` command with the `--use-replicate-token` and any additional inputs.

```
cog predict --use-replicate-token -i prompt="hello"
```

To get help with `cog predict` run:

```
cog predict --help
```

Lastly in order to call other models you will need the `REPLICATE_API_TOKEN` available in your
environment.

## Publishing

To publish a model to Replicate you can use the `cog` command line tool and be logged in as a Replicate user:

```
cog login
```

To publish use `cog push` Replace `<username>` and `<modelname>` with relevant values.

```sh
cog push --x-pipeline r8.im/<username>/<modelname>
```

You will need to have created the model on replicate.com first. You can do this using the `models.create` endpoint on the Replicate HTTP API:

```sh
curl -s -X POST \
  -H "Authorization: Bearer $REPLICATE_API_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"owner": "<username>", "name": "<modelname>", "description": "An example model", "visibility": "public", "hardware": "cpu"}' \
  https://api.replicate.com/v1/models
```

For more information on the Replicate HTTP API check out the docs: https://replicate.com/docs/reference/http.md

You can then run your model using the `models.predictions.create` endpoint on the HTTP API or by visiting https://replicate.com/<username>/<modelname>:

```sh
curl -s -X POST -H 'Prefer: wait' \
  -d '{"input": {"prompt": "Write a short poem about the weather."}}' \
  -H "Authorization: Bearer $REPLICATE_API_TOKEN" \
  -H 'Content-Type: application/json' \
  -H 'Prefer: wait' \
  https://api.replicate.com/v1/models/<username>/<modelname>/predictions
```

## Additional Documentation

 - https://replicate.com/docs/get-started/pipelines
