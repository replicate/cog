//! Cancellation support for predictions.
//!
//! Sync predictors use `PyThreadState_SetAsyncExc` to inject a
//! `CancelationException` (a `BaseException` subclass) into the Python
//! thread running `predict()`.
//!
//! Async predictors use asyncio task cancellation:
//! - Store task reference when prediction starts
//! - Call task.cancel() when cancel requested
//! - Python raises asyncio.CancelledError
//!
//! `CancelationException` deliberately derives from `BaseException` (not
//! `Exception`) so that bare `except Exception` blocks in user code cannot
//! swallow it — matching the semantics of `KeyboardInterrupt` and
//! `asyncio.CancelledError`.

use pyo3::prelude::*;

// Static exception type with automatic stub generation.
// Derives from BaseException so `except Exception` does not catch it.
pyo3_stub_gen::create_exception!(
    coglet,
    CancelationException,
    pyo3::exceptions::PyBaseException,
    "Raised when a running prediction or training is cancelled.\n\
     \n\
     Derives from ``BaseException`` (not ``Exception``) so that bare\n\
     ``except Exception`` blocks do not accidentally swallow cancellation.\n\
     This matches the semantics of ``KeyboardInterrupt`` and\n\
     ``asyncio.CancelledError``."
);

/// Inject CancelationException into a specific Python thread.
///
/// Uses CPython's `PyThreadState_SetAsyncExc` to raise the exception at the
/// next bytecode boundary. This works on any thread (not just the main thread),
/// unlike SIGUSR1-based cancellation.
///
/// Requires the GIL — `Python::attach` acquires it, blocking briefly if the
/// prediction thread currently holds it (CPython releases it every ~5ms).
pub fn cancel_sync_thread(py_thread_id: std::ffi::c_long) {
    Python::attach(|py| {
        let exc = py.get_type::<CancelationException>().as_ptr();

        // SAFETY: We hold the GIL. exc is a valid Python type pointer
        // obtained from the interpreter's type registry.
        let result = unsafe { pyo3::ffi::PyThreadState_SetAsyncExc(py_thread_id, exc) };

        match result {
            0 => {
                tracing::warn!(
                    py_thread_id,
                    "PyThreadState_SetAsyncExc: thread not found (prediction may have completed)"
                );
            }
            1 => {
                tracing::debug!(
                    py_thread_id,
                    "Injected CancelationException into Python thread"
                );
            }
            _ => {
                // CPython docs: if > 1, call again with NULL to reset
                tracing::error!(
                    py_thread_id,
                    count = result,
                    "PyThreadState_SetAsyncExc modified multiple thread states, resetting"
                );
                unsafe {
                    pyo3::ffi::PyThreadState_SetAsyncExc(py_thread_id, std::ptr::null_mut());
                }
            }
        }
    });
}

/// Get the current Python thread identifier (for later use with `cancel_sync_thread`).
///
/// Uses `threading.get_ident()` which returns the same value as
/// `PyThreadState_SetAsyncExc` expects for the thread id argument.
/// Can be called from any thread (acquires the GIL briefly).
pub fn current_py_thread_id() -> std::ffi::c_long {
    Python::attach(|py| {
        let threading = py.import("threading").expect("failed to import threading");
        threading
            .call_method0("get_ident")
            .expect("threading.get_ident() failed")
            .extract::<std::ffi::c_long>()
            .expect("thread ident is not an integer")
    })
}
