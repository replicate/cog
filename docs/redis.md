# Redis queue API

Long-running deep learning models or batch processing is best architected as a queue. Cog has a built-in queue worker that can process predictions from a Redis queue, and return the output back to the queue.

The request queue is implemented with [Redis streams](https://redis.io/topics/streams-intro).

## Start up the model

The entrypoint to run a Cog model against a queue is `cog.server.redis_queue`. You need to provide the following positional arguments:

- `redis_host`: the host your redis server is running on.
- `redis_port`: the port your redis server is listening on.
- `input_queue`: the queue the Cog model will listen to for prediction requests. This queue should already exist.
- `upload_url`: the endpoint Cog will `PUT` output files to. <!-- link to file handling documentation -->
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
- `response_queue`: the Redis queue Cog will send responses to

You can enqueue the request using the `XADD` command:

    redis:6379> XADD my-predict-queue * value {"input":{"tolerance":0.05},"response_queue":"my-response-queue"}

## Get a prediction response

The model will send a message to the queue every time something happens:

- when the model generates some logs
- when the model returns some output
- when the model finishes running

The message body is a JSON object with the following fields:

- `status`: `processing`, `succeeded` or `failed`.
- `output`: The return value of the `predict()` function.
- `logs`: A list of any logs sent to stdout or stderr during the prediction.
- `error`: If `status` is `failed`, the error message.

For example, a message early in the prediction might look like:

    {
        "status": "processing",
        "output": null,
        "logs": [
            "Creating model and diffusion.",
            "Done creating model and diffusion."
        ]
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
        ]
    }

Because each message is a complete snapshot of the current state, you can use the `LRANGE` command to get the latest state from the end of the queue:

    redis:6379> LRANGE my-response-queue -1 -1

Alternatively, to get the latest state whilst clearing the queue you can use the `RPOP` command with a count:

    redis:6379> RPOP my-response-queue 1000
