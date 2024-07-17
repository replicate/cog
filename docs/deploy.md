# Deploy models with Cog

Cog containers are Docker containers that serve an HTTP server 
for running predictions on your model. 
You can deploy them anywhere that Docker containers run.

This guide assumes you have a model packaged with Cog. 
If you don't, [follow our getting started guide](getting-started-own-model.md), 
or use [an example model](https://github.com/replicate/cog-examples).

## Getting started

First, build your model:

```console
cog build -t my-model
```

Then, start the Docker container:

```shell
# If your model uses a CPU:
docker run -d -p 5001:5000 my-model

# If your model uses a GPU:
docker run -d -p 5001:5000 --gpus all my-model

# If you're on an M1 Mac:
docker run -d -p 5001:5000 --platform=linux/amd64 my-model
```

The server is now running locally on port 5001.

To view the OpenAPI schema, 
open [localhost:5001/openapi.json](http://localhost:5001/openapi.json) 
in your browser 
or use cURL to make a request:

```console
curl http://localhost:5001/openapi.json
```

To stop the server, run:

```console
docker kill my-model
```

To run a prediction on the model, 
call the `/predictions` endpoint, 
passing input in the format expected by your model:

```console
curl http://localhost:5001/predictions -X POST \
    --header "Content-Type: application/json" \
    --data '{"input": {"image": "https://.../input.jpg"}}'

For more details about the HTTP API, 
see the [HTTP API reference documentation](http.md).

## Options

Cog Docker images have `python -m cog.server.http` set as the default command, which gets overridden if you pass a command to `docker run`. When you use command-line options, you need to pass in the full command before the options.

### `--threads`

This controls how many threads are used by Cog, which determines how many requests Cog serves in parallel. If your model uses a CPU, this is the number of CPUs on your machine. If your model uses a GPU, this is 1, because typically a GPU can only be used by one process.

You might need to adjust this if you want to control how much memory your model uses, or other similar constraints. To do this, you can use the `--threads` option.

For example:

    docker run -d -p 5000:5000 my-model python -m cog.server.http --threads=10

## `--host`

By default, Cog serves to `0.0.0.0`.
You can override this using the `--host` option.

For example, 
to serve Cog on an IPv6 address, run:

    docker run -d -p 5000:5000 my-model python -m cog.server.http --host="::"
