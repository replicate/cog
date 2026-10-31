//! OpenTelemetry log export for coglet's own runtime events.
//!
//! Only the parent process exports. Worker events reach the parent through
//! `ControlResponse::WorkerLog` and are re-emitted there, so a worker-side
//! exporter would send them twice.

use std::time::Duration;

use opentelemetry_appender_tracing::layer::OpenTelemetryTracingBridge;
use opentelemetry_otlp::{LogExporter, Protocol, WithExportConfig as _};
use opentelemetry_sdk::logs::SdkLoggerProvider;
use tracing::Metadata;
use tracing_subscriber::Layer;
use tracing_subscriber::filter::filter_fn;
use tracing_subscriber::registry::LookupSpan;

use crate::trace::{ProcessRole, base_resource, env_bool};

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
enum OtlpProtocol {
    HttpProtobuf,
    Grpc,
}

#[derive(Debug, Eq, PartialEq)]
struct LogsConfig {
    endpoint: String,
    append_logs_path: bool,
    protocol: OtlpProtocol,
}

impl LogsConfig {
    fn from_env() -> Result<Option<Self>, String> {
        if !env_bool("COG_LOGS_CONFIGURED", false)?
            || !env_bool("COG_LOGS_ENABLED", true)?
            || env_bool("OTEL_SDK_DISABLED", false)?
            || std::env::var("OTEL_LOGS_EXPORTER").as_deref() == Ok("none")
        {
            return Ok(None);
        }

        let (endpoint, append_logs_path) = match std::env::var("OTEL_EXPORTER_OTLP_LOGS_ENDPOINT") {
            Ok(value) if !value.trim().is_empty() => (value, false),
            Ok(_) => {
                eprintln!("Logs enabled without an OTLP endpoint; log export disabled");
                return Ok(None);
            }
            Err(_) => match std::env::var("OTEL_EXPORTER_OTLP_ENDPOINT") {
                Ok(value) if !value.trim().is_empty() => (value, true),
                _ => {
                    eprintln!("Logs enabled without an OTLP endpoint; log export disabled");
                    return Ok(None);
                }
            },
        };

        let protocol = match std::env::var("OTEL_EXPORTER_OTLP_LOGS_PROTOCOL")
            .or_else(|_| std::env::var("OTEL_EXPORTER_OTLP_PROTOCOL"))
            .unwrap_or_else(|_| "http/protobuf".to_string())
            .as_str()
        {
            "http" | "http/protobuf" => OtlpProtocol::HttpProtobuf,
            "grpc" => OtlpProtocol::Grpc,
            value => return Err(format!("unsupported OTLP protocol {value:?}")),
        };

        Ok(Some(Self {
            endpoint,
            append_logs_path,
            protocol,
        }))
    }
}

pub struct LoggingRuntime {
    provider: SdkLoggerProvider,
}

impl LoggingRuntime {
    /// Builds the parent's log exporter, or returns `None` when logs are
    /// disabled or misconfigured. Configuration errors never stop serving.
    pub fn from_env() -> Option<Self> {
        match LogsConfig::from_env().and_then(|config| config.map(build_exporter).transpose()) {
            Ok(Some(exporter)) => Some(Self::new(
                SdkLoggerProvider::builder()
                    .with_resource(base_resource(ProcessRole::Parent))
                    .with_batch_exporter(exporter)
                    .build(),
            )),
            Ok(None) => None,
            Err(error) => {
                eprintln!("Invalid OpenTelemetry logs configuration; log export disabled: {error}");
                None
            }
        }
    }

    fn new(provider: SdkLoggerProvider) -> Self {
        Self { provider }
    }

    /// A tracing layer that exports coglet's own events. Model output that the
    /// parent re-emits as tracing events is excluded.
    pub fn layer<S>(&self) -> impl Layer<S> + use<S>
    where
        S: tracing::Subscriber + for<'a> LookupSpan<'a>,
    {
        OpenTelemetryTracingBridge::new(&self.provider).with_filter(filter_fn(exports_event))
    }

    /// Shutdown drains the batch processor, so no separate flush is needed.
    pub fn shutdown(&self) {
        if let Err(error) = self.provider.shutdown_with_timeout(Duration::from_secs(5)) {
            eprintln!("Failed to shut down OpenTelemetry logs provider: {error}");
        }
    }
}

fn exports_event(metadata: &Metadata<'_>) -> bool {
    if !metadata.is_event() || !metadata.target().starts_with("coglet") {
        return false;
    }
    match metadata.target() {
        // Model stdout/stderr re-emitted during setup and outside predictions.
        "coglet::setup" | "coglet::user" => false,
        // Model output lines carry a `source` field; Cog's own prediction
        // events on this target do not.
        "coglet::prediction" => metadata.fields().field("source").is_none(),
        _ => true,
    }
}

fn build_exporter(config: LogsConfig) -> Result<LogExporter, String> {
    let builder = LogExporter::builder();
    match config.protocol {
        OtlpProtocol::HttpProtobuf => builder
            .with_http()
            .with_protocol(Protocol::HttpBinary)
            .with_endpoint(http_logs_endpoint(
                &config.endpoint,
                config.append_logs_path,
            ))
            .build()
            .map_err(|error| error.to_string()),
        OtlpProtocol::Grpc => {
            #[cfg(feature = "tracing-grpc")]
            {
                builder
                    .with_tonic()
                    .with_endpoint(config.endpoint)
                    .build()
                    .map_err(|error| error.to_string())
            }
            #[cfg(not(feature = "tracing-grpc"))]
            {
                Err("gRPC logs support is not compiled in".to_string())
            }
        }
    }
}

fn http_logs_endpoint(endpoint: &str, append_logs_path: bool) -> String {
    if !append_logs_path {
        return endpoint.to_string();
    }

    let suffix_start = endpoint.find(['?', '#']).unwrap_or(endpoint.len());
    let (path, suffix) = endpoint.split_at(suffix_start);
    let path = path.trim_end_matches('/');
    if path.ends_with("/v1/logs") {
        return format!("{path}{suffix}");
    }
    format!("{path}/v1/logs{suffix}")
}

#[cfg(test)]
mod tests {
    use opentelemetry_sdk::logs::{InMemoryLogExporter, SimpleLogProcessor};
    use tracing_subscriber::layer::SubscriberExt as _;

    use super::*;

    fn exported_bodies(emit: impl FnOnce()) -> Vec<(String, String)> {
        let exporter = InMemoryLogExporter::default();
        let runtime = LoggingRuntime::new(
            SdkLoggerProvider::builder()
                .with_log_processor(SimpleLogProcessor::new(exporter.clone()))
                .build(),
        );
        let subscriber = tracing_subscriber::registry().with(runtime.layer());
        tracing::subscriber::with_default(subscriber, emit);

        let logs = exporter
            .get_emitted_logs()
            .unwrap()
            .into_iter()
            .map(|log| {
                let body = log
                    .record
                    .body()
                    .map(|body| format!("{body:?}"))
                    .unwrap_or_default();
                (
                    log.record
                        .target()
                        .map(ToString::to_string)
                        .unwrap_or_default(),
                    body,
                )
            })
            .collect();
        runtime.shutdown();
        logs
    }

    #[test]
    fn http_logs_endpoint_appends_signal_path_once() {
        assert_eq!(
            http_logs_endpoint("http://collector:4318", true),
            "http://collector:4318/v1/logs"
        );
        assert_eq!(
            http_logs_endpoint("http://collector:4318/v1/logs/", true),
            "http://collector:4318/v1/logs"
        );
        assert_eq!(
            http_logs_endpoint("http://collector:4318/?token=secret", true),
            "http://collector:4318/v1/logs?token=secret"
        );
        assert_eq!(
            http_logs_endpoint("http://collector:4318/custom", false),
            "http://collector:4318/custom"
        );
    }

    #[test]
    fn exports_cog_events_but_not_model_output() {
        let logs = exported_bodies(|| {
            tracing::info!(target: "coglet::prediction", prediction_id = "p1", "Starting prediction");
            tracing::info!(target: "coglet::prediction", prediction_id = "p1", source = "stdout", "model print");
            tracing::info!(target: "coglet::setup", source = "stderr", "setup print");
            tracing::info!(target: "coglet::user", "user print");
            tracing::warn!(target: "coglet::orchestrator", "Worker exited");
            tracing::info!(target: "opentelemetry_sdk", "internal");
            tracing::info!(target: "hyper::client", "transport");
        });

        let targets: Vec<_> = logs.iter().map(|(target, _)| target.as_str()).collect();
        assert_eq!(targets, ["coglet::prediction", "coglet::orchestrator"]);
        assert!(logs[0].1.contains("Starting prediction"), "{logs:?}");
    }
}
