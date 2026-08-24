use std::sync::atomic::{AtomicBool, Ordering};
use std::time::Duration;

use std::collections::HashMap;

use axum::http::HeaderMap;
use opentelemetry::propagation::{Extractor, Injector, TextMapPropagator as _};
use opentelemetry::trace::{TraceContextExt as _, TracerProvider as _};
use opentelemetry::{Context, KeyValue};
use opentelemetry_otlp::{Protocol, SpanExporter, WithExportConfig as _};
use opentelemetry_sdk::Resource;
use opentelemetry_sdk::propagation::TraceContextPropagator;
use opentelemetry_sdk::trace::{Sampler, SdkTracerProvider};
use tracing_opentelemetry::OpenTelemetrySpanExt as _;

use crate::bridge::protocol::TraceCarrier;

pub use opentelemetry_sdk::trace::SdkTracer;

static ACTIVE: AtomicBool = AtomicBool::new(false);

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum ProcessRole {
    Parent,
    Worker,
}

impl ProcessRole {
    fn as_str(self) -> &'static str {
        match self {
            Self::Parent => "parent",
            Self::Worker => "worker",
        }
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
enum OtlpProtocol {
    HttpProtobuf,
    Grpc,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
enum SamplerKind {
    AlwaysOn,
    AlwaysOff,
    TraceIdRatio,
    ParentBasedAlwaysOn,
    ParentBasedAlwaysOff,
    ParentBasedTraceIdRatio,
}

#[derive(Clone, Debug)]
pub struct TracingConfig {
    endpoint: String,
    append_trace_path: bool,
    protocol: OtlpProtocol,
    sampler: SamplerKind,
    sampler_arg: Option<f64>,
    service_name: String,
}

impl TracingConfig {
    pub fn from_env() -> Result<Option<Self>, String> {
        if !env_bool("COG_TRACE_CONFIGURED", false)?
            || !env_bool("COG_TRACE_ENABLED", true)?
            || env_bool("OTEL_SDK_DISABLED", false)?
            || std::env::var("OTEL_TRACES_EXPORTER").as_deref() == Ok("none")
        {
            return Ok(None);
        }

        let (endpoint, append_trace_path) =
            match std::env::var("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT") {
                Ok(value) if !value.trim().is_empty() => (value, false),
                Ok(_) => {
                    eprintln!("Tracing enabled without an OTLP endpoint; tracing disabled");
                    return Ok(None);
                }
                Err(_) => match std::env::var("OTEL_EXPORTER_OTLP_ENDPOINT") {
                    Ok(value) if !value.trim().is_empty() => (value, true),
                    _ => {
                        eprintln!("Tracing enabled without an OTLP endpoint; tracing disabled");
                        return Ok(None);
                    }
                },
            };

        let protocol = match std::env::var("OTEL_EXPORTER_OTLP_TRACES_PROTOCOL")
            .or_else(|_| std::env::var("OTEL_EXPORTER_OTLP_PROTOCOL"))
            .unwrap_or_else(|_| "http/protobuf".to_string())
            .as_str()
        {
            "http" | "http/protobuf" => OtlpProtocol::HttpProtobuf,
            "grpc" => OtlpProtocol::Grpc,
            value => return Err(format!("unsupported OTLP protocol {value:?}")),
        };

        let sampler_name = std::env::var("OTEL_TRACES_SAMPLER")
            .or_else(|_| std::env::var("COG_TRACE_SAMPLER"))
            .unwrap_or_else(|_| "parentbased_always_off".to_string());
        let sampler = parse_sampler(&sampler_name)?;
        let runtime_sampler_arg = std::env::var("OTEL_TRACES_SAMPLER_ARG").ok();
        let sampler_arg = runtime_sampler_arg
            .clone()
            .or_else(|| std::env::var("COG_TRACE_SAMPLER_ARG").ok())
            .map(|value| {
                value
                    .parse::<f64>()
                    .map_err(|_| format!("invalid sampler ratio {value:?}"))
                    .and_then(|ratio| {
                        if (0.0..=1.0).contains(&ratio) {
                            Ok(ratio)
                        } else {
                            Err(format!("sampler ratio {ratio} is outside [0, 1]"))
                        }
                    })
            })
            .transpose()?;
        if !sampler_arg_is_valid(sampler, sampler_arg, runtime_sampler_arg.is_some()) {
            return Err("sampler_arg is only valid for ratio samplers".to_string());
        }

        Ok(Some(Self {
            endpoint,
            append_trace_path,
            protocol,
            sampler,
            sampler_arg,
            service_name: std::env::var("OTEL_SERVICE_NAME").unwrap_or_else(|_| "cog".to_string()),
        }))
    }

    fn sdk_sampler(&self) -> Sampler {
        let ratio = self.sampler_arg.unwrap_or(1.0);
        match self.sampler {
            SamplerKind::AlwaysOn => Sampler::AlwaysOn,
            SamplerKind::AlwaysOff => Sampler::AlwaysOff,
            SamplerKind::TraceIdRatio => Sampler::TraceIdRatioBased(ratio),
            SamplerKind::ParentBasedAlwaysOn => Sampler::ParentBased(Box::new(Sampler::AlwaysOn)),
            SamplerKind::ParentBasedAlwaysOff => Sampler::ParentBased(Box::new(Sampler::AlwaysOff)),
            SamplerKind::ParentBasedTraceIdRatio => {
                Sampler::ParentBased(Box::new(Sampler::TraceIdRatioBased(ratio)))
            }
        }
    }
}

pub struct TracingRuntime {
    provider: SdkTracerProvider,
    tracer: SdkTracer,
}

impl TracingRuntime {
    pub fn from_env(role: ProcessRole) -> Option<Self> {
        match Self::try_from_env(role) {
            Ok(runtime) => runtime,
            Err(error) => {
                eprintln!("Invalid OpenTelemetry tracing configuration; tracing disabled: {error}");
                None
            }
        }
    }

    fn try_from_env(role: ProcessRole) -> Result<Option<Self>, String> {
        let Some(config) = TracingConfig::from_env()? else {
            return Ok(None);
        };

        let exporter = build_exporter(&config)?;
        let resource = Resource::builder()
            .with_service_name(config.service_name.clone())
            .with_attributes([
                KeyValue::new("service.version", crate::COGLET_VERSION),
                KeyValue::new("cog.process.role", role.as_str()),
            ])
            .build();
        let provider = SdkTracerProvider::builder()
            .with_resource(resource)
            .with_sampler(config.sdk_sampler())
            .with_batch_exporter(exporter)
            .build();
        let tracer = provider.tracer("coglet");
        ACTIVE.store(true, Ordering::Release);
        tracing::info!(
            target: "coglet::trace",
            protocol = ?config.protocol,
            service_name = %config.service_name,
            role = role.as_str(),
            "OpenTelemetry tracing initialized"
        );
        Ok(Some(Self { provider, tracer }))
    }

    pub fn tracer(&self) -> SdkTracer {
        self.tracer.clone()
    }

    pub fn shutdown(&self) {
        if let Err(error) = self.provider.force_flush() {
            tracing::warn!(target: "coglet::trace", %error, "Failed to flush tracing provider");
        }
        if let Err(error) = self.provider.shutdown_with_timeout(Duration::from_secs(5)) {
            tracing::warn!(target: "coglet::trace", %error, "Failed to shut down tracing provider");
        }
        ACTIVE.store(false, Ordering::Release);
    }
}

pub fn is_active() -> bool {
    ACTIVE.load(Ordering::Acquire)
}

#[cfg(test)]
pub(crate) struct ActiveTestGuard(bool);

#[cfg(test)]
pub(crate) fn activate_for_test() -> ActiveTestGuard {
    ActiveTestGuard(ACTIVE.swap(true, Ordering::AcqRel))
}

#[cfg(test)]
impl Drop for ActiveTestGuard {
    fn drop(&mut self) {
        ACTIVE.store(self.0, Ordering::Release);
    }
}

pub fn extract_parent(headers: &HeaderMap) -> Option<Context> {
    if !is_active() {
        return None;
    }

    let mut values = HashMap::new();
    for name in ["traceparent", "tracestate"] {
        if let Some(value) = headers.get(name).and_then(|value| value.to_str().ok()) {
            values.insert(name.to_string(), value.to_string());
        }
    }
    let w3c = TraceContextPropagator::new().extract(&MapExtractor(&values));
    if w3c.span().span_context().is_valid() {
        return Some(w3c);
    }

    let header_name = std::env::var("COG_TRACE_HEADER").ok()?;
    let value = headers
        .get(&header_name)
        .and_then(|value| value.to_str().ok())?;
    match std::env::var("COG_TRACE_HEADER_FORMAT")
        .unwrap_or_else(|_| "w3c".to_string())
        .as_str()
    {
        "w3c" => {
            let values = HashMap::from([("traceparent".to_string(), value.to_string())]);
            let context = TraceContextPropagator::new().extract(&MapExtractor(&values));
            context.span().span_context().is_valid().then_some(context)
        }
        "jaeger" => {
            let normalized = value.split(':').take(4).collect::<Vec<_>>().join(":");
            let values = HashMap::from([("uber-trace-id".to_string(), normalized)]);
            #[allow(deprecated)]
            let context =
                opentelemetry_jaeger_propagator::Propagator::new().extract(&MapExtractor(&values));
            context.span().span_context().is_valid().then_some(context)
        }
        _ => None,
    }
}

pub fn set_parent(span: &tracing::Span, parent: Context) {
    let _ = span.set_parent(parent);
}

pub fn set_parent_from_carrier(span: &tracing::Span, carrier: &TraceCarrier) {
    let values = carrier_values(carrier);
    let context = TraceContextPropagator::new().extract(&MapExtractor(&values));
    if context.span().span_context().is_valid() {
        set_parent(span, context);
    }
}

pub fn carrier_from_span(span: &tracing::Span) -> Option<TraceCarrier> {
    if !is_active() || span.is_disabled() {
        return None;
    }
    let context = span.context();
    let span_context = context.span().span_context().clone();
    if !span_context.is_valid() {
        return None;
    }
    let mut values = HashMap::new();
    TraceContextPropagator::new().inject_context(&context, &mut MapInjector(&mut values));
    Some(TraceCarrier {
        traceparent: values.remove("traceparent")?,
        tracestate: values.remove("tracestate"),
    })
}

pub fn custom_header(carrier: &TraceCarrier) -> Option<(String, String)> {
    let name = std::env::var("COG_TRACE_HEADER").ok()?;
    let format = std::env::var("COG_TRACE_HEADER_FORMAT").unwrap_or_else(|_| "w3c".to_string());
    if format == "w3c" {
        return Some((name, carrier.traceparent.clone()));
    }
    if format != "jaeger" {
        return None;
    }
    let mut parts = carrier.traceparent.split('-');
    let _version = parts.next()?;
    let trace_id = parts.next()?;
    let span_id = parts.next()?;
    let flags = parts.next()?;
    Some((name, format!("{trace_id}:{span_id}:0:{flags}")))
}

pub fn set_caller_attributes(
    span: &tracing::Span,
    context: &std::collections::HashMap<String, String>,
) {
    for (key, value) in caller_attributes(context) {
        span.set_attribute(key, value);
    }
}

fn caller_attributes(context: &std::collections::HashMap<String, String>) -> Vec<(String, String)> {
    let mut attributes = context
        .iter()
        .filter_map(|(key, value)| {
            let suffix = key.strip_prefix("trace.")?;
            if suffix.is_empty()
                || suffix.len() > 64
                || !suffix
                    .bytes()
                    .all(|byte| byte.is_ascii_alphanumeric() || matches!(byte, b'.' | b'_' | b'-'))
            {
                return None;
            }
            Some((
                format!("caller.{suffix}"),
                crate::bounded_attribute_value(value).to_string(),
            ))
        })
        .collect::<Vec<_>>();
    attributes.sort_unstable_by(|left, right| left.0.cmp(&right.0));

    let mut bounded = Vec::new();
    let mut total_bytes = 0;
    for (key, value) in attributes.into_iter().take(16) {
        if total_bytes + key.len() + value.len() > 4096 {
            break;
        }
        total_bytes += key.len() + value.len();
        bounded.push((key, value));
    }
    bounded
}

fn carrier_values(carrier: &TraceCarrier) -> HashMap<String, String> {
    let mut values = HashMap::from([("traceparent".to_string(), carrier.traceparent.clone())]);
    if let Some(tracestate) = &carrier.tracestate {
        values.insert("tracestate".to_string(), tracestate.clone());
    }
    values
}

struct MapExtractor<'a>(&'a HashMap<String, String>);

impl Extractor for MapExtractor<'_> {
    fn get(&self, key: &str) -> Option<&str> {
        self.0.get(key).map(String::as_str)
    }

    fn keys(&self) -> Vec<&str> {
        self.0.keys().map(String::as_str).collect()
    }
}

struct MapInjector<'a>(&'a mut HashMap<String, String>);

impl Injector for MapInjector<'_> {
    fn set(&mut self, key: &str, value: String) {
        self.0.insert(key.to_string(), value);
    }
}

fn build_exporter(config: &TracingConfig) -> Result<SpanExporter, String> {
    let builder = SpanExporter::builder();
    match config.protocol {
        OtlpProtocol::HttpProtobuf => {
            let endpoint = http_trace_endpoint(&config.endpoint, config.append_trace_path);
            builder
                .with_http()
                .with_protocol(Protocol::HttpBinary)
                .with_endpoint(endpoint)
                .build()
                .map_err(|error| error.to_string())
        }
        OtlpProtocol::Grpc => {
            #[cfg(feature = "tracing-grpc")]
            {
                builder
                    .with_tonic()
                    .with_endpoint(config.endpoint.clone())
                    .build()
                    .map_err(|error| error.to_string())
            }
            #[cfg(not(feature = "tracing-grpc"))]
            {
                Err("gRPC tracing support is not compiled in".to_string())
            }
        }
    }
}

fn http_trace_endpoint(endpoint: &str, append_trace_path: bool) -> String {
    if !append_trace_path {
        return endpoint.to_string();
    }

    let suffix_start = endpoint.find(['?', '#']).unwrap_or(endpoint.len());
    let (path, suffix) = endpoint.split_at(suffix_start);
    let path = path.trim_end_matches('/');
    if path.ends_with("/v1/traces") {
        return format!("{path}{suffix}");
    }
    format!("{path}/v1/traces{suffix}")
}

fn env_bool(name: &str, default: bool) -> Result<bool, String> {
    let Ok(value) = std::env::var(name) else {
        return Ok(default);
    };
    match value.to_ascii_lowercase().as_str() {
        "1" | "true" | "yes" => Ok(true),
        "0" | "false" | "no" => Ok(false),
        _ => Err(format!("{name} must be true or false")),
    }
}

fn parse_sampler(value: &str) -> Result<SamplerKind, String> {
    match value {
        "always_on" => Ok(SamplerKind::AlwaysOn),
        "always_off" => Ok(SamplerKind::AlwaysOff),
        "traceidratio" => Ok(SamplerKind::TraceIdRatio),
        "parentbased_always_on" => Ok(SamplerKind::ParentBasedAlwaysOn),
        "parentbased_always_off" => Ok(SamplerKind::ParentBasedAlwaysOff),
        "parentbased_traceidratio" => Ok(SamplerKind::ParentBasedTraceIdRatio),
        _ => Err(format!("unsupported sampler {value:?}")),
    }
}

fn sampler_arg_is_valid(
    sampler: SamplerKind,
    sampler_arg: Option<f64>,
    runtime_sampler_arg_set: bool,
) -> bool {
    sampler_arg.is_none()
        || !runtime_sampler_arg_set
        || matches!(
            sampler,
            SamplerKind::TraceIdRatio | SamplerKind::ParentBasedTraceIdRatio
        )
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parses_all_supported_samplers() {
        for sampler in [
            "always_on",
            "always_off",
            "traceidratio",
            "parentbased_always_on",
            "parentbased_always_off",
            "parentbased_traceidratio",
        ] {
            assert!(parse_sampler(sampler).is_ok());
        }
    }

    #[test]
    fn ratio_sampler_without_arg_defaults_to_one() {
        let config = TracingConfig {
            endpoint: String::new(),
            append_trace_path: true,
            protocol: OtlpProtocol::HttpProtobuf,
            sampler: SamplerKind::TraceIdRatio,
            sampler_arg: None,
            service_name: String::new(),
        };

        match config.sdk_sampler() {
            Sampler::TraceIdRatioBased(ratio) => assert_eq!(ratio, 1.0),
            sampler => panic!("unexpected sampler: {sampler:?}"),
        }
    }

    #[test]
    fn image_sampler_arg_does_not_reject_runtime_non_ratio_sampler() {
        assert!(sampler_arg_is_valid(
            SamplerKind::AlwaysOn,
            Some(0.5),
            false
        ));
        assert!(!sampler_arg_is_valid(
            SamplerKind::AlwaysOn,
            Some(0.5),
            true
        ));
    }

    #[test]
    fn http_trace_endpoint_appends_signal_path_once() {
        assert_eq!(
            http_trace_endpoint("https://collector:4318", true),
            "https://collector:4318/v1/traces"
        );
        assert_eq!(
            http_trace_endpoint("https://collector:4318/v1/traces", true),
            "https://collector:4318/v1/traces"
        );
        assert_eq!(
            http_trace_endpoint("https://collector:4318/base?token=secret", true),
            "https://collector:4318/base/v1/traces?token=secret"
        );
        assert_eq!(
            http_trace_endpoint("https://collector:4318/custom?token=secret", false),
            "https://collector:4318/custom?token=secret"
        );
    }

    #[test]
    fn caller_attributes_are_bounded_and_opt_in() {
        let context = HashMap::from([
            ("ordinary".to_string(), "secret".to_string()),
            ("trace.model.name".to_string(), "example".to_string()),
            ("trace.invalid key".to_string(), "ignored".to_string()),
        ]);
        assert_eq!(
            caller_attributes(&context),
            vec![("caller.model.name".to_string(), "example".to_string())]
        );
    }
}
