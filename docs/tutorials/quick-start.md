# Quick start: classify an image with ResNet

In this tutorial, we will package a ResNet image classifier using Cog, run a prediction on it, and build a Docker image. By the end, you will have a working model that classifies images and a Docker image ready to deploy.

## What we will build

We will take a pre-trained ResNet50 model (an image classifier trained on ImageNet) and package it with Cog. We will give it a photo and it will tell us what is in it.

## Prerequisites

Before starting, make sure you have:

- **macOS or Linux** (Cog does not support Windows)
- **Docker** installed and running ([install Docker](https://docs.docker.com/get-docker/))
- **Cog** installed ([install instructions](https://github.com/replicate/cog#install))

Verify both are working:

```bash
docker version
cog --version
```

You should see version numbers for both. If either command fails, revisit the installation links above before continuing.

## Create a project directory

First, create a new directory and move into it:

```bash
mkdir cog-quickstart
cd cog-quickstart
```

Every Cog project needs two things: a `cog.yaml` file that defines the environment, and a `predict.py` file that defines how predictions work. We will create both.

## Define the environment

Create a file called `cog.yaml` with the following content:

```yaml
build:
  python_version: "3.13"
  python_requirements: requirements.txt
predict: "predict.py:Predictor"
```

This tells Cog three things: use Python 3.13, install Python packages from `requirements.txt`, and find the prediction code in `predict.py` in a class called `Predictor`.

Now create the `requirements.txt` file:

```
pillow==11.1.0
torch==2.6.0
torchvision==0.21.0
```

## Write the prediction code

Create a file called `predict.py`:

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

Notice the two methods in the `Predictor` class:

- `setup()` loads the model once when the container starts. This means predictions run fast because the model is already in memory.
- `predict()` takes an image path as input and returns a dictionary of the top 3 classifications with their confidence scores.

Your project directory now contains three files:

```
cog-quickstart/
  cog.yaml
  predict.py
  requirements.txt
```

## Run a prediction

We need a test image. Download one:

```bash
curl -o input.jpg https://gist.githubusercontent.com/bfirsh/3c2115692682ae260932a67d93fd94a8/raw/56b19f53f7643bb6c0b822c410c366c3a6244de2/mystery.jpg
```

Now run a prediction:

```bash
cog predict -i image=@input.jpg
```

The first time you run this, Cog builds a Docker image with all the dependencies. This takes a few minutes. You will see build progress in the terminal.

Once the build completes and the model runs, you will see output like this:

```json
{
  "tiger_cat": 0.4874822497367859,
  "tabby": 0.23169134557247162,
  "Egyptian_cat": 0.09728282690048218
}
```

The model classified the image as a cat. Notice the `@` prefix on `image=@input.jpg` -- this tells Cog the value is a file path, not a string.

Run the same prediction again:

```bash
cog predict -i image=@input.jpg
```

Notice it runs much faster this time. Cog reuses the Docker image it already built.

## Build a Docker image

Now we will build a standalone Docker image that you can run anywhere Docker runs:

```bash
cog build -t resnet
```

You will see build output ending with:

```
Built resnet:latest
```

Verify the image exists:

```bash
docker images resnet
```

You will see something like:

```
REPOSITORY   TAG       IMAGE ID       CREATED          SIZE
resnet       latest    abc123def456   10 seconds ago   5.2GB
```

You now have a self-contained Docker image with the model, its weights, all dependencies, and an HTTP prediction server.

## Test the Docker image

Run a prediction using the built image:

```bash
cog predict resnet -i image=@input.jpg
```

You will see the same classification output as before. The difference is this time it ran from the standalone Docker image, not from your source files.

## What you have built

You have:

1. Defined a model environment in `cog.yaml`
2. Written a prediction interface in `predict.py`
3. Run a prediction with `cog predict`
4. Built a production Docker image with `cog build`

The Docker image `resnet:latest` includes an HTTP server for serving predictions. In the [deploy locally](deploy-local.md) tutorial, you will learn how to run this server and make requests to it with curl.

## Next steps

- [Package your first model](first-model.md) -- learn the Cog patterns by building a model from scratch
- [Build and deploy locally](deploy-local.md) -- run your model as an HTTP server and make requests with curl
- [cog.yaml reference](../yaml.md) -- full reference for the configuration file
- [Python SDK reference](../python.md) -- full reference for the prediction interface
