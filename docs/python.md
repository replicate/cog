# Prediction interface reference

This document defines the API of the `cog` Python module, which is used to define the interface for running predictions on your model.

Tip: Run [`cog init`](getting-started-own-model.md#initialization) to generate an annotated `predict.py` file that can be used as a starting point for setting up your model.

## Contents

- [`BasePredictor`](#basepredictor)
  - [`Predictor.setup()`](#predictorsetup)
  - [`Predictor.predict(**kwargs)`](#predictorpredictkwargs)
    - [Progressive output](#progressive-output)
- [`Input(**kwargs)`](#inputkwargs)
- [Output](#output)
    - [Returning an object](#returning-an-object)
    - [Returning a list](#returning-a-list)
- [Input and output types](#input-and-output-types)
- [`File()`](#file)
- [`Path()`](#path)

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

It's best not to download model weights or any other files in this function. You should bake these into the image when you build it. This means your model doesn't depend on any other system being available and accessible. It also means the Docker image ID becomes an immutable identifier for the precise model you're running, instead of the combination of the image ID and whatever files it might have downloaded.

### `Predictor.predict(**kwargs)`

Run a single prediction.

This _required_ method is where you call the model that was loaded during `setup()`, but you may also want to add pre- and post-processing code here.

The `predict()` method takes an arbitrary list of named arguments, where each argument name must correspond to an [`Input()`](#inputkwargs) annotation.

`predict()` can return strings, numbers, [`cog.Path`](#path) objects representing files on disk, or lists or dicts of those types. You can also define a custom [`Output()`](#outputbasemodel) for more complex return types.

#### Progressive output

Cog models can yield output progressively as the `predict()` method is running. For example, an image generation model can yield a series of images as it is being generated.

To support progressive output in your Cog model, add `from typing import Iterator` to your predict.py file. The `typing` package is a part of Python's standard library so it doesn't need to be installed. Then add a return type annotation to the `predict()` method in the form `-> Iterator[<type>]` where `<type>` can be one of `str`, `int`, `float`, `bool`, `cog.File`, or `cog.Path`.

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

Each of the output object's properties must be one of the supported output types. For the full list, see [Input and output types](#input-and-output-types).

### Returning a list

The `predict()` method can return a list of any of the supported output types. Here's an example that outputs multiple files:

```py
from cog import BasePredictor, Path

class Predictor(BasePredictor):
    def predict(self) -> List[Path]:
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

## `File()`

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
        upscaled_image.save(output)
        return Path(output_path)
```
