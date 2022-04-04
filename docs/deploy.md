# Deploy models with Cog

Cog containers are Docker containers that serve an HTTP server for running predictions on your model. You can deploy them anywhere that Docker containers run.

This guide assumes you have a model packaged with Cog. If you don't, [follow our getting started guide](getting-started-own-model.md), or use [an example model](https://github.com/replicate/cog-examples).

First, build your model:

    cog build -t my-model

Then, start the Docker container:

    docker run -d -p 5000:5000 my-model

    # If your model uses a GPU:
    docker run -d -p 5000:5000 --gpus all my-model

    # if you're on an M1 Mac:
    docker run -d -p 5000:5000 --platform=linux/amd64 my-model

Port 5000 is now serving the API:

    curl http://localhost:5000

To run a prediction on the model, call the `/predictions` endpoint, passing input in the format expected by your model:

    curl http://localhost:5000/predictions -X POST \
        --data '{"input": {"image": "https://.../input.jpg"}}'

To view the API documentation in browser for the model that is running, open [http://localhost:5000/docs](http://localhost:5000/docs).

For more details about the HTTP API, see the [HTTP API reference documentation](http.md).
