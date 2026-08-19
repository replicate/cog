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

### Tracing

Cog configures OpenTelemetry before importing the model. The example only creates model spans with the standard `opentelemetry.trace` API.

Configure the collector at runtime:

```shell
OTEL_EXPORTER_OTLP_ENDPOINT=https://collector.example.com:4318
OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf
OTEL_SERVICE_NAME=hello-concurrency
```

With the default `parentbased_always_off` sampler, Cog exports spans only when the caller supplies a sampled parent trace.
