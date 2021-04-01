# Cog: Machine learning model packaging

- **Store models in a central location.** Never again will you have to hunt for the right model file on S3 and figure out the right code to load it. Cog models are in one place with a content-addressable ID.
- **Package everything that a model needs to run.** Code, weights, pre/post-processing, data types, Python dependencies, system dependencies, CUDA version, etc etc. No more dependency hell.
- **Let anyone ship models to production.** Cog packages can be deployed to anywhere that runs Docker images, without having to understand Dockerfiles, CUDA, and all that horrible stuff.


## How does it work?

1. Define how inferences are run on your model:

```python
import cog

class JazzSoloComposerModel(cog.Model):
    """Generate melody and chords from partial lead sheets"""

    def setup(self):
        self.model = JazzSoloComposer.from_pretrained("./saved_model")

    @cog.input("notes", type=str, help="Partial notes")
    @cog.input("chords", type=str, help="Partial chords")
    @cog.input("strategy", type=str, help="Either 'sample' or 'sequential'")
    def run(self, notes, chords, strategy):
        return self.model(notes, chords, strategy)
```

2. Define the environment it runs in with `cog.yaml`:

```yaml
environment:
  python_version: "3.8"
  python_requirements: "requirements.txt"
  system_packages:
  - "ffmpeg"
  - "libavcodec-dev"
model: "model.py:JazzSoloComposerModel"
```

3. Build and push the model:

```
$ cog repo set http://10.1.2.3:8000/andreas/my-model
$ cog build
...
--> Built and pushed b6a2f8a2d2ff
```

This has:

- **Created a package**, a ZIP file containing your code + weights + environment definition, and assigned it a content-addressable SHA256 ID.
- **Pushed this package up to a central registry** so it never gets lost and can be run by anyone.
- **Built two Docker images** (one for CPU and one for GPU) that contains the package in a reproducible environment, with the correct versions of Python, your dependencies, CUDA, etc.

## Install

No binaries yet! You'll need Go 1.16, then run:

    make install

This installs the `cog` binary to `$GOPATH/bin/cog`.


## Getting started

First step is to start a server. You'll need to point it at a Docker registry to store the Docker images:

    cog server --port=8080 --docker-registry=us-central1-docker.pkg.dev/replicate/andreas-scratch

Then, hook up Cog to the server (replace "localhost" with your server's IP if it is remote):

    cog remote set http://localhost:8080

Next, let's build a package. We have [some models you can play around with](https://github.com/replicate/cog-examples). Clone that repository (you'll need git-lfs) and then build a package out of a model:

    cd example-models/inst-colorization/
    cog build

This will take a few minutes. In the meantime take a look at `cog.yaml` and `infer.py` to see how it works.

When that has finished, you can run inferences on the built model from any machine that is pointed at the server. Replace the ID with your package's ID, and the file with an image on your disk you want to colorize:

    cog infer b31f9f72d8f14f0eacc5452e85b05c957b9a8ed9 -i @hotdog.jpg

You can see more details about the package:

    cog show b31f9f72d8f14f0eacc5452e85b05c957b9a8ed9

You can also list the packages for this repo:

    cog list

In this output is the Docker image. You can run this anywhere a Docker image runs to deploy your model.


## Writing your own model

No docs yet -- sorry! It should be pretty self-explanatory from the examples.


## Next steps

- [Take a look at some examples of using Cog](https://github.com/replicate/cog-examples)
- [Python reference](docs/python.md) to learn how the `cog.Model` interface works
- [`cog.yaml` reference](docs/yaml.md) to learn how to define your model's envrionment
- [Server API documention](docs/server-api.md), if you want to integrate directly with the Cog server
