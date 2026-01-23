//! coglet-python: PyO3 bindings for coglet.

mod audit;
mod cancel;
mod input;
mod log_writer;
mod output;
mod predictor;
mod worker_bridge;

use std::sync::Arc;
use std::sync::atomic::{AtomicBool, Ordering};

use pyo3::prelude::*;
use tracing::{debug, error, info, warn};
use tracing_subscriber::{EnvFilter, fmt, layer::SubscriberExt, util::SubscriberInitExt};

use coglet_core::{
    Health, PredictionService, SetupResult, VersionInfo,
    transport::{ServerConfig, serve as http_serve},
};

/// Global flag: true when running inside a worker subprocess.
static ACTIVE: AtomicBool = AtomicBool::new(false);

fn set_active() {
    ACTIVE.store(true, Ordering::SeqCst);
}

#[pyfunction]
fn active() -> bool {
    ACTIVE.load(Ordering::SeqCst)
}

/// Initialize tracing with COG_LOG and LOG_FORMAT support.
fn init_tracing(_to_stderr: bool) {
    let filter = if std::env::var("RUST_LOG").is_ok() {
        EnvFilter::from_default_env()
    } else {
        let base_level = match std::env::var("COG_LOG").as_deref() {
            Ok("debug") => "debug",
            Ok("warn") | Ok("warning") => "warn",
            Ok("error") => "error",
            _ => "info",
        };

        let filter_str = format!(
            "coglet={level},coglet_worker={level},coglet_worker::schema=off,coglet_worker::protocol=off",
            level = base_level
        );

        EnvFilter::new(filter_str)
    };

    let use_json = std::env::var("LOG_FORMAT").as_deref() == Ok("json");

    if use_json {
        let subscriber = tracing_subscriber::registry()
            .with(filter)
            .with(fmt::layer().json().with_writer(std::io::stderr));
        let _ = subscriber.try_init();
    } else {
        let subscriber = tracing_subscriber::registry()
            .with(filter)
            .with(fmt::layer().with_writer(std::io::stderr));
        let _ = subscriber.try_init();
    }
}

fn detect_version(py: Python<'_>) -> VersionInfo {
    let mut version = VersionInfo::new();

    if let Ok(sys) = py.import("sys")
        && let Ok(py_version) = sys.getattr("version")
        && let Ok(v) = py_version.extract::<String>()
    {
        let short_version = v.split_whitespace().next().unwrap_or(&v);
        version = version.with_python(short_version.to_string());
    }

    if let Ok(cog) = py.import("cog")
        && let Ok(cog_version) = cog.getattr("__version__")
        && let Ok(v) = cog_version.extract::<String>()
    {
        version = version.with_cog(v);
    }

    version
}

fn read_max_concurrency(py: Python<'_>) -> usize {
    let result = (|| -> PyResult<usize> {
        let cog_config = py.import("cog.config")?;
        let config_class = cog_config.getattr("Config")?;
        let config = config_class.call0()?;
        config.getattr("max_concurrency")?.extract::<usize>()
    })();

    match result {
        Ok(max) => max,
        Err(e) => {
            warn!(error = %e, "Failed to read concurrency config, using default=1");
            1
        }
    }
}

#[pyfunction]
#[pyo3(signature = (predictor_ref=None, host="0.0.0.0".to_string(), port=5000, await_explicit_shutdown=false, is_train=false))]
fn serve(
    py: Python<'_>,
    predictor_ref: Option<String>,
    host: String,
    port: u16,
    await_explicit_shutdown: bool,
    is_train: bool,
) -> PyResult<()> {
    init_tracing(false);

    info!("coglet {}", env!("CARGO_PKG_VERSION"));

    let config = ServerConfig {
        host,
        port,
        await_explicit_shutdown,
    };

    // Install Python SIGTERM handler if await_explicit_shutdown
    if await_explicit_shutdown {
        let signal_module = py.import("signal")?;
        let sigterm = signal_module.getattr("SIGTERM")?;
        let sig_ign = signal_module.getattr("SIG_IGN")?;
        signal_module.call_method1("signal", (sigterm, sig_ign))?;
        info!("await_explicit_shutdown: installed SIGTERM ignore handler");
    }

    let version = detect_version(py);

    let Some(pred_ref) = predictor_ref else {
        info!("No predictor specified, serving health endpoints only");
        let service = Arc::new(
            PredictionService::new_no_pool()
                .with_health(Health::Unknown)
                .with_version(version),
        );
        return py.detach(|| {
            let rt = tokio::runtime::Runtime::new()
                .map_err(|e| PyErr::new::<pyo3::exceptions::PyRuntimeError, _>(e.to_string()))?;
            rt.block_on(async {
                http_serve(config, service)
                    .await
                    .map_err(|e| PyErr::new::<pyo3::exceptions::PyRuntimeError, _>(e.to_string()))
            })
        });
    };

    info!(predictor_ref = %pred_ref, is_train, "Using subprocess isolation");
    serve_subprocess(py, pred_ref, config, version, is_train)
}

fn serve_subprocess(
    py: Python<'_>,
    pred_ref: String,
    config: ServerConfig,
    version: VersionInfo,
    is_train: bool,
) -> PyResult<()> {
    let max_concurrency = read_max_concurrency(py);
    info!(
        max_concurrency,
        "Configuring subprocess worker via orchestrator"
    );

    let orch_config = coglet_core::orchestrator::OrchestratorConfig::new(pred_ref)
        .with_num_slots(max_concurrency)
        .with_train(is_train);

    let service = Arc::new(
        PredictionService::new_no_pool()
            .with_health(Health::Starting)
            .with_version(version),
    );

    let service_clone = Arc::clone(&service);
    py.detach(|| {
        let rt = tokio::runtime::Runtime::new()
            .map_err(|e| PyErr::new::<pyo3::exceptions::PyRuntimeError, _>(e.to_string()))?;

        rt.block_on(async {
            let setup_result = SetupResult::starting();
            service_clone.set_setup_result(setup_result.clone()).await;

            let setup_service = Arc::clone(&service_clone);
            tokio::spawn(async move {
                info!("Spawning worker subprocess");
                match coglet_core::orchestrator::spawn_worker(orch_config).await {
                    Ok(ready) => {
                        debug!("Worker ready, configuring service");

                        let num_slots = ready.handle.slot_ids().len();

                        setup_service
                            .set_orchestrator(ready.pool, Arc::new(ready.handle))
                            .await;
                        setup_service.set_health(Health::Ready).await;
                        setup_service
                            .set_setup_result(setup_result.succeeded())
                            .await;

                        if let Some(s) = ready.schema {
                            setup_service.set_schema(s).await;
                        }

                        let mode = if is_train { "train" } else { "predict" };
                        info!(num_slots, mode, "Server ready");
                    }
                    Err(e) => {
                        error!(error = %e, "Worker initialization failed");
                        setup_service.set_health(Health::SetupFailed).await;
                        setup_service
                            .set_setup_result(setup_result.failed(e.to_string()))
                            .await;
                    }
                }
            });

            http_serve(config, service_clone)
                .await
                .map_err(|e| PyErr::new::<pyo3::exceptions::PyRuntimeError, _>(e.to_string()))
        })
    })
}

#[pyfunction]
fn _is_cancelable() -> bool {
    cancel::is_cancelable()
}

#[pyfunction]
#[pyo3(signature = ())]
fn _run_worker(py: Python<'_>) -> PyResult<()> {
    set_active();
    init_tracing(true);

    // Install SlotLogWriters for ContextVar-based log routing
    log_writer::install_slot_log_writers(py)?;

    // Install audit hook to protect stdout/stderr from user replacement
    if let Err(e) = audit::install_audit_hook(py) {
        warn!(error = %e, "Failed to install audit hook, stdout/stderr protection disabled");
    }

    // Install signal handler for cancellation
    if let Err(e) = cancel::install_signal_handler(py) {
        warn!(error = %e, "Failed to install signal handler, cancellation may not work");
    }

    info!(target: "coglet::worker", "Worker subprocess starting, waiting for Init message");

    py.detach(|| {
        let rt = tokio::runtime::Runtime::new()
            .map_err(|e| PyErr::new::<pyo3::exceptions::PyRuntimeError, _>(e.to_string()))?;

        rt.block_on(async {
            run_worker_with_init()
                .await
                .map_err(|e| PyErr::new::<pyo3::exceptions::PyRuntimeError, _>(e.to_string()))
        })
    })
}

async fn run_worker_with_init() -> Result<(), String> {
    use coglet_core::bridge::codec::JsonCodec;
    use coglet_core::bridge::protocol::ControlRequest;
    use futures::StreamExt;
    use tokio::io::stdin;
    use tokio_util::codec::FramedRead;

    let mut ctrl_reader = FramedRead::new(stdin(), JsonCodec::<ControlRequest>::new());

    let init_msg = ctrl_reader
        .next()
        .await
        .ok_or_else(|| "stdin closed before Init received".to_string())?
        .map_err(|e| format!("Failed to read Init: {}", e))?;

    let (predictor_ref, num_slots, transport_info, is_train, _is_async) = match init_msg {
        ControlRequest::Init {
            predictor_ref,
            num_slots,
            transport_info,
            is_train,
            is_async,
        } => (predictor_ref, num_slots, transport_info, is_train, is_async),
        other => {
            return Err(format!("Expected Init message, got: {:?}", other));
        }
    };

    info!(predictor_ref = %predictor_ref, num_slots, is_train, "Init received, connecting to transport");

    // Set transport info for run_worker
    let transport_json = serde_json::to_string(&transport_info)
        .map_err(|e| format!("Failed to serialize transport info: {}", e))?;
    // SAFETY: Single-threaded at this point, only read by our own code
    unsafe {
        std::env::set_var(
            coglet_core::bridge::transport::TRANSPORT_INFO_ENV,
            &transport_json,
        );
    }

    let handler = Arc::new(if is_train {
        worker_bridge::PythonPredictHandler::new_train(predictor_ref)
            .map_err(|e| format!("Failed to create handler: {}", e))?
    } else {
        worker_bridge::PythonPredictHandler::new(predictor_ref)
            .map_err(|e| format!("Failed to create handler: {}", e))?
    });

    // Setup log hook: registers a global sender for control channel logs
    // This lives for the entire worker lifetime (setup + subprocess output)
    let setup_log_hook: coglet_core::SetupLogHook = Box::new(|tx| {
        let sender = Arc::new(log_writer::ControlChannelLogSender::new(tx));
        log_writer::register_control_channel_sender(sender);
        // Cleanup is a no-op: sender stays registered for worker lifetime
        Box::new(|| {})
    });

    let config = coglet_core::WorkerConfig {
        num_slots,
        setup_log_hook: Some(setup_log_hook),
    };

    coglet_core::run_worker(handler, config)
        .await
        .map_err(|e| format!("Worker error: {}", e))
}

#[pymodule]
fn coglet(m: &Bound<'_, PyModule>) -> PyResult<()> {
    // Version from Cargo.toml
    m.add("__version__", env!("CARGO_PKG_VERSION"))?;

    // Core functions
    m.add_function(wrap_pyfunction!(active, m)?)?;
    m.add_function(wrap_pyfunction!(serve, m)?)?;
    m.add_function(wrap_pyfunction!(_is_cancelable, m)?)?;
    m.add_function(wrap_pyfunction!(_run_worker, m)?)?;

    // Audit hook helpers for stdout/stderr protection (internal use by audit hook)
    m.add_function(wrap_pyfunction!(audit::_is_slot_log_writer, m)?)?;
    m.add_function(wrap_pyfunction!(audit::_is_tee_writer, m)?)?;
    m.add_function(wrap_pyfunction!(audit::_get_inner_writer, m)?)?;
    m.add_function(wrap_pyfunction!(audit::_create_tee_writer, m)?)?;

    // Export classes (needed for isinstance checks in audit hook)
    m.add_class::<log_writer::SlotLogWriter>()?;
    m.add_class::<audit::TeeWriter>()?;

    Ok(())
}
