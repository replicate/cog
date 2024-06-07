# Prediction interface reference

This document defines the API of the `cog` Python module, which is used to define the interface for running predictions on your model.

Tip: Run [`cog init`](getting-started-own-model.md#initialization) to generate an annotated `predict.py` file that can be used as a starting point for setting up your model.

## Contents

- [Contents](#contents)
- [`BasePredictor`](#basepredictor)
  - [`Predictor.setup()`](#predictorsetup)
  - [`Predictor.predict(**kwargs)`](#predictorpredictkwargs)
    - [Streaming output](#streaming-output)
- [`Input(**kwargs)`](#inputkwargs)
- [Output](#output)
  - [Returning an object](#returning-an-object)
  - [Returning a list](#returning-a-list)
  - [Optional properties](#optional-properties)
- [Input and output types](#input-and-output-types)
- [`File()`](#file)
- [`Path()`](#path)
- [`Secret`](#secret)
- [`List`](#list)

## `BasePredictor`

You define how Cog runs predictions on your model by defining a class that inherits from `BasePredictor`. It looks something like this:

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

Your Predictor class should define two methods: `setup()` and `predict()`.

### `Predictor.setup()`

Prepare the model so multiple predictions run efficiently.

Use this _optional_ method to include any expensive one-off operations in here like loading trained models, instantiate data transformations, etc.

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

### `Predictor.predict(**kwargs)`

Run a single prediction.

This _required_ method is where you call the model that was loaded during `setup()`, but you may also want to add pre- and post-processing code here.

The `predict()` method takes an arbitrary list of named arguments, where each argument name must correspond to an [`Input()`](#inputkwargs) annotation.

`predict()` can return strings, numbers, [`cog.Path`](#path) objects representing files on disk, or lists or dicts of those types. You can also define a custom [`Output()`](#outputbasemodel) for more complex return types.

#### Streaming output

Cog models can stream output as the `predict()` method is running. For example, a language model can output tokens as they're being generated and an image generation model can output a images they are being generated.

To support streaming output in your Cog model, add `from typing import Iterator` to your predict.py file. The `typing` package is a part of Python's standard library so it doesn't need to be installed. Then add a return type annotation to the `predict()` method in the form `-> Iterator[<type>]` where `<type>` can be one of `str`, `int`, `float`, `bool`, `cog.File`, or `cog.Path`.

```py
from cog import BasePredictor, Path
from typing import Iterator

class Predictor(BasePredictor):
    def predict(self) -> Iterator[Path]:
        done = False
        while not done:
            output_path, done = do_stuff()
            yield Path(output_path)
```

If you're streaming text output, you can use `ConcatenateIterator` to hint that the output should be concatenated together into a single string. This is useful on Replicate to display the output as a string instead of a list of strings.

```py
from cog import BasePredictor, Path, ConcatenateIterator

class Predictor(BasePredictor):
    def predict(self) -> ConcatenateIterator[str]:
        tokens = ["The", "quick", "brown", "fox", "jumps", "over", "the", "lazy", "dog"]
        for token in tokens:
            yield token + " "
```

## `Input(**kwargs)`

Use cog's `Input()` function to define each of the parameters in your `predict()` method:

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

Each parameter of the `predict()` method must be annotated with a type like `str`, `int`, `float`, `bool`, etc. See [Input and output types](#input-and-output-types) for the full list of supported types.

Using the `Input` function provides better documentation and validation constraints to the users of your model, but it is not strictly required. You can also specify default values for your parameters using plain Python, or omit default assignment entirely:

```py
class Predictor(BasePredictor):
    def predict(self,
        prompt: str = "default prompt", # this is valid
        iterations: int                 # also valid
    ) -> str:
        # ...
```

## Output

Cog predictors can return a simple data type like a string, number, float, or boolean. Use Python's `-> <type>` syntax to annotate the return type.

Here's an example of a predictor that returns a string:

```py
from cog import BasePredictor

class Predictor(BasePredictor):
    def predict(self) -> str:
        return "hello"
```

### Returning an object

To return a complex object with multiple values, define an `Output` object with multiple fields to return from your `predict()` method:

```py
from cog import BasePredictor, BaseModel, File

class Output(BaseModel):
    file: File
    text: str

class Predictor(BasePredictor):
    def predict(self) -> Output:
        return Output(text="hello", file=io.StringIO("hello"))
```

Each of the output object's properties must be one of the supported output types. For the full list, see [Input and output types](#input-and-output-types). Also, make sure to name the output class as `Output` and nothing else.

### Returning a list

The `predict()` method can return a list of any of the supported output types. Here's an example that outputs multiple files:

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

Files are named in the format `output.<index>.<extension>`, e.g. `output.0.txt`, `output.1.txt`, and `output.2.txt` from the example above.

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

This example takes an input file, resizes it, and returns the resized image:

```python
import tempfile
from cog import BasePredictor, Input, Path

class Predictor(BasePredictor):
    def predict(self, image: Path = Input(description="Image to enlarge")) -> Path:
        upscaled_image = do_some_processing(image)

        # To output `cog.Path` objects the file needs to exist, so create a temporary file first.
        # This file will automatically be deleted by Cog after it has been returned.
        output_path = Path(tempfile.mkdtemp()) / "upscaled.png"
        upscaled_image.save(output_path)
        return Path(output_path)
```

## `Secret`

The `cog.Secret` type is used to signify that an input holds sensitive information,
like a password or API token.

`cog.Secret` is a subclass of Pydantic's [`SecretStr`](https://docs.pydantic.dev/latest/api/types/#pydantic.types.SecretStr).
Its default string representation redacts its contents to prevent accidental disclure.
You can access its contents with the `get_secret_value()` method.

```python
from cog import BasePredictor, Secret


class Predictor(BasePredictor):
    def predict(self, api_token: Secret) -> None:
        # Prints '**********'
        print(api_token)        

        # Use get_secret_value method to see the secret's content.
        print(api_token.get_secret_value())
```

A predictor's `Secret` inputs are represented in OpenAPI with the following schema:

```json
{
  "type": "string",
  "format": "password",
  "x-cog-secret": true,
}
```

Models uploaded to Replicate treat secret inputs differently throughout its system.
When you create a prediction on Replicate,
any value passed to a `Secret` input is redacted after being sent to the model.

> [!WARNING]  
> Passing secret values to untrusted models can result in 
> unintended disclosure, exfiltration, or misuse of sensitive data.

## `List`

The List type is also supported in inputs. It can hold any supported type.

Example for **List[Path]**:
```py
class Predictor(BasePredictor):
   def predict(self, paths: list[Path]) -> str:
       output_parts = []  # Use a list to collect file contents
       for path in paths:
           with open(path) as f:
             output_parts.append(f.read())
       return "".join(output_parts)
```
The corresponding cog command:
```bash
$ echo test1 > 1.txt
$ echo test2 > 2.txt
$ cog predict -i paths=@1.txt -i paths=@2.txt
Running prediction...
test1

test2
```
- Note the repeated inputs with the same name "paths" which constitute the list
