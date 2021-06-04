# Prediction interface reference

Subclasses of `cog.Model` define how Cog runs a prediction on your model. It looks something like this:

```python
import cog
from pathlib import Path
import torch

class ImageScalingModel(cog.Model):
    def setup(self):
        self.net = torch.load("weights.pth")

    @cog.input("input", type=Path, help="Image to enlarge")
    @cog.input("scale", type=float, default=1.5, help="Factor to scale image by")
    def predict(self, input):
        # ... pre-processing ...
        output = self.net(input)
        # ... post-processing ...
        return output
```

You need to override two functions: `setup()` and `predict()`.

### `Model.setup()`

Set up the model for prediction. This is where you load trained models, instantiate data transformations, etc., so multiple predictions can be run efficiently.

### `Model.predict(**kwargs)`

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
