# Model Source

This document covers what a model author provides to Cog and the primitives they work with.

## What Users Write

A Cog model consists of:

```
my-model/
├── cog.yaml          # Environment configuration
├── predict.py        # Predictor class
└── weights/          # Model weights (optional, can be downloaded)
```

## cog.yaml

Declares the runtime environment:

```yaml
build:
  python_version: "3.11"
  gpu: true
  python_packages:
    - torch==2.1.0
    - transformers==4.35.0
  system_packages:
    - ffmpeg
  run:
    - curl -o /src/model.bin https://example.com/model.bin

predict: "predict.py:Predictor"

concurrency:
  max: 1
```

| Field | Purpose |
|-------|---------|
| `build.python_version` | Python interpreter version (3.8-3.12) |
| `build.gpu` | Enable CUDA support |
| `build.python_packages` | pip packages to install |
| `build.system_packages` | apt packages to install |
| `build.run` | Arbitrary shell commands during build |
| `predict` | Path to predictor class (`module:ClassName`) |
| `train` | Path to training class (optional) |
| `concurrency.max` | Max concurrent predictions (requires async) |

The [Build System](./05-build-system.md) uses this configuration to produce an image containing all necessary dependencies, libraries, and the correct Python/CUDA versions.

## The Predictor Class

A predictor is a Python class with two methods:

```python
from cog import BasePredictor, Input, Path

class Predictor(BasePredictor):
    def setup(self):
        """Load model into memory. Called once at container start."""
        self.model = load_model("./weights")
    
    def predict(self, prompt: str, steps: int = 50) -> Path:
        """Run inference. Called for each prediction request."""
        output = self.model.generate(prompt, steps=steps)
        output.save("/tmp/output.png")
        return Path("/tmp/output.png")
```

### setup()

- Called **once** when the container starts
- Used to load model weights, initialize GPU contexts, warm up caches
- Runs before the HTTP server accepts requests
- Optional: if omitted, Cog proceeds directly to serving

### predict()

- Called **for each prediction request**
- Signature defines the model's input schema (via type hints)
- Return type defines the output schema
- Can be sync (`def`) or async (`async def`)

### train() (optional)

- Same contract as `predict()` but for fine-tuning workflows
- Configured separately in `cog.yaml` with `train:` key

## Input Types

The types used in `predict()` parameters become the model's input schema.

### Basic Types

```python
def predict(
    self,
    text: str,              # String input
    count: int,             # Integer
    temperature: float,     # Float
    verbose: bool,          # Boolean
) -> str:
```

### File Inputs (cog.Path)

URLs are automatically downloaded to local files:

```python
from cog import Path

def predict(self, image: Path) -> Path:
    # Client sends: {"input": {"image": "https://example.com/photo.jpg"}}
    # Cog downloads the URL, `image` is a local path like /tmp/inputabc123.jpg
    img = PIL.Image.open(image)
    ...
```

`cog.Path` extends `pathlib.Path`. At runtime:
- HTTP/HTTPS URLs are downloaded to temp files
- Data URLs are decoded
- The predictor receives a local filesystem path

### Secrets (cog.Secret)

For sensitive values that shouldn't appear in logs:

```python
from cog import Secret

def predict(self, api_key: Secret) -> str:
    # Value is masked in logs and webhooks
    client = SomeAPI(api_key.get_secret_value())
    ...
```

### Input Constraints

Use `Input()` to add metadata and validation:

```python
from cog import Input

def predict(
    self,
    prompt: str = Input(description="The text prompt"),
    steps: int = Input(default=50, ge=1, le=100, description="Inference steps"),
    style: str = Input(choices=["photo", "art", "sketch"]),
) -> str:
```

| Parameter | Effect |
|-----------|--------|
| `description` | Shown in UI and schema |
| `default` | Default value if not provided |
| `ge`, `le` | Numeric bounds (greater/less than or equal) |
| `min_length`, `max_length` | String length bounds |
| `choices` | Enum values (deprecated: prefer `Literal`) |

### Enums with Literal

```python
from typing import Literal

def predict(
    self,
    size: Literal["small", "medium", "large"] = "medium",
) -> str:
```

### Lists

```python
from typing import List
from cog import Path

def predict(
    self,
    images: List[Path],      # Multiple file inputs
    tags: List[str],         # Multiple strings
) -> str:
```

### Optional Inputs

```python
from typing import Optional

def predict(
    self,
    seed: Optional[int] = None,  # Can be omitted or null
) -> str:
```

## Output Types

The return type annotation defines what the model produces.

### Basic Types

```python
def predict(self, prompt: str) -> str:
    return "Generated text..."
```

### File Outputs

Return `cog.Path` pointing to a generated file:

```python
from cog import Path

def predict(self, prompt: str) -> Path:
    # Generate file
    output_path = "/tmp/output.png"
    self.model.generate(prompt).save(output_path)
    return Path(output_path)
```

At runtime, Cog uploads the file and returns a URL to the client.

### Multiple Outputs

Return a list:

```python
from typing import List
from cog import Path

def predict(self, prompt: str) -> List[Path]:
    paths = []
    for i in range(4):
        path = f"/tmp/output_{i}.png"
        self.model.generate(prompt, seed=i).save(path)
        paths.append(Path(path))
    return paths
```

### Streaming with Iterator

Yield values progressively:

```python
from typing import Iterator

def predict(self, prompt: str) -> Iterator[str]:
    for token in self.model.generate_stream(prompt):
        yield token
```

The schema marks this as `x-cog-array-type: iterator`. Clients receive outputs as they're produced via webhooks or streaming responses.

### Streaming Text with ConcatenateIterator

For LLM-style token streaming where outputs should be concatenated:

```python
from cog import ConcatenateIterator

def predict(self, prompt: str) -> ConcatenateIterator[str]:
    for token in self.model.generate(prompt):
        yield token  # "Hello", " ", "world", "!"
    # Client sees progressive: "Hello" -> "Hello " -> "Hello world" -> "Hello world!"
```

The schema includes `x-cog-array-display: concatenate` to signal that outputs should be joined rather than displayed as a list.

## Weights

Model weights can be loaded in several ways:

### Bundled in the Image

Include weights in your source directory - they're copied into the image during build:

```
my-model/
├── cog.yaml
├── predict.py
└── weights/
    └── model.safetensors
```

```python
def setup(self):
    self.model = load("./weights/model.safetensors")
```

### Downloaded at Runtime

Weights can be fetched during `setup()` rather than bundled. Common approaches:

**Using the `weights` parameter** (Cog's built-in mechanism):

```python
class Predictor(BasePredictor):
    def setup(self, weights: Path):
        self.model = load(weights)
```

The `weights` value comes from `COG_WEIGHTS` env var or falls back to `./weights`:

```bash
COG_WEIGHTS=https://example.com/model.tar cog predict ...
```

**Using pget** (parallel download tool, included in Cog images):

```python
import subprocess

def setup(self):
    subprocess.run(["pget", "https://example.com/model.tar", "./weights"])
    self.model = load("./weights/model.safetensors")
```

**Direct download in setup**:

```python
def setup(self):
    # Using requests, huggingface_hub, or any other method
    snapshot_download(repo_id="meta-llama/Llama-2-7b", local_dir="./weights")
    self.model = load("./weights")
```

The choice depends on your deployment needs - bundled weights make images larger but start faster; downloaded weights keep images small but require network access at startup.

## Async Predictors

For concurrent predictions, use async:

```python
class Predictor(BasePredictor):
    async def setup(self):
        self.model = await load_model_async()
    
    async def predict(self, prompt: str) -> str:
        return await self.model.generate(prompt)
```

Requires:
- Python 3.11+
- `concurrency.max > 1` in cog.yaml

See [Container Runtime](./04-container-runtime.md) for concurrency details.

## Code References

| File | Purpose |
|------|---------|
| `python/cog/__init__.py` | Public API exports |
| `python/cog/base_predictor.py` | BasePredictor class |
| `python/cog/types.py` | Input, Path, Secret, ConcatenateIterator |
| `python/cog/predictor.py` | Type introspection, weights handling |
| `pkg/config/config.go` | cog.yaml parsing |
