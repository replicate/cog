# Quickstart

> **Note:** This guide is a work in progress! Please excuse the brevity. Hopefully it'll give you an idea of how it works, though.

This guide will help you understand how Cog works. We're going to take a model that somebody else has built and trained and run it with Cog.

You'll need git-lfs installed to get the trained model weights. On Mac, run `brew install git-lfs`.

First, clone our examples repository and cat a look at the `inst-colorization` model:

    git clone https://github.com/andreasjansson/InstColorization.git
    cd cog-examples/inst-colorization/

In this directory are two files, in addition to the model itself:

- `cog.yaml`, which defines the Docker environment the model will run inside
- `predict.py`, which defines how inferences are run on the model

## Run your model in a consistent environment

The simplest thing you can do with Cog is run a command inside the Docker environment. For example, to get a Python shell:

    $ cog run python
    ✓ Building Docker image from environment in cog.yaml... Successfully built 8f54020c8981
    Running 'python' in Docker with the current directory mounted as a volume...
    ───────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────

    Python 3.8.10 (default, May 12 2021, 23:32:14)
    [GCC 9.3.0] on linux
    Type "help", "copyright", "credits" or "license" for more information.
    >>>

Cog also lets you run inferences on your model with the interface defined in `predict.py`:

    $ cog predict -i

## Pushing a model to a server

First step is to start a server. You'll need to point it at a Docker registry to store the Docker images For example,

First step is to start a Cog server:

    cog server

> Note: This won't persist any Docker images you create to a Docker registry, so you won't be able to share any models you push. In production, you probably want to create a registry on somewhere like [Google Cloud Container Registry](https://cloud.google.com/container-registry/docs/quickstart) and pass it with the `--docker-registry` flag.

Next, connect the directory on your local machine to a model on the server:

    $ cog model set http://localhost:8080/test/inst-colorization

Then, let's push the model:

    $ cog push
    Uploading /Users/ben/p/cog-examples/inst-colorization to http://localhost:8080/test/inst-colorization
    Successfully uploaded version 0665f83fe2d219e22c56d0a3c7b0c7d96cb65ecc

    Docker image for cpu building in the background... See the status with:
      cog build log 1tVML8WbzoVhCz7OrRr3lJIe5aH

    Docker image for gpu building in the background... See the status with:
      cog build log 1tVML8vTraxdWCF6IEZ6db31SiM

This has uploaded the current directory to the server and created a new **version** of your model. In the background, it is now building two Docker images (CPU and GPU variants) that will run your model. You can see the status of the build by running the `cog build log` commands it mentions.

When the build has finished, you can run predictions on the built model from any machine that is pointed at the server. Replace the ID with the ID mentioned by `cog push`, and the file with an image on your disk you want to colorize:

    cog predict 0665f83fe2d219e22c56d0a3c7b0c7d96cb65ecc -i @hotdog.jpg

You can list versions:

    $ cog list
    ID                                        CREATED
    1a7f64c8585020e97d87146e6927af5c147cc49b  about a minute ago
    6b4df032586fb9b4831042a65d4e2cd09a36d064  2 minutes ago

And, you can view more details about a version:

    $ cog show 1a7f64c8585020e97d87146e6927af5c147cc49b
    ID:       1a7f64c8585020e97d87146e6927af5c147cc49b
    Model:    test/hello-world
    Created:  2 minutes ago
    ...

## Deploying the model

Cog builds Docker images for each version of your model. Those Docker images serve an HTTP prediction API (to be documented).

To get the Docker image, run this with the version you want to deploy:

    cog show b31f9f72d8f14f0eacc5452e85b05c957b9a8ed9

In this output is the name of the CPU or GPU Docker images, dependending on whether you are deploying on CPU or GPU. You can run these anywhere a Docker image runs. For example:

    $ docker run -d -p 5000:5000 --gpus all registry.hooli.net/colorization:b6a2f8a2d2ff-gpu
    $ curl http://localhost:5000/predict -F input=@image.png
