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

where `model-directory.zip` is a zip folder of a model directory in the exact same format as https://github.com/replicate/modelserver-example (this is a temporary constraint).

This does the following:
* Builds a Docker image and pushes it to Artifact store with Cloud Build
* Uploads the zip directory to GCS
* Deploys a prediction endpoint to AI Platform

Logs are available here: https://console.cloud.google.com/run/detail/us-central1/modelserver/logs?project=replicate
