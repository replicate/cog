//! coglet-python: PyO3 bindings for coglet.

mod cancel;
mod input;
mod log_writer;
mod output;
mod predictor;
mod worker_bridge;

pub use log_writer::{SlotLogGuard, SlotLogWriter, SlotSender};

use std::sync::Arc;
use std::time::Duration;

use pyo3::prelude::*;
use tokio::sync::Mutex;

use tracing::{error, info, warn};
use tracing_subscriber::EnvFilter;

use coglet_core::{Health, PredictFuture, PredictionError, PredictionOutput, PredictionResult, PredictionService, SetupResult, VersionInfo};
use coglet_transport::{serve as http_serve, ServerConfig};
use coglet_worker::{SlotResponse, SpawnConfig, Worker};

use predictor::PythonPredictor;

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
    async fn predict(&self, id: String, input: serde_json::Value) -> Result<PredictionResult, PredictionError> {
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
                    // Accumulate logs (streamed during prediction)
                    logs.push_str(&data);
                }
                Ok(SlotResponse::Output { output }) => {
                    // Streaming output (for generators)
                    // For now, just keep the last one
                    final_output = Some(output);
                }
                Ok(SlotResponse::Done { output, predict_time, .. }) => {
                    // Prediction completed successfully
                    if let Some(o) = output {
                        final_output = Some(o);
                    }
                    return Ok(PredictionResult {
                        output: PredictionOutput::Single(final_output.unwrap_or(serde_json::Value::Null)),
                        predict_time: Some(Duration::from_secs_f64(predict_time)),
                        logs,
                    });
                }
                Ok(SlotResponse::Failed { error, .. }) => {
                    return Err(PredictionError::Failed(error));
                }
                Ok(SlotResponse::Cancelled { .. }) => {
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

/// Read max concurrency from cog.yaml (or return default).
fn read_max_concurrency(py: Python<'_>) -> usize {
    // Try to read from cog.yaml via cog.config.Config.max_concurrency
    let result = (|| -> PyResult<usize> {
        let cog_config = py.import("cog.config")?;
        let config_class = cog_config.getattr("Config")?;
        let config = config_class.call0()?;
        
        // max_concurrency property reads from cog.yaml concurrency.max
        let max = config.getattr("max_concurrency")?.extract::<usize>()?;
        info!(max_concurrency = max, "Read max_concurrency from cog.yaml");
        Ok(max)
    })();
    
    match result {
        Ok(max) => max,
        Err(e) => {
            warn!(error = %e, "Failed to read concurrency config from cog.yaml, using default=1");
            1
        }
    }
}

/// Start the coglet HTTP server with a predictor.
///
/// For sync predictors:
/// - Uses subprocess isolation (crash/memory isolation)
/// - max_concurrency = 1 (Python GIL serializes execution anyway)
///
/// For async predictors:
/// - Uses in-process execution with true async concurrency
/// - Reads max_concurrency from cog.yaml (concurrency.max)
/// - Multiple predictions can run concurrently via asyncio
/// 
/// Args:
///     predictor_ref: Path to predictor like "predict.py:Predictor"
///     host: Host to bind to (default "0.0.0.0")
///     port: Port to listen on (default 5000)
///     await_explicit_shutdown: If True, ignore SIGTERM and wait for SIGINT or /shutdown
///     is_train: If True, call train() instead of predict() for predictions
#[pyfunction]
#[pyo3(signature = (predictor_ref=None, host="0.0.0.0".to_string(), port=5000, await_explicit_shutdown=false, is_train=false))]
fn serve(py: Python<'_>, predictor_ref: Option<String>, host: String, port: u16, await_explicit_shutdown: bool, is_train: bool) -> PyResult<()> {
    // Initialize tracing (ignore if already initialized)
    let _ = tracing_subscriber::fmt()
        .with_env_filter(EnvFilter::from_default_env())
        .try_init();

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
            PredictionService::new(1)
                .with_health(Health::Unknown)
                .with_version(version)
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

    // Read max concurrency from cog.yaml
    let max_concurrency = read_max_concurrency(py);
    
    // Load predictor to detect if it's async
    info!(predictor_ref = %pred_ref, "Loading predictor to detect async");
    let predictor = PythonPredictor::load(py, &pred_ref)?;
    let is_async = predictor.is_async();
    
    // For async predictors with concurrency > 1, use in-process execution
    // This enables true concurrent predictions via Python asyncio
    if is_async && max_concurrency > 1 {
        info!(predictor_ref = %pred_ref, max_concurrency, "Async predictor detected, using in-process execution");
        serve_async_inprocess(py, predictor, pred_ref, config, version, max_concurrency, is_train)
    } else {
        // For sync predictors (or async with concurrency=1), use subprocess isolation
        // Drop the predictor - it will be reloaded in subprocess
        drop(predictor);
        info!(predictor_ref = %pred_ref, is_train, is_async, "Using subprocess isolation");
        serve_subprocess(py, pred_ref, config, version, is_train)
    }
}

/// Serve with in-process async predictor.
/// 
/// This is used for async predictors where we want true concurrency.
/// The predictor runs in the same process as the server, but predictions
/// are executed concurrently via Python asyncio and Rust tokio.
fn serve_async_inprocess(
    py: Python<'_>,
    predictor: PythonPredictor,
    pred_ref: String,
    config: ServerConfig,
    version: VersionInfo,
    max_concurrency: usize,
    _is_train: bool,  // TODO: support training mode
) -> PyResult<()> {
    // Run setup synchronously before starting server
    info!("Running predictor setup");
    predictor.setup(py)?;
    
    // Get OpenAPI schema
    let schema = predictor.schema();
    
    // Wrap predictor in Arc for sharing across async tasks
    let predictor = Arc::new(predictor);
    
    // Create prediction service with configured concurrency
    let service = PredictionService::new(max_concurrency)
        .with_health(Health::Ready)  // Ready immediately after setup
        .with_version(version);
    
    // Create async predict function using predict_async
    let predictor_clone = Arc::clone(&predictor);
    let async_predict_fn = Arc::new(move |input: serde_json::Value| -> PredictFuture {
        let pred = Arc::clone(&predictor_clone);
        Box::pin(async move { pred.predict_async(input).await })
    }) as Arc<coglet_core::AsyncPredictFn>;
    
    let service = Arc::new(service.with_async_predict_fn(async_predict_fn));
    
    // Set schema if available
    let service_for_schema = Arc::clone(&service);
    let schema_clone = schema.clone();
    
    // Release GIL and run server
    py.detach(move || {
        let rt = tokio::runtime::Runtime::new()
            .map_err(|e| PyErr::new::<pyo3::exceptions::PyRuntimeError, _>(e.to_string()))?;
        
        rt.block_on(async {
            // Set schema
            if let Some(s) = schema_clone {
                info!("Setting OpenAPI schema");
                service_for_schema.set_schema(s).await;
            }
            
            // Set setup result as succeeded
            service_for_schema.set_setup_result(SetupResult::starting().succeeded()).await;
            
            info!(max_concurrency, "Async server ready, starting HTTP server");
            
            http_serve(config, service_for_schema)
                .await
                .map_err(|e| PyErr::new::<pyo3::exceptions::PyRuntimeError, _>(e.to_string()))
        })
    })
}

/// Serve with subprocess worker.
fn serve_subprocess(
    py: Python<'_>,
    pred_ref: String,
    config: ServerConfig,
    version: VersionInfo,
    is_train: bool,
) -> PyResult<()> {
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
        max_concurrency: 1, // Sync predictors use single slot
        env,
    };

    // Create worker handle
    let worker = Arc::new(WorkerHandle::new(pred_ref, spawn_config));

    // Sync predictors use max_concurrency=1
    let max_concurrency = 1;
    info!(max_concurrency, "Configuring sync predictor with subprocess");

    // Start with Starting health - will become Ready after worker init
    let service = PredictionService::new(max_concurrency)
        .with_health(Health::Starting)
        .with_version(version);

    // Create async predict function that routes to worker
    let worker_clone = Arc::clone(&worker);
    let predict_counter = Arc::new(std::sync::atomic::AtomicU64::new(0));
    let async_predict_fn = Arc::new(move |input: serde_json::Value| -> PredictFuture {
        let worker = Arc::clone(&worker_clone);
        let id = format!("pred_{}", predict_counter.fetch_add(1, std::sync::atomic::Ordering::Relaxed));
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
                    service_clone.set_setup_result(setup_result.succeeded()).await;
                    
                    // Set OpenAPI schema if available
                    if let Some(s) = schema {
                        info!("Setting OpenAPI schema from worker");
                        service_clone.set_schema(s).await;
                    }
                }
                Err(e) => {
                    error!(error = %e, "Worker initialization failed");
                    service_clone.set_health(Health::SetupFailed).await;
                    service_clone.set_setup_result(setup_result.failed(e.clone())).await;
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
fn _run_worker(predictor_ref: String) -> PyResult<()> {
    // Initialize tracing
    let _ = tracing_subscriber::fmt()
        .with_env_filter(tracing_subscriber::EnvFilter::from_default_env())
        .with_writer(std::io::stderr) // Log to stderr, stdout is for protocol
        .try_init();

    // Check if we're in training mode
    let is_train = std::env::var("COGLET_IS_TRAIN")
        .map(|v| v == "true" || v == "1")
        .unwrap_or(false);

    info!(predictor_ref = %predictor_ref, is_train, "Worker starting");

    // Create handler in appropriate mode
    let handler = Arc::new(if is_train {
        worker_bridge::PythonPredictHandler::new_train(predictor_ref)
    } else {
        worker_bridge::PythonPredictHandler::new(predictor_ref)
    });
    let config = coglet_worker::WorkerConfig::default();

    // Run worker event loop
    let rt = tokio::runtime::Runtime::new()
        .map_err(|e| PyErr::new::<pyo3::exceptions::PyRuntimeError, _>(e.to_string()))?;

    rt.block_on(async {
        coglet_worker::run_worker(handler, config)
            .await
            .map_err(|e| PyErr::new::<pyo3::exceptions::PyRuntimeError, _>(e.to_string()))
    })
}

/// coglet Python module.
#[pymodule]
fn coglet(m: &Bound<'_, PyModule>) -> PyResult<()> {
    m.add_function(wrap_pyfunction!(serve, m)?)?;
    m.add_function(wrap_pyfunction!(_is_cancelable, m)?)?;
    m.add_function(wrap_pyfunction!(_run_worker, m)?)?;
    Ok(())
}
