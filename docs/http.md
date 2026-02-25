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
by setting the `Prefer: respond-async` header in their request.
When provided, the server responds immediately after starting the prediction 
with `202 Accepted` status and a prediction object in status `processing`.

> [!NOTE]
> The only supported way to receive updates on the status of predictions
> started asynchronously is using [webhooks](#webhooks). 
> Polling for prediction status is not currently supported.

You can also use certain server endpoints to create predictions idempotently,
such that if a client calls this endpoint more than once with the same ID 
(for example, due to a network interruption) 
while the prediction is still running, 
no new prediction is created. 
Instead, the client receives a `202 Accepted` response
with the initial state of the prediction.

---

Here's a summary of the prediction creation endpoints:

| Endpoint                           | Header                  | Behavior                     |
| ---------------------------------- | ----------------------- | ---------------------------- |
| `POST /predictions`                | -                       | Synchronous, non-idempotent  |
| `POST /predictions`                | `Prefer: respond-async` | Asynchronous, non-idempotent |
| `PUT /predictions/<prediction_id>` | -                       | Synchronous, idempotent      |
| `PUT /predictions/<prediction_id>` | `Prefer: respond-async` | Asynchronous, idempotent     |

Choose the endpoint that best fits your needs:

- Use synchronous endpoints when you want to wait for the prediction result.
- Use asynchronous endpoints when you want to start a prediction 
  and receive updates via webhooks.
- Use idempotent endpoints when you need to safely retry requests 
  without creating duplicate predictions.

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
  Each time a predict function generates an output 
  (either once using `return` or multiple times using `yield`)
- `logs`: 
  Each time the predict function writes to `stdout`
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
The server can run only one prediction at a time.
The client must ensure that the running prediction is complete
before creating a new one with a different ID.

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

A model's `predict` function can produce file output by yielding or returning
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

When creating a prediction synchronously,
the client can configure a base URL to upload output files to instead
by setting the `output_file_prefix` parameter in the request body:

```http
POST /predictions HTTP/1.1
Content-Type: application/json; charset=utf-8

{
    "input": {"prompt": "A picture of an onion with sunglasses"},
    "output_file_prefix": "https://example.com/upload",
}
```

When the model produces a file output,
the server sends the following request to upload the file to the configured URL:

```http
PUT /upload HTTP/1.1
Host: example.com
Content-Type: multipart/form-data

--boundary
Content-Disposition: form-data; name="file"; filename="image.png"
Content-Type: image/png

<binary data>
--boundary--
```

If the upload succeeds, the server responds with output:

```http
HTTP/1.1 200 OK
Content-Type: application/json

{
    "status": "succeeded",
    "output": "http://example.com/upload/image.png"
}
```

If the upload fails, the server responds with an error.

> [!IMPORTANT]  
> File uploads for predictions created asynchronously 
> require `--upload-url` to be specified when starting the HTTP server.

<a id="api"></a>

## Endpoints

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
        "python": "3.12.0"
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
  [arguments to the `predict()` function](python.md).
  Any `File` or `Path` inputs are passed as URLs.

The response body is a JSON object with the following fields:

- `status`: Either `succeeded` or `failed`.
- `output`: The return value of the `predict()` function.
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
from cog import CancelationException, Path

def predict(image: Path) -> Path:
    try:
        return process(image)
    except CancelationException:
        cleanup()
        raise  # always re-raise
```
