//! Signal handling for prediction cancellation.
//!
//! Uses SIGUSR1 to interrupt blocking Python code during sync predictions.

use std::sync::atomic::{AtomicBool, Ordering};

use pyo3::prelude::*;

/// Global flag indicating if we're in a cancelable section.
static CANCELABLE: AtomicBool = AtomicBool::new(false);

pub fn is_cancelable() -> bool {
    CANCELABLE.load(Ordering::SeqCst)
}

pub fn set_cancelable(cancelable: bool) {
    CANCELABLE.store(cancelable, Ordering::SeqCst);
}

/// Install SIGUSR1 signal handler for cancellation.
pub fn install_signal_handler(py: Python<'_>) -> PyResult<()> {
    #[cfg(unix)]
    {
        let signal_module = py.import("signal")?;
        let sigusr1 = signal_module.getattr("SIGUSR1")?;

        // Create a Python handler that raises KeyboardInterrupt if cancelable
        let handler_code = c"
def _coglet_cancel_handler(signum, frame):
    import coglet
    if coglet._is_cancelable():
        raise KeyboardInterrupt('Prediction cancelled')
";
        py.run(handler_code, None, None)?;

        let main_module = py.import("__main__")?;
        let handler = main_module.getattr("_coglet_cancel_handler")?;

        signal_module.call_method1("signal", (sigusr1, handler))?;
        tracing::debug!("Installed SIGUSR1 handler for cancellation");
    }

    Ok(())
}
