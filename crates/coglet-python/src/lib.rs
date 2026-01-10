//! coglet-python: PyO3 bindings for coglet.

mod cancel;
mod input;
mod output;
mod predictor;
mod worker_bridge;

use std::sync::Arc;
use std::time::Duration;

use pyo3::prelude::*;
use tokio::sync::Mutex;

use tracing::{error, info, warn};
use tracing_subscriber::EnvFilter;

use coglet_core::{Health, PredictFuture, PredictionError, PredictionOutput, PredictionResult, VersionInfo};
use coglet_http::{serve as http_serve, AppState, ServerConfig};
use coglet_worker::{SpawnConfig, Worker, WorkerResponse};

use crate::predictor::PythonPredictor;

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
    async fn init(&self) -> Result<(), String> {
        let mut guard = self.worker.lock().await;
        if guard.is_some() {
            return Ok(()); // Already initialized
        }

        info!(predictor_ref = %self.predictor_ref, "Spawning worker subprocess");
        let worker = Worker::spawn(&self.predictor_ref, self.ready_timeout, &self.spawn_config)
            .await
            .map_err(|e| format!("Failed to spawn worker: {}", e))?;
        
        *guard = Some(worker);
        Ok(())
    }

    /// Run a prediction, respawning worker if needed.
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

        // Send prediction to worker
        // Use generous timeout for predictions (model inference can be slow)
        let response = worker.predict(id.clone(), input).await;

        match response {
            Ok(resp) => match resp {
                WorkerResponse::Output { output, status, .. } => {
                    use coglet_worker::PredictionStatus;
                    match status {
                        PredictionStatus::Succeeded => Ok(PredictionResult {
                            output: PredictionOutput::Single(output),
                            predict_time: None,
                        }),
                        PredictionStatus::Canceled => Err(PredictionError::Cancelled),
                        _ => Err(PredictionError::Failed(format!("Prediction status: {:?}", status))),
                    }
                }
                WorkerResponse::Error { error, .. } => {
                    Err(PredictionError::Failed(error))
                }
                WorkerResponse::Cancelled { .. } => {
                    Err(PredictionError::Cancelled)
                }
                other => {
                    Err(PredictionError::Failed(format!("Unexpected response: {:?}", other)))
                }
            },
            Err(e) => {
                // Worker might have crashed - mark for respawn
                error!(error = %e, "Worker error, will respawn on next request");
                *guard = None;
                Err(PredictionError::Failed(format!("Worker error: {}", e)))
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

/// Start the coglet HTTP server with a predictor.
///
/// This function blocks until the server shuts down.
///
/// For sync predictors, uses subprocess isolation for true cancellation support.
/// For async predictors, runs in-process for lower latency.
#[pyfunction]
#[pyo3(signature = (predictor_ref=None, host="0.0.0.0".to_string(), port=5000))]
fn serve(py: Python<'_>, predictor_ref: Option<String>, host: String, port: u16) -> PyResult<()> {
    // Initialize tracing (ignore if already initialized)
    let _ = tracing_subscriber::fmt()
        .with_env_filter(EnvFilter::from_default_env())
        .try_init();

    let config = ServerConfig {
        host,
        port,
        max_concurrency: 1, // Will be overridden based on predictor type
    };

    // Detect version info (do this in parent process)
    let version = detect_version(py);

    // If no predictor, just serve health endpoints
    let Some(pred_ref) = predictor_ref else {
        info!("No predictor specified, serving health endpoints only");
        let app_state = Arc::new(
            AppState::new(1)
                .with_health(Health::Unknown)
                .with_version(version)
        );
        return py.detach(|| {
            let rt = tokio::runtime::Runtime::new()
                .map_err(|e| PyErr::new::<pyo3::exceptions::PyRuntimeError, _>(e.to_string()))?;
            rt.block_on(async {
                http_serve(config, app_state)
                    .await
                    .map_err(|e| PyErr::new::<pyo3::exceptions::PyRuntimeError, _>(e.to_string()))
            })
        });
    };

    // Check if predictor is async (need to load briefly to check)
    info!(predictor_ref = %pred_ref, "Checking predictor type");
    let is_async_predictor = match PythonPredictor::load(py, &pred_ref) {
        Ok(p) => p.is_async(),
        Err(e) => {
            error!(error = %e, "Failed to load predictor");
            return Err(e);
        }
    };

    if is_async_predictor {
        // Async predictor: run in-process for lower latency
        info!("Async predictor - running in-process");
        serve_inprocess(py, pred_ref, config, version)
    } else {
        // Sync predictor: use subprocess for true cancellation
        info!("Sync predictor - using subprocess isolation");
        serve_subprocess(py, pred_ref, config, version)
    }
}

/// Serve with in-process predictor (for async predictors).
fn serve_inprocess(
    py: Python<'_>,
    pred_ref: String,
    config: ServerConfig,
    version: VersionInfo,
) -> PyResult<()> {
    // Load predictor
    let predictor = PythonPredictor::load(py, &pred_ref)?;

    // Run setup
    info!("Running predictor setup");
    let setup_succeeded = match predictor.setup(py) {
        Ok(()) => {
            info!("Setup completed successfully");
            true
        }
        Err(e) => {
            error!(error = %e, "Setup failed");
            false
        }
    };

    let initial_health = if setup_succeeded {
        Health::Ready
    } else {
        Health::SetupFailed
    };

    // Async predictors can handle multiple concurrent requests
    let max_concurrency = 10;
    info!(max_concurrency, "Configuring async predictor");

    let mut app_state = AppState::new(max_concurrency)
        .with_health(initial_health)
        .with_version(version);

    // Create async predict function
    let pred = Arc::new(predictor);
    let pred_clone = Arc::clone(&pred);
    let async_predict_fn = Arc::new(move |input: serde_json::Value| -> PredictFuture {
        let pred = Arc::clone(&pred_clone);
        Box::pin(async move { pred.predict_async(input).await })
    }) as Arc<coglet_core::AsyncPredictFn>;
    app_state = app_state.with_async_predict_fn(async_predict_fn);

    let app_state = Arc::new(app_state);

    // Release GIL and run server
    py.detach(|| {
        let rt = tokio::runtime::Runtime::new()
            .map_err(|e| PyErr::new::<pyo3::exceptions::PyRuntimeError, _>(e.to_string()))?;
        rt.block_on(async {
            http_serve(config, app_state)
                .await
                .map_err(|e| PyErr::new::<pyo3::exceptions::PyRuntimeError, _>(e.to_string()))
        })
    })
}

/// Serve with subprocess worker (for sync predictors).
fn serve_subprocess(
    py: Python<'_>,
    pred_ref: String,
    config: ServerConfig,
    version: VersionInfo,
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

    let spawn_config = SpawnConfig {
        python_exe,
        env: vec![],
    };

    // Create worker handle
    let worker = Arc::new(WorkerHandle::new(pred_ref, spawn_config));

    // Sync predictors use max_concurrency=1
    let max_concurrency = 1;
    info!(max_concurrency, "Configuring sync predictor with subprocess");

    // Start with Starting health - will become Ready after worker init
    let app_state = AppState::new(max_concurrency)
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

    let app_state = Arc::new(app_state.with_async_predict_fn(async_predict_fn));

    // Release GIL and run server
    let app_state_clone = Arc::clone(&app_state);
    py.detach(|| {
        let rt = tokio::runtime::Runtime::new()
            .map_err(|e| PyErr::new::<pyo3::exceptions::PyRuntimeError, _>(e.to_string()))?;

        rt.block_on(async {
            // Initialize worker (spawns subprocess, runs setup)
            match worker.init().await {
                Ok(()) => {
                    info!("Worker initialized, server ready");
                    app_state_clone.set_health(Health::Ready).await;
                }
                Err(e) => {
                    error!(error = %e, "Worker initialization failed");
                    app_state_clone.set_health(Health::SetupFailed).await;
                }
            }

            http_serve(config, app_state_clone)
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
#[pyfunction]
fn _run_worker(predictor_ref: String) -> PyResult<()> {
    // Initialize tracing
    let _ = tracing_subscriber::fmt()
        .with_env_filter(tracing_subscriber::EnvFilter::from_default_env())
        .with_writer(std::io::stderr) // Log to stderr, stdout is for protocol
        .try_init();

    info!("Worker starting with predictor: {}", predictor_ref);

    // Create handler
    let handler = worker_bridge::PythonPredictHandler::new(predictor_ref);
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
