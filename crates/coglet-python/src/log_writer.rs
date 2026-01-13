//! SlotLogWriter - captures Python stdout/stderr and routes via Rust-owned ContextVar.
//!
//! Architecture:
//! - Rust owns a ContextVar that holds the current SlotId
//! - Rust maintains a registry mapping SlotId → SlotSender
//! - SlotLogWriter is a singleton that reads the ContextVar to route logs
//! - User code cannot bypass this - even if they replace sys.stdout, we control the ContextVar
//!
//! This design supports:
//! - Async predictions with proper per-task isolation (ContextVar is task-local)
//! - Free-threaded Python (ContextVar is thread-safe)
//! - Future interactive channels (same routing mechanism)

use std::collections::HashMap;
use std::sync::{Arc, Mutex, OnceLock};

use pyo3::prelude::*;

use coglet_worker::{LogSource, SlotId, SlotSender};

// ============================================================================
// Rust-owned ContextVar for slot routing
// ============================================================================

/// The Rust-owned ContextVar instance. Created once, lives forever.
/// User code cannot replace this - we hold the only reference.
static SLOT_CONTEXTVAR: OnceLock<Py<PyAny>> = OnceLock::new();

/// Registry mapping SlotId → SlotSender.
/// When a prediction starts, we register the sender.
/// When SlotLogWriter.write() is called, we look up the sender here.
static SLOT_REGISTRY: OnceLock<Mutex<HashMap<SlotId, Arc<SlotSender>>>> = OnceLock::new();

fn get_registry() -> &'static Mutex<HashMap<SlotId, Arc<SlotSender>>> {
    SLOT_REGISTRY.get_or_init(|| Mutex::new(HashMap::new()))
}

/// Get or create the Rust-owned ContextVar.
fn get_slot_contextvar(py: Python<'_>) -> PyResult<&'static Py<PyAny>> {
    if let Some(cv) = SLOT_CONTEXTVAR.get() {
        return Ok(cv);
    }

    let contextvars = py.import("contextvars")?;
    let cv = contextvars.call_method1("ContextVar", ("_coglet_slot_id",))?;
    
    // Try to store it. Race is fine - worst case we create an extra that gets dropped.
    let _ = SLOT_CONTEXTVAR.set(cv.unbind());
    Ok(SLOT_CONTEXTVAR.get().unwrap())
}

/// Register a SlotSender for a SlotId.
/// Called when starting a prediction.
pub fn register_slot(slot_id: SlotId, sender: Arc<SlotSender>) {
    let mut registry = get_registry().lock().unwrap();
    registry.insert(slot_id, sender);
}

/// Unregister a SlotId.
/// Called when prediction completes.
pub fn unregister_slot(slot_id: SlotId) {
    let mut registry = get_registry().lock().unwrap();
    registry.remove(&slot_id);
}

/// Get the SlotSender for a SlotId.
fn get_slot_sender(slot_id: SlotId) -> Option<Arc<SlotSender>> {
    let registry = get_registry().lock().unwrap();
    registry.get(&slot_id).cloned()
}

/// Set the current slot ID in the ContextVar.
/// Returns a token that can be used to reset.
pub fn set_current_slot(py: Python<'_>, slot_id: SlotId) -> PyResult<Py<PyAny>> {
    let cv = get_slot_contextvar(py)?;
    let slot_id_str = slot_id.to_string();
    let token = cv.call_method1(py, "set", (slot_id_str,))?;
    Ok(token)
}

/// Reset the current slot ID using a token from set_current_slot.
pub fn reset_current_slot(py: Python<'_>, token: &Py<PyAny>) -> PyResult<()> {
    let cv = get_slot_contextvar(py)?;
    cv.call_method1(py, "reset", (token,))?;
    Ok(())
}

/// Get the current slot ID from the ContextVar.
/// Returns None if not set (outside prediction context).
fn get_current_slot_id(py: Python<'_>) -> PyResult<Option<SlotId>> {
    let cv = get_slot_contextvar(py)?;
    
    // Try to get the value - returns the value or raises LookupError
    match cv.call_method0(py, "get") {
        Ok(val) => {
            let slot_id_str: String = val.extract(py)?;
            let slot_id = SlotId::parse(&slot_id_str).map_err(|e| {
                pyo3::exceptions::PyValueError::new_err(format!("Invalid SlotId: {}", e))
            })?;
            Ok(Some(slot_id))
        }
        Err(e) if e.is_instance_of::<pyo3::exceptions::PyLookupError>(py) => {
            // ContextVar not set - outside prediction context
            Ok(None)
        }
        Err(e) => Err(e),
    }
}

// ============================================================================
// SlotLogWriter - singleton that routes via ContextVar
// ============================================================================

/// A Python file-like object that routes writes via the Rust-owned ContextVar.
///
/// This is installed as sys.stdout/stderr once at worker startup.
/// Each write looks up the current SlotId from the ContextVar and routes
/// to the appropriate SlotSender.
///
/// If no SlotId is set (outside prediction), writes go to the original stream.
#[pyclass]
pub struct SlotLogWriter {
    /// Which stream this captures (stdout or stderr).
    source: LogSource,
    /// Original stream to fall back to when outside prediction context.
    original: Py<PyAny>,
    /// Whether writes should be ignored (used after errors).
    #[pyo3(get)]
    closed: bool,
}

#[pymethods]
impl SlotLogWriter {
    /// Write data, routing via ContextVar.
    ///
    /// If inside a prediction (ContextVar set), routes to the slot's sender.
    /// Otherwise, writes to the original stream.
    fn write(&self, py: Python<'_>, data: &str) -> PyResult<usize> {
        if self.closed || data.is_empty() {
            return Ok(data.len());
        }

        // Try to get current slot from ContextVar
        match get_current_slot_id(py)? {
            Some(slot_id) => {
                // Inside prediction - route to slot
                if let Some(sender) = get_slot_sender(slot_id) {
                    sender.send_log(self.source, data).map_err(|e| {
                        pyo3::exceptions::PyIOError::new_err(e.to_string())
                    })?;
                } else {
                    // Slot not registered (shouldn't happen) - fall back to original
                    self.write_to_original(py, data)?;
                }
            }
            None => {
                // Outside prediction - write to original stream
                self.write_to_original(py, data)?;
            }
        }

        Ok(data.len())
    }

    /// Flush the stream.
    fn flush(&self, py: Python<'_>) -> PyResult<()> {
        // Flush the original stream
        self.original.call_method0(py, "flush")?;
        Ok(())
    }

    /// Return whether the stream is readable.
    fn readable(&self) -> bool {
        false
    }

    /// Return whether the stream is writable.
    fn writable(&self) -> bool {
        !self.closed
    }

    /// Return whether the stream is seekable.
    fn seekable(&self) -> bool {
        false
    }

    /// Return whether the stream is a TTY.
    fn isatty(&self, py: Python<'_>) -> PyResult<bool> {
        // Delegate to original
        let result = self.original.call_method0(py, "isatty")?;
        result.extract(py)
    }

    /// Return the file number.
    fn fileno(&self, py: Python<'_>) -> PyResult<i32> {
        // Delegate to original - needed for some libraries
        let result = self.original.call_method0(py, "fileno")?;
        result.extract(py)
    }

    /// Close the stream.
    fn close(&mut self) {
        self.closed = true;
    }

    /// Context manager enter.
    fn __enter__(slf: PyRef<'_, Self>) -> PyRef<'_, Self> {
        slf
    }

    /// Context manager exit.
    fn __exit__(
        &mut self,
        _exc_type: Option<&Bound<'_, PyAny>>,
        _exc_val: Option<&Bound<'_, PyAny>>,
        _exc_tb: Option<&Bound<'_, PyAny>>,
    ) -> bool {
        false // Don't suppress exceptions
    }

    /// Encoding property - needed for compatibility.
    #[getter]
    fn encoding(&self, py: Python<'_>) -> PyResult<Option<String>> {
        match self.original.getattr(py, "encoding") {
            Ok(enc) => enc.extract(py),
            Err(_) => Ok(Some("utf-8".to_string())),
        }
    }

    /// Newlines property - needed for compatibility.
    #[getter]
    fn newlines(&self) -> Option<String> {
        None
    }

    /// Buffer property - some code checks for this.
    #[getter]
    fn buffer(&self, py: Python<'_>) -> PyResult<Py<PyAny>> {
        // Return original's buffer if it has one, otherwise return self
        match self.original.getattr(py, "buffer") {
            Ok(buf) => Ok(buf),
            Err(_) => Ok(self.original.clone_ref(py)),
        }
    }
}

impl SlotLogWriter {
    /// Create a new stdout writer with fallback to original.
    pub fn new_stdout(original: Py<PyAny>) -> Self {
        Self {
            source: LogSource::Stdout,
            original,
            closed: false,
        }
    }

    /// Create a new stderr writer with fallback to original.
    pub fn new_stderr(original: Py<PyAny>) -> Self {
        Self {
            source: LogSource::Stderr,
            original,
            closed: false,
        }
    }

    /// Write to the original stream.
    fn write_to_original(&self, py: Python<'_>, data: &str) -> PyResult<()> {
        self.original.call_method1(py, "write", (data,))?;
        Ok(())
    }
}

// ============================================================================
// Installation - called once at worker startup
// ============================================================================

/// Install SlotLogWriters as sys.stdout/stderr.
/// Called once at worker startup. The writers persist for the lifetime of the process.
/// Returns true if installation succeeded.
pub fn install_slot_log_writers(py: Python<'_>) -> PyResult<bool> {
    let sys = py.import("sys")?;

    // Get originals
    let original_stdout = sys.getattr("stdout")?.unbind();
    let original_stderr = sys.getattr("stderr")?.unbind();

    // Create writers
    let stdout_writer = SlotLogWriter::new_stdout(original_stdout);
    let stderr_writer = SlotLogWriter::new_stderr(original_stderr);

    // Install
    sys.setattr("stdout", stdout_writer.into_pyobject(py)?)?;
    sys.setattr("stderr", stderr_writer.into_pyobject(py)?)?;

    // Initialize the ContextVar
    get_slot_contextvar(py)?;

    tracing::debug!("Installed SlotLogWriters with ContextVar routing");
    Ok(true)
}

// ============================================================================
// SlotLogGuard - RAII guard for prediction context
// ============================================================================

/// RAII guard that sets the current slot in the ContextVar.
///
/// On creation, registers the SlotSender and sets the ContextVar.
/// On drop, resets the ContextVar and unregisters the sender.
pub struct SlotLogGuard {
    slot_id: SlotId,
    token: Py<PyAny>,
}

impl SlotLogGuard {
    /// Enter prediction context for a slot.
    ///
    /// Registers the sender and sets the ContextVar.
    pub fn enter(py: Python<'_>, slot_id: SlotId, sender: Arc<SlotSender>) -> PyResult<Self> {
        // Register sender in global registry
        register_slot(slot_id, sender);

        // Set ContextVar
        let token = set_current_slot(py, slot_id)?;

        Ok(Self { slot_id, token })
    }

    /// Exit prediction context.
    ///
    /// Resets the ContextVar and unregisters the sender.
    pub fn exit(self, py: Python<'_>) -> PyResult<()> {
        // Reset ContextVar
        reset_current_slot(py, &self.token)?;

        // Unregister sender
        unregister_slot(self.slot_id);

        Ok(())
    }
}

impl Drop for SlotLogGuard {
    fn drop(&mut self) {
        // Best-effort cleanup
        unregister_slot(self.slot_id);
        
        // Note: We can't reset the ContextVar here without the GIL.
        // The caller should call exit() explicitly in async contexts.
        // For sync predictions, the ContextVar reset happens naturally
        // when the task ends.
    }
}

// ============================================================================
// Tests
// ============================================================================

#[cfg(test)]
mod tests {
    use super::*;
    use coglet_worker::SlotResponse;
    use tokio::sync::mpsc;

    #[test]
    fn registry_operations() {
        let slot_id = SlotId::new();
        let (tx, _rx) = mpsc::unbounded_channel();
        let sender = Arc::new(SlotSender::new(tx));

        // Register
        register_slot(slot_id, sender.clone());
        assert!(get_slot_sender(slot_id).is_some());

        // Unregister
        unregister_slot(slot_id);
        assert!(get_slot_sender(slot_id).is_none());
    }

    #[test]
    fn slot_sender_sends_log() {
        let (tx, mut rx) = mpsc::unbounded_channel();
        let sender = SlotSender::new(tx);

        sender.send_log(LogSource::Stdout, "hello").unwrap();

        let msg = rx.try_recv().unwrap();
        match msg {
            SlotResponse::Log { source, data } => {
                assert_eq!(source, LogSource::Stdout);
                assert_eq!(data, "hello");
            }
            _ => panic!("expected Log message"),
        }
    }

    #[test]
    fn slot_sender_ignores_empty() {
        let (tx, mut rx) = mpsc::unbounded_channel();
        let sender = SlotSender::new(tx);

        sender.send_log(LogSource::Stderr, "").unwrap();

        // No message should be sent
        assert!(rx.try_recv().is_err());
    }

    #[test]
    fn slot_sender_detects_closed_channel() {
        let (tx, rx) = mpsc::unbounded_channel::<SlotResponse>();
        drop(rx); // Close receiver

        let sender = SlotSender::new(tx);
        let result = sender.send_log(LogSource::Stdout, "hello");

        assert!(result.is_err());
    }
}
