//! coglet-python: PyO3 bindings for coglet.

mod predictor;

use std::sync::Arc;

use pyo3::prelude::*;
use tokio::sync::RwLock;
use tracing::{error, info};
use tracing_subscriber::EnvFilter;

use coglet_core::Health;
use coglet_http::{serve as http_serve, AppState, ServerConfig};

use crate::predictor::PythonPredictor;

/// Shared state that holds the predictor.
struct RuntimeState {
    predictor: Option<PythonPredictor>,
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

    let config = ServerConfig { host, port };

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

    // Create app state with initial health
    let app_state = Arc::new(AppState {
        health: RwLock::new(initial_health),
    });

    // Release the GIL while running the async runtime
    py.allow_threads(|| {
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
