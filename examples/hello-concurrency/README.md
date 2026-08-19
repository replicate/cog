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

### Tracing with Honeycomb

Cog loads `telemetry.py` before importing the model. Its `create_tracer_provider()` function configures resource attributes, sampling, span limits, and exporters for Python spans. The model adds spans with the standard `opentelemetry.trace` API.

Set a Honeycomb API key in your shell, then pass the OTLP configuration at runtime:

```shell
export HONEYCOMB_API_KEY=your-api-key

cog run \
  -e OTEL_EXPORTER_OTLP_ENDPOINT=https://api.honeycomb.io \
  -e OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf \
  -e OTEL_EXPORTER_OTLP_HEADERS="x-honeycomb-team=${HONEYCOMB_API_KEY}" \
  -e OTEL_SERVICE_NAME=hello-concurrency \
  -i total=5 \
  -i interval=1
```

The `parentbased_always_on` sampler preserves an upstream trace's sampling decision and samples predictions that start a new trace locally.

To print Python spans locally without an OTLP endpoint, run:

```shell
cog run -e OTEL_DEBUG_TRACES=true -i total=5 -i interval=1
```

See [Honeycomb's OpenTelemetry endpoint documentation](https://docs.honeycomb.io/send-data/opentelemetry/#using-the-honeycomb-opentelemetry-endpoint) for regional endpoints and Honeycomb Classic dataset headers.
