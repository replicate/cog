use std::collections::HashSet;
use std::sync::{Arc, OnceLock, RwLock};
use std::time::Duration;

use opentelemetry::KeyValue;
use opentelemetry::metrics::{
    Counter, Histogram, MeterProvider as _, ObservableGauge, UpDownCounter,
};
use opentelemetry_otlp::{MetricExporter, Protocol, WithExportConfig as _};
use opentelemetry_sdk::metrics::SdkMeterProvider;

use crate::bridge::protocol::{RuntimeMetric, RuntimeMetricsConfig};
use crate::permit::PermitPool;
use crate::trace::{ProcessRole, base_resource};

const PREDICTION_DURATION_BUCKETS: [f64; 14] = [
    0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0, 10.0, 30.0, 60.0, 120.0, 300.0,
];
const SETUP_DURATION_BUCKETS: [f64; 11] = [
    0.1, 0.5, 1.0, 2.5, 5.0, 10.0, 30.0, 60.0, 120.0, 300.0, 600.0,
];

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
enum OtlpProtocol {
    HttpProtobuf,
    Grpc,
}

struct MetricsConfig {
    endpoint: String,
    append_metrics_path: bool,
    protocol: OtlpProtocol,
}

impl MetricsConfig {
    fn from_env() -> Result<Option<Self>, String> {
        if !env_bool("COG_METRICS_CONFIGURED", false)?
            || !env_bool("COG_METRICS_ENABLED", true)?
            || env_bool("OTEL_SDK_DISABLED", false)?
            || std::env::var("OTEL_METRICS_EXPORTER").as_deref() == Ok("none")
        {
            return Ok(None);
        }

        let (endpoint, append_metrics_path) =
            match std::env::var("OTEL_EXPORTER_OTLP_METRICS_ENDPOINT") {
                Ok(value) if !value.trim().is_empty() => (value, false),
                Ok(_) => {
                    eprintln!("Metrics enabled without an OTLP endpoint; metrics disabled");
                    return Ok(None);
                }
                Err(_) => match std::env::var("OTEL_EXPORTER_OTLP_ENDPOINT") {
                    Ok(value) if !value.trim().is_empty() => (value, true),
                    _ => {
                        eprintln!("Metrics enabled without an OTLP endpoint; metrics disabled");
                        return Ok(None);
                    }
                },
            };

        let protocol = match std::env::var("OTEL_EXPORTER_OTLP_METRICS_PROTOCOL")
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
            append_metrics_path,
            protocol,
        }))
    }
}

struct RuntimeMetrics {
    provider: SdkMeterProvider,
    prediction_count: Option<Counter<u64>>,
    prediction_rejected: Option<Counter<u64>>,
    prediction_active: Option<UpDownCounter<i64>>,
    prediction_duration: Option<Histogram<f64>>,
    setup_duration: Option<Histogram<f64>>,
    _slot_count: Option<ObservableGauge<u64>>,
}

impl RuntimeMetrics {
    fn from_env(
        config: RuntimeMetricsConfig,
        pool: Option<Arc<PermitPool>>,
    ) -> Result<Option<Self>, String> {
        if !config.enabled {
            return Ok(None);
        }

        let Some(export_config) = MetricsConfig::from_env()? else {
            return Ok(None);
        };
        let disabled = config.disabled.into_iter().collect::<HashSet<_>>();
        if disabled.len() == RuntimeMetric::ALL.len() {
            return Ok(None);
        }

        let exporter = build_exporter(&export_config)?;
        let provider = SdkMeterProvider::builder()
            .with_resource(base_resource(ProcessRole::Parent))
            .with_periodic_exporter(exporter)
            .build();
        Ok(Some(Self::new(provider, disabled, pool)))
    }

    fn new(
        provider: SdkMeterProvider,
        disabled: HashSet<RuntimeMetric>,
        pool: Option<Arc<PermitPool>>,
    ) -> Self {
        let meter = provider.meter("coglet");

        let prediction_count = (!disabled.contains(&RuntimeMetric::PredictionCount)).then(|| {
            meter
                .u64_counter("cog.runtime.prediction.count")
                .with_unit("{prediction}")
                .build()
        });
        let prediction_rejected =
            (!disabled.contains(&RuntimeMetric::PredictionRejected)).then(|| {
                meter
                    .u64_counter("cog.runtime.prediction.rejected")
                    .with_unit("{prediction}")
                    .build()
            });
        let prediction_active = (!disabled.contains(&RuntimeMetric::PredictionActive)).then(|| {
            meter
                .i64_up_down_counter("cog.runtime.prediction.active")
                .with_unit("{prediction}")
                .build()
        });
        let prediction_duration =
            (!disabled.contains(&RuntimeMetric::PredictionDuration)).then(|| {
                meter
                    .f64_histogram("cog.runtime.prediction.duration")
                    .with_unit("s")
                    .with_boundaries(PREDICTION_DURATION_BUCKETS.to_vec())
                    .build()
            });
        let setup_duration = (!disabled.contains(&RuntimeMetric::SetupDuration)).then(|| {
            meter
                .f64_histogram("cog.runtime.setup.duration")
                .with_unit("s")
                .with_boundaries(SETUP_DURATION_BUCKETS.to_vec())
                .build()
        });
        let slot_count = if disabled.contains(&RuntimeMetric::SlotCount) {
            None
        } else {
            pool.map(|pool| {
                meter
                    .u64_observable_gauge("cog.runtime.slot.count")
                    .with_unit("{slot}")
                    .with_callback(move |observer| {
                        for (state, count) in pool.slot_state_counts() {
                            observer.observe(count, &[KeyValue::new("state", state.as_str())]);
                        }
                    })
                    .build()
            })
        };

        Self {
            provider,
            prediction_count,
            prediction_rejected,
            prediction_active,
            prediction_duration,
            setup_duration,
            _slot_count: slot_count,
        }
    }

    fn shutdown(self) {
        if let Err(error) = self.provider.force_flush() {
            tracing::warn!(target: "coglet::metrics", %error, "Failed to flush metrics provider");
        }
        if let Err(error) = self.provider.shutdown_with_timeout(Duration::from_secs(5)) {
            tracing::warn!(target: "coglet::metrics", %error, "Failed to shut down metrics provider");
        }
    }
}

#[derive(Default)]
struct Registry {
    metrics: Option<RuntimeMetrics>,
    initialized: bool,
    pending_rejections: [[u64; 3]; 2],
}

static REGISTRY: OnceLock<RwLock<Registry>> = OnceLock::new();

fn registry() -> &'static RwLock<Registry> {
    REGISTRY.get_or_init(|| RwLock::new(Registry::default()))
}

pub fn install(config: RuntimeMetricsConfig, pool: Option<Arc<PermitPool>>) {
    let metrics = match RuntimeMetrics::from_env(config, pool) {
        Ok(metrics) => metrics,
        Err(error) => {
            eprintln!("Invalid OpenTelemetry metrics configuration; metrics disabled: {error}");
            None
        }
    };

    let Ok(mut registry) = registry().write() else {
        tracing::warn!(target: "coglet::metrics", "Metrics registry lock poisoned");
        return;
    };
    let previous = std::mem::replace(&mut registry.metrics, metrics);
    registry.initialized = true;
    if registry.metrics.is_some() {
        drain_pending_rejections(&mut registry);
    } else {
        registry.pending_rejections = [[0; 3]; 2];
    }
    drop(registry);
    if let Some(previous) = previous {
        previous.shutdown();
    }
}

pub fn shutdown() {
    let Ok(mut registry) = registry().write() else {
        tracing::warn!(target: "coglet::metrics", "Metrics registry lock poisoned");
        return;
    };
    if let Some(metrics) = registry.metrics.take() {
        drop(registry);
        metrics.shutdown();
    }
}

pub fn record_prediction_admitted(operation: &'static str) {
    if let Ok(registry) = registry().read()
        && let Some(active) = registry
            .metrics
            .as_ref()
            .and_then(|metrics| metrics.prediction_active.as_ref())
    {
        active.add(1, &[KeyValue::new("operation", operation)]);
    }
}

pub fn record_prediction_terminal(
    operation: &'static str,
    status: &'static str,
    duration: Duration,
) {
    if let Ok(registry) = registry().read() {
        let Some(metrics) = registry.metrics.as_ref() else {
            return;
        };
        let attributes = [
            KeyValue::new("operation", operation),
            KeyValue::new("status", status),
        ];
        if let Some(count) = metrics.prediction_count.as_ref() {
            count.add(1, &attributes);
        }
        if let Some(duration_metric) = metrics.prediction_duration.as_ref() {
            duration_metric.record(duration.as_secs_f64(), &attributes);
        }
        if let Some(active) = metrics.prediction_active.as_ref() {
            active.add(-1, &[KeyValue::new("operation", operation)]);
        }
    }
}

pub fn record_prediction_rejected(operation: &'static str, reason: &'static str) {
    let Ok(mut registry) = registry().write() else {
        return;
    };
    if let Some(rejected) = registry
        .metrics
        .as_ref()
        .and_then(|metrics| metrics.prediction_rejected.as_ref())
    {
        rejected.add(1, &rejection_attributes(operation, reason));
    } else if !registry.initialized {
        record_pending_rejection(&mut registry.pending_rejections, operation, reason);
    }
}

fn rejection_attributes(operation: &'static str, reason: &'static str) -> [KeyValue; 2] {
    [
        KeyValue::new("operation", operation),
        KeyValue::new("reason", reason),
    ]
}

fn record_pending_rejection(pending: &mut [[u64; 3]; 2], operation: &str, reason: &str) {
    let operation_index = match operation {
        "predict" => 0,
        "train" => 1,
        _ => return,
    };
    let reason_index = match reason {
        "invalid_input" => 0,
        "not_ready" => 1,
        "at_capacity" => 2,
        _ => return,
    };
    pending[operation_index][reason_index] =
        pending[operation_index][reason_index].saturating_add(1);
}

fn drain_pending_rejections(registry: &mut Registry) {
    let Some(rejected) = registry
        .metrics
        .as_ref()
        .and_then(|metrics| metrics.prediction_rejected.as_ref())
    else {
        registry.pending_rejections = [[0; 3]; 2];
        return;
    };
    for (operation_index, operation) in ["predict", "train"].iter().enumerate() {
        for (reason_index, reason) in ["invalid_input", "not_ready", "at_capacity"]
            .iter()
            .enumerate()
        {
            let count =
                std::mem::take(&mut registry.pending_rejections[operation_index][reason_index]);
            if count > 0 {
                rejected.add(count, &rejection_attributes(operation, reason));
            }
        }
    }
}

pub fn record_setup_duration(status: &'static str, duration: Duration) {
    if let Ok(registry) = registry().read()
        && let Some(setup_duration) = registry
            .metrics
            .as_ref()
            .and_then(|metrics| metrics.setup_duration.as_ref())
    {
        setup_duration.record(duration.as_secs_f64(), &[KeyValue::new("status", status)]);
    }
}

fn build_exporter(config: &MetricsConfig) -> Result<MetricExporter, String> {
    let builder = MetricExporter::builder();
    match config.protocol {
        OtlpProtocol::HttpProtobuf => builder
            .with_http()
            .with_protocol(Protocol::HttpBinary)
            .with_endpoint(http_metrics_endpoint(
                &config.endpoint,
                config.append_metrics_path,
            ))
            .build()
            .map_err(|error| error.to_string()),
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
                Err("gRPC metrics support is not compiled in".to_string())
            }
        }
    }
}

fn http_metrics_endpoint(endpoint: &str, append_metrics_path: bool) -> String {
    if !append_metrics_path {
        return endpoint.to_string();
    }

    let suffix_start = endpoint.find(['?', '#']).unwrap_or(endpoint.len());
    let (path, suffix) = endpoint.split_at(suffix_start);
    let path = path.trim_end_matches('/');
    if path.ends_with("/v1/metrics") {
        return format!("{path}{suffix}");
    }
    format!("{path}/v1/metrics{suffix}")
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

#[cfg(test)]
mod tests {
    use std::collections::HashSet;
    use std::sync::{Arc, Mutex, OnceLock};
    use std::time::Duration;

    use opentelemetry_sdk::metrics::{InMemoryMetricExporter, PeriodicReader};

    use super::{
        RuntimeMetric, RuntimeMetrics, drain_pending_rejections, http_metrics_endpoint,
        record_prediction_admitted, record_prediction_rejected, record_prediction_terminal,
        record_setup_duration, registry, shutdown,
    };
    use crate::permit::PermitPool;

    static TEST_MUTEX: OnceLock<Mutex<()>> = OnceLock::new();

    #[test]
    fn http_metrics_endpoint_appends_signal_path_once() {
        assert_eq!(
            http_metrics_endpoint("https://collector:4318", true),
            "https://collector:4318/v1/metrics"
        );
        assert_eq!(
            http_metrics_endpoint("https://collector:4318/v1/metrics", true),
            "https://collector:4318/v1/metrics"
        );
        assert_eq!(
            http_metrics_endpoint("https://collector:4318/base?token=secret", true),
            "https://collector:4318/base/v1/metrics?token=secret"
        );
    }

    #[test]
    fn records_fixed_runtime_metric_instruments() {
        let _guard = TEST_MUTEX.get_or_init(|| Mutex::new(())).lock().unwrap();
        shutdown();

        let exporter = InMemoryMetricExporter::default();
        let provider = opentelemetry_sdk::metrics::SdkMeterProvider::builder()
            .with_reader(PeriodicReader::builder(exporter.clone()).build())
            .build();
        let metrics = RuntimeMetrics::new(
            provider.clone(),
            HashSet::new(),
            Some(Arc::new(PermitPool::new(1))),
        );
        let mut registry = registry().write().unwrap();
        registry.metrics = Some(metrics);
        registry.initialized = true;
        drop(registry);

        record_prediction_admitted("predict");
        record_prediction_terminal("predict", "succeeded", Duration::from_secs(2));
        record_prediction_rejected("train", "at_capacity");
        record_setup_duration("succeeded", Duration::from_secs(3));
        provider.force_flush().unwrap();

        let mut names = exporter
            .get_finished_metrics()
            .unwrap()
            .iter()
            .flat_map(|resource| resource.scope_metrics())
            .flat_map(|scope| scope.metrics())
            .map(|metric| metric.name().to_string())
            .collect::<Vec<_>>();
        names.sort();
        assert_eq!(
            names,
            vec![
                "cog.runtime.prediction.active",
                "cog.runtime.prediction.count",
                "cog.runtime.prediction.duration",
                "cog.runtime.prediction.rejected",
                "cog.runtime.setup.duration",
                "cog.runtime.slot.count",
            ]
        );
        shutdown();
    }

    #[test]
    fn disabled_selectors_do_not_create_runtime_instruments() {
        let disabled = RuntimeMetric::ALL.iter().copied().collect::<HashSet<_>>();
        let metrics = RuntimeMetrics::new(
            opentelemetry_sdk::metrics::SdkMeterProvider::builder().build(),
            disabled,
            None,
        );

        assert!(metrics.prediction_count.is_none());
        assert!(metrics.prediction_rejected.is_none());
        assert!(metrics.prediction_active.is_none());
        assert!(metrics.prediction_duration.is_none());
        assert!(metrics.setup_duration.is_none());
        assert!(metrics._slot_count.is_none());
    }

    #[test]
    fn disabled_setup_duration_keeps_other_runtime_instruments() {
        let metrics = RuntimeMetrics::new(
            opentelemetry_sdk::metrics::SdkMeterProvider::builder().build(),
            HashSet::from([RuntimeMetric::SetupDuration]),
            None,
        );

        assert!(metrics.prediction_count.is_some());
        assert!(metrics.setup_duration.is_none());
    }

    #[test]
    fn buffers_startup_rejections_until_metrics_are_installed() {
        let _guard = TEST_MUTEX.get_or_init(|| Mutex::new(())).lock().unwrap();
        shutdown();
        let mut registry_guard = registry().write().unwrap();
        registry_guard.initialized = false;
        registry_guard.pending_rejections = [[0; 3]; 2];
        drop(registry_guard);

        record_prediction_rejected("predict", "not_ready");
        assert_eq!(registry().read().unwrap().pending_rejections[0][1], 1);

        let exporter = InMemoryMetricExporter::default();
        let provider = opentelemetry_sdk::metrics::SdkMeterProvider::builder()
            .with_reader(PeriodicReader::builder(exporter.clone()).build())
            .build();
        let mut registry_guard = registry().write().unwrap();
        registry_guard.metrics = Some(RuntimeMetrics::new(
            provider.clone(),
            HashSet::new(),
            Some(Arc::new(PermitPool::new(1))),
        ));
        registry_guard.initialized = true;
        drain_pending_rejections(&mut registry_guard);
        drop(registry_guard);
        provider.force_flush().unwrap();

        assert!(
            exporter
                .get_finished_metrics()
                .unwrap()
                .iter()
                .flat_map(|resource| resource.scope_metrics())
                .flat_map(|scope| scope.metrics())
                .any(|metric| metric.name() == "cog.runtime.prediction.rejected")
        );
        assert_eq!(registry().read().unwrap().pending_rejections[0][1], 0);
        shutdown();
    }
}
