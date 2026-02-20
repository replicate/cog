# Getting started

This guide will walk you through what you can do with Cog by using an example model.

> [!TIP]
> Using a language model to help you write the code for your new Cog model?
>
> Feed it [https://cog.run/llms.txt](https://cog.run/llms.txt), which has all of Cog's documentation bundled into a single file. To learn more about this format, check out [llmstxt.org](https://llmstxt.org).

## Prerequisites

- **macOS or Linux**. Cog works on macOS and Linux, but does not currently support Windows.
- **Docker**. Cog uses Docker to create a container for your model. You'll need to [install Docker](https://docs.docker.com/get-docker/) before you can run Cog.

## Install Cog

**macOS (recommended):**

```bash
brew install replicate/tap/cog
```

**Linux or macOS (manual):**

```bash
sudo curl -o /usr/local/bin/cog -L https://github.com/replicate/cog/releases/latest/download/cog_`uname -s`_`uname -m`
sudo chmod +x /usr/local/bin/cog
sudo xattr -d com.apple.quarantine /usr/local/bin/cog 2>/dev/null || true

```

> [!NOTE]
> **macOS: "cannot be opened because the developer cannot be verified"**
>
> If you downloaded the binary manually (via `curl` or a browser) and see this Gatekeeper warning, run:
>
> ```bash
> sudo xattr -d com.apple.quarantine /usr/local/bin/cog
> ```
>
> Installing via `brew install replicate/tap/cog` handles this automatically.

## Create a project

Let's make a directory to work in:

```bash
mkdir cog-quickstart
cd cog-quickstart

```

## Run commands

The simplest thing you can do with Cog is run a command inside a Docker environment.

The first thing you need to do is create a file called `cog.yaml`:

```yaml
build:
  python_version: "3.12"
```

Then, you can run any command inside this environment. For example, enter

```bash
cog run python

```

and you'll get an interactive Python shell:

```none
✓ Building Docker image from cog.yaml... Successfully built 8f54020c8981
Running 'python' in Docker with the current directory mounted as a volume...
───────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────

Python 3.12.0 (main, Oct  2 2023, 15:45:55)
[GCC 12.2.0] on linux
Type "help", "copyright", "credits" or "license" for more information.
>>>
```

(Hit Ctrl-D to exit the Python shell.)

Inside this Docker environment you can do anything – run a Jupyter notebook, your training script, your evaluation script, and so on.

## Run predictions on a model

Let's pretend we've trained a model. With Cog, we can define how to run predictions on it in a standard way, so other people can easily run predictions on it without having to hunt around for a prediction script.

We need to write some code to describe how predictions are run on the model.

Save this to `predict.py`:

```python
import os
os.environ["TORCH_HOME"] = "."

import torch
from cog import BasePredictor, Input, Path
from PIL import Image
from torchvision import models

WEIGHTS = models.ResNet50_Weights.IMAGENET1K_V1


class Predictor(BasePredictor):
    def setup(self):
        """Load the model into memory to make running multiple predictions efficient"""
        self.device = torch.device("cuda" if torch.cuda.is_available() else "cpu")
        self.model = models.resnet50(weights=WEIGHTS).to(self.device)
        self.model.eval()

    def predict(self, image: Path = Input(description="Image to classify")) -> dict:
        """Run a single prediction on the model"""
        img = Image.open(image).convert("RGB")
        preds = self.model(WEIGHTS.transforms()(img).unsqueeze(0).to(self.device))
        top3 = preds[0].softmax(0).topk(3)
        categories = WEIGHTS.meta["categories"]
        return {categories[i]: p.detach().item() for p, i in zip(*top3)}
```

We also need to point Cog at this, and tell it what Python dependencies to install.

Save this to `requirements.txt`:

```
pillow==11.1.0
torch==2.6.0
torchvision==0.21.0
```

Then update `cog.yaml` to look like this:

```yaml
build:
  python_version: "3.12"
  python_requirements: requirements.txt
predict: "predict.py:Predictor"
```

> [!TIP]
> If you have a machine with an NVIDIA GPU attached, add `gpu: true` to the `build` section of your `cog.yaml` to enable GPU acceleration.

Let's grab an image to test the model with:

```bash
IMAGE_URL=https://gist.githubusercontent.com/bfirsh/3c2115692682ae260932a67d93fd94a8/raw/56b19f53f7643bb6c0b822c410c366c3a6244de2/mystery.jpg
curl $IMAGE_URL > input.jpg

```

Now, let's run the model using Cog:

```bash
cog predict -i image=@input.jpg

```

If you see the following output

```json
{
  "tiger_cat": 0.4874822497367859,
  "tabby": 0.23169134557247162,
  "Egyptian_cat": 0.09728282690048218
}
```

then it worked!

Note: The first time you run `cog predict`, the build process will be triggered to generate a Docker container that can run your model. The next time you run `cog predict` the pre-built container will be used.

## Build an image

We can bake your model's code, the trained weights, and the Docker environment into a Docker image. This image serves predictions with an HTTP server, and can be deployed to anywhere that Docker runs to serve real-time predictions.

```bash
cog build -t resnet
# Building Docker image...
# Built resnet:latest

```

You can run this image with `cog predict` by passing the filename as an argument:

```bash
cog predict resnet -i image=@input.jpg

```

Or, you can run it with Docker directly, and it'll serve an HTTP server:

```bash
docker run -d --rm -p 5000:5000 resnet

```

We can send inputs directly with `curl`:

```bash
curl http://localhost:5000/predictions -X POST \
    -H 'Content-Type: application/json' \
    -d '{"input": {"image": "https://gist.githubusercontent.com/bfirsh/3c2115692682ae260932a67d93fd94a8/raw/56b19f53f7643bb6c0b822c410c366c3a6244de2/mystery.jpg"}}'

```

As a shorthand, you can add the Docker image's name as an extra line in `cog.yaml`:

```yaml
image: "r8.im/replicate/resnet"
```

Once you've done this, you can use `cog push` to build and push the image to a Docker registry:

```bash
cog push
# Building r8.im/replicate/resnet...
# Pushing r8.im/replicate/resnet...
# Pushed!
```

The Docker image is now accessible to anyone or any system that has access to this Docker registry.

## Next steps

Those are the basics! Next, you might want to take a look at:

- [A guide to help you set up your own model on Cog.](getting-started-own-model.md)
- [A guide explaining how to deploy a model.](deploy.md)
- [Reference for `cog.yaml`](yaml.md)
- [Reference for the Python library](python.md)
