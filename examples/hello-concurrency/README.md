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

### Telemetry

It also uses the open-telemetry package to demonstrate how to collect telemetry for your model.

This requires a file named `honeycomb_token.key` to be included in the image build.

It will then start sending events to the `cog-model` data source. You can configure this by
editing the `OTEL_SERVICE_NAME`. If you use a custom endpoint this can be configured via `OTEL_EXPORTER_OTLP_ENDPOINT`.

Lastly, there is a section in `run.py` that can be uncommented to run telemetry locally and print events to the console for debugging.
