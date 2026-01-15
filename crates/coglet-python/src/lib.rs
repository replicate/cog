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

use std::sync::Arc;
use std::time::Duration;

use pyo3::prelude::*;
use tokio::sync::Mutex;

use tracing::{error, info, warn};
use tracing_subscriber::{EnvFilter, fmt, layer::SubscriberExt, util::SubscriberInitExt};

use coglet_core::{
    Health, PredictFuture, PredictionError, PredictionOutput, PredictionResult, PredictionService,
    SetupResult, VersionInfo, PermitPool, ServerConfig, serve as http_serve,
    SpawnConfig, Worker,
};
use coglet_core::bridge::protocol::SlotResponse;

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

/// Wrapper around Worker that handles respawning on crash.
struct WorkerHandle {
    worker: Mutex<Option<Worker>>,
    predictor_ref: String,
    spawn_config: SpawnConfig,
    ready_timeout: Duration,
}

impl WorkerHandle {
    fn new(predictor_ref: String, spawn_config: SpawnConfig) -> Self {
        Self {
            worker: Mutex::new(None),
            predictor_ref,
            spawn_config,
            ready_timeout: Duration::from_secs(300), // 5 min for setup
        }
    }

    /// Initialize the worker (spawn subprocess, wait for ready).
    /// Returns the OpenAPI schema if available.
    async fn init(&self) -> Result<Option<serde_json::Value>, String> {
        let mut guard = self.worker.lock().await;
        if guard.is_some() {
            // Already initialized, return cached schema
            return Ok(guard.as_ref().and_then(|w| w.schema().cloned()));
        }

        info!(predictor_ref = %self.predictor_ref, "Spawning worker subprocess");
        let worker = Worker::spawn(&self.predictor_ref, self.ready_timeout, &self.spawn_config)
            .await
            .map_err(|e| format!("Failed to spawn worker: {}", e))?;

        let schema = worker.schema().cloned();
        *guard = Some(worker);
        Ok(schema)
    }

    /// Run a prediction, respawning worker if needed.
    ///
    /// Uses slot 0 since sync predictors only have one slot.
    async fn predict(
        &self,
        id: String,
        input: serde_json::Value,
    ) -> Result<PredictionResult, PredictionError> {
        let mut guard = self.worker.lock().await;

        // If no worker, try to spawn one
        if guard.is_none() {
            warn!("Worker not initialized, spawning...");
            let worker = Worker::spawn(&self.predictor_ref, self.ready_timeout, &self.spawn_config)
                .await
                .map_err(|e| PredictionError::Failed(format!("Failed to spawn worker: {}", e)))?;
            *guard = Some(worker);
        }

        let worker = guard.as_mut().unwrap();
        let slot = 0; // Sync predictors use single slot

        info!(prediction_id = %id, "Starting prediction");

        // Send prediction request on slot socket
        if let Err(e) = worker.send_predict(slot, id.clone(), input).await {
            error!(error = %e, "Failed to send prediction");
            *guard = None;
            return Err(PredictionError::Failed(format!("Worker error: {}", e)));
        }

        // Collect logs and wait for completion
        let mut logs = String::new();
        let mut final_output = None;

        loop {
            match worker.recv_slot(slot).await {
                Ok(SlotResponse::Log { data, .. }) => {
                    // Emit logs via tracing with prediction target
                    for line in data.lines() {
                        tracing::info!(target: "coglet_core::prediction", prediction_id = %id, "{}", line);
                    }
                    // Accumulate for response
                    logs.push_str(&data);
                }
                Ok(SlotResponse::Output { output }) => {
                    // Streaming output (for generators)
                    // For now, just keep the last one
                    final_output = Some(output);
                }
                Ok(SlotResponse::Done {
                    output,
                    predict_time,
                    ..
                }) => {
                    // Prediction completed successfully
                    if let Some(o) = output {
                        final_output = Some(o);
                    }
                    info!(prediction_id = %id, predict_time, "Prediction succeeded");
                    return Ok(PredictionResult {
                        output: PredictionOutput::Single(
                            final_output.unwrap_or(serde_json::Value::Null),
                        ),
                        predict_time: Some(Duration::from_secs_f64(predict_time)),
                        logs,
                    });
                }
                Ok(SlotResponse::Failed { error, .. }) => {
                    warn!(prediction_id = %id, %error, "Prediction failed");
                    return Err(PredictionError::Failed(error));
                }
                Ok(SlotResponse::Cancelled { .. }) => {
                    info!(prediction_id = %id, "Prediction cancelled");
                    return Err(PredictionError::Cancelled);
                }
                Err(e) => {
                    // Worker might have crashed - mark for respawn
                    error!(error = %e, "Worker error, will respawn on next request");
                    *guard = None;
                    return Err(PredictionError::Failed(format!("Worker error: {}", e)));
                }
            }
        }
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
        let pool = Arc::new(PermitPool::new(1));
        let service = Arc::new(
            PredictionService::new(pool)
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

/// Serve with subprocess worker.
fn serve_subprocess(
    py: Python<'_>,
    pred_ref: String,
    config: ServerConfig,
    version: VersionInfo,
    is_train: bool,
) -> PyResult<()> {
    // Read max concurrency from cog.yaml
    let max_concurrency = read_max_concurrency(py);
    info!(max_concurrency, "Configuring subprocess worker");

    // Get Python executable for worker subprocess
    let python_exe = std::env::var("COG_PYTHON_EXE")
        .or_else(|_| {
            Python::attach(|py| {
                py.import("sys")
                    .and_then(|sys| sys.getattr("executable"))
                    .and_then(|exe| exe.extract::<String>())
            })
        })
        .unwrap_or_else(|_| "python3".to_string());

    let mut env = vec![];
    if is_train {
        env.push(("COGLET_IS_TRAIN".to_string(), "true".to_string()));
    }
    let spawn_config = SpawnConfig {
        python_exe,
        max_concurrency,
        env,
    };

    // Create worker handle
    let worker = Arc::new(WorkerHandle::new(pred_ref, spawn_config));

    // Start with Starting health - will become Ready after worker init
    let pool = Arc::new(PermitPool::new(max_concurrency));
    let service = PredictionService::new(pool)
        .with_health(Health::Starting)
        .with_version(version);

    // Create async predict function that routes to worker
    let worker_clone = Arc::clone(&worker);
    let predict_counter = Arc::new(std::sync::atomic::AtomicU64::new(0));
    let async_predict_fn = Arc::new(move |input: serde_json::Value| -> PredictFuture {
        let worker = Arc::clone(&worker_clone);
        let id = format!(
            "pred_{}",
            predict_counter.fetch_add(1, std::sync::atomic::Ordering::Relaxed)
        );
        Box::pin(async move { worker.predict(id, input).await })
    }) as Arc<coglet_core::AsyncPredictFn>;

    let service = Arc::new(service.with_async_predict_fn(async_predict_fn));

    // Release GIL and run server
    let service_clone = Arc::clone(&service);
    py.detach(|| {
        let rt = tokio::runtime::Runtime::new()
            .map_err(|e| PyErr::new::<pyo3::exceptions::PyRuntimeError, _>(e.to_string()))?;

        rt.block_on(async {
            // Track setup timing
            let setup_result = SetupResult::starting();
            service_clone.set_setup_result(setup_result.clone()).await;

            // Initialize worker (spawns subprocess, runs setup)
            match worker.init().await {
                Ok(schema) => {
                    info!("Worker initialized, server ready");
                    service_clone.set_health(Health::Ready).await;
                    service_clone
                        .set_setup_result(setup_result.succeeded())
                        .await;

                    // Set OpenAPI schema if available
                    if let Some(s) = schema {
                        service_clone.set_schema(s).await;
                    }
                }
                Err(e) => {
                    error!(error = %e, "Worker initialization failed");
                    service_clone.set_health(Health::SetupFailed).await;
                    service_clone
                        .set_setup_result(setup_result.failed(e.clone()))
                        .await;
                }
            }

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
/// This function is called when coglet is spawned as a worker.
/// It reads requests from stdin, runs predictions, writes responses to stdout.
/// Exits when stdin closes (parent died) or shutdown requested.
///
/// Environment variables:
/// - COGLET_IS_TRAIN: If "true", call train() instead of predict()
#[pyfunction]
fn _run_worker(py: Python<'_>, predictor_ref: String, num_slots: usize) -> PyResult<()> {
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

    // Check if we're in training mode
    let is_train = std::env::var("COGLET_IS_TRAIN")
        .map(|v| v == "true" || v == "1")
        .unwrap_or(false);

    info!(predictor_ref = %predictor_ref, is_train, num_slots, "Worker starting");

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
    // IMPORTANT: Release the GIL before blocking, so spawned tasks can acquire it
    py.detach(|| {
        let rt = tokio::runtime::Runtime::new()
            .map_err(|e| PyErr::new::<pyo3::exceptions::PyRuntimeError, _>(e.to_string()))?;

        rt.block_on(async {
            coglet_core::run_worker(handler, config)
                .await
                .map_err(|e| PyErr::new::<pyo3::exceptions::PyRuntimeError, _>(e.to_string()))
        })
    })
}

/// coglet Python module.
#[pymodule]
fn coglet(m: &Bound<'_, PyModule>) -> PyResult<()> {
    // Version from Cargo.toml
    m.add("__version__", env!("CARGO_PKG_VERSION"))?;

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
