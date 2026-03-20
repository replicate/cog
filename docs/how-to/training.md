# How to fine-tune with the training API

This guide shows you how to define a training interface for your Cog model so users can bring their own data to create fine-tuned model weights.

> **Note**: The `cog train` CLI command is deprecated and will be removed in a future version. The training API itself is still fully supported and can be used through the HTTP API's `/trainings` endpoint.

## Define a train function

The simplest way to add training support is to define a standalone `train` function. Create a `train.py` file and point to it in your `cog.yaml`:

`cog.yaml`:

```yaml
build:
  gpu: true
  python_version: "3.12"
  python_requirements: requirements.txt
train: "train.py:train"
predict: "predict.py:Predictor"
```

`train.py`:

```python
from cog import Input, Path

def train(
    train_data: Path = Input(description="Training data file"),
    learning_rate: float = Input(description="Learning rate", default=1e-4, ge=0),
    epochs: int = Input(description="Number of training epochs", default=10, ge=1),
    seed: int = Input(description="Random seed", default=42),
) -> Path:
    model = load_base_model()
    model.train(train_data, lr=learning_rate, epochs=epochs, seed=seed)

    output_path = Path("/tmp/trained-weights.safetensors")
    model.save(output_path)
    return output_path
```

The `train` function works like `predict`: it takes typed inputs annotated with `Input()` and returns an output. The return value is typically a `Path` to the trained weights file.

## Use a Trainer class with setup()

If training involves loading a large base model, use a class with a `setup()` method to avoid reloading it on every training run:

`cog.yaml`:

```yaml
train: "train.py:Trainer"
```

`train.py`:

```python
from cog import Input, Path

class Trainer:
    def setup(self):
        self.base_model = load_base_model()

    def train(
        self,
        train_data: Path = Input(description="Training data file"),
        learning_rate: float = Input(description="Learning rate", default=1e-4, ge=0),
        epochs: int = Input(description="Number of training epochs", default=10, ge=1),
    ) -> Path:
        self.base_model.finetune(train_data, lr=learning_rate, epochs=epochs)

        output_path = Path("/tmp/trained-weights.safetensors")
        self.base_model.save(output_path)
        return output_path
```

The `setup()` method is called once when the container starts. Subsequent training requests reuse the loaded base model.

## Define training inputs

Use `Input()` to annotate each parameter of your `train()` function with descriptions, defaults, and validation constraints:

```python
from cog import Input, Path

def train(
    train_data: Path = Input(description="Archive of training images"),
    num_train_epochs: int = Input(description="Number of training epochs", default=100, ge=1, le=1000),
    train_batch_size: int = Input(description="Batch size", default=4, choices=[1, 2, 4, 8, 16]),
    learning_rate: float = Input(description="Learning rate", default=1e-4, ge=0),
    seed: int = Input(description="Random seed", default=None),
    resolution: int = Input(description="Image resolution for training", default=512, choices=[256, 512, 768, 1024]),
) -> Path:
    # ...
```

Supported types are the same as for `predict()`: `str`, `int`, `float`, `bool`, `cog.Path`, and `cog.Secret`. See the [prediction interface reference](../python.md#input-and-output-types) for the full list.

## Return structured training output

If you need to return multiple files or metadata alongside the weights, define a `TrainingOutput` object:

```python
from cog import BaseModel, Input, Path

class TrainingOutput(BaseModel):
    weights: Path
    training_log: Path

def train(
    train_data: Path = Input(description="Training data"),
    epochs: int = Input(description="Epochs", default=10),
) -> TrainingOutput:
    model = load_and_train(train_data, epochs=epochs)

    weights_path = Path("/tmp/weights.safetensors")
    log_path = Path("/tmp/training.log")
    model.save(weights_path)
    save_log(log_path)

    return TrainingOutput(weights=weights_path, training_log=log_path)
```

## Run training locally

Use the `cog train` CLI command to test training locally:

```console
cog train -i train_data=@my-training-data.zip -i learning_rate=0.001 -i epochs=5
```

File inputs are prefixed with `@`, just like `cog predict`.

## Test the trained weights with predict

To verify that your model's `predict()` function works with fine-tuned weights, pass the weights URL using the `COG_WEIGHTS` environment variable:

```console
cog predict -e COG_WEIGHTS=https://example.com/trained-weights.tar -i prompt="a photo of TOK"
```

Your `predict.py` should check for this environment variable in `setup()` and load the fine-tuned weights if present:

```python
import os
from cog import BasePredictor, Input, Path

class Predictor(BasePredictor):
    def setup(self):
        weights_url = os.environ.get("COG_WEIGHTS")
        if weights_url:
            self.model = load_model(download(weights_url))
        else:
            self.model = load_model("default-weights/")

    def predict(self, prompt: str = Input(description="Input prompt")) -> Path:
        return self.model.generate(prompt)
```

## Use the HTTP training endpoint

When a Cog image is built with a `train` configuration, the HTTP server exposes a `/trainings` endpoint that works like `/predictions`:

```console
curl http://localhost:5001/trainings -X POST \
    -H "Content-Type: application/json" \
    -d '{
        "input": {
            "train_data": "https://example.com/data.zip",
            "learning_rate": 0.001,
            "epochs": 10
        }
    }'
```

The response follows the same format as predictions, with `status`, `output`, and `metrics` fields.

For async training with webhooks:

```console
curl http://localhost:5001/trainings -X POST \
    -H "Content-Type: application/json" \
    -H "Prefer: respond-async" \
    -d '{
        "input": {
            "train_data": "https://example.com/data.zip",
            "epochs": 10
        },
        "webhook": "https://your-app.example.com/training-complete"
    }'
```

## Next steps

- See the [training interface reference](../training.md) for the full training API documentation.
- See the [prediction interface reference](../python.md#inputkwargs) for `Input()` parameter details.
- See the [HTTP API reference](../http.md) for endpoint details.
