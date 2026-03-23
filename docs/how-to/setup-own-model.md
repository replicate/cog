# How to set up your own model

This guide shows you how to package your own machine learning model with Cog. It assumes you already have Cog and Docker installed. If not, see the [installation instructions](https://github.com/replicate/cog).

## Initialize your project

Run `cog init` in your model's directory to generate the two required files:

```sh
cd path/to/your/model
cog init
```

This creates:

- [`cog.yaml`](../yaml.md) -- defines the Docker environment (Python version, dependencies, system packages)
- [`predict.py`](../python.md) -- defines the prediction interface for your model

## Configure cog.yaml

Edit `cog.yaml` to specify your Python version and dependencies:

```yaml
build:
  python_version: "3.13"
  python_requirements: requirements.txt
```

Add your model's dependencies to `requirements.txt`:

```
torch==2.6.0
```

Cog builds a Docker image with the correct versions of CUDA and cuDNN based on your dependencies. For all available configuration options, see the [`cog.yaml` reference](../yaml.md).

To verify the environment works, run a command inside it:

```sh
cog run python
```

## Write your predict.py

Edit `predict.py` to load your model and define its prediction interface:

```python
from cog import BasePredictor, Path, Input
import torch

class Predictor(BasePredictor):
    def setup(self):
        """Load the model into memory to make running multiple predictions efficient"""
        self.net = torch.load("weights.pth")

    def predict(self,
            image: Path = Input(description="Image to enlarge"),
            scale: float = Input(description="Factor to scale image by", default=1.5)
    ) -> Path:
        """Run a single prediction on the model"""
        # ... pre-processing ...
        output = self.net(input)
        # ... post-processing ...
        return output
```

### Define inputs

Each argument to `predict()` must have a type annotation. Supported types:

- `str`, `int`, `float`, `bool`
- `cog.Path` -- a path to a file on disk
- `cog.File` -- a file-like object (deprecated; use `cog.Path` instead)

Use `Input()` to add constraints and metadata:

- `description` -- describes the input for users
- `default` -- sets a default value. If omitted, the input is required. If set to `None`, the input is optional.
- `ge` / `le` -- minimum/maximum for `int` or `float`
- `min_length` / `max_length` -- length bounds for `str`
- `regex` -- pattern match for `str`
- `choices` -- allowed values for `str` or `int`

For the full list of options, see the [prediction interface reference](../python.md).

### Define outputs

The return type annotation on `predict()` determines the output type. Return `Path` for files, `str` for text, or other supported types.

### Connect predict.py to cog.yaml

Add the `predict` directive to `cog.yaml`:

```yaml
build:
  python_version: "3.13"
  python_requirements: requirements.txt
predict: "predict.py:Predictor"
```

If your `Predictor` class is in a different file or has a different name, adjust the path accordingly.

## Test predictions

Run a prediction to verify everything works:

```sh
cog predict -i image=@input.jpg
```

To pass multiple inputs:

```sh
cog predict -i image=@input.jpg -i scale=2.0
```

Use `@` before file paths. Scalar values (numbers, strings) don't need the prefix.

## Enable GPU support

If your model requires a GPU, add `gpu: true` to the `build` section:

```yaml
build:
  gpu: true
  python_version: "3.13"
  python_requirements: requirements.txt
predict: "predict.py:Predictor"
```

Cog automatically selects the correct CUDA and cuDNN versions based on your Python and framework versions. For details, see the [`gpu` section of the `cog.yaml` reference](../yaml.md#gpu).

## Next steps

- [Deploy your model](../deploy.md)
- [`cog.yaml` reference](../yaml.md)
- [Python SDK reference](../python.md)
