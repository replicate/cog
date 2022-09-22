# Redis queue API

> **Note:** The queue API is experimental and subject to change, but it's ready to use if you like living on the edge!

Long-running deep learning models or batch processing is best architected as a queue. Cog has a built-in queue worker that can process predictions from a Redis queue, and return the output back to the queue.

The request queue is implemented with [Redis streams](https://redis.io/topics/streams-intro).

See [github.com/replicate/cog-redis-example](https://github.com/replicate/cog-redis-example), which contains a Docker Compose setup for running a Cog model with the built-in Redis worker.

## Start up the model

The entrypoint to run a Cog model against a queue is `cog.server.redis_queue`. You need to provide the following positional arguments:

- `redis_host`: the host your redis server is running on.
- `redis_port`: the port your redis server is listening on.
- `input_queue`: the queue the Cog model will listen to for prediction requests. This queue should already exist.
- `upload_url`: the endpoint Cog will `PUT` output files to. (Note: this will change in the near future. [See this pull request for more details.](https://github.com/replicate/cog/issues/496))
- `consumer_id`: The name the Cog model will use to identify itself in the Redis group (also called "consumer name" by Redis).
- `model_id`: a unique ID for the Cog model, used to label setup logs.
- `log_queue`: the queue the Cog model should send setup logs to (prediction logs are sent as part of prediction responses).

Note: logging is changing as part of 0.3.0, so the `model_id` and `log_queue` arguments are likely to change soon.

You can optionally provide the following positional argument:

- `predict_timeout`: the maximum time in seconds a prediction will be allowed to run for before it is terminated.

For example:

    docker run python -m cog.server.redis_queue \
        redis 6379 my-predict-queue \
        https://example.com/ab48b7ff-1589-4360-a54b-47f9d8d3f6b7/ \
        worker-1 \
        widget-classifier logs-queue \
        120

After starting, [the `setup()` method of the predictor](python.md#predictorsetup) is called. When setup is finished the model will start polling the input queue for prediction request messages.

## Enqueue a prediction

The message body should be a JSON object with the following fields:

- `input`: a JSON object with the same keys as the [arguments to the `predict()` function](python.md). Any `File` or `Path` inputs are passed as URLs.
- `webhook`: the URL Cog will send responses to.

There's also one deprecated field:

- `response_queue`: the Redis key to send responses to; ignored if `webhook` is set.

You can enqueue the request using the `XADD` command:

    redis:6379> XADD my-predict-queue * value {"input":{"tolerance":0.05},"response_queue":"my-response-queue"}

## Get a prediction response

The model will send a POST request to the webhook endpoint every time something happens:

- when the prediction starts running
- when the prediction generates some logs
- when the prediction returns some output
- when the prediction finishes running

The message body is a JSON object with the following fields:

- `status`: `processing`, `succeeded` or `failed`.
- `output`: The return value of the `predict()` function.
- `logs`: A list of any logs sent to stdout or stderr during the prediction.
- `error`: If `status` is `failed`, the error message.
- `started_at`: An ISO8601/RFC3339 timestamp of when the prediction started.
- `completed_at`: An ISO8601/RFC3339 timestamp of when the prediction finished.
- `metrics.predict_time`: If succeeded, the time in seconds the prediction took to finish.

For example, a message early in the prediction might look like:

    {
        "status": "processing",
        "output": null,
        "logs": [
            "Creating model and diffusion.",
            "Done creating model and diffusion."
        ],
        "started_at": "2022-09-22T14:31:17Z"
    }

If the model yields [progressive output](python.md#progressive-output) then a mid-prediction message might look like:

    {
        "status": "processing",
        "output": [
            "https://example.com/ab48b7ff-1589-4360-a54b-47f9d8d3f6b7/0.jpg",
            "https://example.com/ab48b7ff-1589-4360-a54b-47f9d8d3f6b7/20.jpg",
            "https://example.com/ab48b7ff-1589-4360-a54b-47f9d8d3f6b7/40.jpg",
        ],
        "logs": [
            "Creating model and diffusion.",
            "Done creating model and diffusion.",
            "Iteration: 0, loss: -0.767578125",
            "Iteration: 20, loss: -1.2333984375",
            "Iteration: 40, loss: -1.380859375"
        ],
        "started_at": "2022-09-22T14:31:17Z"
    }

### Redis responses

Note: this section documents a deprecated feature, which will be removed in a future version of Cog.

If you set `response_queue`, then the response is written to a string key using `SET`. This is instead of sending webhooks, as documented above. Because each message is a complete snapshot of the current state, the previous snapshots are not needed. You can read the values using the `GET` command:

    redis:6379> GET my-response-queue

To get notified of updates to the value, you can `SUBSCRIBE` to [keyspace notifications] for the key:

    redis:6379> SUBSCRIBE __keyspace@0__:my-response-queue

[keyspace notifications]: https://redis.io/docs/manual/keyspace-notifications/

## Telemetry

Cog's queue worker is instrumented using [OpenTelemetry](https://opentelemetry.io). For setup it sends:

- a span when the queue worker starts
- an event when it spawns the predictor subprocess
- a span when the predictor subprocess starts
- a span wrapping your `setup()` method

For each prediction it sends:

- a span when the request is received
- a span wrapping your `predict()` method
- an event when the first output is received from your `predict()` method
- for progressive output, an event when the final output is received from your `predict()` method

If the runner encounters an error during the prediction it will record it and set the span's status to error.

### Configuration

Telemetry is enabled when the `OTEL_SERVICE_NAME` environment variable is set. The OTLP exporter also needs to be [configured via environment variables][1].

[1]: https://opentelemetry-python.readthedocs.io/en/latest/sdk/environment_variables.html

If a `traceparent` parameter is provided with the prediction request, Cog will use that value as the parent for the prediction spans. This allows spans from Cog to show up in distributed traces. The parameter should be in the W3C format, eg:

    00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01
