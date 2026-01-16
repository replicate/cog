//! coglet-python: PyO3 bindings for coglet.

mod audit;
mod cancel;
mod input;
mod log_writer;
mod output;
mod predictor;
mod worker_bridge;

pub use audit::TeeWriter;
pub use log_writer::{PredictionLogGuard, SetupLogSender, SlotLogWriter};

use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::Arc;

use pyo3::prelude::*;

use tracing::{error, info, warn};

/// Global flag indicating whether we're running inside a worker subprocess.
/// - `false` in the parent process (serve() spawns worker)
/// - `true` in the worker subprocess (_run_worker() sets this)
static ACTIVE: AtomicBool = AtomicBool::new(false);

/// Set the active flag (called at start of _run_worker).
fn set_active() {
    ACTIVE.store(true, Ordering::SeqCst);
}

/// Get the active flag.
///
/// Returns True when running inside a worker subprocess, False in the parent.
/// Call as: `coglet.active()`
#[pyfunction]
fn active() -> bool {
    ACTIVE.load(Ordering::SeqCst)
}
use tracing_subscriber::{EnvFilter, fmt, layer::SubscriberExt, util::SubscriberInitExt};

use coglet_core::{
    Health, PredictionService,
    SetupResult, VersionInfo, ServerConfig, serve as http_serve,
};

/// Initialize tracing with COG_LOG and LOG_FORMAT support.
///
/// Environment variables:
/// - `COG_LOG`: Sets log level for coglet targets (info/debug). No trace support.
/// - `LOG_FORMAT`: Set to "json" for JSON output format.
/// - `RUST_LOG`: Standard tracing filter, takes precedence for granular control.
///
/// Opt-in only targets (require explicit RUST_LOG to enable):
/// - `coglet_worker::schema` - Full OpenAPI schema dump
/// - `coglet_worker::protocol` - Raw protocol frame logging
fn init_tracing(_to_stderr: bool) {
    // Check if RUST_LOG is set - if so, use it directly for full control
    let filter = if std::env::var("RUST_LOG").is_ok() {
        EnvFilter::from_default_env()
    } else {
        // Build filter from COG_LOG
        let base_level = match std::env::var("COG_LOG").as_deref() {
            Ok("debug") => "debug",
            Ok("warn") | Ok("warning") => "warn",
            Ok("error") => "error",
            _ => "info", // default
        };

        // Build filter: coglet and coglet_worker at the configured level,
        // but opt-in targets are off by default
        let filter_str = format!(
            "coglet={level},coglet_worker={level},coglet_worker::schema=off,coglet_worker::protocol=off",
            level = base_level
        );

        EnvFilter::new(filter_str)
    };

    // Check if JSON format requested
    let use_json = std::env::var("LOG_FORMAT").as_deref() == Ok("json");

    // Build subscriber - always write to stderr (stdout may be protocol pipe)
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

/// Detect Python and cog SDK versions.
fn detect_version(py: Python<'_>) -> VersionInfo {
    let mut version = VersionInfo::new();

    // Get Python version
    if let Ok(sys) = py.import("sys")
        && let Ok(py_version) = sys.getattr("version")
        && let Ok(v) = py_version.extract::<String>()
    {
        // sys.version is like "3.13.1 (main, Dec 18 2024, ...)"
        // Take just the version number
        let short_version = v.split_whitespace().next().unwrap_or(&v);
        version = version.with_python(short_version.to_string());
    }

    // Get cog SDK version
    if let Ok(cog) = py.import("cog")
        && let Ok(cog_version) = cog.getattr("__version__")
        && let Ok(v) = cog_version.extract::<String>()
    {
        version = version.with_cog(v);
    }

    version
}

/// Read max concurrency from cog.yaml (or return default of 1).
fn read_max_concurrency(py: Python<'_>) -> usize {
    // Try to read from cog.yaml via cog.config.Config.max_concurrency
    let result = (|| -> PyResult<usize> {
        let cog_config = py.import("cog.config")?;
        let config_class = cog_config.getattr("Config")?;
        let config = config_class.call0()?;

        // max_concurrency property reads from cog.yaml concurrency.max
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

/// Start the coglet HTTP server with a predictor.
///
/// All predictors (sync and async) use subprocess isolation for:
/// - Crash isolation (subprocess crash doesn't kill server)
/// - Memory isolation (clean slate per worker)
/// - Flexibility (transport abstraction enables sidecar/remote workers)
///
/// Args:
///     predictor_ref: Path to predictor like "predict.py:Predictor"
///     host: Host to bind to (default "0.0.0.0")
///     port: Port to listen on (default 5000)
///     await_explicit_shutdown: If True, ignore SIGTERM and wait for SIGINT or /shutdown
///     is_train: If True, call train() instead of predict() for predictions
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
    // Initialize tracing with COG_LOG and LOG_FORMAT support
    init_tracing(false);

    // Log version at startup
    info!("coglet {}", env!("CARGO_PKG_VERSION"));

    let config = ServerConfig {
        host,
        port,
        await_explicit_shutdown,
    };

    // If await_explicit_shutdown, install Python signal handler to ignore SIGTERM
    if await_explicit_shutdown {
        let signal_module = py.import("signal")?;
        let sigterm = signal_module.getattr("SIGTERM")?;
        let sig_ign = signal_module.getattr("SIG_IGN")?;
        signal_module.call_method1("signal", (sigterm, sig_ign))?;
        info!("await_explicit_shutdown: installed SIGTERM ignore handler");
    }

    // Detect version info (do this in parent process)
    let version = detect_version(py);

    // If no predictor, just serve health endpoints
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

/// Serve with subprocess worker using the orchestrator.
fn serve_subprocess(
    py: Python<'_>,
    pred_ref: String,
    config: ServerConfig,
    version: VersionInfo,
    is_train: bool,
) -> PyResult<()> {
    // Read max concurrency from cog.yaml
    let max_concurrency = read_max_concurrency(py);
    info!(max_concurrency, "Configuring subprocess worker via orchestrator");

    // Create orchestrator config
    let orch_config = coglet_core::OrchestratorConfig::new(pred_ref)
        .with_num_slots(max_concurrency)
        .with_train(is_train);

    // Create service without pool (will be set when worker is ready)
    let service = Arc::new(
        PredictionService::new_no_pool()
            .with_health(Health::Starting)
            .with_version(version),
    );

    // Release GIL and run server
    let service_clone = Arc::clone(&service);
    py.detach(|| {
        let rt = tokio::runtime::Runtime::new()
            .map_err(|e| PyErr::new::<pyo3::exceptions::PyRuntimeError, _>(e.to_string()))?;

        rt.block_on(async {
            // Track setup timing
            let setup_result = SetupResult::starting();
            service_clone.set_setup_result(setup_result.clone()).await;

            // Spawn worker setup as background task
            // HTTP starts immediately, returns 503 until worker is ready
            let setup_service = Arc::clone(&service_clone);
            tokio::spawn(async move {
                info!("Spawning worker subprocess");
                match coglet_core::spawn_worker(orch_config).await {
                    Ok(ready) => {
                        info!("Worker ready, configuring service");

                        // CRITICAL ORDER: pool → orchestrator → health=Ready
                        // This prevents race conditions where predictions arrive before routing is set up
                        setup_service.set_pool(ready.pool).await;
                        setup_service.set_orchestrator(Arc::new(ready.handle)).await;
                        setup_service.set_health(Health::Ready).await;
                        setup_service
                            .set_setup_result(setup_result.succeeded())
                            .await;

                        // Set OpenAPI schema if available
                        if let Some(s) = ready.schema {
                            setup_service.set_schema(s).await;
                        }

                        info!("Server ready to accept predictions");
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

            // Start HTTP immediately - returns 503 for predictions until worker is ready
            http_serve(config, service_clone)
                .await
                .map_err(|e| PyErr::new::<pyo3::exceptions::PyRuntimeError, _>(e.to_string()))
        })
    })
}

/// Check if we're in a cancelable section (called from Python signal handler).
#[pyfunction]
fn _is_cancelable() -> bool {
    cancel::is_cancelable()
}

/// Run as a worker subprocess.
///
/// This function is called when coglet is spawned as a worker subprocess by the orchestrator.
/// It reads the Init message from stdin, runs setup, then processes predictions.
/// Exits when stdin closes (parent died) or shutdown requested.
///
/// Called via: `python -c "import coglet; coglet._run_worker()"`
///
/// The Init message contains: predictor_ref, num_slots, transport_info, is_train, is_async
#[pyfunction]
#[pyo3(signature = ())]
fn _run_worker(py: Python<'_>) -> PyResult<()> {
    // Mark that we're running in worker mode
    set_active();

    // Initialize tracing with COG_LOG and LOG_FORMAT support
    // Always writes to stderr since stdout is the protocol pipe
    init_tracing(true);

    // Install SlotLogWriters for ContextVar-based log routing
    // This replaces sys.stdout/stderr with our writers that route via SlotId
    log_writer::install_slot_log_writers(py)?;

    // Install audit hook to protect stdout/stderr from user replacement
    // If user code replaces them, we tee to both our routing and their stream
    if let Err(e) = audit::install_audit_hook(py) {
        warn!(error = %e, "Failed to install audit hook, stdout/stderr protection disabled");
    }

    // Install SIGUSR1 signal handler for sync predictor cancellation
    // This allows cancel requests to interrupt blocking Python code
    if let Err(e) = cancel::install_signal_handler(py) {
        warn!(error = %e, "Failed to install signal handler, cancellation may not work");
    }

    info!("Worker subprocess starting, waiting for Init message");

    // Run worker event loop - reads Init from stdin, connects to transport, runs setup
    // IMPORTANT: Release the GIL before blocking, so spawned tasks can acquire it
    py.detach(|| {
        let rt = tokio::runtime::Runtime::new()
            .map_err(|e| PyErr::new::<pyo3::exceptions::PyRuntimeError, _>(e.to_string()))?;

        rt.block_on(async {
            run_worker_with_init().await
                .map_err(|e| PyErr::new::<pyo3::exceptions::PyRuntimeError, _>(e.to_string()))
        })
    })
}

/// Internal worker entry point that reads Init from stdin and runs the worker loop.
async fn run_worker_with_init() -> Result<(), String> {
    use futures::StreamExt;
    use tokio::io::stdin;
    use tokio_util::codec::FramedRead;
    use coglet_core::bridge::codec::JsonCodec;
    use coglet_core::bridge::protocol::ControlRequest;

    // Read Init message from stdin
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

    // Set transport info in environment for legacy run_worker compatibility
    // (run_worker reads from COGLET_TRANSPORT_INFO)
    let transport_json = serde_json::to_string(&transport_info)
        .map_err(|e| format!("Failed to serialize transport info: {}", e))?;
    // SAFETY: We're single-threaded at this point (before spawning any tasks)
    // and this env var is only read by our own code in the same process.
    unsafe {
        std::env::set_var(coglet_core::bridge::transport::TRANSPORT_INFO_ENV, &transport_json);
    }

    // Create handler in appropriate mode
    let handler = Arc::new(if is_train {
        worker_bridge::PythonPredictHandler::new_train(predictor_ref)
    } else {
        worker_bridge::PythonPredictHandler::new(predictor_ref)
    });

    // Setup log hook: registers a global sender so SlotLogWriter can route setup logs
    let setup_log_hook: coglet_core::SetupLogHook = Box::new(|tx| {
        let sender = Arc::new(SetupLogSender::new(tx));
        log_writer::register_setup_sender(sender);
        Box::new(log_writer::unregister_setup_sender)
    });

    let config = coglet_core::WorkerConfig {
        num_slots,
        setup_log_hook: Some(setup_log_hook),
    };

    // Run worker event loop
    coglet_core::run_worker(handler, config)
        .await
        .map_err(|e| format!("Worker error: {}", e))
}

/// coglet Python module.
#[pymodule]
fn coglet(m: &Bound<'_, PyModule>) -> PyResult<()> {
    // Version from Cargo.toml
    m.add("__version__", env!("CARGO_PKG_VERSION"))?;

    // active() function - returns True in worker subprocess, False in parent
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
