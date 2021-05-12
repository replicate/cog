# Getting started with Cog

First step is to start a server. You'll need to point it at a Docker registry to store the Docker images For example, [create one on Google Cloud Container Registry](https://cloud.google.com/container-registry/docs/quickstart).

Then, start the server, pointing at your Docker registry:

    cog server --port=8080 --docker-registry=gcr.io/some-project/cog

Next, let's build a model. We have [some models you can play around with](https://github.com/replicate/cog-examples). Clone that repository (you'll need git-lfs) and hook up that directory to your Cog server:

    cd example-models/inst-colorization/
    cog model set http://localhost:8080/examples/inst-colorization

Take a look at `cog.yaml` and `model.py` to see what we're building.

Then, let's push it:

    cog push

This has uploaded the currently directory to the server and the server has stored that as a version of your model. In the background, it is now building two Docker images (CPU and GPU variants) that will run your model. You can see the status of this build with `cog show`, replacing the ID with the ID of the version you created:

    $ cog show b31f9f72d8f14f0eacc5452e85b05c957b9a8ed9
    ...
    CPU image: Building. Run `cog build logs 8f3a05c42ee5` to see its progress.
    GPU image: Building. Run `cog build logs b087a0bb5b7a` to see its progress.

When the build has finished, you can run inferences on the built model from any machine that is pointed at the server. Replace the ID with yours, and the file with an image on your disk you want to colorize:

    cog infer b31f9f72d8f14f0eacc5452e85b05c957b9a8ed9 -i @hotdog.jpg

You can also list the versions of this model:

    cog list

## Deploying the model

Cog builds Docker images for each version of your model. Those Docker images serve an HTTP inference API (to be documented).

To get the Docker image, run this with the version you want to deploy:

    cog show b31f9f72d8f14f0eacc5452e85b05c957b9a8ed9

In this output is the name of the CPU or GPU Docker images, dependending on whether you are deploying on CPU or GPU. You can run these anywhere a Docker image runs. For example:

    $ docker run -d -p 5000:5000 --gpus all registry.hooli.net/colorization:b6a2f8a2d2ff-gpu
    $ curl http://localhost:5000/infer -F input=@image.png
