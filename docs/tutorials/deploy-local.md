# Build and deploy locally

In this tutorial, we will take the model from [Package your first model](first-model.md), build it into a Docker image, run it as an HTTP server, and make predictions using curl. By the end, you will have a running prediction server that you can send requests to from any HTTP client.

## Prerequisites

Before starting, you should have:

- Completed the [Package your first model](first-model.md) tutorial
- The `cog-first-model` project directory with `cog.yaml` and `predict.py`
- **Docker** running

If you no longer have the project, create it now. Make a directory called `cog-first-model` and add these two files:

`cog.yaml`:

```yaml
build:
  python_version: "3.13"
predict: "predict.py:Predictor"
```

`predict.py`:

```python
import tempfile
from typing import Optional

from cog import BasePredictor, Input, Path


class Predictor(BasePredictor):
    def setup(self):
        """Load resources needed for predictions"""
        self.substitutions = {
            "hello": "greetings",
            "world": "planet",
            "goodbye": "farewell",
            "friend": "companion",
        }

    def predict(
        self,
        text: str = Input(description="Text to transform", default=""),
        text_file: Optional[Path] = Input(description="Text file to transform (used instead of text)", default=None),
        repeat: int = Input(description="Number of times to repeat the output", default=1, ge=1, le=5),
        uppercase: bool = Input(description="Convert output to uppercase", default=False),
    ) -> Path:
        if text_file is not None:
            with open(text_file, "r") as f:
                source_text = f.read().strip()
        else:
            source_text = text

        words = source_text.lower().split()
        result = " ".join(self.substitutions.get(word, word) for word in words)

        if uppercase:
            result = result.upper()

        lines = [result] * repeat
        output_text = "\n".join(lines)

        output_path = Path(tempfile.mkdtemp()) / "output.txt"
        with open(output_path, "w") as f:
            f.write(output_text)

        return output_path
```

## Build the Docker image

From the `cog-first-model` directory, build the image:

```bash
cog build -t my-model
```

You will see build progress as Cog creates a Docker image with Python, the Cog runtime, and your model code. It ends with:

```
Built my-model:latest
```

Verify the image was created:

```bash
docker images my-model
```

You will see output like:

```
REPOSITORY   TAG       IMAGE ID       CREATED          SIZE
my-model     latest    abc123def456   5 seconds ago    1.2GB
```

## Start the prediction server

Run the Docker image. It starts an HTTP server on port 5000:

```bash
docker run -d --rm -p 5000:5000 --name my-model my-model
```

The flags mean:

- `-d` runs the container in the background
- `--rm` removes the container when it stops
- `-p 5000:5000` maps port 5000 on your machine to port 5000 in the container
- `--name my-model` gives the container a name so we can refer to it later

## Check the health endpoint

Before sending predictions, verify the server is ready. The server exposes a health check endpoint:

```bash
curl http://localhost:5000/health-check
```

If the model is still loading, you will see:

```json
{
  "status": "STARTING",
  "setup": {
    "started_at": "2025-01-15T10:00:00.000000+00:00",
    "status": "starting",
    "logs": ""
  }
}
```

Wait a moment and try again. When the model is ready, you will see:

```json
{
  "status": "READY",
  "setup": {
    "started_at": "2025-01-15T10:00:00.000000+00:00",
    "completed_at": "2025-01-15T10:00:01.000000+00:00",
    "status": "succeeded",
    "logs": ""
  }
}
```

Notice the `"status": "READY"` at the top. This means the `setup()` method has completed and the model is accepting predictions.

## Make a prediction with curl

Send a prediction request:

```bash
curl -s http://localhost:5000/predictions \
  -X POST \
  -H 'Content-Type: application/json' \
  -d '{"input": {"text": "hello world"}}'
```

You will see a response like:

```json
{
  "status": "succeeded",
  "output": "data:text/plain;base64,Z3JlZXRpbmdzIHBsYW5ldA==",
  "metrics": {
    "predict_time": 0.001
  }
}
```

The response has three fields:

- `status` tells you the prediction succeeded.
- `output` contains the result. Because our model returns a file (via `cog.Path`), the output is a base64-encoded data URL. We will decode it in a moment.
- `metrics` includes timing information. The `predict_time` value is how long the `predict()` method took in seconds.

## Decode the file output

The output is base64-encoded because our model returns a `cog.Path` (a file). To see the actual text, decode it:

```bash
curl -s http://localhost:5000/predictions \
  -X POST \
  -H 'Content-Type: application/json' \
  -d '{"input": {"text": "hello world"}}' \
  | python3 -c "
import sys, json, base64
resp = json.load(sys.stdin)
data_url = resp['output']
encoded = data_url.split(',', 1)[1]
print(base64.b64decode(encoded).decode())
"
```

You will see:

```
greetings planet
```

## Pass multiple inputs

Send a request with all the input options:

```bash
curl -s http://localhost:5000/predictions \
  -X POST \
  -H 'Content-Type: application/json' \
  -d '{"input": {"text": "hello world goodbye friend", "repeat": 3, "uppercase": true}}'
```

You will see a response with `"status": "succeeded"` and a base64-encoded output. The decoded content would be:

```
GREETINGS PLANET FAREWELL COMPANION
GREETINGS PLANET FAREWELL COMPANION
GREETINGS PLANET FAREWELL COMPANION
```

Notice that the input keys in the JSON (`text`, `repeat`, `uppercase`) match the parameter names in the `predict()` method exactly.

## Explore the API schema

The server generates an OpenAPI specification from your model's type annotations. Fetch it:

```bash
curl -s http://localhost:5000/openapi.json | python3 -m json.tool | head -20
```

You will see the beginning of the OpenAPI schema, which includes input descriptions, types, defaults, and constraints -- all generated from the `Input()` definitions in your `predict.py`. This schema can be used to auto-generate client libraries or documentation.

You can also check the root endpoint to see available URLs:

```bash
curl -s http://localhost:5000/
```

You will see:

```json
{
  "docs_url": "/openapi.json",
  "openapi_url": "/openapi.json",
  "predictions_url": "/predictions",
  "health_check_url": "/health-check"
}
```

## View the server logs

To see what the server is doing, check the container logs:

```bash
docker logs my-model
```

You will see setup output, incoming request logs, and any output your model prints to stdout.

## Stop the server

When you are done, stop the container:

```bash
docker stop my-model
```

The container stops and is automatically removed (because of the `--rm` flag we used when starting it).

## What you have built

You have:

1. Built a production Docker image with `cog build`
2. Run the image as an HTTP prediction server with Docker
3. Checked server readiness with the `/health-check` endpoint
4. Made predictions using curl and the `/predictions` endpoint
5. Inspected the auto-generated OpenAPI schema

This is the same pattern used to deploy Cog models in production. The Docker image runs anywhere Docker does -- cloud VMs, Kubernetes clusters, or serverless platforms.

## Next steps

- [Deploying models](../deploy.md) -- deploy your model to Replicate or other platforms
- [HTTP API reference](../http.md) -- full reference for the prediction server endpoints
- [cog.yaml reference](../yaml.md) -- all environment configuration options
- [Python SDK reference](../python.md) -- input types, output types, streaming, and more
