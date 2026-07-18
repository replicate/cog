# HTTP API

> [!TIP]
> For information about how to run the HTTP server,
> see [our documentation on deploying models](deploy.md).

When you run a Docker image built by Cog,
it serves an HTTP API for making predictions.

The server supports both synchronous and asynchronous prediction creation:

- **Synchronous**:
  The server waits until the prediction is completed
  and responds with the result.
- **Asynchronous**:
  The server immediately returns a response
  and processes the prediction in the background.

The client can create a prediction asynchronously
by setting the `Prefer: respond-async` header in their request
or by requesting a streamed response with `Accept: text/event-stream`.
With `Prefer: respond-async`,
the server responds immediately after starting the prediction
with `202 Accepted` status and a prediction object in status `starting`.
With `Accept: text/event-stream`,
the server responds with `200 OK` and keeps the response open
as a server-sent event stream.

> [!NOTE]
> For JSON responses, the only supported way to receive updates on the status
> of predictions started asynchronously is using [webhooks](#webhooks).
> Polling for prediction status is not currently supported.

You can also use certain server endpoints to create predictions idempotently,
such that if a client calls this endpoint more than once with the same ID
(for example, due to a network interruption)
while the prediction is still running,
no new prediction is created.
Instead, the client receives the response type requested by the retry:
JSON for regular requests or a server-sent event stream for streaming requests.

---

Here's a summary of the prediction creation endpoints:

| Endpoint                           | Header                      | Behavior                     |
| ---------------------------------- | --------------------------- | ---------------------------- |
| `POST /predictions`                | -                           | Synchronous, non-idempotent  |
| `POST /predictions`                | `Prefer: respond-async`     | Asynchronous, non-idempotent |
| `POST /predictions`                | `Accept: text/event-stream` | Streaming, non-idempotent    |
| `PUT /predictions/<prediction_id>` | -                           | Synchronous, idempotent      |
| `PUT /predictions/<prediction_id>` | `Prefer: respond-async`     | Asynchronous, idempotent     |
| `PUT /predictions/<prediction_id>` | `Accept: text/event-stream` | Streaming, idempotent        |

Choose the endpoint that best fits your needs:

- Use synchronous endpoints when you want to wait for the prediction result.
- Use asynchronous endpoints when you want to start a prediction
  and receive updates via webhooks.
- Use streaming endpoints when you want to receive prediction lifecycle events
  over the HTTP response as they happen.
- Use idempotent endpoints when you need to safely retry requests
  without creating duplicate predictions.

## Streaming predictions with server-sent events

To produce streamed prediction events,
the model must return an iterator and opt in to SSE streaming
with the `streaming` decorator.

```python
from typing import Iterator

from cog import BaseRunner, Input, streaming


class Runner(BaseRunner):
    @streaming
    def run(self, prompt: str = Input(description="Prompt")) -> Iterator[str]:
        for token in generate_tokens(prompt):
            yield token
```

The decorator can also be written as `@cog.streaming`
or, if imported directly from `cog`, `@streaming`.
The parenthesized forms `@cog.streaming()` and `@streaming()` are also accepted.
Without the decorator,
iterator outputs still work in normal JSON responses,
but requests with `Accept: text/event-stream` return `406 Not Acceptable`.

To consume a streamed prediction,
send the prediction request with `Accept: text/event-stream`:

```http
POST /predictions HTTP/1.1
Content-Type: application/json; charset=utf-8
Accept: text/event-stream

{
    "input": {"prompt": "Write a haiku about onions"}
}
```

The server starts the prediction asynchronously
and keeps the HTTP response open as a server-sent event stream.
Each event has an `event` name and JSON `data` payload:

```text
event: start
data: {"id":"abc123","status":"processing"}

event: output
data: {"chunk":"Onions","index":0}

event: output
data: {"chunk":" bloom","index":1}

event: completed
data: {"id":"abc123","status":"succeeded","output":["Onions"," bloom"],"metrics":{"predict_time":0.42}}
```

Prediction streams can emit these event types:

- `start`: The prediction started processing.
- `output`: The model yielded an output chunk.
  The payload includes `chunk` and `index`.
- `log`: The model wrote to `stdout` or `stderr`.
  The payload includes `source` and `data`.
- `metric`: The model recorded a custom metric.
  The payload includes `name`, `value`, and `mode`.
- `completed`: The prediction reached a terminal state.
  The payload is the final prediction object,
  with `status` set to `succeeded`, `failed`, or `canceled`.

For command-line clients,
use a client that prints the response as data arrives:

```bash
curl -N \
  -H 'Accept: text/event-stream' \
  -H 'Content-Type: application/json' \
  -d '{"input":{"prompt":"Write a haiku about onions"}}' \
  http://localhost:5000/predictions
```

For browser clients,
use `fetch()` or another client that supports request bodies.
The browser `EventSource` API only supports `GET` requests,
so it cannot create a prediction with `POST /predictions` or
`PUT /predictions/<prediction_id>`.

```js
const response = await fetch("/predictions", {
  method: "POST",
  headers: {
    "Content-Type": "application/json",
    Accept: "text/event-stream",
  },
  body: JSON.stringify({ input: { prompt: "Write a haiku about onions" } }),
});

const reader = response.body.pipeThrough(new TextDecoderStream()).getReader();

while (true) {
  const { value, done } = await reader.read();
  if (done) break;
  console.log(value);
}
```

Use `PUT /predictions/<prediction_id>` when the client needs safe retries
or wants to reconnect to an in-flight prediction by ID:

```http
PUT /predictions/wjx3whax6rf4vphkegkhcvpv6a HTTP/1.1
Content-Type: application/json; charset=utf-8
Accept: text/event-stream

{
    "input": {"prompt": "Write a haiku about onions"}
}
```

If the prediction is still running,
the server returns a stream for the existing prediction
instead of creating a duplicate prediction.
If earlier events have been dropped from the replay buffer,
the stream emits an `error` event and closes.
The replay buffer keeps the most recent 1024 events by default.
Set `COG_STREAM_HISTORY_CAPACITY` to change this limit,
or set it to `0` to disable replay history while keeping live streaming enabled.
Training endpoints do not support SSE streaming;
requests to `/trainings` with `Accept: text/event-stream`
return `406 Not Acceptable`.

## Webhooks

You can provide a `webhook` parameter in the client request body
when creating a prediction.

```http
POST /predictions HTTP/1.1
Content-Type: application/json; charset=utf-8
Prefer: respond-async

{
    "input": {"prompt": "A picture of an onion with sunglasses"},
    "webhook": "https://example.com/webhook/prediction"
}
```

The server makes requests to the provided URL
with the current state of the prediction object in the request body
at the following times.

- `start`:
  Once, when the prediction starts
  (`status` is `starting`).
- `output`:
  Each time a run function generates an output
  (either once using `return` or multiple times using `yield`)
- `logs`:
  Each time the run function writes to `stdout`
- `completed`:
  Once, when the prediction reaches a terminal state
  (`status` is `succeeded`, `canceled`, or `failed`)

Webhook requests for `start` and `completed` event types
are sent immediately.
Webhook requests for `output` and `logs` event types
are sent at most once every 500ms.
This interval is not configurable.

By default, the server sends requests for all event types.
Clients can specify which events trigger webhook requests
with the `webhook_events_filter` parameter in the prediction request body.
For example,
the following request specifies that webhooks are sent by the server
only at the start and end of the prediction:

```http
POST /predictions HTTP/1.1
Content-Type: application/json; charset=utf-8
Prefer: respond-async

{
    "input": {"prompt": "A picture of an onion with sunglasses"},
    "webhook": "https://example.com/webhook/prediction",
    "webhook_events_filter": ["start", "completed"]
}
```

## Generating unique prediction IDs

Endpoints for creating and canceling a prediction idempotently
accept a `prediction_id` parameter in their path.
By default, the server runs one prediction at a time,
but this can be increased with [`@cog.concurrent(max=N)`](python.md#async-runners-and-concurrency).
When all prediction slots are in use, the server returns `409 Conflict`.
The client should ensure prediction slots are available
before creating a new prediction with a different ID.

Clients are responsible for providing unique prediction IDs.
We recommend generating a UUIDv4 or [UUIDv7](https://uuid7.com),
base32-encoding that value,
and removing padding characters (`==`).
This produces a random identifier that is 26 ASCII characters long.

```python
>> from uuid import uuid4
>> from base64 import b32encode
>> b32encode(uuid4().bytes).decode('utf-8').lower().rstrip('=')
'wjx3whax6rf4vphkegkhcvpv6a'
```

## File uploads

A model's `run` function can produce file output by yielding or returning
a `cog.Path` or `cog.File` value.

By default,
files are returned as a base64-encoded
[data URL](https://developer.mozilla.org/en-US/docs/Web/HTTP/Basics_of_HTTP/Data_URLs).

```http
POST /predictions HTTP/1.1
Content-Type: application/json; charset=utf-8

{
    "input": {"prompt": "A picture of an onion with sunglasses"},
}
```

```http
HTTP/1.1 200 OK
Content-Type: application/json

{
    "status": "succeeded",
    "output": "data:image/png;base64,..."
}
```

To upload output files to external storage instead,
start the HTTP server with the `--upload-url` flag set to a base URL.
This applies to both synchronous and asynchronous predictions.

```console
python -m cog.server.http --upload-url https://example.com/upload/
```

When the model produces a file output,
the server uploads it with an HTTP `PUT` request to
`{upload-url}/{filename}`, sending the raw file bytes
with the file's `Content-Type`:

```http
PUT /upload/image.png HTTP/1.1
Host: example.com
Content-Type: image/png

<binary data>
```

The resulting URL is taken from the response's `Location` header
(falling back to the request URL), with query parameters stripped.
If the upload succeeds, the prediction output is the uploaded URL
instead of a data URL:

```http
HTTP/1.1 200 OK
Content-Type: application/json

{
    "status": "succeeded",
    "output": "https://example.com/upload/image.png"
}
```

If the upload fails, the server responds with an error.

<a id="api"></a>

## Endpoints

### `GET /`

Returns a discovery document listing available API endpoints, the OpenAPI schema URL, and version information.

```http
GET / HTTP/1.1
```

```http
HTTP/1.1 200 OK
Content-Type: application/json

{
    "cog_version": "0.17.0",
    "docs_url": "/docs",
    "openapi_url": "/openapi.json",
    "shutdown_url": "/shutdown",
    "healthcheck_url": "/health-check",
    "predictions_url": "/predictions",
    "predictions_idempotent_url": "/predictions/{prediction_id}",
    "predictions_cancel_url": "/predictions/{prediction_id}/cancel"
}
```

If training is configured, the response also includes
`trainings_url`, `trainings_idempotent_url`, and `trainings_cancel_url` fields.

### `GET /health-check`

Returns the current health status of the model container.
This endpoint always responds with `200 OK` —
check the `status` field in the response body to determine readiness.

The response body is a JSON object with the following fields:

- `status`: One of the following values:
  - `STARTING`: The model's `setup()` method is still running.
  - `READY`: The model is ready to accept predictions.
  - `BUSY`: The model is ready but all prediction slots are in use.
  - `SETUP_FAILED`: The model's `setup()` method raised an exception.
  - `DEFUNCT`: The model encountered an unrecoverable error.
  - `UNHEALTHY`: The model is ready
    but a user-defined `healthcheck()` method returned `False`.
- `setup`: Setup phase details (included once setup has started):
  - `started_at`: ISO 8601 timestamp of when setup began.
  - `completed_at`: ISO 8601 timestamp of when setup finished (if complete).
  - `status`: One of `starting`, `succeeded`, or `failed`.
  - `logs`: Output captured during setup.
- `version`: Runtime version information:
  - `coglet`: Coglet version.
  - `cog`: Cog Python SDK version (if available).
  - `python`: Python version (if available).
- `user_healthcheck_error`:
  Error message from a user-defined `healthcheck()` method (if applicable).

```http
GET /health-check HTTP/1.1
```

```http
HTTP/1.1 200 OK
Content-Type: application/json

{
    "status": "READY",
    "setup": {
        "started_at": "2025-01-01T00:00:00.000000+00:00",
        "completed_at": "2025-01-01T00:00:05.000000+00:00",
        "status": "succeeded",
        "logs": ""
    },
    "version": {
        "coglet": "0.17.0",
        "cog": "0.14.0",
        "python": "3.13.0"
    }
}
```

### `GET /openapi.json`

The [OpenAPI](https://swagger.io/specification/) specification of the API,
which is derived from the input and output types specified in your model's
[Predictor](python.md) and [Training](training.md) objects.

### `POST /predictions`

Makes a single prediction.

The request body is a JSON object with the following fields:

- `input`:
  A JSON object with the same keys as the
  [arguments to the `run()` function](python.md).
  Any `File` or `Path` inputs are passed as URLs.

The response body is a JSON object with the following fields:

- `status`: Either `succeeded` or `failed`.
- `output`: The return value of the `run()` function.
- `error`: If `status` is `failed`, the error message.
- `metrics`: An object containing prediction metrics.
  Always includes `predict_time` (elapsed seconds).
  May also include custom metrics recorded by the model
  using [`self.record_metric()`](python.md#metrics).

```http
POST /predictions HTTP/1.1
Content-Type: application/json; charset=utf-8

{
    "input": {
        "image": "https://example.com/image.jpg",
        "text": "Hello world!"
    }
}
```

```http
HTTP/1.1 200 OK
Content-Type: application/json

{
    "status": "succeeded",
    "output": "data:image/png;base64,...",
    "metrics": {
        "predict_time": 4.52
    }
}
```

If the client sets the `Prefer: respond-async` header in their request,
the server responds immediately after starting the prediction
with `202 Accepted` status and a prediction object in status `processing`.

```http
POST /predictions HTTP/1.1
Content-Type: application/json; charset=utf-8
Prefer: respond-async

{
    "input": {"prompt": "A picture of an onion with sunglasses"}
}
```

```http
HTTP/1.1 202 Accepted
Content-Type: application/json

{
    "status": "starting",
}
```

If the client sets the `Accept: text/event-stream` header,
the server starts the prediction asynchronously and responds with a
server-sent event stream.
See [Streaming predictions with server-sent events](#streaming-predictions-with-server-sent-events).

### `PUT /predictions/<prediction_id>`

Make a single prediction.
This is the idempotent version of the `POST /predictions` endpoint.

```http
PUT /predictions/wjx3whax6rf4vphkegkhcvpv6a HTTP/1.1
Content-Type: application/json; charset=utf-8

{
    "input": {"prompt": "A picture of an onion with sunglasses"}
}
```

```http
HTTP/1.1 200 OK
Content-Type: application/json

{
    "status": "succeeded",
    "output": "data:image/png;base64,..."
}
```

If the client sets the `Prefer: respond-async` header in their request,
the server responds immediately after starting the prediction
with `202 Accepted` status and a prediction object in status `processing`.

```http
PUT /predictions/wjx3whax6rf4vphkegkhcvpv6a HTTP/1.1
Content-Type: application/json; charset=utf-8
Prefer: respond-async

{
    "input": {"prompt": "A picture of an onion with sunglasses"}
}
```

```http
HTTP/1.1 202 Accepted
Content-Type: application/json

{
    "id": "wjx3whax6rf4vphkegkhcvpv6a",
    "status": "starting"
}
```

If the client sets the `Accept: text/event-stream` header,
the server starts the prediction asynchronously and responds with a
server-sent event stream.
If a prediction with the same ID is already running,
the server returns a stream for the existing prediction.
See [Streaming predictions with server-sent events](#streaming-predictions-with-server-sent-events).

### `POST /predictions/<prediction_id>/cancel`

A client can cancel an asynchronous prediction by making a
`POST /predictions/<prediction_id>/cancel` request
using the prediction `id` provided when the prediction was created.

For example,
if the client creates a prediction by sending the request:

```http
POST /predictions HTTP/1.1
Content-Type: application/json; charset=utf-8
Prefer: respond-async

{
    "id": "abcd1234",
    "input": {"prompt": "A picture of an onion with sunglasses"},
}
```

The client can cancel the prediction by sending the request:

```http
POST /predictions/abcd1234/cancel HTTP/1.1
```

A prediction cannot be canceled if it's
created synchronously, without the `Prefer: respond-async` header,
or created without a provided `id`.

If a prediction exists with the provided `id`,
the server responds with status `200 OK`.
Otherwise, the server responds with status `404 Not Found`.

When a prediction is canceled,
Cog raises [`CancelationException`](python.md#cancelationexception)
in sync predictors (or `asyncio.CancelledError` in async predictors).
This exception may be caught by the model to perform necessary cleanup.
The cleanup should be brief, ideally completing within a few seconds.
After cleanup, the exception must be re-raised using a bare `raise` statement.
Failure to re-raise the exception may result in the termination of the container.

```python
from cog import BaseRunner, CancelationException, Input, Path

class Runner(BaseRunner):
    def run(self, image: Path = Input(description="Image to process")) -> Path:
        try:
            return self.process(image)
        except CancelationException:
            self.cleanup()
            raise  # always re-raise
```
