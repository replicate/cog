# Model server

Work-in-progress service for model storage and serving.

## Deploy

```
make deploy
```

This deploys the service to Cloud Run.

## Usage

```
curl -X POST https://modelserver-idzklgqdsa-uc.a.run.app/upload -F "file=@model-directory.zip"
```

where `model-directory.zip` is a zip folder of a model directory with `yid.yaml` in it. [There are some example repository.](https://github.com/replicate/example-models)

This does the following:
* Builds a Docker image and pushes it to Artifact store with Cloud Build
* Uploads the zip directory to GCS
* Deploys a prediction endpoint to AI Platform
* Inserts a record into a Cloud SQL postgres database

Logs are available here: https://console.cloud.google.com/run/detail/us-central1/modelserver/logs?project=replicate
