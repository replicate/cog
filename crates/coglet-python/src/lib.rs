//! coglet-python: PyO3 bindings for coglet.

mod input;
mod predictor;

use std::sync::Arc;

use pyo3::prelude::*;

use tracing::{error, info};
use tracing_subscriber::EnvFilter;

use coglet_core::{Health, PredictFuture, VersionInfo};
use coglet_http::{serve as http_serve, AppState, ServerConfig};

use crate::predictor::PythonPredictor;

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

    // Load predictor if ref provided
    let predictor = if let Some(ref pred_ref) = predictor_ref {
        info!(predictor_ref = %pred_ref, "Loading predictor");
        match PythonPredictor::load(py, pred_ref) {
            Ok(p) => Some(p),
            Err(e) => {
                error!(error = %e, "Failed to load predictor");
                return Err(e);
            }
        }
    } else {
        None
    };

    // Run setup if predictor loaded
    let setup_succeeded = if let Some(ref pred) = predictor {
        info!("Running predictor setup");
        match pred.setup(py) {
            Ok(()) => {
                info!("Setup completed successfully");
                true
            }
            Err(e) => {
                error!(error = %e, "Setup failed");
                false
            }
        }
    } else {
        true // No predictor = setup "succeeds"
    };

    // Determine initial health state
    let initial_health = if predictor.is_some() {
        if setup_succeeded {
            Health::Ready
        } else {
            Health::SetupFailed
        }
    } else {
        Health::Unknown
    };

    // Detect version info
    let version = detect_version(py);

    // Determine max_concurrency based on predictor type
    // Async predictors can handle multiple concurrent requests
    let is_async_predictor = predictor.as_ref().is_some_and(|p| p.is_async());
    let max_concurrency = if is_async_predictor { 10 } else { 1 };

    info!(
        is_async = is_async_predictor,
        max_concurrency = max_concurrency,
        "Configuring predictor"
    );

    // Create app state with initial health and version
    let mut app_state = AppState::new(max_concurrency)
        .with_health(initial_health)
        .with_version(version);

    // Create the predict function closure if we have a predictor
    if let Some(pred) = predictor {
        // Wrap predictor in Arc so the closure can be cloned
        let pred = Arc::new(pred);

        if is_async_predictor {
            // Async predictor: use async predict function
            let pred_clone = Arc::clone(&pred);
            let async_predict_fn = Arc::new(move |input: serde_json::Value| -> PredictFuture {
                let pred = Arc::clone(&pred_clone);
                Box::pin(async move { pred.predict_async(input).await })
            }) as Arc<coglet_core::AsyncPredictFn>;
            app_state = app_state.with_async_predict_fn(async_predict_fn);
        } else {
            // Sync predictor: use sync predict function (runs in spawn_blocking)
            let predict_fn = Arc::new(move |input: serde_json::Value| {
                pred.predict(input)
            }) as Arc<coglet_core::PredictFn>;
            app_state = app_state.with_predict_fn(predict_fn);
        }
    }

    let app_state = Arc::new(app_state);

    // Release the GIL while running the async runtime
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

/// coglet Python module.
#[pymodule]
fn coglet(m: &Bound<'_, PyModule>) -> PyResult<()> {
    m.add_function(wrap_pyfunction!(serve, m)?)?;
    Ok(())
}
