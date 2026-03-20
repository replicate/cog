# How to manage model weights

This guide shows you how to include model weights in your Cog image and choose between baking them into the image or downloading them at runtime.

## Decide where to store weights

There are two approaches, each with different tradeoffs:

| Approach | Image size | Build time | Setup time | Reproducibility |
|---|---|---|---|---|
| Weights in the image | Larger | Slower | Fast | Self-contained |
| Download in `setup()` | Smaller | Faster | Slower | Depends on external host |

If your weights are small (under a few GB) or you need fully reproducible, self-contained images, bake them into the image. If your weights are very large or change rarely while your code changes often, download them in `setup()`.

## Bake weights into the image

Place your weight files in the same directory as your `cog.yaml`. Cog copies the entire directory into the image during build.

```
my-model/
  cog.yaml
  predict.py
  weights/
    model.safetensors
```

Make sure your `.dockerignore` does not exclude the weights directory. If you have a `.dockerignore`, check that it does not contain a line like `weights/` or `*.safetensors`.

Load the weights from their local path in `setup()`:

```python
from cog import BasePredictor
import torch

class Predictor(BasePredictor):
    def setup(self):
        self.model = torch.load("weights/model.safetensors")
```

### Use `--separate-weights` for faster pushes

When weights are baked into the image, every code change rebuilds a layer that contains both code and weights. To avoid re-uploading gigabytes of weights on every push, use the `--separate-weights` flag:

```console
cog build --separate-weights -t my-model
```

This stores model weights in a separate Docker layer from your code. When you push, only the layers that changed are uploaded. If you only changed code, the weights layer is skipped entirely.

This flag works with both `cog build` and `cog push`:

```console
cog push r8.im/your-username/my-model --separate-weights
```

## Download weights in `setup()`

If you prefer smaller images, download weights at container start time. This is common for models hosted on Hugging Face, S3, or similar services.

### Using pget for fast parallel downloads

[pget](https://github.com/replicate/pget) is a fast file downloader that supports parallel chunk downloads. Install it via `build.run` in your `cog.yaml`:

```yaml
build:
  python_version: "3.12"
  gpu: true
  python_requirements: requirements.txt
  run:
    - curl -o /usr/local/bin/pget -L "https://github.com/replicate/pget/releases/latest/download/pget_$(uname -s)_$(uname -m)" && chmod +x /usr/local/bin/pget
predict: "predict.py:Predictor"
```

Then download in `setup()`:

```python
import subprocess
from cog import BasePredictor

class Predictor(BasePredictor):
    def setup(self):
        url = "https://weights.replicate.delivery/default/my-model/weights.tar"
        subprocess.check_call(["pget", "-x", url, "weights/"])
        self.model = load_model("weights/")
```

### Using urllib for simple downloads

For smaller files or when you want no extra dependencies:

```python
import urllib.request
from cog import BasePredictor

class Predictor(BasePredictor):
    def setup(self):
        url = "https://example.com/model-weights.bin"
        urllib.request.urlretrieve(url, "weights.bin")
        self.model = load_model("weights.bin")
```

### Using Hugging Face hub

If your model is hosted on Hugging Face, add `huggingface_hub` to your `requirements.txt` and download in `setup()`:

```python
from huggingface_hub import snapshot_download
from cog import BasePredictor

class Predictor(BasePredictor):
    def setup(self):
        self.model_path = snapshot_download(repo_id="your-org/your-model")
        self.model = load_model(self.model_path)
```

## Control setup timeout

If downloading weights takes a long time, you may need to increase the setup timeout. Set the `COG_SETUP_TIMEOUT` environment variable when running the container:

```console
COG_SETUP_TIMEOUT=600 docker run -p 5001:5000 my-model
```

By default there is no timeout. See the [environment variables reference](../environment.md) for details.

## Next steps

- See [How to optimise Docker image size](image-size.md) for more strategies to reduce image bloat.
- See the [`cog.yaml` reference](../yaml.md) for full build configuration options.
- See the [prediction interface reference](../python.md#predictorsetup) for `setup()` documentation.
