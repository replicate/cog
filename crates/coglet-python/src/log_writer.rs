//! Log routing via prediction_id ContextVar.
//!
//! Architecture:
//! - Rust owns a ContextVar `_coglet_prediction_id` that holds the current prediction ID
//! - Rust maintains a registry mapping prediction_id -> SlotSender
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
use pyo3_stub_gen::derive::*;

use coglet_core::bridge::protocol::{ControlResponse, LogSource};
use coglet_core::worker::SlotSender;
use tokio::sync::mpsc::Sender;

// ============================================================================
// Rust-owned ContextVar for prediction routing
// ============================================================================

/// The Rust-owned ContextVar instance. Created once, lives forever.
/// Named `cog_prediction_id` - documented as internal, don't modify.
static PREDICTION_CONTEXTVAR: OnceLock<Py<PyAny>> = OnceLock::new();

/// Registry mapping prediction_id -> SlotSender.
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

/// Control channel log sender - used when outside prediction context.
/// Set by worker before setup(), lives for entire worker lifetime.
static CONTROL_CHANNEL_LOG_SENDER: OnceLock<Mutex<Option<Arc<ControlChannelLogSender>>>> =
    OnceLock::new();

fn get_registry() -> &'static Mutex<HashMap<String, Arc<SlotSender>>> {
    PREDICTION_REGISTRY.get_or_init(|| Mutex::new(HashMap::new()))
}

fn get_control_channel_sender_slot() -> &'static Mutex<Option<Arc<ControlChannelLogSender>>> {
    CONTROL_CHANNEL_LOG_SENDER.get_or_init(|| Mutex::new(None))
}

// ============================================================================
// ControlChannelLogSender - sends logs via control channel
// ============================================================================

/// Sender for logs that go through the control channel.
/// Used for Python logs during setup and subprocess output throughout worker lifetime.
pub struct ControlChannelLogSender {
    tx: Sender<ControlResponse>,
}

impl ControlChannelLogSender {
    /// Create a new control channel log sender.
    pub fn new(tx: Sender<ControlResponse>) -> Self {
        Self { tx }
    }

    /// Try to send a log message.
    ///
    /// Uses try_send() to avoid blocking (called from Python code on tokio runtime threads).
    /// If the channel is full, the log is dropped and counted for periodic reporting.
    pub fn try_send_log(&self, source: LogSource, data: &str) {
        if self
            .tx
            .try_send(ControlResponse::Log {
                source,
                data: data.to_string(),
            })
            .is_err()
        {
            coglet_core::worker::increment_dropped_log_count();
        }
    }
}

// NOTE: All mutex locks in the worker use .expect().
//
// If a mutex is poisoned (another thread panicked while holding it), the worker
// is in an unrecoverable state. We cannot safely continue because:
// - Log routing shares channels with prediction updates
// - Prediction→slot mappings could be inconsistent
// - Continuing risks cross-prediction data bleed
//
// The panic hook installed by coglet_core::worker sends a Fatal IPC message
// to the parent (which poisons all slots) and aborts the process.

/// Register the control channel log sender.
/// Called by worker before setup().
pub fn register_control_channel_sender(sender: Arc<ControlChannelLogSender>) {
    let mut slot = get_control_channel_sender_slot()
        .lock()
        .expect("control_channel_sender mutex poisoned");
    *slot = Some(sender);
}

/// Unregister the control channel log sender.
/// Called by worker when shutting down (not after setup).
#[allow(dead_code)]
pub fn unregister_control_channel_sender() {
    let mut slot = get_control_channel_sender_slot()
        .lock()
        .expect("control_channel_sender mutex poisoned");
    *slot = None;
}

/// Get the control channel log sender if registered.
fn get_control_channel_sender() -> Option<Arc<ControlChannelLogSender>> {
    let slot = get_control_channel_sender_slot()
        .lock()
        .expect("control_channel_sender mutex poisoned");
    slot.clone()
}

/// Get or create the Rust-owned ContextVar.
///
/// This returns the same ContextVar instance used by SlotLogWriter for log routing.
/// Public so predictor.rs can pass it to async coroutine wrappers.
pub fn get_prediction_contextvar(py: Python<'_>) -> PyResult<&'static Py<PyAny>> {
    if let Some(cv) = PREDICTION_CONTEXTVAR.get() {
        return Ok(cv);
    }

    let contextvars = py.import("contextvars")?;
    let cv = contextvars.call_method1("ContextVar", ("_coglet_prediction_id",))?;

    // Try to store it. Race is fine - if another thread won, use their value.
    match PREDICTION_CONTEXTVAR.set(cv.unbind()) {
        Ok(()) => {}
        Err(_already_set) => {
            // Another thread initialized it first - that's fine
        }
    }

    // This should always succeed now - either we set it or another thread did.
    PREDICTION_CONTEXTVAR.get().ok_or_else(|| {
        pyo3::exceptions::PyRuntimeError::new_err(
            "Failed to initialize prediction context variable",
        )
    })
}

/// Register a SlotSender for a prediction ID.
/// Called when starting a prediction.
pub fn register_prediction(prediction_id: String, sender: Arc<SlotSender>) {
    let mut registry = get_registry()
        .lock()
        .expect("prediction_registry mutex poisoned");
    tracing::trace!(%prediction_id, "Registering prediction sender");
    registry.insert(prediction_id, sender);
}

/// Unregister a prediction ID.
/// Called when prediction completes.
pub fn unregister_prediction(prediction_id: &str) {
    let mut registry = get_registry()
        .lock()
        .expect("prediction_registry mutex poisoned");
    registry.remove(prediction_id);

    // Clear sync prediction ID if it matches
    let mut slot = get_sync_prediction_id_slot()
        .lock()
        .expect("sync_prediction_id mutex poisoned");
    if slot.as_deref() == Some(prediction_id) {
        *slot = None;
    }
}

/// Get the SlotSender for a prediction ID.
fn get_prediction_sender(prediction_id: &str) -> Option<Arc<SlotSender>> {
    let registry = get_registry()
        .lock()
        .expect("prediction_registry mutex poisoned");
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
    let mut slot = get_sync_prediction_id_slot()
        .lock()
        .expect("sync_prediction_id mutex poisoned");
    *slot = prediction_id.map(|s| s.to_string());
}

/// Get the current prediction ID from sync static or ContextVar.
/// Returns None if not set (outside prediction context).
fn get_current_prediction_id(py: Python<'_>) -> PyResult<Option<String>> {
    // First check sync prediction static (works for sync predictions)
    {
        let slot = get_sync_prediction_id_slot()
            .lock()
            .expect("sync_prediction_id mutex poisoned");
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
///
/// Uses line buffering: accumulates writes until a newline is received, then
/// emits complete lines. This coalesces Python's print() which does separate
/// writes for content and newline.
#[gen_stub_pyclass]
#[pyclass(name = "_SlotLogWriter", module = "coglet._sdk")]
pub struct SlotLogWriter {
    /// Which stream this captures (stdout or stderr).
    source: LogSource,
    /// Original stream (used for delegation of methods like isatty, fileno).
    original: Py<PyAny>,
    /// Whether writes should be ignored (used after errors).
    #[pyo3(get)]
    closed: bool,
    /// Line buffer for coalescing writes into complete lines.
    line_buffer: Mutex<String>,
}

#[gen_stub_pymethods]
#[pymethods]
impl SlotLogWriter {
    /// Write data, routing to the appropriate destination.
    ///
    /// Uses line buffering: accumulates data until a newline is received, then
    /// emits complete lines. This coalesces Python's print() which does separate
    /// writes for content and the trailing newline.
    ///
    /// Priority for routing:
    /// 1. If inside a prediction (ContextVar set), route to slot sender
    /// 2. If setup sender registered, route to control channel  
    /// 3. Fall back to stderr (for orphan tasks or unexpected cases)
    fn write(&self, py: Python<'_>, data: &str) -> PyResult<usize> {
        if self.closed || data.is_empty() {
            return Ok(data.len());
        }

        let len = data.len();

        // Append to line buffer and extract complete lines
        let complete = {
            let mut buffer = self.line_buffer.lock().expect("line_buffer mutex poisoned");
            buffer.push_str(data);

            // Check if we have complete lines to emit
            if let Some(last_newline) = buffer.rfind('\n') {
                // Extract complete lines (including the newline)
                let complete = buffer[..=last_newline].to_string();
                // Keep remainder in buffer
                let remainder = buffer[last_newline + 1..].to_string();
                *buffer = remainder;
                Some(complete)
            } else {
                None
            }
        };

        // Emit complete lines (outside lock)
        if let Some(complete) = complete {
            self.emit_data(py, &complete)?;
        }

        Ok(len)
    }

    /// Emit data to the appropriate destination.
    fn emit_data(&self, py: Python<'_>, data: &str) -> PyResult<()> {
        if data.is_empty() {
            return Ok(());
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
                    sender
                        .send_log(self.source, data)
                        .map_err(|e| pyo3::exceptions::PyIOError::new_err(e.to_string()))?;
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

        Ok(())
    }

    /// Flush the stream.
    ///
    /// Emits any buffered content that hasn't been terminated with a newline.
    fn flush(&self, py: Python<'_>) -> PyResult<()> {
        // Emit any buffered content
        let buffered = {
            let mut buffer = self.line_buffer.lock().expect("line_buffer mutex poisoned");
            std::mem::take(&mut *buffer)
        };
        if !buffered.is_empty() {
            self.emit_data(py, &buffered)?;
        }

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
            line_buffer: Mutex::new(String::new()),
        }
    }

    /// Create a new stderr writer.
    pub fn new_stderr(original: Py<PyAny>) -> Self {
        Self {
            source: LogSource::Stderr,
            original,
            closed: false,
            line_buffer: Mutex::new(String::new()),
        }
    }

    /// Write when outside prediction context.
    ///
    /// During setup: routes to control channel (for health-check).
    /// Otherwise: emits via tracing to stderr locally (not shipped).
    fn write_outside_prediction(&self, _py: Python<'_>, data: &str) -> PyResult<()> {
        // Try control channel sender (registered for worker lifetime)
        if let Some(sender) = get_control_channel_sender() {
            sender.try_send_log(self.source, data);
            tracing::trace!(
                source = ?self.source,
                bytes = data.len(),
                "Log routed via control channel"
            );
            return Ok(());
        }
        // Outside setup/prediction context - orphan log
        // This happens with orphan tasks or edge cases
        for line in data.lines() {
            tracing::info!(target: "coglet::user", "{}", line);
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
        // Register sender in global registry
        register_prediction(prediction_id.clone(), sender);

        // Set ContextVar
        let token = set_current_prediction(py, &prediction_id)?;

        tracing::trace!(%prediction_id, "Entered prediction log context");
        Ok(Self {
            prediction_id,
            token,
        })
    }

    /// Get the prediction ID.
    #[allow(dead_code)]
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
    use coglet_core::bridge::protocol::SlotResponse;
    use tokio::sync::mpsc;

    #[test]
    fn registry_operations() {
        let prediction_id = "pred_123".to_string();
        let (tx, _rx) = mpsc::unbounded_channel();
        let sender = Arc::new(SlotSender::new(tx, std::env::temp_dir()));

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
        let sender = SlotSender::new(tx, std::env::temp_dir());

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
        let sender = SlotSender::new(tx, std::env::temp_dir());

        sender.send_log(LogSource::Stderr, "").unwrap();

        // No message should be sent
        assert!(rx.try_recv().is_err());
    }

    #[test]
    fn slot_sender_detects_closed_channel() {
        let (tx, rx) = mpsc::unbounded_channel::<SlotResponse>();
        drop(rx); // Close receiver

        let sender = SlotSender::new(tx, std::env::temp_dir());
        let result = sender.send_log(LogSource::Stdout, "hello");

        assert!(result.is_err());
    }
}
