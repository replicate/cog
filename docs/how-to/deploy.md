# How to deploy to production

This guide shows you how to build a Cog image, run it in production environments, push it to container registries, and configure it for production workloads.

## Build the image

Build a tagged Docker image from your `cog.yaml`:

```console
cog build -t my-model:latest
```

If you want weights in a separate layer for faster subsequent pushes:

```console
cog build -t my-model:latest --separate-weights
```

## Run with Docker

### CPU models

```console
docker run -d -p 5001:5000 my-model:latest
```

### GPU models

Pass the `--gpus` flag to give the container access to GPUs:

```console
docker run -d -p 5001:5000 --gpus all my-model:latest
```

The server listens on port 5000 inside the container. Map it to any host port you need.

### Test the running container

Wait for the model to finish setup, then send a prediction:

```console
# Check readiness
curl http://localhost:5001/health-check

# Send a prediction
curl http://localhost:5001/predictions -X POST \
    -H "Content-Type: application/json" \
    -d '{"input": {"prompt": "hello world"}}'
```

## Configure health checks

The `/health-check` endpoint returns the current status of the container. Use it for readiness and liveness probes.

The response always returns `200 OK` -- check the `status` field in the body to determine readiness:

| Status | Meaning |
|---|---|
| `STARTING` | `setup()` is still running |
| `READY` | Ready to accept predictions |
| `BUSY` | Ready but all prediction slots are in use |
| `SETUP_FAILED` | `setup()` raised an exception |
| `DEFUNCT` | Unrecoverable error |

For Kubernetes, configure a readiness probe that checks for `READY`:

```yaml
readinessProbe:
  httpGet:
    path: /health-check
    port: 5000
  initialDelaySeconds: 10
  periodSeconds: 5
livenessProbe:
  httpGet:
    path: /health-check
    port: 5000
  initialDelaySeconds: 30
  periodSeconds: 10
```

Your readiness check logic should parse the JSON response and only mark the pod as ready when `status` is `READY`. A simple shell check:

```console
curl -sf http://localhost:5000/health-check | python3 -c "import sys,json; sys.exit(0 if json.load(sys.stdin)['status']=='READY' else 1)"
```

## Set runtime environment variables

Configure runtime behaviour with environment variables passed to `docker run`:

```console
docker run -d -p 5001:5000 \
    -e COG_SETUP_TIMEOUT=300 \
    my-model:latest
```

Key runtime variables:

- `COG_SETUP_TIMEOUT`: Maximum seconds for `setup()` to complete (default: no timeout).

See the [environment variables reference](../environment.md) for the full list.

## Push to a container registry

### Push to Replicate

Authenticate first, then push:

```console
cog login
cog push r8.im/your-username/my-model
```

If you set `image` in your `cog.yaml`, you can push without specifying the target:

```yaml
image: "r8.im/your-username/my-model"
```

```console
cog push
```

### Push to other registries

Cog can push to any OCI-compliant registry. Authenticate with Docker first:

```console
docker login registry.example.com
cog push registry.example.com/your-org/my-model
```

Or build and push separately:

```console
cog build -t registry.example.com/your-org/my-model:latest
docker push registry.example.com/your-org/my-model:latest
```

## Configure webhooks for async predictions

For long-running predictions, use async mode with webhooks so clients do not need to hold open a connection:

```console
curl http://localhost:5001/predictions -X POST \
    -H "Content-Type: application/json" \
    -H "Prefer: respond-async" \
    -d '{
        "input": {"prompt": "a photo of a cat"},
        "webhook": "https://your-app.example.com/webhook",
        "webhook_events_filter": ["start", "completed"]
    }'
```

The server returns `202 Accepted` immediately and delivers results to your webhook URL. Available webhook events are `start`, `output`, `logs`, and `completed`.

If your model produces file output and you use async predictions, configure the upload URL when starting the server:

```console
docker run -d -p 5001:5000 my-model:latest \
    --upload-url https://your-storage.example.com/upload
```

See the [HTTP API reference on webhooks](../http.md#webhooks) for the full protocol.

## Deploy on Kubernetes

A minimal Kubernetes deployment for a GPU model:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: my-model
spec:
  replicas: 1
  selector:
    matchLabels:
      app: my-model
  template:
    metadata:
      labels:
        app: my-model
    spec:
      containers:
        - name: my-model
          image: registry.example.com/your-org/my-model:latest
          ports:
            - containerPort: 5000
          resources:
            limits:
              nvidia.com/gpu: 1
          readinessProbe:
            httpGet:
              path: /health-check
              port: 5000
            initialDelaySeconds: 30
            periodSeconds: 5
          env:
            - name: COG_SETUP_TIMEOUT
              value: "300"
---
apiVersion: v1
kind: Service
metadata:
  name: my-model
spec:
  selector:
    app: my-model
  ports:
    - port: 80
      targetPort: 5000
```

This requires the [NVIDIA device plugin](https://github.com/NVIDIA/k8s-device-plugin) to be installed on your cluster for GPU support.

If your model has a slow `setup()` (e.g. downloading weights), set `initialDelaySeconds` high enough that the pod is not killed before setup completes.

## Next steps

- See the [HTTP API reference](../http.md) for the full API specification.
- See [How to run concurrent predictions](concurrency.md) to increase throughput.
- See [How to set up CI/CD](ci-cd.md) to automate builds and pushes.
- See the [CLI reference](../cli.md) for all `cog build` and `cog push` options.
