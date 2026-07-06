# Deploy models with Cog

Cog containers are Docker containers that serve an HTTP server
for running your model.
You can deploy them anywhere that Docker containers run.

The server inside Cog containers is **coglet**, a Rust-based inference server
that handles HTTP requests, worker process management, and run execution.

This guide assumes you have a model packaged with Cog.
If you don't, [follow our getting started guide](getting-started-own-model.md),
or start from one of the [examples in the Cog repository](examples.md).

## Build a Docker image

Build your model into a Docker image:

```console
cog build -t my-model
```

The image contains your model code, dependencies, the Cog runtime, and everything in between.
It serves an HTTP server on port 5000 when run.

## Run the model

You have several options for running a built image.

### Docker

Run the image directly with Docker.
This is the approach you'd use for production deployment.

```shell
# If your model uses a CPU:
docker run -d -p 5001:5000 my-model

# If your model uses a GPU:
docker run -d -p 5001:5000 --gpus all my-model
```

The server listens on port 5000 inside the container (mapped to 5001 above).

### cog serve

For local development, `cog serve` builds the image and starts the server
with your project directory mounted in:

```console
cog serve
```

By default the server runs on port 8393.
Use `-p` to choose a different port:

```console
cog serve -p 5000
```

## Make a prediction

Once the server is running, make predictions by sending a POST request
to the `/predictions` endpoint.
Inputs go inside an `"input"` object in the JSON body.

> [!NOTE]
> The examples below use `localhost:5001`, matching the Docker command above
> (`-p 5001:5000`). If you used `cog serve`, use `localhost:8393` by default,
> or the port you passed with `-p`.

```console
curl http://localhost:5001/predictions -X POST \
    -H "Content-Type: application/json" \
    -d '{"input": {"prompt": "a photo of a cat", "steps": 50}}'
```

```json
{
  "status": "succeeded",
  "output": "data:image/png;base64,...",
  "metrics": {
    "predict_time": 4.52
  }
}
```

> [!IMPORTANT]
> Inputs **must** be wrapped in an `"input"` object.
> `{"input": {"scale": 2.0}}` is correct; `{"scale": 2.0}` is not.

To discover what inputs your model accepts,
view the OpenAPI schema:

```console
curl http://localhost:5001/openapi.json
```

### Passing file inputs

File inputs (`cog.Path` or `cog.File` types) are passed as strings
inside the `"input"` object.
There are two ways to do this:

**1. HTTP/HTTPS URLs**

Pass a URL to a publicly accessible file.
The server downloads it inside the container:

```console
curl http://localhost:5001/predictions -X POST \
    -H "Content-Type: application/json" \
    -d '{"input": {"image": "https://example.com/photo.jpg"}}'
```

**2. Data URLs (base64)**

To pass a local file, encode it as a [data URL](https://developer.mozilla.org/en-US/docs/Web/HTTP/Basics_of_HTTP/Data_URLs):

```bash
# Construct a data URL from a local file
DATA_URL=$(python3 -c "
import base64, mimetypes
with open('input.jpg', 'rb') as f:
    data = base64.b64encode(f.read()).decode()
mime = mimetypes.guess_type('input.jpg')[0] or 'application/octet-stream'
print(f'data:{mime};base64,{data}')
")

curl http://localhost:5001/predictions -X POST \
    -H "Content-Type: application/json" \
    -d "{\"input\": {\"image\": \"$DATA_URL\"}}"
```

> [!NOTE]
> The HTTP API only accepts JSON (`application/json`).
> Multipart form uploads are not supported.
> When you use `cog run -i image=@photo.jpg`,
> the CLI handles the base64 encoding for you automatically.

### Getting output files

When a model returns a file output (`cog.Path` or `cog.File`),
the response contains a base64-encoded data URL by default:

```json
{
  "status": "succeeded",
  "output": "data:image/png;base64,iVBORw0KGgo..."
}
```

To have the server upload output files to external storage instead,
start the server with the `--upload-url` flag. The server then uploads each
file output to that URL prefix and returns the resulting URL in the response.

With `cog serve`:

```console
cog serve --upload-url https://example.com/upload/
```

When running the image directly with Docker, override the command to start the
server with `--upload-url`:

```shell
docker run -d -p 5001:5000 my-model \
    python -m cog.server.http --upload-url https://example.com/upload/
```

With an upload URL configured, file outputs are uploaded and the response
contains the uploaded URL instead of a data URL:

```json
{
  "status": "succeeded",
  "output": "https://example.com/upload/image.png"
}
```

## Run a one-off prediction

The Docker and `cog serve` commands above leave an HTTP server running.
If you instead want to run a single prediction and exit — without starting a
server — use `cog run`:

```console
cog run my-model -i image=@input.jpg
```

This starts the container, runs one prediction, prints the result, and exits.
File inputs are passed with the `@` prefix (e.g. `-i image=@photo.jpg`),
and the CLI handles base64 encoding for you.

## Health checks

The server exposes a `GET /health-check` endpoint that returns the current status of the model container. Use this for readiness probes in orchestration systems like Kubernetes.

```console
curl http://localhost:5001/health-check
```

The response includes a `status` field with values like `STARTING`, `READY`, `BUSY`, `SETUP_FAILED`, or `DEFUNCT`. See the [HTTP API reference](http.md#get-health-check) for full details.

## Stop the server

If you started the container with `docker run -d`, stop it with:

```console
docker kill <container-id>
```

If you used `cog serve`, press `Ctrl+C` in the terminal.
(`cog run` exits on its own once the prediction finishes, so there's nothing
to stop.)

## Concurrency

By default, the server processes one run at a time. To enable concurrent runs, make your `run()` method async and decorate it with `@cog.concurrent(max=N)`:

```py
import cog

class Runner(cog.BaseRunner):
    @cog.concurrent(max=4)
    async def run(self) -> str:
        return "hello world"
```

The deprecated [`concurrency.max`](yaml.md#concurrency) field in `cog.yaml` is still supported and takes precedence over the decorator by baking `COG_MAX_CONCURRENCY` into the image.

## Environment variables

You can configure runtime behavior with environment variables:

- `COG_SETUP_TIMEOUT`: Maximum time in seconds for the `setup()` method (default: no timeout).
- `COG_MAX_CONCURRENCY`: Number of concurrent prediction slots (default: 1). Overrides both `@cog.concurrent` and deprecated `cog.yaml` concurrency.

See the [environment variables reference](environment.md) for the full list.

## Next steps

- [HTTP API reference](http.md) for full endpoint documentation
- [Private registries](private-package-registry.md) for using private Python package registries
- [`cog.yaml` reference](yaml.md) for configuration options
