# Observability

Cog can join an incoming distributed trace, trace work across its parent and worker processes, and make the active context available to model-authored OpenTelemetry spans. Tracing is opt-in and uses OTLP, so it works with collectors and backends that support OpenTelemetry.

Metrics and OpenTelemetry log export are not part of this tracing release.

## Enable tracing

Enable tracing in `cog.yaml`:

```yaml
observability:
  traces:
    enabled: true
    sampler: parentbased_always_off
```

Configure the collector when running the image:

```shell
OTEL_EXPORTER_OTLP_ENDPOINT=https://collector.example.com:4317
OTEL_EXPORTER_OTLP_PROTOCOL=grpc
OTEL_SERVICE_NAME=cog
```

Cog supports OTLP gRPC and HTTP/protobuf. Collector endpoints, authentication headers, certificates, and other `OTEL_*` values are runtime configuration and cannot be set through the general `cog.yaml` `environment` list.

Framework tracing starts only when the image opts in and a collector endpoint is present. `COG_TRACE_ENABLED=false` or `OTEL_SDK_DISABLED=true` disables all tracing at runtime. `OTEL_TRACES_EXPORTER=none` disables framework tracing and Cog's built-in Python exporter but does not suppress an explicitly configured custom Python provider.

## Getting started with tracing

A model can have a plain `run()` method and still get tracing. Model code does not need to import OpenTelemetry or create spans:

```python
from cog import BaseRunner


class Runner(BaseRunner):
    def run(self, prompt: str) -> str:
        return expensive_model_call(prompt)
```

Cog automatically produces:

```text
POST /predictions
└── cog.prediction
    ├── cog.prediction.validate
    └── cog.prediction.execute
        └── cog.prediction.invoke
            └── cog.prediction.prepare_input
```

Add this to `cog.yaml` to enable tracing:

```yaml
observability:
  traces:
    enabled: true
```

For information about continuing upstream traces or starting standalone traces, see [Sampling](#sampling).

Custom model spans are optional. Add them only when the automatic `cog.prediction.invoke` duration needs to be split into model-specific phases.

## Automatic spans

Cog creates framework spans without requiring tracing code in the model:

```text
POST /predictions
└── cog.prediction
    ├── cog.prediction.validate
    └── cog.prediction.execute
        └── cog.prediction.invoke
            └── cog.prediction.prepare_input
```

`cog.prediction.invoke` covers input preparation and the complete `run()` or legacy `predict()` call. For generators and async generators, it remains open while Cog consumes the returned output.

Training uses operation-specific worker spans:

```text
POST /trainings
└── cog.train
    └── cog.train.execute
        └── cog.train.invoke
            └── cog.train.prepare_input
```

File outputs may add `cog.prediction.upload_output`. Setup uses a separate `cog.setup` and `cog.setup.predictor` trace when the sampler records root spans.

Models can add spans around any Python function, including every function call if needed. Cog does not enable function-level tracing automatically because it adds overhead and can produce very large traces. For routine use, add spans around meaningful internal operations; use a profiler when a complete function-level call stack is required.

## Model-authored spans

Cog installs the Python tracer provider before importing the model. Use the standard OpenTelemetry API:

```python
from opentelemetry import trace

tracer = trace.get_tracer(__name__)


class Runner(BaseRunner):
    def run(self, prompt: str) -> str:
        with tracer.start_as_current_span("model.preprocessing"):
            inputs = prepare(prompt)

        with tracer.start_as_current_span("model.inference"):
            return self.model(inputs)
```

These spans become children of `cog.prediction.invoke`, or `cog.train.invoke` during training. Cog owns the tracer providers for its parent process, worker process, and Python model spans. Do not replace the global provider in model code. Use `observability.config` when the Python provider needs custom configuration.

Asyncio tasks inherit the active Python context. Raw threads and child processes require explicit context propagation. A background task that outlives the prediction may produce an uncorrelated span.

## Custom Python tracing

Set `observability.config` to a project-relative Python file:

```yaml
observability:
  config: telemetry.py
  traces:
    enabled: true
```

Cog validates the file during configuration, copies it to a fixed path in the image, and loads it before importing the model. The file must define `create_tracer_provider()` and may define `configure_instrumentation()`:

```python
import os

from opentelemetry.exporter.otlp.proto.http.trace_exporter import OTLPSpanExporter
from opentelemetry.sdk.resources import Resource
from opentelemetry.sdk.trace import SpanLimits, TracerProvider
from opentelemetry.sdk.trace.export import BatchSpanProcessor
from opentelemetry.sdk.trace.sampling import ParentBased, TraceIdRatioBased


def create_tracer_provider() -> TracerProvider:
    provider = TracerProvider(
        resource=Resource.create(
            {
                "service.name": os.getenv("OTEL_SERVICE_NAME", "my-model"),
                "model.name": "acme/example",
            }
        ),
        sampler=ParentBased(TraceIdRatioBased(0.1)),
        span_limits=SpanLimits(max_span_attributes=64),
        shutdown_on_exit=False,
    )
    provider.add_span_processor(BatchSpanProcessor(OTLPSpanExporter()))
    return provider


def configure_instrumentation() -> None:
    from opentelemetry.instrumentation.requests import RequestsInstrumentor

    RequestsInstrumentor().instrument()
```

Add instrumentation packages such as `opentelemetry-instrumentation-requests` to the model's requirements. Cog installs the provider globally before calling `configure_instrumentation()`, then imports the model. Cog force-flushes and shuts down the provider with the worker, so custom providers should set `shutdown_on_exit=False`.

The custom provider controls Python spans only. The Rust parent and worker providers continue to use `observability.traces` and standard `OTEL_*` variables. Without an OTLP endpoint, the custom provider can still emit Python spans to a console or another exporter, but there is no `cog.prediction.invoke` parent or other framework spans.

Import errors, a missing factory, the wrong return type, or instrumentation errors fail model setup. Auto-instrumentation can capture model inputs, HTTP headers, or other sensitive data; review each instrumentation package before enabling it.

## Streaming predictions

Normal JSON and Server-Sent Events requests share the same prediction, worker, and model spans. The HTTP span may end after the SSE response starts, while `cog.prediction`, `cog.prediction.invoke`, and model spans continue until generation finishes or the request is canceled.

Models can add a span or span event for each output chunk. Cog does not do this automatically because long token streams can produce large, noisy traces. The automatic invocation span covers the full generator lifetime, and terminal prediction attributes report the final outcome. Add per-chunk instrumentation only when that detail justifies the added telemetry volume.

## Caller-supplied trace tags

Any caller can attach bounded tags to `cog.prediction` through the existing request `context` map. Prefix an entry with `trace.` to opt it into telemetry:

```json
{
  "id": "request-123",
  "input": {
    "prompt": "hello"
  },
  "context": {
    "trace.model.name": "example/model",
    "trace.deployment": "production",
    "ordinary.secret": "not exported"
  }
}
```

Cog exports:

```text
caller.model.name = example/model
caller.deployment = production
```

The `caller.` namespace prevents callers from replacing framework attributes such as `cog.prediction.status`, `http.route`, or `service.name`.

Limits:

- Only keys beginning with `trace.` are promoted.
- Values must already be strings because request context is `dict[str, str]`.
- At most 16 tags are exported.
- Attribute suffixes are at most 64 bytes and may contain letters, digits, `.`, `_`, and `-`.
- Values are truncated to 128 bytes at a valid UTF-8 boundary.
- The total exported caller metadata is limited to 4 KiB.
- Caller tags are added only to `cog.prediction`, not every child span.

Caller tags are untrusted. Do not put prompts, outputs, credentials, authorization headers, personal data, or other secrets under `trace.*` keys.

## Calling Cog from another service

Use W3C Trace Context when a gateway or service calls Cog:

```http
traceparent: 00-4bf92f3577b34da6a3ce929d0e0e4736-1111111111111111-01
tracestate: vendor=value
```

Cog creates a child HTTP span, carries the resulting context over worker IPC, and attaches it before model execution. A valid standard `traceparent` takes precedence over a configured custom header.

Services may also send `trace.*` context entries. For example, a proxy can forward its selected model name without Cog depending on that proxy's private header names:

```json
{
  "input": {},
  "context": {
    "trace.model.name": "provider/model-name"
  }
}
```

This contract is not specific to any hosting provider.

## Sampling

The default sampler is `parentbased_always_off`. It continues sampled parent traces but does not start new traces.

| Sampler                    | Sampled parent | Unsampled parent | No parent |
| -------------------------- | -------------- | ---------------- | --------- |
| `parentbased_always_off`   | keep           | drop             | drop      |
| `parentbased_always_on`    | keep           | drop             | keep      |
| `parentbased_traceidratio` | keep           | drop             | ratio     |
| `always_on`                | keep           | keep             | keep      |
| `always_off`               | drop           | drop             | drop      |
| `traceidratio`             | ratio          | ratio            | ratio     |

Ratio samplers require `sampler_arg` as a string between `"0"` and `"1"`.
See OpenTelemetry's [sampler configuration](https://opentelemetry.io/docs/languages/sdk-configuration/general/#otel_traces_sampler) for the standard sampler behavior.

## Custom trace headers

W3C `traceparent` and `tracestate` are always supported. An operator may configure one additional W3C- or Jaeger-formatted header:

```yaml
observability:
  traces:
    enabled: true
    trace_header: x-company-trace
    trace_header_format: jaeger
```

Malformed trace headers are ignored and never reject a prediction. Signed output uploads never receive trace headers.

## Resource identity

Use standard resource variables for values fixed across the running container:

```shell
OTEL_SERVICE_NAME=cog
OTEL_RESOURCE_ATTRIBUTES=deployment.environment.name=production,service.instance.id=instance-123
```

Request-specific values belong on `cog.prediction` through caller tags rather than resources.

## Failure behavior

- Missing collector endpoint: warn and serve without framework tracing; a custom Python provider may still run.
- Unreachable collector: Cog's built-in exporters retry or drop without failing predictions.
- Malformed parent context: ignore it and continue.
- Worker shutdown: flush and shut down parent and worker providers with bounded best effort; call custom Python provider cleanup synchronously.
- Forced termination, crashes, and OOM: final spans may be lost.

Delivery from Cog's built-in exporters never determines whether a prediction succeeds. Custom processors and exporters run model-owned code and may raise or block. Invalid explicit tracing configuration fails model setup.

## What's next

Metrics and OpenTelemetry log export are planned next. They will use the same opt-in approach as tracing.
