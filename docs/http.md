# HTTP API

When a Cog Docker image is run, it serves an HTTP API for making predictions. For more information, take a look at [the documentation for deploying models](deploy.md).

## `GET /openapi.json`

The [OpenAPI](https://swagger.io/specification/) specification of the API, which is derived from the input and output types specified in your model's [Predictor](python.md) object.

## `POST /predictions`

Make a single prediction. The request body should be a JSON object with the following fields:

- `input`: a JSON object with the same keys as the [arguments to the `predict()` function](python.md). Any `File` or `Path` inputs are passed as URLs.
- `output_file_prefix`: A base URL to upload output files to. <!-- link to file handling documentation -->

The response is a JSON object with the following fields:

- `status`: Either `success` or `failed`.
- `output`: The return value of the `predict()` function.
- `error`: If `status` is `failed`, the error message.

For example:

    POST /predictions
    {
        "input": {
            "image": "https://example.com/image.jpg",
            "text": "Hello world!"
        }
    }

Responds with:

    {
        "status": "success",
        "output": "data:image/png;base64,..."
    }

Or, with curl:

    curl -X POST -H "Content-Type: application/json" -d '{"input": {"image": "https://example.com/image.jpg", "text": "Hello world!"}}' http://localhost:5000/predictions
