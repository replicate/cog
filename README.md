# Cog: Containers for machine learning

Cog is an open-source tool that lets you package machine learning models in a standard, production-ready container.

You can deploy your packaged model to your own infrastructure, or to [Replicate](https://replicate.com/).

## Highlights

- 📦 **Docker containers without the pain.** Writing your own `Dockerfile` can be a bewildering process. With Cog, you define your environment with a [simple configuration file](#how-it-works) and it generates a Docker image with all the best practices: Nvidia base images, efficient caching of dependencies, installing specific Python versions, sensible environment variable defaults, and so on.

- 🤬️ **No more CUDA hell.** Cog knows which CUDA/cuDNN/PyTorch/Tensorflow/Python combos are compatible and will set it all up correctly for you.

- ✅ **Define the inputs and outputs for your model with standard Python.** Then, Cog generates an OpenAPI schema and validates the inputs and outputs with Pydantic.

- 🎁 **Automatic HTTP prediction server**: Your model's types are used to dynamically generate a RESTful HTTP API using [FastAPI](https://fastapi.tiangolo.com/).

- 🥞 **Automatic queue worker.** Long-running deep learning models or batch processing is best architected with a queue. Cog models do this out of the box. Redis is currently supported, with more in the pipeline.

- ☁️ **Cloud storage.** Files can be read and written directly to Amazon S3 and Google Cloud Storage. (Coming soon.)

- 🚀 **Ready for production.** Deploy your model anywhere that Docker images run. Your own infrastructure, or [Replicate](https://replicate.com).

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

Define how predictions are run on your model with `predict.py`:

```python
from cog import BasePredictor, Input, Path
import torch

class Predictor(BasePredictor):
    def setup(self):
        """Load the model into memory to make running multiple predictions efficient"""
        self.model = torch.load("./weights.pth")

    # The arguments and types the model takes as input
    def predict(self,
          image: Path = Input(title="Grayscale input image")
    ) -> Path:
        """Run a single prediction on the model"""
        processed_image = preprocess(image)
        output = self.model(processed_image)
        return postprocess(output)
```

Now, you can run predictions on this model:

```console
$ cog predict -i @input.jpg
--> Building Docker image...
--> Running Prediction...
--> Output written to output.jpg
```

Or, build a Docker image for deployment:

```console
$ cog build -t my-colorization-model
--> Building Docker image...
--> Built my-colorization-model:latest

$ docker run -d -p 5000:5000 --gpus all my-colorization-model

$ curl http://localhost:5000/predictions -X POST \
    -H 'Content-Type: application/json' \
    -d '{"input": {"image": "https://.../input.jpg"}}'
```

<!-- NOTE (bfirsh): Development environment instructions intentionally left out of readme for now, so as not to confuse the "ship a model to production" message.

In development, you can also run arbitrary commands inside the Docker environment:

```console
$ cog run python train.py
...
```

Or, [spin up a Jupyter notebook](docs/notebooks.md):

```console
$ cog run -p 8888 jupyter notebook --allow-root --ip=0.0.0.0
```
-->

## Why are we building this?

It's really hard for researchers to ship machine learning models to production.

Part of the solution is Docker, but it is so complex to get it to work: Dockerfiles, pre-/post-processing, Flask servers, CUDA versions. More often than not the researcher has to sit down with an engineer to get the damn thing deployed.

[Andreas](https://github.com/andreasjansson) and [Ben](https://github.com/bfirsh) created Cog. Andreas used to work at Spotify, where he built tools for building and deploying ML models with Docker. Ben worked at Docker, where he created [Docker Compose](https://github.com/docker/compose).

We realized that, in addition to Spotify, other companies were also using Docker to build and deploy machine learning models. [Uber](https://eng.uber.com/michelangelo-pyml/) and others have built similar systems. So, we're making an open source version so other people can do this too.

Hit us up if you're interested in using it or want to collaborate with us. [We're on Discord](https://discord.gg/replicate) or email us at [team@replicate.com](mailto:team@replicate.com).

## Prerequisites

- **macOS, Linux or Windows 11**. Cog works on macOS, Linux and Windows 11 with [WSL 2](docs/wsl2/wsl2.md)
- **Docker**. Cog uses Docker to create a container for your model. You'll need to [install Docker](https://docs.docker.com/get-docker/) before you can run Cog.

## Install

<a id="upgrade"></a>

You can download and install the latest release of Cog
by running the following commands in a terminal:

```console
sudo curl -o /usr/local/bin/cog -L "https://github.com/replicate/cog/releases/latest/download/cog_$(uname -s)_$(uname -m)"
sudo chmod +x /usr/local/bin/cog
```

Alternatively, you can build Cog from source and install it with these commands:

```console
make
sudo make install
```

## Next steps

- [Get started with an example model](docs/getting-started.md)
- [Get started with your own model](docs/getting-started-own-model.md)
- [Using Cog with notebooks](docs/notebooks.md)
- [Using Cog with Windows 11](docs/wsl2/wsl2.md)
- [Take a look at some examples of using Cog](https://github.com/replicate/cog-examples)
- [Deploy models with Cog](docs/deploy.md)
- [`cog.yaml` reference](docs/yaml.md) to learn how to define your model's environment
- [Prediction interface reference](docs/python.md) to learn how the `Predictor` interface works
- [HTTP API reference](docs/http.md) to learn how to use the HTTP API that models serve
- [Redis queue API reference](docs/redis.md) to learn how to run models via Redis

## Need help?

[Join us in #cog on Discord.](https://discord.gg/replicate)

## Contributors ✨

Thanks goes to these wonderful people ([emoji key](https://allcontributors.org/docs/en/emoji-key)):

<!-- ALL-CONTRIBUTORS-LIST:START - Do not remove or modify this section -->
<!-- prettier-ignore-start -->
<!-- markdownlint-disable -->
<table>
  <tbody>
    <tr>
      <td align="center"><a href="https://fir.sh/"><img src="https://avatars.githubusercontent.com/u/40906?v=4?s=100" width="100px;" alt="Ben Firshman"/><br /><sub><b>Ben Firshman</b></sub></a><br /><a href="https://github.com/replicate/cog/commits?author=bfirsh" title="Code">💻</a> <a href="https://github.com/replicate/cog/commits?author=bfirsh" title="Documentation">📖</a></td>
      <td align="center"><a href="https://replicate.ai/"><img src="https://avatars.githubusercontent.com/u/713993?v=4?s=100" width="100px;" alt="Andreas Jansson"/><br /><sub><b>Andreas Jansson</b></sub></a><br /><a href="https://github.com/replicate/cog/commits?author=andreasjansson" title="Code">💻</a> <a href="https://github.com/replicate/cog/commits?author=andreasjansson" title="Documentation">📖</a></td>
      <td align="center"><a href="http://zeke.sikelianos.com/"><img src="https://avatars.githubusercontent.com/u/2289?v=4?s=100" width="100px;" alt="Zeke Sikelianos"/><br /><sub><b>Zeke Sikelianos</b></sub></a><br /><a href="https://github.com/replicate/cog/commits?author=zeke" title="Code">💻</a> <a href="https://github.com/replicate/cog/commits?author=zeke" title="Documentation">📖</a> <a href="#tool-zeke" title="Tools">🔧</a></td>
      <td align="center"><a href="https://rory.bio/"><img src="https://avatars.githubusercontent.com/u/9436784?v=4?s=100" width="100px;" alt="Rory Byrne"/><br /><sub><b>Rory Byrne</b></sub></a><br /><a href="https://github.com/replicate/cog/commits?author=synek" title="Code">💻</a> <a href="https://github.com/replicate/cog/commits?author=synek" title="Documentation">📖</a> <a href="https://github.com/replicate/cog/commits?author=synek" title="Tests">⚠️</a></td>
      <td align="center"><a href="https://github.com/hangtwenty"><img src="https://avatars.githubusercontent.com/u/2420688?v=4?s=100" width="100px;" alt="Michael Floering"/><br /><sub><b>Michael Floering</b></sub></a><br /><a href="https://github.com/replicate/cog/commits?author=hangtwenty" title="Code">💻</a> <a href="https://github.com/replicate/cog/commits?author=hangtwenty" title="Documentation">📖</a> <a href="#ideas-hangtwenty" title="Ideas, Planning, & Feedback">🤔</a></td>
      <td align="center"><a href="https://bencevans.io/"><img src="https://avatars.githubusercontent.com/u/638535?v=4?s=100" width="100px;" alt="Ben Evans"/><br /><sub><b>Ben Evans</b></sub></a><br /><a href="https://github.com/replicate/cog/commits?author=bencevans" title="Documentation">📖</a></td>
      <td align="center"><a href="https://shashank.pw/"><img src="https://avatars.githubusercontent.com/u/778870?v=4?s=100" width="100px;" alt="shashank agarwal"/><br /><sub><b>shashank agarwal</b></sub></a><br /><a href="https://github.com/replicate/cog/commits?author=imshashank" title="Code">💻</a> <a href="https://github.com/replicate/cog/commits?author=imshashank" title="Documentation">📖</a></td>
    </tr>
    <tr>
      <td align="center"><a href="https://victorxlr.me/"><img src="https://avatars.githubusercontent.com/u/22397950?v=4?s=100" width="100px;" alt="VictorXLR"/><br /><sub><b>VictorXLR</b></sub></a><br /><a href="https://github.com/replicate/cog/commits?author=VictorXLR" title="Code">💻</a> <a href="https://github.com/replicate/cog/commits?author=VictorXLR" title="Documentation">📖</a> <a href="https://github.com/replicate/cog/commits?author=VictorXLR" title="Tests">⚠️</a></td>
      <td align="center"><a href="https://annahung31.github.io/"><img src="https://avatars.githubusercontent.com/u/39179888?v=4?s=100" width="100px;" alt="hung anna"/><br /><sub><b>hung anna</b></sub></a><br /><a href="https://github.com/replicate/cog/issues?q=author%3Aannahung31" title="Bug reports">🐛</a></td>
      <td align="center"><a href="http://notes.variogr.am/"><img src="https://avatars.githubusercontent.com/u/76612?v=4?s=100" width="100px;" alt="Brian Whitman"/><br /><sub><b>Brian Whitman</b></sub></a><br /><a href="https://github.com/replicate/cog/issues?q=author%3Abwhitman" title="Bug reports">🐛</a></td>
      <td align="center"><a href="https://github.com/JimothyJohn"><img src="https://avatars.githubusercontent.com/u/24216724?v=4?s=100" width="100px;" alt="JimothyJohn"/><br /><sub><b>JimothyJohn</b></sub></a><br /><a href="https://github.com/replicate/cog/issues?q=author%3AJimothyJohn" title="Bug reports">🐛</a></td>
      <td align="center"><a href="https://github.com/ericguizzo"><img src="https://avatars.githubusercontent.com/u/26746670?v=4?s=100" width="100px;" alt="ericguizzo"/><br /><sub><b>ericguizzo</b></sub></a><br /><a href="https://github.com/replicate/cog/issues?q=author%3Aericguizzo" title="Bug reports">🐛</a></td>
      <td align="center"><a href="http://www.dominicbaggott.com"><img src="https://avatars.githubusercontent.com/u/74812?v=4?s=100" width="100px;" alt="Dominic Baggott"/><br /><sub><b>Dominic Baggott</b></sub></a><br /><a href="https://github.com/replicate/cog/commits?author=evilstreak" title="Code">💻</a> <a href="https://github.com/replicate/cog/commits?author=evilstreak" title="Tests">⚠️</a></td>
      <td align="center"><a href="https://github.com/dashstander"><img src="https://avatars.githubusercontent.com/u/7449128?v=4?s=100" width="100px;" alt="Dashiell Stander"/><br /><sub><b>Dashiell Stander</b></sub></a><br /><a href="https://github.com/replicate/cog/issues?q=author%3Adashstander" title="Bug reports">🐛</a> <a href="https://github.com/replicate/cog/commits?author=dashstander" title="Code">💻</a> <a href="https://github.com/replicate/cog/commits?author=dashstander" title="Tests">⚠️</a></td>
    </tr>
    <tr>
      <td align="center"><a href="https://github.com/Hurricane-eye"><img src="https://avatars.githubusercontent.com/u/31437546?v=4?s=100" width="100px;" alt="Shuwei Liang"/><br /><sub><b>Shuwei Liang</b></sub></a><br /><a href="https://github.com/replicate/cog/issues?q=author%3AHurricane-eye" title="Bug reports">🐛</a> <a href="#question-Hurricane-eye" title="Answering Questions">💬</a></td>
      <td align="center"><a href="https://github.com/ericallam"><img src="https://avatars.githubusercontent.com/u/534?v=4?s=100" width="100px;" alt="Eric Allam"/><br /><sub><b>Eric Allam</b></sub></a><br /><a href="#ideas-ericallam" title="Ideas, Planning, & Feedback">🤔</a></td>
      <td align="center"><a href="https://perdomo.me"><img src="https://avatars.githubusercontent.com/u/178474?v=4?s=100" width="100px;" alt="Iván Perdomo"/><br /><sub><b>Iván Perdomo</b></sub></a><br /><a href="https://github.com/replicate/cog/issues?q=author%3Aiperdomo" title="Bug reports">🐛</a></td>
      <td align="center"><a href="http://charlesfrye.github.io"><img src="https://avatars.githubusercontent.com/u/10442975?v=4?s=100" width="100px;" alt="Charles Frye"/><br /><sub><b>Charles Frye</b></sub></a><br /><a href="https://github.com/replicate/cog/commits?author=charlesfrye" title="Documentation">📖</a></td>
      <td align="center"><a href="https://github.com/phamquiluan"><img src="https://avatars.githubusercontent.com/u/24642166?v=4?s=100" width="100px;" alt="Luan Pham"/><br /><sub><b>Luan Pham</b></sub></a><br /><a href="https://github.com/replicate/cog/issues?q=author%3Aphamquiluan" title="Bug reports">🐛</a> <a href="https://github.com/replicate/cog/commits?author=phamquiluan" title="Documentation">📖</a></td>
      <td align="center"><a href="https://github.com/TommyDew42"><img src="https://avatars.githubusercontent.com/u/46992350?v=4?s=100" width="100px;" alt="TommyDew"/><br /><sub><b>TommyDew</b></sub></a><br /><a href="https://github.com/replicate/cog/commits?author=TommyDew42" title="Code">💻</a></td>
      <td align="center"><a href="https://m4ke.org"><img src="https://avatars.githubusercontent.com/u/27?v=4?s=100" width="100px;" alt="Jesse Andrews"/><br /><sub><b>Jesse Andrews</b></sub></a><br /><a href="https://github.com/replicate/cog/commits?author=anotherjesse" title="Code">💻</a></td>
    </tr>
  </tbody>
</table>

<!-- markdownlint-restore -->
<!-- prettier-ignore-end -->

<!-- ALL-CONTRIBUTORS-LIST:END -->

This project follows the [all-contributors](https://github.com/all-contributors/all-contributors) specification. Contributions of any kind welcome!
