# hello-concurrency

This is an example Cog project that demonstrates concurrency support within Cog.

The key piece is the `@concurrent(max=4)` decorator on the async `run()` method.

```py
from cog import BaseRunner, concurrent

class Runner(BaseRunner):
    @concurrent(max=4)
    async def run(self) -> str:
        return "hello"
```

This combined with the async setup and run methods in `run.py` allows Cog to run up to
4 concurrent predictions. If Cog reaches the max concurrency threshold it will reject subsequent
predictions with a `409 Conflict` response.

### Tracing and metrics

Cog loads `telemetry.py` before importing the model. Its provider factories configure model spans and metrics, while `configure_runtime_metrics()` selects fixed Cog runtime instruments. The model uses the standard `opentelemetry.trace` and `opentelemetry.metrics` APIs.

Pass the collector configuration at runtime:

```shell
cog run \
  -e OTEL_EXPORTER_OTLP_ENDPOINT=https://collector.example.com \
  -e OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf \
  -e OTEL_SERVICE_NAME=hello-concurrency \
  -i total=5 \
  -i interval=1
```

The `parentbased_always_on` sampler preserves an upstream trace's sampling decision and samples predictions that start a new trace locally. `model.output_tokens` is a model-owned OpenTelemetry counter; `current_scope().record_metric()` continues to populate the prediction response separately.
