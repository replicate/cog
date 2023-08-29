# Training interface reference

> [!NOTE]  
> The training API is still experimental, and is subject to change.

Cog's training API allows you to define a fine-tuning interface for an existing Cog model, so users of the model can bring their own training data to create derivative fune-tuned models. Real-world examples of this API in use include [fine-tuning SDXL with images](https://replicate.com/blog/fine-tune-sdxl) or [fine-tuning Llama 2 with structured text](https://replicate.com/blog/fine-tune-llama-2).

## How it works

If you've used Cog before, you've probably seen the [Predictor](./python.md) class, which defines the interface for creating predictions against your model. Cog's training API works similarly: You define a Python function that describes the inputs and outputs of the training process. The inputs are things like training data, epochs, batch size, seed, etc. The output is typically a file with the fine-tuned weights.

`cog.yaml`:

```yaml
build:
  python_version: "3.10"
train: "train.py:train"
```

`train.py`:

```python
from cog import BasePredictor, File
import io

def train(param: str) -> File:
    return io.StringIO("hello " + param)
```

Then you can run it like this:

```
$ cog train -i param=train
...

$ cat weights
hello train
```

## `Input(**kwargs)`

Use Cog's `Input()` function to define each of the parameters in your `train()` function:

```py
from cog import Input, Path

def train(
    train_data: Path = Input(description="HTTPS URL of a file containg training data"),
    learning_rate: float = Input(description="learning rate, for learning!", default=1e-4, ge=0),
    seed: int = Input(description="random seed to use for training", default=None)
) -> str:
  return "hello, weights"
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

Each parameter of the `train()` function must be annotated with a type like `str`, `int`, `float`, `bool`, etc. See [Input and output types](./python.md#input-and-output-types) for the full list of supported types.

Using the `Input` function provides better documentation and validation constraints to the users of your model, but it is not strictly required. You can also specify default values for your parameters using plain Python, or omit default assignment entirely:

```py
def predict(self,
  training_data: str = "foo bar", # this is valid
  iterations: int                 # also valid
) -> str:
  # ...
```

## Training Output

Training output is typically a binary weights file. To return a custom output object or a complex object with multiple values, define a `TrainingOutput` object with multiple fields to return from your `train()` function, and specify it as the return type for the train function using Python's `->` return type annotation:

```python
from cog import BaseModel, Input, Path

class TrainingOutput(BaseModel):
    weights: Path

def train(
    train_data: Path = Input(description="HTTPS URL of a file containg training data"),
    learning_rate: float = Input(description="learning rate, for learning!", default=1e-4, ge=0),
    seed: int = Input(description="random seed to use for training", default=42)
) -> TrainingOutput:
  weights_file = generate_weights("...")
  return TrainingOutput(weights=Path(weights_file))
```

## Testing

If you are doing development of a Cog model like Llama or SDXL, you can test that the fine-tuned code path works before pushing by specifying a `COG_WEIGHTS` environment variable when running `predict`:

```console
cog predict -e COG_WEIGHTS=https://replicate.delivery/pbxt/xyz/weights.tar -i prompt="a photo of TOK"
```
