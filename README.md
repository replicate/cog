# Cog: Containers for machine learning

Use Docker for machine learning, without all the pain.

- [What is Cog?](#what-is-cog)
- [Develop and train in a consistent environment](#develop-and-train-in-a-consistent-environment)
- [Put a trained model in a Docker image](#put-a-trained-model-in-a-docker-image)
- [Why are we building this?](#why-are-we-building-this)
- [Prerequisites](#prerequisites)
- [Install](#install)
- [Upgrade](#upgrade)
- [Next steps](#next-steps)
- [Need help?](#need-help)
- [Contributors ✨](#contributors-%E2%9C%A8)

## What is Cog?

Cog is an open-source command-line tool that gives you a consistent environment to run your model in – for developing on your laptop, training on GPU machines, and for other people working on the model. Once you've trained your model and you want to share or deploy it, you can bake the model into a Docker image that serves a standard HTTP API and can be deployed anywhere.

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
    - "libgl1-mesa-glx"
    - "libglib2.0-0"
  python_version: "3.8"
  python_packages:
    - "torch==1.8.1"
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

<!-- TODO: this doesn't work yet (needs ports etc)
Or, spin up a Jupyter notebook:

```
$ cog run jupyter notebook
```
-->

## Put a trained model in a Docker image

First, you define how predictions are run on your model:

```python
import cog
import torch

class ColorizationPredictor(cog.Predictor):
    def setup(self):
        """Load the model into memory to make running multiple predictions efficient"""
        self.model = torch.load("./weights.pth")
    
    # The arguments and types the model takes as input
    @cog.input("input", type=cog.Path, help="Grayscale input image")
    def predict(self, input):
        """Run a single prediction on the model"""
        processed_input = preprocess(input)
        output = self.model(processed_input)
        return postprocess(output)
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

We are [Andreas](https://github.com/andreasjansson) and [Ben](https://github.com/bfirsh), and we're trying to fix this. Andreas used to work at Spotify, where he built tools for building and deploying ML models with Docker. Ben worked at Docker, where he created [Docker Compose](https://github.com/docker/compose).

We realized that, in addition to Spotify, other companies were also using Docker to build and deploy machine learning models. [Uber](https://eng.uber.com/michelangelo-pyml/), Coinbase, and others have built similar systems. So, we're making an open source version so other people can do this too.

Hit us up if you're interested in using it or want to collaborate with us. [We're on Discord](https://discord.gg/QmzJApGjyE) or email us at [team@replicate.com](mailto:team@replicate.com).

## Prerequisites

- **macOS or Linux**. Cog works on macOS and Linux, but does not currently support Windows.
- **Docker**. Cog uses Docker to create a container for your model. You'll need to [install Docker](https://docs.docker.com/get-docker/) before you can run Cog.

## Install

First, [install Docker if you haven't already](https://docs.docker.com/get-docker/). Then, run this in a terminal:

```
sudo curl -o /usr/local/bin/cog -L https://github.com/replicate/cog/releases/latest/download/cog_`uname -s`_`uname -m`
sudo chmod +x /usr/local/bin/cog
```

## Upgrade

If you're already got Cog installed and want to update to a newer version:

```
sudo rm $(which cog)
sudo curl -o /usr/local/bin/cog -L https://github.com/replicate/cog/releases/latest/download/cog_`uname -s`_`uname -m`
sudo chmod +x /usr/local/bin/cog
```

## Next steps

- [Get started with an example model](docs/getting-started.md)
- [Get started with your own model](docs/getting-started-own-model.md)
- [Take a look at some examples of using Cog](https://github.com/replicate/cog-examples)
- [`cog.yaml` reference](docs/yaml.md) to learn how to define your model's environment
- [Prediction interface reference](docs/python.md) to learn how the `cog.Predictor` interface works

## Need help?
 
[Join us in #cog on Discord.](https://discord.gg/QmzJApGjyE)

## Contributors ✨

Thanks goes to these wonderful people ([emoji key](https://allcontributors.org/docs/en/emoji-key)):

<!-- ALL-CONTRIBUTORS-LIST:START - Do not remove or modify this section -->
<!-- prettier-ignore-start -->
<!-- markdownlint-disable -->
<table>
  <tr>
    <td align="center"><a href="https://fir.sh/"><img src="https://avatars.githubusercontent.com/u/40906?v=4?s=100" width="100px;" alt=""/><br /><sub><b>Ben Firshman</b></sub></a><br /><a href="https://github.com/replicate/cog/commits?author=bfirsh" title="Code">💻</a> <a href="https://github.com/replicate/cog/commits?author=bfirsh" title="Documentation">📖</a></td>
    <td align="center"><a href="https://replicate.ai/"><img src="https://avatars.githubusercontent.com/u/713993?v=4?s=100" width="100px;" alt=""/><br /><sub><b>Andreas Jansson</b></sub></a><br /><a href="https://github.com/replicate/cog/commits?author=andreasjansson" title="Code">💻</a> <a href="https://github.com/replicate/cog/commits?author=andreasjansson" title="Documentation">📖</a></td>
    <td align="center"><a href="http://zeke.sikelianos.com/"><img src="https://avatars.githubusercontent.com/u/2289?v=4?s=100" width="100px;" alt=""/><br /><sub><b>Zeke Sikelianos</b></sub></a><br /><a href="https://github.com/replicate/cog/commits?author=zeke" title="Code">💻</a> <a href="https://github.com/replicate/cog/commits?author=zeke" title="Documentation">📖</a> <a href="#tool-zeke" title="Tools">🔧</a></td>
    <td align="center"><a href="https://rory.bio/"><img src="https://avatars.githubusercontent.com/u/9436784?v=4?s=100" width="100px;" alt=""/><br /><sub><b>Rory Byrne</b></sub></a><br /><a href="https://github.com/replicate/cog/commits?author=synek" title="Code">💻</a> <a href="https://github.com/replicate/cog/commits?author=synek" title="Documentation">📖</a></td>
    <td align="center"><a href="https://github.com/hangtwenty"><img src="https://avatars.githubusercontent.com/u/2420688?v=4?s=100" width="100px;" alt=""/><br /><sub><b>Michael Floering</b></sub></a><br /><a href="https://github.com/replicate/cog/commits?author=hangtwenty" title="Code">💻</a> <a href="https://github.com/replicate/cog/commits?author=hangtwenty" title="Documentation">📖</a> <a href="#ideas-hangtwenty" title="Ideas, Planning, & Feedback">🤔</a></td>
    <td align="center"><a href="https://bencevans.io/"><img src="https://avatars.githubusercontent.com/u/638535?v=4?s=100" width="100px;" alt=""/><br /><sub><b>Ben Evans</b></sub></a><br /><a href="https://github.com/replicate/cog/commits?author=bencevans" title="Documentation">📖</a></td>
    <td align="center"><a href="https://shashank.pw/"><img src="https://avatars.githubusercontent.com/u/778870?v=4?s=100" width="100px;" alt=""/><br /><sub><b>shashank agarwal</b></sub></a><br /><a href="https://github.com/replicate/cog/commits?author=imshashank" title="Code">💻</a> <a href="https://github.com/replicate/cog/commits?author=imshashank" title="Documentation">📖</a></td>
  </tr>
  <tr>
    <td align="center"><a href="https://victorxlr.me/"><img src="https://avatars.githubusercontent.com/u/22397950?v=4?s=100" width="100px;" alt=""/><br /><sub><b>VictorXLR</b></sub></a><br /><a href="https://github.com/replicate/cog/commits?author=VictorXLR" title="Code">💻</a> <a href="https://github.com/replicate/cog/commits?author=VictorXLR" title="Documentation">📖</a> <a href="https://github.com/replicate/cog/commits?author=VictorXLR" title="Tests">⚠️</a></td>
    <td align="center"><a href="https://annahung31.github.io/"><img src="https://avatars.githubusercontent.com/u/39179888?v=4?s=100" width="100px;" alt=""/><br /><sub><b>hung anna</b></sub></a><br /><a href="https://github.com/replicate/cog/issues?q=author%3Aannahung31" title="Bug reports">🐛</a></td>
    <td align="center"><a href="http://notes.variogr.am/"><img src="https://avatars.githubusercontent.com/u/76612?v=4?s=100" width="100px;" alt=""/><br /><sub><b>Brian Whitman</b></sub></a><br /><a href="https://github.com/replicate/cog/issues?q=author%3Abwhitman" title="Bug reports">🐛</a></td>
    <td align="center"><a href="https://github.com/JimothyJohn"><img src="https://avatars.githubusercontent.com/u/24216724?v=4?s=100" width="100px;" alt=""/><br /><sub><b>JimothyJohn</b></sub></a><br /><a href="https://github.com/replicate/cog/issues?q=author%3AJimothyJohn" title="Bug reports">🐛</a></td>
    <td align="center"><a href="https://github.com/ericguizzo"><img src="https://avatars.githubusercontent.com/u/26746670?v=4?s=100" width="100px;" alt=""/><br /><sub><b>ericguizzo</b></sub></a><br /><a href="https://github.com/replicate/cog/issues?q=author%3Aericguizzo" title="Bug reports">🐛</a></td>
  </tr>
</table>

<!-- markdownlint-restore -->
<!-- prettier-ignore-end -->

<!-- ALL-CONTRIBUTORS-LIST:END -->

This project follows the [all-contributors](https://github.com/all-contributors/all-contributors) specification. Contributions of any kind welcome!
