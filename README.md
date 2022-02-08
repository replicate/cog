# Cog: Containers for machine learning

Put your machine learning model in a standard, production-ready Docker container without having to know how Docker works.

Cog does a few things for you:

- **Automatic Docker image.** Define your environment with a simple configuration file, and Cog generates a `Dockerfile` with all the best practices.
- **Standard, production-ready HTTP and AMQP interface.** Automatically generate APIs for integrating with production systems, battle hardened on Replicate.
- **No more CUDA hell.** Cog knows which CUDA/cuDNN/PyTorch/Tensorflow/Python combos are compatible and will set it all up correctly for you.

## How it works

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
predict: "predict.py:Predictor"
```

And define how predictions are run on your model with `predict.py`:

```python
from cog import BasePredictor, Input, Path
import torch

class Predictor(BasePredictor):
    def setup(self):
        """Load the model into memory to make running multiple predictions efficient"""
        self.model = torch.load("./weights.pth")

    # The arguments and types the model takes as input
    def predict(self,
          input: Path = Input(title="Grayscale input image")
    ) -> Path:
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

$ curl http://localhost:5000/predictions -X POST \
   --data '{"input": "https://.../input.jpg"}'
```

<!-- In development, you can also run arbitrary commands inside the Docker environment:

```
$ cog run python train.py
...
``` -->

<!-- TODO: this doesn't work yet (needs ports etc)
Or, spin up a Jupyter notebook:

```
$ cog run jupyter notebook
```
-->

## Deploying models to production

Cog does a number of things out of the box to help you deploy models to production:

- **Standard interface.** Put models inside Cog containers, and they'll run anywhere that runs Docker containers.
- **HTTP prediction server, based on FastAPI.**
- **Type checking, based on Pydantic.** Cog models define their input and output with JSON Schema, and the HTTP server is defined with OpenAPI.
- **AMQP RPC interface.** Long-running deep learning models or batch processing is best architected with a queue. Cog models can do this out of the box.
- **Read/write files from cloud storage.** Files can be read and written directly on Amazon S3 and Google Cloud Storage for efficiency.

## Why are we building this?

It's really hard for researchers to ship machine learning models to production.

Part of the solution is Docker, but it is so complex to get it to work: Dockerfiles, pre-/post-processing, Flask servers, CUDA versions. More often than not the researcher has to sit down with an engineer to get the damn thing deployed.

We are [Andreas](https://github.com/andreasjansson) and [Ben](https://github.com/bfirsh), and we're trying to fix this. Andreas used to work at Spotify, where he built tools for building and deploying ML models with Docker. Ben worked at Docker, where he created [Docker Compose](https://github.com/docker/compose).

We realized that, in addition to Spotify, other companies were also using Docker to build and deploy machine learning models. [Uber](https://eng.uber.com/michelangelo-pyml/), Coinbase, and others have built similar systems. So, we're making an open source version so other people can do this too.

Hit us up if you're interested in using it or want to collaborate with us. [We're on Discord](https://discord.gg/QmzJApGjyE) or email us at [team@replicate.com](mailto:team@replicate.com).

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
- [Prediction interface reference](docs/python.md) to learn how the `Predictor` interface works

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
