//! Log routing via prediction_id ContextVar.
//!
//! Architecture:
//! - Rust owns a ContextVar `_coglet_prediction_id` that holds the current prediction ID
//! - Rust maintains a registry mapping prediction_id → SlotSender
//! - SlotLogWriter reads the ContextVar to route logs to the correct sender
//!
//! This design supports:
//! - Async predictions with proper per-task isolation (ContextVar is task-local)
//! - Orphan task detection (prediction completed but task still running)
//! - Slot reuse safety (new prediction = new ID, old tasks can't pollute)
//! - Setup logs routed through control channel before predictions start
//!
//! The ContextVar is private (`_coglet_` prefix). Users who need the prediction ID
//! should use the public API (e.g., `cog.current_prediction_id()`) which we'll
//! inject onto the cog namespace later.

use std::collections::HashMap;
use std::sync::{Arc, Mutex, OnceLock};

use pyo3::prelude::*;

use coglet_worker::{LogSource, SlotSender};

// ============================================================================
// Rust-owned ContextVar for prediction routing
// ============================================================================

/// The Rust-owned ContextVar instance. Created once, lives forever.
/// Named `cog_prediction_id` - documented as internal, don't modify.
static PREDICTION_CONTEXTVAR: OnceLock<Py<PyAny>> = OnceLock::new();

/// Registry mapping prediction_id → SlotSender.
/// When a prediction starts, we register the sender.
/// When SlotLogWriter.write() is called, we look up the sender here.
static PREDICTION_REGISTRY: OnceLock<Mutex<HashMap<String, Arc<SlotSender>>>> = OnceLock::new();

/// Current sync prediction ID.
/// For sync predictions (single slot, blocking), there's exactly one active prediction.
/// ContextVars don't work across separate Python::attach calls, so we use this.
/// Protected by mutex since it's accessed from Python callbacks.
static SYNC_PREDICTION_ID: OnceLock<Mutex<Option<String>>> = OnceLock::new();

fn get_sync_prediction_id_slot() -> &'static Mutex<Option<String>> {
    SYNC_PREDICTION_ID.get_or_init(|| Mutex::new(None))
}

/// Setup log sender - used when outside prediction context during setup.
/// Set by worker before setup(), cleared after setup completes.
static SETUP_LOG_SENDER: OnceLock<Mutex<Option<Arc<SetupLogSender>>>> = OnceLock::new();

fn get_registry() -> &'static Mutex<HashMap<String, Arc<SlotSender>>> {
    PREDICTION_REGISTRY.get_or_init(|| Mutex::new(HashMap::new()))
}

fn get_setup_sender_slot() -> &'static Mutex<Option<Arc<SetupLogSender>>> {
    SETUP_LOG_SENDER.get_or_init(|| Mutex::new(None))
}

// ============================================================================
// SetupLogSender - sends logs via control channel during setup
// ============================================================================

use tokio::sync::mpsc::UnboundedSender;
use coglet_worker::ControlResponse;

/// Sender for logs during setup (before slots are active).
/// Sends through the control channel.
pub struct SetupLogSender {
    tx: UnboundedSender<ControlResponse>,
}

impl SetupLogSender {
    /// Create a new setup log sender.
    pub fn new(tx: UnboundedSender<ControlResponse>) -> Self {
        Self { tx }
    }

    /// Send a log message.
    pub fn send_log(&self, source: LogSource, data: &str) -> Result<(), String> {
        self.tx
            .send(ControlResponse::Log {
                source,
                data: data.to_string(),
            })
            .map_err(|e| e.to_string())
    }
}

/// Register the setup log sender.
/// Called by worker before setup().
pub fn register_setup_sender(sender: Arc<SetupLogSender>) {
    let mut slot = get_setup_sender_slot().lock().unwrap();
    *slot = Some(sender);
}

/// Unregister the setup log sender.
/// Called by worker after setup() completes.
pub fn unregister_setup_sender() {
    let mut slot = get_setup_sender_slot().lock().unwrap();
    *slot = None;
}

/// Get the setup log sender if registered.
fn get_setup_sender() -> Option<Arc<SetupLogSender>> {
    let slot = get_setup_sender_slot().lock().unwrap();
    slot.clone()
}

/// Get or create the Rust-owned ContextVar.
fn get_prediction_contextvar(py: Python<'_>) -> PyResult<&'static Py<PyAny>> {
    if let Some(cv) = PREDICTION_CONTEXTVAR.get() {
        return Ok(cv);
    }

    let contextvars = py.import("contextvars")?;
    let cv = contextvars.call_method1("ContextVar", ("_coglet_prediction_id",))?;

    // Try to store it. Race is fine - worst case we create an extra that gets dropped.
    let _ = PREDICTION_CONTEXTVAR.set(cv.unbind());
    Ok(PREDICTION_CONTEXTVAR.get().unwrap())
}

/// Register a SlotSender for a prediction ID.
/// Called when starting a prediction.
pub fn register_prediction(prediction_id: String, sender: Arc<SlotSender>) {
    let mut registry = get_registry().lock().unwrap();
    tracing::trace!(%prediction_id, "Registering prediction sender");
    registry.insert(prediction_id, sender);
}

/// Unregister a prediction ID.
/// Called when prediction completes.
pub fn unregister_prediction(prediction_id: &str) {
    let mut registry = get_registry().lock().unwrap();
    registry.remove(prediction_id);
    
    // Clear sync prediction ID if it matches
    let mut slot = get_sync_prediction_id_slot().lock().unwrap();
    if slot.as_deref() == Some(prediction_id) {
        *slot = None;
    }
}

/// Get the SlotSender for a prediction ID.
fn get_prediction_sender(prediction_id: &str) -> Option<Arc<SlotSender>> {
    let registry = get_registry().lock().unwrap();
    registry.get(prediction_id).cloned()
}

/// Set the current prediction ID in the ContextVar (for async).
/// Returns a token that can be used to reset (for explicit cleanup).
pub fn set_current_prediction(py: Python<'_>, prediction_id: &str) -> PyResult<Py<PyAny>> {
    // Set ContextVar for async predictions
    let cv = get_prediction_contextvar(py)?;
    let token = cv.call_method1(py, "set", (prediction_id,))?;
    Ok(token)
}

/// Set the current sync prediction ID (for sync predictions only).
/// Call this before running a sync prediction, clear after.
pub fn set_sync_prediction_id(prediction_id: Option<&str>) {
    let mut slot = get_sync_prediction_id_slot().lock().unwrap();
    *slot = prediction_id.map(|s| s.to_string());
}

/// Get the current prediction ID from sync static or ContextVar.
/// Returns None if not set (outside prediction context).
fn get_current_prediction_id(py: Python<'_>) -> PyResult<Option<String>> {
    // First check sync prediction static (works for sync predictions)
    {
        let slot = get_sync_prediction_id_slot().lock().unwrap();
        if let Some(ref prediction_id) = *slot {
            tracing::trace!(%prediction_id, "Sync prediction ID found");
            return Ok(Some(prediction_id.clone()));
        }
    }
    
    // Fall back to ContextVar (works for async predictions)
    let cv = get_prediction_contextvar(py)?;

    // Try to get the value - returns the value or raises LookupError
    match cv.call_method0(py, "get") {
        Ok(val) => {
            let prediction_id: String = val.extract(py)?;
            tracing::trace!(%prediction_id, "ContextVar lookup succeeded");
            Ok(Some(prediction_id))
        }
        Err(e) if e.is_instance_of::<pyo3::exceptions::PyLookupError>(py) => {
            // ContextVar not set - outside prediction context
            // Don't log here - caller will log the routing decision
            Ok(None)
        }
        Err(e) => Err(e),
    }
}

// ============================================================================
// SlotLogWriter - routes via ContextVar lookup
// ============================================================================

/// A Python file-like object that routes writes via the prediction_id ContextVar.
///
/// This is installed as sys.stdout/stderr once at worker startup.
/// Each write looks up the current prediction_id from the ContextVar and routes
/// to the appropriate SlotSender.
///
/// If no prediction_id is set, or the prediction has completed (orphan task),
/// writes go to tracing (logged as orphan).
#[pyclass]
pub struct SlotLogWriter {
    /// Which stream this captures (stdout or stderr).
    source: LogSource,
    /// Original stream (used for delegation of methods like isatty, fileno).
    original: Py<PyAny>,
    /// Whether writes should be ignored (used after errors).
    #[pyo3(get)]
    closed: bool,
}

#[pymethods]
impl SlotLogWriter {
    /// Write data, routing to the appropriate destination.
    ///
    /// Priority:
    /// 1. If inside a prediction (ContextVar set), route to slot sender
    /// 2. If setup sender registered, route to control channel  
    /// 3. Fall back to stderr (for orphan tasks or unexpected cases)
    fn write(&self, py: Python<'_>, data: &str) -> PyResult<usize> {
        if self.closed || data.is_empty() {
            return Ok(data.len());
        }

        // Try to get current prediction from ContextVar
        match get_current_prediction_id(py)? {
            Some(prediction_id) => {
                // Have prediction ID - check if still active
                if let Some(sender) = get_prediction_sender(&prediction_id) {
                    // Active prediction - route to slot
                    tracing::trace!(
                        prediction_id = %prediction_id,
                        source = ?self.source,
                        bytes = data.len(),
                        "Log routed to slot"
                    );
                    sender.send_log(self.source, data).map_err(|e| {
                        pyo3::exceptions::PyIOError::new_err(e.to_string())
                    })?;
                } else {
                    // Orphan task - prediction completed but task still running
                    tracing::trace!(
                        prediction_id = %prediction_id,
                        source = ?self.source,
                        "Orphan log (prediction completed)"
                    );
                    self.write_outside_prediction(py, data)?;
                }
            }
            None => {
                // Outside prediction context
                // Try setup sender (for setup logs), then fallback to stderr
                self.write_outside_prediction(py, data)?;
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
    /// Create a new stdout writer.
    pub fn new_stdout(original: Py<PyAny>) -> Self {
        Self {
            source: LogSource::Stdout,
            original,
            closed: false,
        }
    }

    /// Create a new stderr writer.
    pub fn new_stderr(original: Py<PyAny>) -> Self {
        Self {
            source: LogSource::Stderr,
            original,
            closed: false,
        }
    }

    /// Write when outside prediction context.
    /// 
    /// During setup: routes to control channel (for health-check).
    /// Otherwise: emits via tracing to stderr locally (not shipped).
    fn write_outside_prediction(&self, _py: Python<'_>, data: &str) -> PyResult<()> {
        // Try setup sender (registered during setup phase)
        if let Some(sender) = get_setup_sender() {
            if sender.send_log(self.source, data).is_ok() {
                tracing::trace!(
                    source = ?self.source,
                    bytes = data.len(),
                    "Log routed via control channel (setup)"
                );
                return Ok(());
            }
            // If send fails, fall through to tracing
        }
        // Outside setup/prediction context - orphan log
        // This happens with orphan tasks or edge cases
        for line in data.lines() {
            tracing::info!(target: "coglet::orphan", "{}", line);
        }
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
    get_prediction_contextvar(py)?;

    tracing::debug!("Installed SlotLogWriters with prediction_id routing");
    Ok(true)
}

// ============================================================================
// PredictionLogGuard - RAII guard for prediction context
// ============================================================================

/// RAII guard that sets the current prediction in the ContextVar.
///
/// On creation, registers the SlotSender and sets the ContextVar.
/// On drop, unregisters the prediction (but ContextVar reset is automatic for async).
pub struct PredictionLogGuard {
    prediction_id: String,
    #[allow(dead_code)]
    token: Py<PyAny>,
}

impl PredictionLogGuard {
    /// Enter prediction context.
    ///
    /// Registers the sender and sets the ContextVar.
    pub fn enter(py: Python<'_>, prediction_id: String, sender: Arc<SlotSender>) -> PyResult<Self> {
        tracing::debug!(%prediction_id, "PredictionLogGuard::enter - registering sender");
        // Register sender in global registry
        register_prediction(prediction_id.clone(), sender);

        tracing::debug!(%prediction_id, "PredictionLogGuard::enter - setting ContextVar");
        // Set ContextVar
        let token = set_current_prediction(py, &prediction_id)?;

        tracing::debug!(%prediction_id, "PredictionLogGuard::enter - done");
        Ok(Self { prediction_id, token })
    }

    /// Get the prediction ID.
    pub fn prediction_id(&self) -> &str {
        &self.prediction_id
    }
}

impl Drop for PredictionLogGuard {
    fn drop(&mut self) {
        // Unregister prediction - this makes orphan tasks fall back to stderr
        unregister_prediction(&self.prediction_id);

        // Note: We don't reset the ContextVar here because:
        // 1. For sync: the context resets naturally when the function returns
        // 2. For async: each task has its own ContextVar copy, no reset needed
        // The token is kept just in case we need explicit reset in the future.
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
        let prediction_id = "pred_123".to_string();
        let (tx, _rx) = mpsc::unbounded_channel();
        let sender = Arc::new(SlotSender::new(tx));

        // Register
        register_prediction(prediction_id.clone(), sender.clone());
        assert!(get_prediction_sender(&prediction_id).is_some());

        // Unregister
        unregister_prediction(&prediction_id);
        assert!(get_prediction_sender(&prediction_id).is_none());
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
