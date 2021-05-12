# Cog server

The Cog server stores models and builds Docker images. It can be started by running `cog server`. See `cog server --help` for more information.

## API documentation

The client and server communicated using an HTTP API.

### PUT `/v1/models/<user>/<name>/versions/`

Upload a new model.

Example:

```
$ curl -X PUT localhost:8080/v1/models/andreas/my-model/versions/ -F "file=@package.zip"
```

where `package.zip` is a zip folder of a directory with `cog.yaml` in it.

This does the following:

- Computes a content-addressable ID
- Validates and completes config (e.g. sets correct CUDA version for PyTorch)
- Saves the model to storage (local files)
- Builds and pushes Docker images to registry
- Tests that the model works by running the Docker image locally and performing an inference
- Inserts model metadata into database (local files)

### GET `/v1/models/<user>/<name>/versions/<id>`

Fetch model metadata.

Example:

```
$ curl localhost:8080/v1/models/andreas/my-model/versions/c43b98b37776656e6b3dac3ea3270660ffc21ca7 | jq .
{
  "ID": "c43b98b37776656e6b3dac3ea3270660ffc21ca7",
  "Artifacts": [
    {
      "Target": "docker-cpu",
      "URI": "us-central1-docker.pkg.dev/replicate/andreas-scratch/andreas/scratch:2c7492b7d3d6"
    }
  ],
  "Config": {
    "Environment": {
      "PythonVersion": "3.8",
      "PythonRequirements": "",
      "PythonPackages": null,
      "SystemPackages": null,
      "Architectures": [
        "cpu"
      ],
      "CUDA": "",
      "CuDNN": ""
    },
    "Model": "infer.py:Model"
  }
}
```

### GET `/v1/models/<user>/<name>/versions/`

List metadata for all versions.

Example:

```
$ curl localhost:8080/v1/models/andreas/my-model/versions/ | jq .
[
  {
    "ID": "c43b98b37776656e6b3dac3ea3270660ffc21ca7",
    "Artifacts": [
      {
        "Target": "docker-cpu",
  [...]
]
```

### GET `/v1/models/<user>/<name>/versions/<id>.zip`

Download a model.

Example:

```
$ curl localhost:8080/v1/models/andreas/my-model/versions/c43b98b37776656e6b3dac3ea3270660ffc21ca7.zip > my-package.zip
$ unzip my-package.zip
Archive:  my-package.zip
  inflating: cog.yaml
  inflating: infer.py
```
