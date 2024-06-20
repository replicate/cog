# HTTP API

When a Cog Docker image is run, it serves an HTTP API for making predictions. For more information, take a look at [the documentation for deploying models](deploy.md).

## Contents

- [Contents](#contents)
- [Running the server](#running-the-server)
- [Stopping the server](#stopping-the-server)
- [API](#api)
  - [`GET /openapi.json`](#get-openapijson)
  - [`POST /predictions` (synchronous)](#post-predictions-synchronous)
  - [`POST /predictions` (asynchronous)](#post-predictions-asynchronous)
    - [Webhooks](#webhooks)
  - [`PUT /predictions/<prediction_id>` (synchronous)](#put-predictionsprediction_id-synchronous)
  - [`PUT /predictions/<prediction_id>` (asynchronous)](#put-predictionsprediction_id-asynchronous)
  - [`POST /predictions/<prediction_id>/cancel`](#post-predictionsprediction_idcancel)

## Running the server

First, build your model:

```
cog build -t my-model
```

Then, start the Docker container:

```console
# If your model uses a CPU:
docker run -d -p 5001:5000 my-model

# If your model uses a GPU:
docker run -d -p 5001:5000 --gpus all my-model

# If you're on an M1 Mac:
docker run -d -p 5001:5000 --platform=linux/amd64 my-model
```

The server is now running locally on port 5001.

To view the OpenAPI schema, open [localhost:5001/openapi.json](http://localhost:5001/openapi.json) in your browser or use cURL to make requests:

```console
curl http://localhost:5001/openapi.json
```

## Stopping the server

To stop the server, run:

```console
docker kill my-model
```

## API

### `GET /openapi.json`

The [OpenAPI](https://swagger.io/specification/) specification of the API, which is derived from the input and output types specified in your model's [Predictor](python.md) and [Training](training.md) objects.

### `POST /predictions` (synchronous)

Make a single prediction. The request body should be a JSON object with the following fields:

- `input`: a JSON object with the same keys as the [arguments to the `predict()` function](python.md). Any `File` or `Path` inputs are passed as URLs.
- `output_file_prefix`: A base URL to upload output files to. <!-- link to file handling documentation -->

The response is a JSON object with the following fields:

- `status`: Either `succeeded` or `failed`.
- `output`: The return value of the `predict()` function.
- `error`: If `status` is `failed`, the error message.

For example:

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

Responds with:

```
HTTP/1.1 200 OK
Content-Type: application/json

{
    "status": "succeeded",
    "output": "data:image/png;base64,..."
}
```

Or, with curl:

    curl -X POST -H "Content-Type: application/json" -d '{"input": {"image": "https://example.com/image.jpg", "text": "Hello world!"}}' http://localhost:5000/predictions

### `POST /predictions` (asynchronous)

Make a single prediction without waiting for the prediction to complete.

Callers can specify an HTTP header of `Prefer: respond-async` when calling the
`POST /predictions` endpoint. If provided, the request will return immediately
after starting the prediction with an HTTP `202 Accepted` status and a
prediction object in status `processing`.

```http
POST /predictions HTTP/1.1
Content-Type: application/json; charset=utf-8
Prefer: respond-async

{
    "input": {"prompt": "A picture of an onion with sunglasses"}
}
```

The only supported mechanism for receiving updates on the status of predictions
started asynchronously is via webhooks. There is as yet no support for polling
for prediction status.

**Note 1:** that while this allows clients to create predictions
"asynchronously," Cog can only run one prediction at a time, and it is currently
the caller's responsibility to make sure that earlier predictions are complete
before new ones are created.

**Note 2:** predictions created asynchronously use a different mechanism for
file upload than those created using the synchronous API. You must specify an
`--upload-url` when running the Cog server process. All uploads will be `PUT`
using the provided `--upload-url` as a prefix, in much the same way that
`output_file_prefix` worked. There is currently no single upload mechanism which
works the same way for both synchronous and asynchronous prediction creation.
This will be addressed in a future version of Cog.

#### Webhooks

Clients can (and should, if a prediction is created asynchronously) provide a
`webhook` parameter at the top level of the prediction request, e.g.

```http
POST /predictions HTTP/1.1
Content-Type: application/json; charset=utf-8
Prefer: respond-async

{
    "input": {"prompt": "A picture of an onion with sunglasses"},
    "webhook": "https://example.com/webhook/prediction"
}
```

Cog will make requests to the URL supplied with the state of the prediction
object in the request body. Requests are made when specific events occur during
the prediction, namely:

- `start`: immediately on prediction start
- `output`: each time a prediction generates an output (note that predictions can generate multiple outputs)
- `logs`: each time log output is generated by a prediction
- `completed`: when the prediction reaches a terminal state (succeeded/canceled/failed)

Requests for event types `output` and `logs` will be sent at most once every
500ms. This interval is currently not configurable. Requests for event types
`start` and `completed` will be sent immediately.

By default, Cog will send requests for all event types. Clients can change which
events trigger webhook requests by specifying `webhook_events_filter` in the
prediction request. For example, if you only wanted requests to be sent at the
start and end of the prediction, you would provide:

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

### `PUT /predictions/<prediction_id>` (synchronous)

Make a single prediction.

This is the idempotent version of the `POST /predictions` endpoint. If you call
it multiple times with the same ID (for example, because of a network
interruption) and the prediction is still running, the request will not create
further predictions but will wait for the original prediction to complete.

**Note:** It is currently the caller's responsibility to ensure that the
supplied prediction ID is unique. We recommend you use base32-encoded UUID4s
(stripped of any padding characters) to ensure forward compatibility: these will
be 26 ASCII characters long.

### `PUT /predictions/<prediction_id>` (asynchronous)

Make a single prediction without waiting for the prediction to complete.

Callers can specify an HTTP header of `Prefer: respond-async` when calling the
`PUT /predictions/<prediction_id>` endpoint. If provided, the request will
return immediately after starting the prediction with an HTTP `202 Accepted`
status and a prediction object in status `processing`.

This is the idempotent version of the asynchronous `POST /predictions` endpoint.
If you call it multiple times with the same ID (for example, because of a
network interruption) and the prediction is still running, the request will not
create further predictions. The caller will receive a 202 Accepted response
with the initial state of the prediction.

**Note 1:** It is currently the caller's responsibility to ensure that the
supplied prediction ID is unique. We recommend you use base32-encoded UUID4s
(stripped of any padding characters) to ensure forward compatibility: these will
be 26 ASCII characters long.

**Note 2:** As noted earlier, Cog can only run one prediction at a time, and it is
the caller's responsibility to make sure that earlier predictions are complete
before new ones (with new IDs) are created.

### `POST /predictions/<prediction_id>/cancel`

A client can cancel an asynchronous prediction by making a
`POST /predictions/<prediction_id>/cancel` request
using the prediction `id` provided when the prediction was created.

For example, if a prediction is created with:

```http
POST /predictions HTTP/1.1
Content-Type: application/json; charset=utf-8
Prefer: respond-async

{
    "id": "abcd1234",
    "input": {"prompt": "A picture of an onion with sunglasses"},
}
```

It can be canceled with:

```http
POST /predictions/abcd1234/cancel HTTP/1.1
```

Predictions cannot be canceled if they're
created without a provided `id`
or synchronously, without the `Prefer: respond-async` header.

If no prediction exists with the provided `id`,
then the server responds with status `404 Not Found`.
Otherwise, the server responds with `200 OK`.

When a prediction is canceled,
Cog raises `cog.server.exceptions.CancelationException`
in the model's `predict` function.
This exception may be caught by the model to perform necessary cleanup.
The cleanup should be brief, ideally completing within a few seconds.
After cleanup, the exception must be re-raised using a bare raise statement.
Failure to re-raise the exception may result in the termination of the container.

```python
from cog import Path
from cog.server.exceptions import CancelationException

def predict(image: Path) -> Path:
    try:
        return process(image)
    except CancelationException as e:
        cleanup() 
        raise e
```
