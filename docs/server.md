# Cog server

The Cog server stores models and builds Docker images. It can be started by running `cog server`. See `cog server --help` for more information.

## API documentation

The client and server communicated using an HTTP API.

### PUT `/v1/models/<user>/<name>/versions/`

Upload a new model.

Example:

```
$ curl -X PUT localhost:8080/v1/models/andreas/my-model/versions/ -F "file=@version.zip"
```

where `version.zip` is a zip folder of a directory with `cog.yaml` in it.

This does the following:

- Computes a content-addressable ID
- Validates and completes config (e.g. sets correct CUDA version for PyTorch)
- Saves the model to storage (local files)
- Builds and pushes Docker images to registry
- Tests that the model works by running the Docker image locally and performing a prediction
- Inserts model metadata into database (local files)

### GET `/v1/models/<user>/<name>/versions/<id>`

Fetch version metadata.

Example:

```
$ curl localhost:8080/v1/models/andreas/my-model/versions/c43b98b37776656e6b3dac3ea3270660ffc21ca7 | jq .
{
  "id": "c43b98b37776656e6b3dac3ea3270660ffc21ca7",
  "config": {
    "environment": {
      "python_version": "3.8",
      "python_requirements": "",
      "python_packages": null,
      "system_packages": null,
      "architectures": [
        "cpu"
      ],
    },
    "model": "predict.py:Model"
  },
  "images": [
    {
      "uri": "gcr.io/bfirsh-dev/hello-world:cb39be9f6323",
      "arch": "cpu"
    }
  ]
}
```

### GET `/v1/models/<user>/<name>/versions/`

List metadata for all versions.

Example:

```
$ curl localhost:8080/v1/models/andreas/my-model/versions/ | jq .
[
  {
    "id": "c43b98b37776656e6b3dac3ea3270660ffc21ca7",
  [...]
]
```

### GET `/v1/models/<user>/<name>/versions/<id>.zip`

Download a version.

Example:

```
$ curl localhost:8080/v1/models/andreas/my-model/versions/c43b98b37776656e6b3dac3ea3270660ffc21ca7.zip > version.zip
$ unzip version.zip
Archive:  version.zip
  inflating: cog.yaml
  inflating: predict.py
```
