# The prediction lifecycle

A prediction in Cog passes through a well-defined sequence of states, from container startup through model loading, request handling, output delivery, and completion. Understanding this lifecycle helps you design your predictor for reliability and performance, diagnose issues when predictions fail, and integrate with Cog's HTTP API effectively.

## Container startup and setup

Before any prediction can run, the container must complete its startup sequence. This is not a simple "start the server" step -- it involves two processes coordinating to bring the model online.

### The startup sequence

1. **HTTP server starts immediately.** The coglet parent process begins listening on port 5000 as soon as the container starts. The health check endpoint (`GET /health-check`) is available right away, returning `STARTING` status. The reason for starting the HTTP server before the model is ready is to give orchestration systems (like Kubernetes readiness probes) a way to monitor progress.

2. **Worker subprocess spawns.** Coglet spawns a Python subprocess that will host your model. This process is separate from the HTTP server for isolation -- if your model crashes during setup, the HTTP server survives to report the failure.

3. **Your predictor loads.** The worker imports your predictor module (e.g., `predict.py`) and instantiates your `Predictor` class.

4. **`setup()` runs.** If your predictor defines a `setup()` method, it is called now. This is where you load model weights, initialise pipelines, download resources, or perform other expensive one-time work. Setup can take anywhere from milliseconds to several minutes depending on the model.

5. **Worker signals readiness.** Once `setup()` completes successfully, the worker sends a "ready" message to the parent process along with the OpenAPI schema derived from your `predict()` method's type annotations.

6. **Server becomes READY.** The health check now returns `READY`, and the server begins accepting prediction requests.

### What happens when setup fails

If `setup()` raises an exception, the health check transitions to `SETUP_FAILED`. The server remains running -- it continues to respond to health checks and will return errors for prediction requests -- but it will not attempt to run predictions. This is by design: a clean failure state is more useful for debugging than a container that silently restarts in a loop.

You can set a setup timeout using the `COG_SETUP_TIMEOUT` environment variable. If setup exceeds this duration, it is treated as a failure. By default, there is no timeout.

## Health check states

The health check endpoint (`GET /health-check`) always returns HTTP 200, with a `status` field in the response body that indicates the container's true state. The reason it always returns 200 -- rather than using HTTP status codes to signal readiness -- is that different orchestration systems interpret non-200 responses differently. A consistent 200 with a status field works reliably across all platforms.

The possible states are:

| Status | Meaning |
|--------|---------|
| `STARTING` | The model's `setup()` method is still running. The container is alive but not ready for predictions. |
| `READY` | Setup succeeded and the server is accepting predictions. |
| `BUSY` | The model is healthy, but all prediction slots are currently occupied. New requests will receive a `409 Conflict` response. |
| `SETUP_FAILED` | The `setup()` method raised an exception. The container will not accept predictions. |
| `DEFUNCT` | An unrecoverable error occurred (e.g., the worker process crashed and could not be restarted). |
| `UNHEALTHY` | The model is nominally ready, but a user-defined `healthcheck()` method returned `False`. |

The distinction between `BUSY` and `READY` matters for load balancers. A `BUSY` container is healthy but temporarily unable to accept work -- the load balancer should route to a different instance. A `SETUP_FAILED` or `DEFUNCT` container should be replaced entirely.

## The prediction request

When a prediction request arrives at `POST /predictions`, a sequence of steps occurs before your `predict()` method is ever called.

### Input validation

The server validates the incoming JSON against the OpenAPI schema derived from your `predict()` method's type annotations and `Input()` descriptors. This happens in the Rust HTTP server, before any Python code runs.

The reason validation happens at this level is performance and safety. Rejecting malformed requests before they reach the Python worker avoids wasting GPU time on inputs that will inevitably fail. It also provides clear, structured error messages to the client -- rather than a Python traceback from a type error deep in your model code.

For example, if your `predict()` signature includes `scale: float = Input(ge=1.0, le=10.0)` and a request sends `scale: 15.0`, the server returns a validation error immediately. Your Python code never sees the request.

### Slot acquisition

After validation, the server attempts to acquire a prediction slot. By default there is one slot, meaning predictions run sequentially. With `concurrency.max` configured, multiple slots are available.

If no slot is available, the behaviour depends on the request type:

- **Synchronous requests**: Return `409 Conflict` immediately
- **Asynchronous requests** (with `Prefer: respond-async` header): Also return `409 Conflict`

The reason Cog does not queue requests is simplicity and predictability. Queuing introduces complex questions about queue depth, timeouts, ordering guarantees, and back-pressure. By returning 409, Cog pushes queuing responsibility to the client or orchestration layer, where it can be managed with full knowledge of the broader system architecture.

### Dispatch to worker

Once a slot is acquired, the prediction is dispatched to the Python worker over a Unix domain socket dedicated to that slot. Each slot has its own socket, which avoids head-of-line blocking -- a slow prediction on one slot does not delay communication for another.

The request includes the prediction ID and the validated input payload. The worker sets up the execution context (for log routing and metrics collection) and then calls your `predict()` method.

## Prediction execution

Your `predict()` method runs in the Python worker subprocess. From your code's perspective, it is a normal Python function call. But several things are happening around it.

### Output handling

The output of `predict()` is handled differently depending on the return type:

- **Simple values** (strings, numbers, booleans): Returned directly in the response.
- **`cog.Path` or `cog.File` objects**: The file is read and either base64-encoded into a data URL or uploaded to a configured URL. The response contains the URL, not the file contents.
- **Iterators** (`Iterator[T]`): Each yielded value is streamed to the client as it is produced. This is particularly important for language models, where tokens can be delivered incrementally rather than waiting for the entire response.
- **`ConcatenateIterator`**: A special case of iterator output where the yielded values are meant to be concatenated into a single string. This is a hint for display purposes -- platforms like Replicate show the output as accumulating text rather than a list of fragments.

### Log capture

Everything your code writes to `stdout` and `stderr` during a prediction is captured and attributed to that specific prediction. The reason logs are per-prediction rather than global is to support concurrent predictions -- when multiple predictions run simultaneously, you need to know which log line belongs to which prediction.

Coglet achieves this through a `ContextVar`-based routing mechanism. Even if your code spawns async tasks or threads, logs are routed to the correct prediction as long as the context propagates.

### Metrics

The runtime automatically records `predict_time` for every prediction. You can record additional metrics using `self.record_metric()` inside your `predict()` method. Metrics appear in the prediction response alongside the output.

Metrics support accumulation modes (`incr` for counters, `append` for arrays), nested keys via dot-path notation, and type safety -- once a metric key has a type, it cannot be silently changed to a different type. These features are designed for instrumenting model inference at a level of detail that matters for production monitoring.

## Synchronous vs. asynchronous predictions

Cog supports two modes of prediction creation, and understanding the difference is important for choosing the right integration pattern.

### Synchronous predictions

A synchronous request (`POST /predictions` without special headers) blocks until the prediction completes. The server holds the HTTP connection open and returns the full result in the response body. This is the simplest integration pattern and works well for predictions that complete in seconds.

The tradeoff is that long-running predictions tie up the HTTP connection. If the client has a short timeout, or if a load balancer or proxy sits between the client and the server, the connection may be dropped before the prediction finishes.

### Asynchronous predictions

An asynchronous request (with the `Prefer: respond-async` header) returns immediately with `202 Accepted`. The server processes the prediction in the background and delivers results via webhooks.

The reason Cog uses webhooks rather than polling is efficiency and timeliness. With polling, the client must repeatedly ask "is it done yet?", wasting bandwidth and adding latency. With webhooks, the server pushes updates to the client as they happen.

Webhook events are fired at specific moments:

- `start`: When the prediction begins processing (once)
- `output`: Each time the predict function yields or returns output (debounced to at most once per 500ms)
- `logs`: Each time the predict function writes to stdout (debounced to at most once per 500ms)
- `completed`: When the prediction reaches a terminal state -- `succeeded`, `failed`, or `canceled` (once)

The 500ms debounce for `output` and `logs` is a fixed interval, chosen to balance responsiveness against webhook volume. For streaming models that produce many small outputs, this prevents overwhelming the webhook receiver with hundreds of requests per second.

You can filter which events trigger webhooks using the `webhook_events_filter` field in the request. For example, if you only care about the final result, specifying `["completed"]` avoids intermediate webhook traffic.

## Cancellation

Predictions can be cancelled by sending `POST /predictions/<id>/cancel`. Cancellation is only available for asynchronous predictions (those created with the `Prefer: respond-async` header and an explicit `id`).

When a cancellation request arrives:

1. The parent process sends a cancel signal to the worker
2. For **sync predictors** (`def predict`), a `CancelationException` is raised in the running predict function. This is a `BaseException` subclass -- not an `Exception` subclass -- so it will not be caught by generic `except Exception` handlers. This is deliberate: it matches the semantics of `KeyboardInterrupt` and ensures cancellation propagates cleanly through most code.
3. For **async predictors** (`async def predict`), an `asyncio.CancelledError` is raised, following standard Python async conventions.

If your code needs to perform cleanup on cancellation (releasing GPU memory, closing file handles), you can catch the exception, perform cleanup, and re-raise it. You *must* re-raise it -- swallowing the cancellation exception prevents the runtime from marking the prediction as cancelled and may result in the container being terminated.

The design of cancellation reflects a practical constraint: there is no reliable way to interrupt arbitrary Python code from outside the process. The signal-based approach (SIGUSR1 for sync predictors) is the most portable mechanism available, and raising an exception at the Python level gives your code a chance to clean up gracefully.

## Terminal states

A prediction ends in one of three states:

- **`succeeded`**: The `predict()` method returned or yielded its final output normally
- **`failed`**: The `predict()` method raised an unhandled exception, or input validation failed
- **`canceled`**: The prediction was cancelled via the cancel endpoint

Once a prediction reaches a terminal state, its slot is released and becomes available for the next request. If webhooks are configured, a `completed` event is sent with the final prediction state, output, and any error information.

## The bigger picture

The prediction lifecycle is designed around a few core principles:

**Separation of concerns.** The HTTP server handles networking, validation, and routing. The Python worker handles model execution. Neither needs to know the details of the other's implementation.

**Observability.** Every stage of the lifecycle is visible through the health check endpoint, prediction status fields, structured logs, and metrics. When something goes wrong, there is always a way to determine what happened and where.

**Graceful degradation.** Setup failures, prediction errors, and worker crashes all result in well-defined states with clear error messages, rather than silent failures or hanging connections.

These properties matter less when you are running `cog predict` on your laptop and more when you are running a model in production handling thousands of requests. But they are baked into the lifecycle from the start, so the model you test locally behaves the same way when deployed.

## Further reading

- [How Cog works](how-cog-works.md) -- the overall architecture of Cog containers
- [HTTP API reference](../http.md) -- endpoints, request formats, and response schemas
- [Prediction interface reference](../python.md) -- defining `setup()`, `predict()`, inputs, and outputs
- [Deploy models with Cog](../deploy.md) -- running Cog containers in production
