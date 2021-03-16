# Model server

Work-in-progress service for model storage and serving.

## Run server

```
go run ./cmd/modelserver/main.go server --port=8080 --docker-registry=us-central1-docker.pkg.dev/replicate/andreas-scratch
```

The modelserver requires Go 1.16.

## API

### POST `/v1/packages/upload`

Upload a new package.

Example:

```
$ curl -X POST localhost:8080/v1/packages/upload -F "file=@model-directory.zip"
```

where `model-directory.zip` is a zip folder of a model directory with `jid.yaml` in it. [There are some example repository.](https://github.com/replicate/example-models)

This does the following:
* Computes a content-addressable ID
* Validates and completes config (e.g. sets correct CUDA version for PyTorch)
* Saves the model to storage (local files)
* Builds and pushes Docker images to registry
* Tests that the model works by running the Docker image locally and performing an inference
* Inserts model metadata into database (local files)

### GET `/v1/packages/<id>`

Fetch package metadata.

Example:

```
$ curl localhost:8080/v1/packages/c43b98b37776656e6b3dac3ea3270660ffc21ca7 | jq .
{
  "ID": "c43b98b37776656e6b3dac3ea3270660ffc21ca7",
  "Name": "andreas/scratch",
  "Artifacts": [
    {
      "Target": "docker-cpu",
      "URI": "us-central1-docker.pkg.dev/replicate/andreas-scratch/andreas/scratch:2c7492b7d3d6"
    }
  ],
  "Config": {
    "Name": "andreas/scratch",
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

### GET `/v1/packages/<id>.zip`

Download the model package zip.

Example:

```
$ curl localhost:8080/v1/packages/c43b98b37776656e6b3dac3ea3270660ffc21ca7.zip > my-package.zip
$ unzip my-package.zip
Archive:  my-package.zip
  inflating: infer.py
  inflating: jid.yaml
```

### GET `/v1/packages/`

List all packages.

Example:

```
$ curl localhost:8080/v1/packages/ | jq .
[
  {
    "ID": "af3ff5288247833f5f9d8d9f6ecd5fe2b586f6aa",
    "Name": "andreas/fastgan",
    "Artifacts": [
      {
        "Target": "docker-cpu",
        "URI": "us-central1-docker.pkg.dev/replicate/andreas-scratch/andreas/fastgan:a034b8a9bf46"
      },
[...]
```
