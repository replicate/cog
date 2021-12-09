# Prediction interface reference

You define how Cog runs predictions on your model by defining a class that inherits from `cog.Predictor`. It looks something like this:

```python
import cog
from pathlib import Path
import torch

class ImageScalingPredictor(cog.Predictor):
    def setup(self):
        self.model = torch.load("weights.pth")

    @cog.input("input", type=Path, help="Image to enlarge")
    @cog.input("scale", type=float, default=1.5, help="Factor to scale image by")
    def predict(self, input):
        # ... pre-processing ...
        output = self.model(input)
        # ... post-processing ...
        return output
```

Tip: Run [`cog init`](getting-started-own-model#initialization) to generate an annotated `predict.py` file that can be used as a starting point for setting up your model.

You need to override two functions: `setup()` and `predict()`.

### `Predictor.setup()`

Set up the model for prediction so multiple predictions run efficiently. Include any expensive one-off operations in here like loading trained models, instantiate data transformations, etc.

It's best not to download model weights or any other files in this function. You should bake these into the image when you build it. This means your model doesn't depend on any other system being available and accessible. It also means the Docker image ID becomes an immutable identifier for the precise model you're running, instead of the combination of the image ID and whatever files it might have downloaded.

### `Predictor.predict(**kwargs)`

Run a single prediction. This is where you call the model that was loaded during `setup()`, but you may also want to add pre- and post-processing code here.

The `predict()` function takes an arbitrary list of named arguments, where each argument name must correspond to a `@cog.input()` annotation.

`predict()` can output strings, numbers, `pathlib.Path` objects, or lists or dicts of those types. We are working on support for other types of output, but for now we recommend using base-64 encoded strings or `pathlib.Path`s for more complex outputs.

#### Returning `pathlib.Path` objects

If the output is a `pathlib.Path` object, that will be returned by the built-in HTTP server as a file download.

To output `pathlib.Path` objects the file needs to exist, which means that you probably need to create a temporary file first. This file will automatically be deleted by Cog after it has been returned. For example:

```python
def predict(self, input):
    output = do_some_processing(input)
    out_path = Path(tempfile.mkdtemp()) / "my-file.txt"
    out_path.write_text(output)
    return out_path
```

### `@cog.input(name, type, help, default=None, min=None, max=None, options=None)`

The `@cog.input()` annotation describes a single input to the `predict()` function. The `name` must correspond to an argument name in `predict()`.

It takes these arguments:

- `type`: Either `str`, `int`, `float`, `bool`, or `Path` (be sure to add the import, as in the example above). `Path` is used for files. For more complex inputs, save it to a file and use `Path`.
- `help`: A description of what to pass to this input for users of the model
- `default`: A default value to set the input to. If this argument is not passed, the input is required. If it is explicitly set to `None`, the input is optional.
- `min`: A minimum value for `int` or `float` types.
- `max`: A maximum value for `int` or `float` types.
- `options`: A list of values to limit the input to. It can be used with `str`, `int`, and `float` inputs.
