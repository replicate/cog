# Model server

Work-in-progress service for model storage and serving.

## Run server

```
go run ./cmd/modelserver/main.go server --port=8080 --docker-registry=us-central1-docker.pkg.dev/replicate/andreas-scratch
```

The modelserver requires Go 1.16.

## Usage

```
curl -X POST localhost:8080/upload -F "file=@model-directory.zip"
```

where `model-directory.zip` is a zip folder of a model directory with `jid.yaml` in it. [There are some example repository.](https://github.com/replicate/example-models)

This does the following:
* Computes a content-addressable ID
* Validates and completes config (e.g. sets correct CUDA version for PyTorch)
* Saves the model to storage (local files)
* Builds and pushes Docker images to registry
* Tests that the model works by running the Docker image locally and performing an inference
* Inserts model metadata into database (local files)
