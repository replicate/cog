# Deploy models with Cog

Cog containers are Docker containers that serve an HTTP server
for running predictions on your model.
You can deploy them anywhere that Docker containers run.

The server inside Cog containers is **coglet**, a Rust-based prediction server
that handles HTTP requests, worker process management, and prediction execution.

This guide assumes you have a model packaged with Cog.
If you don't, [follow our getting started guide](getting-started-own-model.md),
or use [an example model](https://github.com/replicate/cog-examples).

## Getting started

First, build your model:

```console
cog build -t my-model
```

You can serve predictions locally with `cog serve`:

```console
cog serve
# or, from a built image:
cog serve my-model
```

Alternatively, start the Docker container directly:

```shell
# If your model uses a CPU:
docker run -d -p 5001:5000 my-model

# If your model uses a GPU:
docker run -d -p 5001:5000 --gpus all my-model
```

The server listens on port 5000 inside the container (mapped to 5001 above).

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
```

For more details about the HTTP API,
see the [HTTP API reference documentation](http.md).

## Health checks

The server exposes a `GET /health-check` endpoint that returns the current status of the model container. Use this for readiness probes in orchestration systems like Kubernetes.

```console
curl http://localhost:5001/health-check
```

The response includes a `status` field with values like `STARTING`, `READY`, `BUSY`, `SETUP_FAILED`, or `DEFUNCT`. See the [HTTP API reference](http.md#get-health-check) for full details.

## Concurrency

By default, the server processes one prediction at a time. To enable concurrent predictions, set the `concurrency.max` option in `cog.yaml`:

```yaml
concurrency:
  max: 4
```

See the [`cog.yaml` reference](yaml.md#concurrency) for more details.

## Environment variables

You can configure runtime behavior with environment variables:

- `COG_SETUP_TIMEOUT`: Maximum time in seconds for the `setup()` method (default: no timeout).

See the [environment variables reference](environment.md) for the full list.
