# Cog: Containers for machine learning

Use Docker for machine learning, without all the pain.

Cog gives you a consistent environment to run your model in – for developing on your laptop, training on GPU machines, and for other people working on the model. Then, when the model is trained and you want to share or deploy it, you can bake the model into a Docker image that serves a standard HTTP API.

Cog does a few handy things beyond normal Docker:

- **Automatic Docker image.** Define your environment with a simple configuration file, then Cog will generate Dockerfiles with best practices and do all the GPU configuration for you.
- **Automatic HTTP service.** Cog will generate an HTTP service from the definition of your model, so you don't need to write a Flask server in the right way.
- **No more CUDA hell.** Cog knows which CUDA/cuDNN/PyTorch/Tensorflow/Python combos are compatible and will pick the right versions for you.

## Develop and train in a consistent environment

Define the Docker environment your model runs in with `cog.yaml`:

```yaml
build:
  gpu: true
  system_packages:
    - libgl1-mesa-glx
    - libglib2.0-0
  python_version: "3.8"
  python_requirements: "requirements.txt"
```

Now, you can run commands inside this environment:

```
$ cog run python train.py
...
```

This will:

- Generate a `Dockerfile` with best practices
- Pick the right CUDA version
- Build an image
- Run `python train.py` in the image with the current directory mounted as a volume and GPUs hooked up correctly

Or, spin up a Jupyter notebook:

```
$ cog run jupyter notebook
```

## Put a trained model in a Docker image

First, you define how predictions are run on your model:

```python
import cog
import torch

class ColorizationPredictor(cog.Predictor):
    def setup(self):
        self.model = torch.load("./weights.pth")

    @cog.input("input", type=cog.Path, help="Grayscale input image")
    def predict(self, input):
        # ... pre-processing ...
        output = self.model(processed_input)
        # ... post-processing ...
        return processed_output
```

Now, you can run predictions on this model:

```
$ cog predict -i @input.jpg
--> Building Docker image...
--> Running Prediction...
--> Output written to output.jpg
```

Or, build a Docker image for deployment:

```
$ cog build -t my-colorization-model
--> Building Docker image...
--> Built my-colorization-model:latest

$ docker run -d -p 5000:5000 --gpus all my-colorization-model

$ curl http://localhost:5000/predict -X POST -F input=@image.png
```

That's it! Your model will now run forever in this reproducible Docker environment.

## Why are we building this?

It's really hard for researchers to ship machine learning models to production.

Part of the solution is Docker, but it is so complex to get it to work: Dockerfiles, pre-/post-processing, Flask servers, CUDA versions. More often than not the researcher has to sit down with an engineer to get the damn thing deployed.

We are [Andreas](https://github.com/andreasjansson) and [Ben](https://github.com/bfirsh), and we're trying to fix this. Andreas used to work at Spotify, where he built tools for building and deploying ML models with Docker. Ben worked at Docker, where he created Docker Compose.

We realized that, in addition to Spotify, other companies were also using Docker to build and deploy machine learning models. [Uber](https://eng.uber.com/michelangelo-pyml/), Coinbase, and others have built similar systems. So, we're making an open source version so other people can do this too.

Hit us up if you're interested in using it or want to collaborate with us. [We're on Discord](https://discord.gg/QmzJApGjyE) or email us at [team@replicate.ai](mailto:team@replicate.ai).

## Install

Run this in a terminal:

    curl -L https://github.com/replicate/cog/releases/latest/download/cog_`uname -s`_`uname -m` > /usr/local/bin/cog
    chmod +x /usr/local/bin/cog

## Next steps

- [Get started with an example model](docs/getting-started.md)
- [Get started with your own model](docs/getting-started-own-model.md)
- [Take a look at some examples of using Cog](https://github.com/replicate/cog-examples)
- [`cog.yaml` reference](docs/yaml.md) to learn how to define your model's envrionment
- [Prediction interface reference](docs/python.md) to learn how the `cog.Predictor` interface works
