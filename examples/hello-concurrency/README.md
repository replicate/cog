# hello-concurrency

This is an example Cog project that demonstrates the newly added concurrency support within
cog >= 0.14.0.

The key piece is the new `concurrency` field in the cog.yaml.

```yaml
concurrency:
  max: 32
```

This combined with the async setup and predict methods in the predict.py allows Cog to run up to
32 concurrent predictions. If cog reaches the max concurrency threshold it will reject subsequent
predictions with a `409 Conflict` response.

### Telemetry

It also uses the open-telemetry package to demonstrate how to collect telemetry for your model.

This requires a file named `honeycomb_token.key` to be included in the image build.

It will then start sending events to the `cog-model` data source. You can configure this by
editing the `OTEL_SERVICE_NAME`. If you use a custom endpoint this can be configured via `OTEL_EXPORTER_OTLP_ENDPOINT`.

Lastly, there is a section in predict.py that can be uncommented to run telemetry locally and print events to the console for debugging.
