# Getting started

This guide will walk you through what you can do with Cog by using an example model.

## Prerequisites

- **macOS or Linux**. Cog works on macOS and Linux, but does not currently support Windows.
- **Docker**. Cog uses Docker to create a container for your model. You'll need to [install Docker](https://docs.docker.com/get-docker/) before you can run Cog.

## Install Cog

First, install Cog:

```bash
sudo curl -o /usr/local/bin/cog -L https://github.com/replicate/cog/releases/latest/download/cog_`uname -s`_`uname -m`
sudo chmod +x /usr/local/bin/cog

```

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
  python_version: "3.11"
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

Python 3.11.1 (main, Jan 27 2023, 10:52:46)
[GCC 9.3.0] on linux
Type "help", "copyright", "credits" or "license" for more information.
>>>
```

(Hit Ctrl-D to exit the Python shell.)

Inside this Docker environment you can do anything – run a Jupyter notebook, your training script, your evaluation script, and so on.

## Run predictions on a model

Let's pretend we've trained a model. With Cog, we can define how to run predictions on it in a standard way, so other people can easily run predictions on it without having to hunt around for a prediction script.

First, run this to get some pre-trained model weights:

```bash
WEIGHTS_URL=https://storage.googleapis.com/tensorflow/keras-applications/resnet/resnet50_weights_tf_dim_ordering_tf_kernels.h5
curl -O $WEIGHTS_URL

```

Then, we need to write some code to describe how predictions are run on the model.

Save this to `predict.py`:

```python
from typing import Any
from cog import BasePredictor, Input, Path
from tensorflow.keras.applications.resnet50 import ResNet50
from tensorflow.keras.preprocessing import image as keras_image
from tensorflow.keras.applications.resnet50 import preprocess_input, decode_predictions
import numpy as np


class Predictor(BasePredictor):
    def setup(self):
        """Load the model into memory to make running multiple predictions efficient"""
        self.model = ResNet50(weights='resnet50_weights_tf_dim_ordering_tf_kernels.h5')

    # Define the arguments and types the model takes as input
    def predict(self, image: Path = Input(description="Image to classify")) -> Any:
        """Run a single prediction on the model"""
        # Preprocess the image
        img = keras_image.load_img(image, target_size=(224, 224))
        x = keras_image.img_to_array(img)
        x = np.expand_dims(x, axis=0)
        x = preprocess_input(x)
        # Run the prediction
        preds = self.model.predict(x)
        # Return the top 3 predictions
        return decode_predictions(preds, top=3)[0]
```

We also need to point Cog at this, and tell it what Python dependencies to install. Update `cog.yaml` to look like this:

```yaml
build:
  python_version: "3.11"
  python_packages:
    - pillow==9.5.0
    - tensorflow==2.12.0
predict: "predict.py:Predictor"
```

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

```
[
  [
    "n02123159",
    "tiger_cat",
    0.4874822497367859
  ],
  [
    "n02123045",
    "tabby",
    0.23169134557247162
  ],
  [
    "n02124075",
    "Egyptian_cat",
    0.09728282690048218
  ]
]
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

Once you've built the image, you can optionally view the generated dockerfile to get a sense of what Cog is doing under the hood:

```bash
cog debug
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

> **Note**
> Model repos often contain large data files, like weights and checkpoints. If you put these files in their own subdirectory and run `cog build` with the `--separate-weights` flag, Cog will copy these files into a separate Docker layer, which reduces the time needed to rebuild after making changes to code.
>
> ```shell
> # ✅ Yes
> .
> ├── checkpoints/
> │   └── weights.ckpt
> ├── predict.py
> └── cog.yaml
>
> # ❌ No
> .
> ├── weights.ckpt # <- Don't put weights in root directory
> ├── predict.py
> └── cog.yaml
>
> # ❌ No
> .
> ├── checkpoints/
> │   ├── weights.ckpt
> │   └── load_weights.py # <- Don't put code in weights directory
> ├── predict.py
> └── cog.yaml
> ```

## Next steps

Those are the basics! Next, you might want to take a look at:

- [A guide to help you set up your own model on Cog.](getting-started-own-model.md)
- [A guide explaining how to deploy a model.](deploy.md)
- [Reference for `cog.yaml`](yaml.md)
- [Reference for the Python library](python.md)
