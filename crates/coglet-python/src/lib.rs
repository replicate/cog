//! coglet-python: PyO3 bindings for coglet.

mod predictor;

use pyo3::prelude::*;
use tracing_subscriber::EnvFilter;

use coglet_http::{serve as http_serve, ServerConfig};

/// Start the coglet HTTP server.
///
/// This function blocks until the server shuts down.
#[pyfunction]
#[pyo3(signature = (host = "0.0.0.0".to_string(), port = 5000))]
fn serve(py: Python<'_>, host: String, port: u16) -> PyResult<()> {
    // Initialize tracing (ignore if already initialized)
    let _ = tracing_subscriber::fmt()
        .with_env_filter(EnvFilter::from_default_env())
        .try_init();

    let config = ServerConfig { host, port };

    // Release the GIL while running the async runtime
    py.allow_threads(|| {
        let rt = tokio::runtime::Runtime::new()
            .map_err(|e| PyErr::new::<pyo3::exceptions::PyRuntimeError, _>(e.to_string()))?;

        rt.block_on(async {
            http_serve(config)
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
