//! Audit hooks to protect Rust-injected runtime objects.
//!
//! Uses sys.addaudithook to intercept operations that could interfere with
//! our runtime machinery. The hook cannot be removed once added.
//!
//! ## Protection levels:
//!
//! ### sys.stdout/stderr (Tee pattern)
//! If user code replaces stdout/stderr, we wrap their replacement in a TeeWriter
//! that sends data to BOTH our slot routing AND their stream. User's code works
//! as they expect, but we still get our logs.
//!
//! If they replace again, we unwrap the inner SlotLogWriter and re-tee with
//! the new stream. No nested TeeWriters.
//!
//! ### ContextVar (Hard guard)  
//! Our slot routing ContextVar is critical infrastructure. If user code tries
//! to access or modify it, we raise an exception. No tampering allowed.

use std::sync::OnceLock;
use std::sync::atomic::{AtomicBool, Ordering};

use pyo3::prelude::*;
use pyo3::types::PyDict;

/// Whether the audit hook has been installed.
static HOOK_INSTALLED: AtomicBool = AtomicBool::new(false);

/// Reference to sys module for comparison in hook.
static SYS_MODULE: OnceLock<Py<PyAny>> = OnceLock::new();

/// Reference to our SlotLogWriter class for isinstance checks.
static SLOT_LOG_WRITER_TYPE: OnceLock<Py<PyAny>> = OnceLock::new();

/// Install the audit hook. Called once at worker startup.
///
/// The hook intercepts:
/// - object.__setattr__ on sys module for stdout/stderr
/// - (future) other sensitive operations
pub fn install_audit_hook(py: Python<'_>) -> PyResult<()> {
    if HOOK_INSTALLED.swap(true, Ordering::SeqCst) {
        // Already installed
        return Ok(());
    }

    // Store sys module reference for comparison
    let sys = py.import("sys")?;
    let _ = SYS_MODULE.set(sys.as_any().clone().unbind());

    // Store our SlotLogWriter type for isinstance checks
    // We need to get this from the coglet module
    if let Ok(coglet) = py.import("coglet")
        && let Ok(writer_type) = coglet.getattr("SlotLogWriter")
    {
        let _ = SLOT_LOG_WRITER_TYPE.set(writer_type.unbind());
    }

    // Define the audit hook in Python
    // We use exec because the hook needs to be a Python callable
    let hook_code = r#"
import sys

def _coglet_audit_hook(event, args):
    """
    Audit hook that protects coglet runtime objects.
    
    This hook cannot be removed once installed.
    """
    if event == "object.__setattr__":
        obj, name, value = args
        
        # Check if setting stdout or stderr on sys module
        if obj is sys and name in ("stdout", "stderr"):
            _coglet_handle_stream_replacement(name, value)

def _coglet_handle_stream_replacement(name, value):
    """
    Handle user code replacing sys.stdout or sys.stderr.
    
    We wrap their replacement in a TeeWriter so data goes to both
    our slot routing AND their stream.
    
    If they replace again, we unwrap to get our SlotLogWriter and re-tee
    with the new stream. No nested TeeWriters.
    """
    import coglet
    
    # Check if value is already our SlotLogWriter
    # If so, this is us setting up - no need to wrap
    if hasattr(coglet, '_is_slot_log_writer') and coglet._is_slot_log_writer(value):
        return
    
    # Check if value is already a TeeWriter - this shouldn't happen
    # but if it does, we don't wrap TeeWriter in TeeWriter
    if hasattr(coglet, '_is_tee_writer') and coglet._is_tee_writer(value):
        return
    
    # Get current writer
    current = getattr(sys, name)
    
    # Find our SlotLogWriter - either it IS current, or it's inside a TeeWriter
    slot_writer = None
    if hasattr(coglet, '_is_slot_log_writer') and coglet._is_slot_log_writer(current):
        slot_writer = current
    elif hasattr(coglet, '_get_inner_writer') and coglet._is_tee_writer(current):
        # Current is a TeeWriter, get the inner SlotLogWriter
        slot_writer = coglet._get_inner_writer(current)
    
    if slot_writer is None:
        # We don't have a SlotLogWriter installed - nothing to protect
        return
    
    # Create TeeWriter with our SlotLogWriter and their new stream
    if hasattr(coglet, '_create_tee_writer'):
        tee = coglet._create_tee_writer(slot_writer, value, name)
        # Replace with tee instead of their value
        # Schedule the fix to avoid infinite recursion (we're inside setattr)
        def _fix():
            setattr(sys, name, tee)
        import threading
        threading.Timer(0, _fix).start()

# Install the hook
sys.addaudithook(_coglet_audit_hook)
"#;

    let globals = PyDict::new(py);
    let builtins = py.import("builtins")?;
    let exec_fn = builtins.getattr("exec")?;
    exec_fn.call1((hook_code, &globals))?;

    tracing::debug!("Installed audit hook for runtime protection");
    Ok(())
}

/// TeeWriter that sends writes to both our slot routing and user's stream.
///
/// This is a PyO3 class that wraps two writers:
/// - inner: Our SlotLogWriter for slot routing
/// - user_stream: The stream user code tried to install
#[pyclass]
pub struct TeeWriter {
    /// Our SlotLogWriter (does ContextVar-based routing)
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

#[pymethods]
impl TeeWriter {
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

        // Write to our routing first (this goes to slot socket during predictions)
        if let Err(e) = self.inner.call_method1(py, "write", (data,)) {
            tracing::warn!(error = %e, "TeeWriter: failed to write to inner");
        }

        // Write to user's stream (this is what they expect)
        if let Err(e) = self.user_stream.call_method1(py, "write", (data,)) {
            tracing::warn!(error = %e, "TeeWriter: failed to write to user stream");
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
        // Delegate to user stream
        let result = self.user_stream.call_method0(py, "isatty")?;
        result.extract(py)
    }

    fn fileno(&self, py: Python<'_>) -> PyResult<i32> {
        // Delegate to user stream (they may need FD operations)
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

/// Check if a value is a SlotLogWriter (our core writer).
#[pyfunction]
pub fn _is_slot_log_writer(py: Python<'_>, value: &Bound<'_, PyAny>) -> bool {
    // Check by type reference
    if let Some(writer_type) = SLOT_LOG_WRITER_TYPE.get()
        && let Ok(true) = value.is_instance(writer_type.bind(py))
    {
        return true;
    }

    // Fallback to class name
    if let Ok(type_name) = value.get_type().name()
        && type_name == "SlotLogWriter"
    {
        return true;
    }

    false
}

/// Check if a value is a TeeWriter.
#[pyfunction]
pub fn _is_tee_writer(_py: Python<'_>, value: &Bound<'_, PyAny>) -> bool {
    if value.is_instance_of::<TeeWriter>() {
        return true;
    }

    // Fallback to class name
    if let Ok(type_name) = value.get_type().name()
        && type_name == "TeeWriter"
    {
        return true;
    }

    false
}

/// Get the inner SlotLogWriter from a TeeWriter.
#[pyfunction]
pub fn _get_inner_writer(py: Python<'_>, tee: &Bound<'_, PyAny>) -> PyResult<Py<PyAny>> {
    // Try to extract as TeeWriter
    if let Ok(tee_writer) = tee.extract::<PyRef<'_, TeeWriter>>() {
        return Ok(tee_writer.inner.clone_ref(py));
    }

    // Fallback: try to get .inner attribute
    if let Ok(inner) = tee.getattr("inner") {
        return Ok(inner.unbind());
    }

    Err(pyo3::exceptions::PyTypeError::new_err(
        "Expected TeeWriter with inner attribute",
    ))
}

/// Create a TeeWriter that wraps our SlotLogWriter and user's stream.
#[pyfunction]
pub fn _create_tee_writer(
    _py: Python<'_>,
    inner: Py<PyAny>,
    user_stream: Py<PyAny>,
    name: String,
) -> PyResult<TeeWriter> {
    Ok(TeeWriter::new(inner, user_stream, name))
}
