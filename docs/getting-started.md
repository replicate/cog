# Quickstart

> **Note:** This guide is a work in progress! Please excuse the brevity. Hopefully it'll give you an idea of how it works, though.

This guide will help you understand how Cog works. We're going to take a model that somebody else has built and trained and run it with Cog.

You'll need git-lfs installed to get the trained model weights. On Mac, run `brew install git-lfs`.

First, install Cog if you haven't already:

    curl -L https://github.com/replicate/cog/releases/latest/download/cog_`uname -s`_`uname -m` > /usr/local/bin/cog
    chmod +x /usr/local/bin/cog

Then, clone our example repository:

    git clone https://github.com/andreasjansson/InstColorization.git
    cd InstColorization/

In this directory are two files, in addition to the model itself:

- `cog.yaml`, which defines the Docker environment the model will run inside
- `cog_predict.py`, which defines how predictions are run on the model

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

## Run inferences on a model

Cog also lets you run inferences on your model with the interface defined in `cog_predict.py`:

    $ cog predict -i @input.jpg
    ✓ Building Docker image from environment in cog.yaml... Successfully built 664ef88bc1f4
    ✓ Model running in Docker image 664ef88bc1f4

    Written output to output.png

## Pushing a model to a server

Cog models can be stored centrally on a server so other people can run them and you can deploy them to production.

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
    0665f83fe2d219e22c56d0a3c7b0c7d96cb65ecc  about a minute ago
    6b4df032586fb9b4831042a65d4e2cd09a36d064  2 minutes ago

And, you can view more details about a version:

    $ cog show 0665f83fe2d219e22c56d0a3c7b0c7d96cb65ecc
    ID:       0665f83fe2d219e22c56d0a3c7b0c7d96cb65ecc
    Model:    test/hello-world
    Created:  2 minutes ago
    ...

## Deploying the model

Cog builds Docker images for each version of your model. Those Docker images serve an HTTP prediction API (to be documented).

To get the Docker image, run this with the version you want to deploy:

    cog show 0665f83fe2d219e22c56d0a3c7b0c7d96cb65ecc

In this output is the name of the CPU or GPU Docker images, dependending on whether you are deploying on CPU or GPU. You can run these anywhere a Docker image runs. For example:

    $ docker run -d -p 5000:5000 --gpus all registry.hooli.net/colorization:b6a2f8a2d2ff-gpu
    $ curl http://localhost:5000/predict -F input=@image.png

## Next steps

[You might want to take a look at the guide to help you set up your own model on Cog.](https://github.com/replicate/cog/blob/main/docs/getting-started-own-model.md)
