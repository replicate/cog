# Training API reference

> [!WARNING]
> The `cog train` command is deprecated and will be removed in the next version of Cog. The training API described below may still be used with the HTTP API's `/trainings` endpoint, but the CLI command is no longer recommended for new projects.

Cog's training API defines a fine-tuning interface for an existing Cog model. Users of the model can bring their own training data to create derivative fine-tuned models.

For a practical guide to fine-tuning, see [How to fine-tune with the training API](../how-to/training.md).

## `cog.yaml` configuration

The `train` key in `cog.yaml` points to the training entry point:

```yaml
build:
  python_version: "3.13"
train: "train.py:train"
```

The value follows the same `module:object` format as the [`predict`](yaml.md#predict) key. It can point to either a [function](#train-function) or a [class](#trainer-class).

## `train()` function

A standalone function that accepts typed inputs and returns a training output.

```python
from cog import File
import io

def train(param: str) -> File:
    return io.StringIO("hello " + param)
```

**Signature:** `def train(**kwargs) -> <output_type>`

Each parameter must be annotated with a type and may use [`Input()`](#inputkwargs) for validation. The return type must be annotated. See [Training output](#training-output) for supported return types.

## `Trainer` class

A class-based entry point that separates one-off setup from the training function. This is useful when running many training jobs that share expensive initialization.

```python
from cog import File

class Trainer:
    def setup(self) -> None:
        self.base_model = ...  # Load a big base model

    def train(self, param: str) -> File:
        return self.base_model.train(param)
```

**Methods:**

- `setup(self) -> None` -- Optional. Runs once when the container starts. Use for expensive one-off operations such as loading base models.
- `train(self, **kwargs) -> <output_type>` -- Required. Runs a single training job. Parameters and return type follow the same rules as the [standalone `train()` function](#train-function).

## `Input(**kwargs)`

Defines a parameter for the `train()` function. The interface is identical to the [prediction `Input()`](python.md#inputkwargs).

```py
from cog import Input, Path

def train(
    train_data: Path = Input(description="HTTPS URL of a file containing training data"),
    learning_rate: float = Input(description="learning rate", default=1e-4, ge=0),
    seed: int = Input(description="random seed to use for training", default=None)
) -> str:
    return "hello, weights"
```

**Keyword arguments:**

- `description`: A description of what to pass to this input.
- `default`: A default value. If not passed, the input is required. If explicitly `None`, the input is optional.
- `ge`: For `int` or `float` types, minimum value (inclusive).
- `le`: For `int` or `float` types, maximum value (inclusive).
- `min_length`: For `str` types, the minimum length.
- `max_length`: For `str` types, the maximum length.
- `regex`: For `str` types, a regular expression the value must match.
- `choices`: For `str` or `int` types, a list of allowed values.

Each parameter must be annotated with a type. See [Input and output types](python.md#input-and-output-types) for the full list of supported types.

Default values may also be specified using plain Python without `Input()`.

## Training output

The `train()` function's return type defines the training output.

**Simple return types:** The function may return any supported type directly (`str`, `int`, `float`, `bool`, `cog.Path`, `cog.File`).

**Structured output:** For complex outputs (e.g. weights plus metadata), define a `TrainingOutput` class:

```python
from cog import BaseModel, Input, Path

class TrainingOutput(BaseModel):
    weights: Path

def train(
    train_data: Path = Input(description="HTTPS URL of a file containing training data"),
    learning_rate: float = Input(description="learning rate", default=1e-4, ge=0),
    seed: int = Input(description="random seed to use for training", default=42)
) -> TrainingOutput:
    weights_file = generate_weights("...")
    return TrainingOutput(weights=Path(weights_file))
```

## HTTP endpoint

Training is exposed via the `/trainings` endpoint on the HTTP API. The request and response format mirrors the [prediction endpoints](http.md#post-predictions), with `input` containing the training parameters and `output` containing the training result.

## Testing

To test a fine-tuned code path during development, specify a `COG_WEIGHTS` environment variable when running `predict`:

```console
cog predict -e COG_WEIGHTS=https://replicate.delivery/pbxt/xyz/weights.tar -i prompt="a photo of TOK"
```
