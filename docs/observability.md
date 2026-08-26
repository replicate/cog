# Observability

Cog can join incoming distributed traces, export fixed runtime metrics, and make standard OpenTelemetry APIs available to model code. Signals are opt-in and use OTLP, so they work with collectors and backends that support OpenTelemetry.

Cog has two telemetry ownership domains. The Rust parent owns fixed runtime metrics. The Python worker owns model-authored spans and metrics. Both can export to the same collector, but a Python provider never replaces the parent runtime provider.

## Enable telemetry

Enable either signal with the boolean shorthand:

```yaml
observability:
  traces: true
  metrics: true
```

Signals can also use objects when tracing needs sampler or propagation settings:

```yaml
observability:
  traces:
    enabled: true
    sampler: parentbased_always_off
  metrics:
    enabled: true
```

An image can enable either signal independently. Runtime configuration can disable an enabled signal, but cannot enable a signal omitted from the image.

Configure the collector when running the image:

```shell
OTEL_EXPORTER_OTLP_ENDPOINT=https://collector.example.com:4317
OTEL_EXPORTER_OTLP_PROTOCOL=grpc
OTEL_SERVICE_NAME=cog
```

Cog supports OTLP gRPC and HTTP/protobuf. Collector endpoints, authentication headers, certificates, and other `OTEL_*` values are runtime configuration and cannot be set through the general `cog.yaml` `environment` list.

Framework telemetry starts only when the image opts in and a collector endpoint is present. `COG_TRACE_ENABLED=false` and `COG_METRICS_ENABLED=false` disable the entire matching signal, including custom Python providers. `OTEL_SDK_DISABLED=true` disables all signals. `OTEL_TRACES_EXPORTER=none` and `OTEL_METRICS_EXPORTER=none` disable only the matching built-in provider. An explicitly configured Python factory can still use a different exporter or no exporter.

## Getting started with tracing

A model can have a plain `run()` method and still get tracing. Model code does not need to import OpenTelemetry or create spans:

```python
from cog import BaseRunner


class Runner(BaseRunner):
    def run(self, prompt: str) -> str:
        return expensive_model_call(prompt)
```

Add this to `cog.yaml` to enable tracing:

```yaml
observability:
  traces: true
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

## Custom Python telemetry

Set `observability.config` to a project-relative Python file:

```yaml
observability:
  config: telemetry.py
  traces: true
  metrics: true
```

Cog validates the file during configuration, copies it to a fixed path in the image, and loads it before importing the model. Provider factories are optional. A missing factory uses Cog's default provider for that signal. Factories receive Cog's base `Resource` and may merge or replace its attributes.

```python
from opentelemetry.sdk.resources import Resource
from opentelemetry.sdk.metrics import MeterProvider
from opentelemetry.sdk.metrics.export import PeriodicExportingMetricReader
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import BatchSpanProcessor
from opentelemetry.exporter.otlp.proto.http.metric_exporter import OTLPMetricExporter
from opentelemetry.exporter.otlp.proto.http.trace_exporter import OTLPSpanExporter
from opentelemetry.sdk.trace.sampling import ParentBased, TraceIdRatioBased


def create_tracer_provider(resource: Resource) -> TracerProvider:
    provider = TracerProvider(
        resource=resource.merge(Resource({"model.name": "example"})),
        sampler=ParentBased(TraceIdRatioBased(0.1)),
        shutdown_on_exit=False,
    )
    provider.add_span_processor(BatchSpanProcessor(OTLPSpanExporter()))
    return provider


def create_meter_provider(resource: Resource) -> MeterProvider:
    return MeterProvider(
        metric_readers=[PeriodicExportingMetricReader(OTLPMetricExporter())],
        resource=resource,
        shutdown_on_exit=False,
    )


def configure_instrumentation() -> None:
    from opentelemetry.instrumentation.requests import RequestsInstrumentor

    RequestsInstrumentor().instrument()
```

Add instrumentation packages such as `opentelemetry-instrumentation-requests` to the model's requirements. Cog constructs every selected provider, validates their types, installs them globally, then calls `configure_instrumentation()` before importing the model. Cog force-flushes and shuts down providers with the worker, so custom providers should set `shutdown_on_exit=False`.

Custom Python providers control model-authored telemetry only. The Rust parent provider continues to own fixed runtime metrics. Without an OTLP endpoint, a custom provider can still emit model telemetry to a different destination, but there are no framework trace parents.

Import errors, a wrong return type, factory errors, and instrumentation errors fail model setup. Auto-instrumentation can capture model inputs, HTTP headers, or other sensitive data; review each instrumentation package before enabling it.

## Metrics

`metrics: true` enables two providers. The Rust parent exports fixed Cog runtime instruments. The Python worker installs a standard `MeterProvider` so model code can create its own instruments with the OpenTelemetry API. The worker does not export a second copy of the fixed runtime metrics.

### Runtime metrics

| Instrument                        | Type            | Unit           | Attributes            |
| --------------------------------- | --------------- | -------------- | --------------------- |
| `cog.runtime.prediction.count`    | Counter         | `{prediction}` | `operation`, `status` |
| `cog.runtime.prediction.rejected` | Counter         | `{prediction}` | `operation`, `reason` |
| `cog.runtime.prediction.active`   | UpDownCounter   | `{prediction}` | `operation`           |
| `cog.runtime.prediction.duration` | Histogram       | `s`            | `operation`, `status` |
| `cog.runtime.setup.duration`      | Histogram       | `s`            | `status`              |
| `cog.runtime.slot.count`          | ObservableGauge | `{slot}`       | `state`               |

`operation` is `predict` or `train`. Terminal status is `succeeded`, `failed`, or `canceled`. Rejection reasons are `invalid_input`, `not_ready`, and `at_capacity`. Slot state is `available`, `busy`, or `poisoned`.

Prediction duration starts after readiness validation and permit acquisition. It includes request preparation, worker execution, streaming, and output upload work. Setup duration is measured by the parent from setup start to its terminal result. Runtime metrics have fixed names, units, attributes, and histogram boundaries so dashboard queries remain stable.

### Model metrics

Use the standard OpenTelemetry API for model-owned metrics:

```python
from opentelemetry import metrics

meter = metrics.get_meter(__name__)
tokens = meter.create_counter("model.tokens")


class Runner(BaseRunner):
    def run(self, prompt: str) -> str:
        result = self.model(prompt)
        tokens.add(result.token_count)
        return result.text
```

Use names outside the reserved `cog.runtime.*` namespace for model instruments. `self.record_metric()` is separate from OpenTelemetry. It continues to populate the prediction response and does not create an OpenTelemetry instrument.

### Runtime metric selection

Models may disable fixed runtime instruments, but cannot rename, relabel, or change their buckets. Put this optional hook in `telemetry.py`:

```python
from cog.telemetry import RuntimeMetric, RuntimeMetricsConfig


def configure_runtime_metrics() -> RuntimeMetricsConfig:
    return RuntimeMetricsConfig(
        disabled={RuntimeMetric.SETUP_DURATION},
    )
```

Set `enabled=False` to disable all current and future Cog runtime metrics. This does not disable the Python `MeterProvider`, so model metrics can still export.

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
OTEL_RESOURCE_ATTRIBUTES=deployment.environment.name=production
```

Cog adds `service.version`, a process-local `service.instance.id`, and `cog.process.role=parent|worker` to the base resource. Request-specific values belong on `cog.prediction` through caller tags rather than resources.

## Failure behavior

- Missing collector endpoint or invalid built-in exporter settings: warn and serve without the matching built-in provider; a custom Python provider may still run.
- Unreachable collector: Cog's built-in exporters retry or drop without failing predictions.
- Malformed parent context: ignore it and continue.
- Worker shutdown: flush and shut down parent and worker providers with bounded best effort; call custom Python provider cleanup synchronously.
- Forced termination, crashes, and OOM: final spans may be lost.

If metrics telemetry configuration fails before the worker sends `Ready`, and the runtime metric exporter is configured, Cog uses the default runtime metric selection only long enough to record the failed setup result. The failed configuration cannot supply its own selections.

Delivery from Cog's built-in exporters never determines whether a prediction succeeds. Custom processors and exporters run model-owned code and may raise or block. Invalid explicit telemetry configuration fails model setup.
