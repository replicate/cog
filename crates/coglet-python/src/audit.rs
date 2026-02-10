//! Audit hooks to protect Rust-injected runtime objects.
//!
//! Uses sys.addaudithook to intercept operations that could interfere with
//! our runtime machinery. The hook cannot be removed once added.
//!
//! ## Protection: sys.stdout/stderr (Tee pattern)
//!
//! If user code replaces stdout/stderr, we wrap their replacement in a _TeeWriter
//! that sends data to BOTH our slot routing AND their stream. User's code works
//! as they expect, but we still get our logs.
//!
//! If they replace again, we unwrap the inner _SlotLogWriter from the current
//! _TeeWriter and re-tee with the new stream. No nested _TeeWriters.

use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Mutex, OnceLock};

use pyo3::prelude::*;
use pyo3_stub_gen::derive::*;

/// Whether the audit hook has been installed.
static HOOK_INSTALLED: AtomicBool = AtomicBool::new(false);

/// Re-entrancy guard for the audit hook.
/// Prevents infinite recursion when the hook itself sets sys.stdout/stderr.
static IN_HOOK: AtomicBool = AtomicBool::new(false);

/// Serializes stream replacement so concurrent threads don't race
/// on the read-current → create-tee → set-new sequence.
static STREAM_LOCK: Mutex<()> = Mutex::new(());

/// Reference to sys module for identity comparison in hook.
static SYS_MODULE: OnceLock<Py<PyAny>> = OnceLock::new();

/// Reference to our _SlotLogWriter class for isinstance checks.
static SLOT_LOG_WRITER_TYPE: OnceLock<Py<PyAny>> = OnceLock::new();

/// Install the audit hook. Called once at worker startup.
///
/// The hook intercepts object.__setattr__ on sys for stdout/stderr.
pub fn install_audit_hook(py: Python<'_>) -> PyResult<()> {
    if HOOK_INSTALLED.swap(true, Ordering::SeqCst) {
        return Ok(());
    }

    // Store sys module reference for identity comparison
    let sys = py.import("sys")?;
    let _ = SYS_MODULE.set(sys.as_any().clone().unbind());

    // Store our _SlotLogWriter type for isinstance checks
    if let Ok(coglet) = py.import("coglet")
        && let Ok(writer_type) = coglet.getattr("_SlotLogWriter")
    {
        let _ = SLOT_LOG_WRITER_TYPE.set(writer_type.unbind());
    }

    // Register the Rust audit hook callable
    let hook = wrap_pyfunction!(_coglet_audit_hook, py)?;
    sys.call_method1("addaudithook", (hook,))?;

    tracing::debug!("Installed audit hook for runtime protection");
    Ok(())
}

/// Audit hook implemented in Rust.
///
/// Intercepts `object.__setattr__` events on `sys` for stdout/stderr.
/// Uses an AtomicBool re-entrancy guard instead of deferred threading.Timer.
#[pyfunction]
fn _coglet_audit_hook(py: Python<'_>, event: &str, args: &Bound<'_, PyAny>) -> PyResult<()> {
    if event != "object.__setattr__" {
        return Ok(());
    }

    // Re-entrancy guard: skip if we're already inside the hook
    // (because we're setting sys.stdout/stderr ourselves).
    if IN_HOOK.load(Ordering::SeqCst) {
        return Ok(());
    }

    // args is (obj, name, value)
    let obj = args.get_item(0)?;
    let name: String = args.get_item(1)?.extract()?;

    if name != "stdout" && name != "stderr" {
        return Ok(());
    }

    // Check if obj is the sys module (identity comparison)
    let Some(sys_ref) = SYS_MODULE.get() else {
        return Ok(());
    };
    if !obj.is(sys_ref.bind(py)) {
        return Ok(());
    }

    let value = args.get_item(2)?;
    handle_stream_replacement(py, &name, &value)?;

    Ok(())
}

/// Handle user code replacing sys.stdout or sys.stderr.
///
/// If the new value is already our _SlotLogWriter, this is our own setup — skip.
/// Otherwise, find our _SlotLogWriter from the current stream (direct or inside
/// a _TeeWriter), and wrap the user's new stream in a fresh _TeeWriter.
fn handle_stream_replacement(py: Python<'_>, name: &str, value: &Bound<'_, PyAny>) -> PyResult<()> {
    // If value is our _SlotLogWriter, this is us installing — skip
    if is_slot_log_writer(py, value) {
        return Ok(());
    }

    // Serialize the read-current → create-tee → set-new sequence.
    // Without this, two threads replacing stdout simultaneously could race
    // and one tee gets silently dropped.
    // The lock protects no data (just `()`), so poisoned is safe to recover.
    let _lock = STREAM_LOCK.lock().unwrap_or_else(|poisoned| {
        tracing::warn!(
            target: "coglet::worker_local",
            "stream lock was poisoned (a thread panicked during stream replacement) — \
             recovering, but log routing may be inconsistent"
        );
        poisoned.into_inner()
    });

    // Get current writer from sys
    let sys = py.import("sys")?;
    let current = sys.getattr(name)?;

    // Find our _SlotLogWriter — either it IS current, or it's inside a _TeeWriter
    let slot_writer = if is_slot_log_writer(py, &current) {
        Some(current.clone().unbind())
    } else if is_tee_writer(&current) {
        get_inner_writer(py, &current).ok()
    } else {
        None
    };

    let Some(slot_writer) = slot_writer else {
        // No _SlotLogWriter installed — nothing to protect
        return Ok(());
    };

    // Create new _TeeWriter wrapping our _SlotLogWriter and user's stream
    let tee = _TeeWriter::new(slot_writer, value.clone().unbind(), name.to_string());
    let tee_obj = tee.into_pyobject(py)?;

    // Set under re-entrancy guard to prevent hook from re-triggering
    IN_HOOK.store(true, Ordering::SeqCst);
    let result = sys.setattr(name, tee_obj);
    IN_HOOK.store(false, Ordering::SeqCst);

    result
}

// ============================================================================
// Type checks — pub(crate) only, not exported to Python
// ============================================================================

/// Check if a value is a _SlotLogWriter.
pub(crate) fn is_slot_log_writer(py: Python<'_>, value: &Bound<'_, PyAny>) -> bool {
    if let Some(writer_type) = SLOT_LOG_WRITER_TYPE.get()
        && let Ok(true) = value.is_instance(writer_type.bind(py))
    {
        return true;
    }

    // Fallback: check by class name (handles cross-module edge cases)
    if let Ok(type_name) = value.get_type().name() {
        return type_name == "_SlotLogWriter";
    }

    false
}

/// Check if a value is a _TeeWriter.
pub(crate) fn is_tee_writer(value: &Bound<'_, PyAny>) -> bool {
    if value.is_instance_of::<_TeeWriter>() {
        return true;
    }

    if let Ok(type_name) = value.get_type().name() {
        return type_name == "_TeeWriter";
    }

    false
}

/// Get the inner _SlotLogWriter from a _TeeWriter.
pub(crate) fn get_inner_writer(py: Python<'_>, tee: &Bound<'_, PyAny>) -> PyResult<Py<PyAny>> {
    if let Ok(tee_writer) = tee.extract::<PyRef<'_, _TeeWriter>>() {
        return Ok(tee_writer.inner.clone_ref(py));
    }

    if let Ok(inner) = tee.getattr("inner") {
        return Ok(inner.unbind());
    }

    Err(pyo3::exceptions::PyTypeError::new_err(
        "Expected _TeeWriter with inner attribute",
    ))
}

// ============================================================================
// _TeeWriter — private pyclass
// ============================================================================

/// Tee writer that sends writes to both our slot routing and user's stream.
///
/// - inner: Our _SlotLogWriter for slot-based log routing
/// - user_stream: The stream user code tried to install
#[gen_stub_pyclass]
#[pyclass(name = "_TeeWriter", module = "coglet")]
pub struct _TeeWriter {
    /// Our _SlotLogWriter (does ContextVar-based routing)
    #[pyo3(get)]
    inner: Py<PyAny>,
    /// User's replacement stream
    #[pyo3(get)]
    user_stream: Py<PyAny>,
    /// Stream name (stdout or stderr)
    #[pyo3(get)]
    name: String,
    /// Closed flag
    #[pyo3(get)]
    closed: bool,
}

#[gen_stub_pymethods]
#[pymethods]
impl _TeeWriter {
    #[new]
    fn new(inner: Py<PyAny>, user_stream: Py<PyAny>, name: String) -> Self {
        Self {
            inner,
            user_stream,
            name,
            closed: false,
        }
    }

    /// Write to both streams.
    fn write(&self, py: Python<'_>, data: &str) -> PyResult<usize> {
        if self.closed || data.is_empty() {
            return Ok(data.len());
        }

        if let Err(e) = self.inner.call_method1(py, "write", (data,)) {
            tracing::warn!(error = %e, "_TeeWriter: failed to write to inner");
        }

        if let Err(e) = self.user_stream.call_method1(py, "write", (data,)) {
            tracing::warn!(error = %e, "_TeeWriter: failed to write to user stream");
        }

        Ok(data.len())
    }

    /// Flush both streams.
    fn flush(&self, py: Python<'_>) -> PyResult<()> {
        let _ = self.inner.call_method0(py, "flush");
        let _ = self.user_stream.call_method0(py, "flush");
        Ok(())
    }

    fn readable(&self) -> bool {
        false
    }

    fn writable(&self) -> bool {
        !self.closed
    }

    fn seekable(&self) -> bool {
        false
    }

    fn isatty(&self, py: Python<'_>) -> PyResult<bool> {
        let result = self.user_stream.call_method0(py, "isatty")?;
        result.extract(py)
    }

    fn fileno(&self, py: Python<'_>) -> PyResult<i32> {
        let result = self.user_stream.call_method0(py, "fileno")?;
        result.extract(py)
    }

    fn close(&mut self) {
        self.closed = true;
    }

    fn __enter__(slf: PyRef<'_, Self>) -> PyRef<'_, Self> {
        slf
    }

    fn __exit__(
        &mut self,
        _exc_type: Option<&Bound<'_, PyAny>>,
        _exc_val: Option<&Bound<'_, PyAny>>,
        _exc_tb: Option<&Bound<'_, PyAny>>,
    ) -> bool {
        false
    }

    #[getter]
    fn encoding(&self, py: Python<'_>) -> PyResult<Option<String>> {
        match self.user_stream.getattr(py, "encoding") {
            Ok(enc) => enc.extract(py),
            Err(_) => Ok(Some("utf-8".to_string())),
        }
    }

    #[getter]
    fn newlines(&self) -> Option<String> {
        None
    }
}
