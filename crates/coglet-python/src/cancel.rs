//! Cancellation support for predictions.
//!
//! Sync predictors use SIGUSR1 signal handling (like cog):
//! - Install Python signal handler at startup
//! - When cancel requested, send SIGUSR1 to self
//! - Handler raises CancelationException in Python
//!
//! Async predictors use asyncio task cancellation:
//! - Store task reference when prediction starts
//! - Call task.cancel() when cancel requested
//! - Python raises asyncio.CancelledError

use std::sync::OnceLock;
use std::sync::atomic::{AtomicBool, Ordering};

use pyo3::prelude::*;

/// Global flag indicating if a sync prediction is currently cancelable.
/// Only set to true while inside predict() for sync predictors.
static CANCELABLE: AtomicBool = AtomicBool::new(false);

/// CancelationException class, stored once at startup.
static CANCELATION_EXCEPTION: OnceLock<Py<PyAny>> = OnceLock::new();

/// SIGUSR1 signal handler implemented in Rust.
///
/// Raises CancelationException if we're currently inside a cancelable predict().
#[pyfunction]
fn _sigusr1_handler(_signum: i32, _frame: Option<&Bound<'_, PyAny>>) -> PyResult<()> {
    if is_cancelable() {
        if let Some(exc) = CANCELATION_EXCEPTION.get() {
            Python::attach(|py| Err(PyErr::from_value(exc.bind(py).clone())))
        } else {
            Err(pyo3::exceptions::PyRuntimeError::new_err(
                "CancelationException not initialized",
            ))
        }
    } else {
        Ok(())
    }
}

/// Install the SIGUSR1 signal handler for sync predictor cancellation.
///
/// This should be called once at startup. The handler will raise
/// CancelationException when SIGUSR1 is received and CANCELABLE is true.
pub fn install_signal_handler(py: Python<'_>) -> PyResult<()> {
    let signal = py.import("signal")?;

    // Import or define CancelationException
    let cancel_exc = if let Ok(exceptions) = py.import("cog.server.exceptions") {
        exceptions.getattr("CancelationException")?
    } else {
        // Define a simple exception class if cog.server.exceptions not available
        let builtins = py.import("builtins")?;
        let exception_class = builtins.getattr("Exception")?;
        exception_class.call1(("CancelationException",))?
    };

    // Store in Rust static (no module setattr needed)
    let _ = CANCELATION_EXCEPTION.set(cancel_exc.unbind());

    // Install the Rust handler for SIGUSR1
    let sigusr1 = signal.getattr("SIGUSR1")?;
    let handler = wrap_pyfunction!(_sigusr1_handler, py)?;
    signal.call_method1("signal", (sigusr1, handler))?;

    tracing::debug!("Installed SIGUSR1 signal handler for sync cancellation");
    Ok(())
}

/// Mark the current context as cancelable (for sync predictors).
/// Returns a guard that clears the flag on drop.
pub fn enter_cancelable() -> CancelableGuard {
    CANCELABLE.store(true, Ordering::SeqCst);
    CancelableGuard { _private: () }
}

/// Check if we're currently in a cancelable section.
/// Called from Python signal handler.
pub fn is_cancelable() -> bool {
    CANCELABLE.load(Ordering::SeqCst)
}

/// RAII guard that clears cancelable flag on drop.
pub struct CancelableGuard {
    _private: (),
}

impl Drop for CancelableGuard {
    fn drop(&mut self) {
        CANCELABLE.store(false, Ordering::SeqCst);
    }
}

/// Send SIGUSR1 to the current process to trigger cancellation.
/// This will cause the Python signal handler to raise CancelationException.
pub fn send_cancel_signal() -> std::io::Result<()> {
    #[cfg(unix)]
    {
        use std::process;
        let pid = process::id();
        // Send SIGUSR1 to self
        unsafe {
            if libc::kill(pid as i32, libc::SIGUSR1) != 0 {
                return Err(std::io::Error::last_os_error());
            }
        }
        tracing::debug!("Sent SIGUSR1 to pid {}", pid);
        Ok(())
    }

    #[cfg(not(unix))]
    {
        Err(std::io::Error::new(
            std::io::ErrorKind::Unsupported,
            "Signal-based cancellation only supported on Unix",
        ))
    }
}
