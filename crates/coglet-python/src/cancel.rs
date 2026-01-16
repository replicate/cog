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

use std::sync::atomic::{AtomicBool, Ordering};

use pyo3::prelude::*;

/// Global flag indicating if a sync prediction is currently cancelable.
/// Only set to true while inside predict() for sync predictors.
static CANCELABLE: AtomicBool = AtomicBool::new(false);

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

    // Store the exception class in the coglet module for the handler to use
    let coglet_module = py.import("coglet")?;
    coglet_module.setattr("_CancelationException", &cancel_exc)?;

    // Create the signal handler as a Python function
    // We use exec to define a function that can be used as a signal handler
    let globals = pyo3::types::PyDict::new(py);
    globals.set_item("coglet", coglet_module)?;

    let handler_code = c"
def _sigusr1_handler(signum, frame):
    if coglet._is_cancelable():
        raise coglet._CancelationException()
";
    py.run(handler_code, Some(&globals), None)?;

    let handler = globals.get_item("_sigusr1_handler")?.ok_or_else(|| {
        pyo3::exceptions::PyRuntimeError::new_err("Failed to create signal handler")
    })?;

    // Install the handler for SIGUSR1
    let sigusr1 = signal.getattr("SIGUSR1")?;
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
