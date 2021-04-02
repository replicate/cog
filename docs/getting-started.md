# Getting started with Cog

First step is to start a server. You'll need to point it at a Docker registry to store the Docker images For example, [create one on Google Cloud Container Registry](https://cloud.google.com/container-registry/docs/quickstart). 

Then, start the server, pointing at your Docker registry:

    cog server --port=8080 --docker-registry=gcr.io/some-project/cog


Next, let's build a model. We have [some models you can play around with](https://github.com/replicate/cog-examples). Clone that repository (you'll need git-lfs) and hook up that directory to your Cog server:

    cd example-models/inst-colorization/
    cog repo set localhost:8080/examples/inst-colorization

Then, let's build it:

    cog build

This will take a few minutes. In the meantime take a look at `cog.yaml` and `model.py` to see what we're building.

When that has finished, you can run inferences on the built model from any machine that is pointed at the server. Replace the ID with your model's ID, and the file with an image on your disk you want to colorize:

    cog infer b31f9f72d8f14f0eacc5452e85b05c957b9a8ed9 -i @hotdog.jpg

You can also list the models for this repo:

    cog list

You can see more details about the model:

    cog show b31f9f72d8f14f0eacc5452e85b05c957b9a8ed9

In this output is the Docker image. You can run this anywhere a Docker image runs to deploy your model. For example:

    $ docker run -d -p 8000:8000 --gpus all registry.hooli.net/colorization:b6a2f8a2d2ff-gpu
    $ curl http://localhost:8000/infer -F input=@image.png
