# Package your first model

In this tutorial, we will build a Cog model from scratch. Instead of using a pre-trained neural network, we will create a simple text processing model that takes text and transforms it. This keeps the focus on learning Cog's patterns -- `setup()`, `predict()`, typed inputs, and file outputs -- without waiting for large downloads.

By the end, you will have a working Cog model that accepts text and configuration options, processes them, and returns results as files.

## Prerequisites

Before starting, you should have:

- Completed the [quick start tutorial](quick-start.md)
- **Cog** and **Docker** installed and working

## Create the project

Create a new directory for this tutorial:

```bash
mkdir cog-first-model
cd cog-first-model
```

## Start with the simplest possible model

We will build up the model step by step, running predictions at each stage so you can see the results.

Create `cog.yaml`:

```yaml
build:
  python_version: "3.13"
predict: "predict.py:Predictor"
```

Notice there is no `python_requirements` line. This model has no external dependencies -- it uses only Python's standard library.

Create `predict.py`:

```python
from cog import BasePredictor


class Predictor(BasePredictor):
    def predict(self, text: str) -> str:
        return text.upper()
```

This is the simplest possible Cog model. It takes a string and returns it in uppercase. Run it:

```bash
cog predict -i text="hello world"
```

After the build completes, you will see:

```
HELLO WORLD
```

You have a working model. Now we will add structure to it, one piece at a time.

## Add the setup method

The `setup()` method runs once when the model loads. It is the right place for expensive one-time work like loading weights, initialising data structures, or reading configuration files.

Update `predict.py`:

```python
from cog import BasePredictor


class Predictor(BasePredictor):
    def setup(self):
        """Load resources needed for predictions"""
        self.substitutions = {
            "hello": "greetings",
            "world": "planet",
            "goodbye": "farewell",
            "friend": "companion",
        }

    def predict(self, text: str) -> str:
        words = text.lower().split()
        result = [self.substitutions.get(word, word) for word in words]
        return " ".join(result)
```

The `setup()` method builds a dictionary of word substitutions. The `predict()` method uses that dictionary to transform input text.

Run it:

```bash
cog predict -i text="hello world"
```

You will see:

```
greetings planet
```

Try another input:

```bash
cog predict -i text="goodbye friend"
```

You will see:

```
farewell companion
```

Notice the pattern: `setup()` prepares resources, `predict()` uses them. In a real model, `setup()` would load model weights from disk and `predict()` would run inference. The structure is the same regardless of what the model does.

## Add typed inputs with Input()

So far, `predict()` takes a bare `text: str` argument. Cog's `Input()` function lets you add descriptions, defaults, and validation to each parameter. This makes your model self-documenting and generates better API schemas.

Update `predict.py`:

```python
from cog import BasePredictor, Input


class Predictor(BasePredictor):
    def setup(self):
        """Load resources needed for predictions"""
        self.substitutions = {
            "hello": "greetings",
            "world": "planet",
            "goodbye": "farewell",
            "friend": "companion",
        }

    def predict(
        self,
        text: str = Input(description="Text to transform"),
        repeat: int = Input(description="Number of times to repeat the output", default=1, ge=1, le=5),
        uppercase: bool = Input(description="Convert output to uppercase", default=False),
    ) -> str:
        words = text.lower().split()
        result = " ".join(self.substitutions.get(word, word) for word in words)

        if uppercase:
            result = result.upper()

        lines = [result] * repeat
        return "\n".join(lines)
```

We added two new inputs:

- `repeat` is an integer with a default of `1`, constrained between 1 and 5 using `ge` (greater than or equal) and `le` (less than or equal).
- `uppercase` is a boolean that defaults to `False`.

Run a prediction with the defaults:

```bash
cog predict -i text="hello world"
```

You will see:

```
greetings planet
```

Now use the new inputs:

```bash
cog predict -i text="hello world" -i repeat=3 -i uppercase=true
```

You will see:

```
GREETINGS PLANET
GREETINGS PLANET
GREETINGS PLANET
```

Notice that `text` has no default, so it is required. The `repeat` and `uppercase` inputs have defaults, so they are optional.

## Return a file with cog.Path

Models often produce files as output -- images, audio, documents. Cog handles this with `cog.Path`. When your `predict()` method returns a `cog.Path`, Cog knows it is a file and handles it appropriately.

Update `predict.py`:

```python
import tempfile

from cog import BasePredictor, Input, Path


class Predictor(BasePredictor):
    def setup(self):
        """Load resources needed for predictions"""
        self.substitutions = {
            "hello": "greetings",
            "world": "planet",
            "goodbye": "farewell",
            "friend": "companion",
        }

    def predict(
        self,
        text: str = Input(description="Text to transform"),
        repeat: int = Input(description="Number of times to repeat the output", default=1, ge=1, le=5),
        uppercase: bool = Input(description="Convert output to uppercase", default=False),
    ) -> Path:
        words = text.lower().split()
        result = " ".join(self.substitutions.get(word, word) for word in words)

        if uppercase:
            result = result.upper()

        lines = [result] * repeat
        output_text = "\n".join(lines)

        output_path = Path(tempfile.mkdtemp()) / "output.txt"
        with open(output_path, "w") as f:
            f.write(output_text)

        return output_path
```

Notice three changes:

1. We import `Path` from `cog` (not `pathlib`).
2. The return type changed from `-> str` to `-> Path`.
3. We write the result to a temporary file and return the path.

Run it:

```bash
cog predict -i text="hello world" -i repeat=2
```

You will see:

```
Written output to output.txt
```

Cog created the file `output.txt` in your current directory. Check its contents:

```bash
cat output.txt
```

You will see:

```
greetings planet
greetings planet
```

When a model returns a `cog.Path`, Cog copies the file out of the container and saves it locally. When running as an HTTP server, the file is returned as a URL instead.

## Accept a file as input

Models that process files -- images, audio, documents -- accept `cog.Path` as an input type. Let us add a mode where the model reads text from a file instead of a string argument.

Update `predict.py`:

```python
import tempfile
from typing import Optional

from cog import BasePredictor, Input, Path


class Predictor(BasePredictor):
    def setup(self):
        """Load resources needed for predictions"""
        self.substitutions = {
            "hello": "greetings",
            "world": "planet",
            "goodbye": "farewell",
            "friend": "companion",
        }

    def predict(
        self,
        text: str = Input(description="Text to transform", default=""),
        text_file: Optional[Path] = Input(description="Text file to transform (used instead of text)", default=None),
        repeat: int = Input(description="Number of times to repeat the output", default=1, ge=1, le=5),
        uppercase: bool = Input(description="Convert output to uppercase", default=False),
    ) -> Path:
        if text_file is not None:
            with open(text_file, "r") as f:
                source_text = f.read().strip()
        else:
            source_text = text

        words = source_text.lower().split()
        result = " ".join(self.substitutions.get(word, word) for word in words)

        if uppercase:
            result = result.upper()

        lines = [result] * repeat
        output_text = "\n".join(lines)

        output_path = Path(tempfile.mkdtemp()) / "output.txt"
        with open(output_path, "w") as f:
            f.write(output_text)

        return output_path
```

Create a test input file:

```bash
echo "hello world goodbye friend" > input.txt
```

Run the model with the file:

```bash
cog predict -i text_file=@input.txt -i uppercase=true
```

You will see:

```
Written output to output.txt
```

Check the result:

```bash
cat output.txt
```

You will see:

```
GREETINGS PLANET FAREWELL COMPANION
```

Notice the `@` prefix on `text_file=@input.txt`. This tells Cog to pass the file contents, not the literal string "input.txt". You saw this same pattern in the quick start tutorial when passing an image file.

## Review the final project

Your project now contains:

```
cog-first-model/
  cog.yaml
  predict.py
  input.txt
```

The `cog.yaml` is minimal:

```yaml
build:
  python_version: "3.13"
predict: "predict.py:Predictor"
```

The `predict.py` demonstrates all the core Cog patterns:

- **`setup()`** for one-time initialisation
- **`predict()`** for per-request processing
- **`Input()`** for typed, documented, validated inputs
- **`cog.Path`** for file inputs and outputs
- **Type annotations** (`str`, `int`, `bool`, `Optional[Path]`) for the prediction interface

## What you have built

You have created a Cog model from scratch that:

1. Loads resources in `setup()`
2. Accepts multiple typed inputs with defaults and validation
3. Processes both string and file inputs
4. Returns file output using `cog.Path`

These are the same patterns used by real machine learning models -- the only difference is what happens inside `setup()` and `predict()`.

## Next steps

- [Build and deploy locally](deploy-local.md) -- build a Docker image and serve predictions over HTTP
- [cog.yaml reference](../yaml.md) -- full reference for environment configuration
- [Python SDK reference](../python.md) -- all input types, output types, and advanced features
