//! SlotLogWriter - captures Python stdout/stderr and sends through slot socket.
//!
//! This module provides a PyO3 class that replaces sys.stdout/stderr during predictions.
//! Writes are immediately sent as framed SlotResponse::Log messages through the slot socket.
//! This allows per-slot log streaming without head-of-line blocking.

use std::io;
use std::sync::Arc;

use pyo3::prelude::*;
use tokio::sync::mpsc;

use coglet_worker::{LogSource, SlotResponse};

// ============================================================================
// SlotSender - sends messages on slot socket
// ============================================================================

/// Handle for sending messages on a slot socket.
///
/// This is cloned for each SlotLogWriter (stdout, stderr) on a slot.
/// Thread-safe via tokio mpsc channel.
#[derive(Clone)]
pub struct SlotSender {
    /// Channel sender for slot responses.
    tx: mpsc::UnboundedSender<SlotResponse>,
}

impl SlotSender {
    /// Create a new slot sender with the given channel.
    pub fn new(tx: mpsc::UnboundedSender<SlotResponse>) -> Self {
        Self { tx }
    }

    /// Send a log message.
    pub fn send_log(&self, source: LogSource, data: &str) -> io::Result<()> {
        if data.is_empty() {
            return Ok(());
        }

        let msg = SlotResponse::Log {
            source,
            data: data.to_string(),
        };

        self.tx.send(msg).map_err(|_| {
            io::Error::new(io::ErrorKind::BrokenPipe, "slot socket closed")
        })
    }

    /// Send a streaming output value.
    pub fn send_output(&self, output: serde_json::Value) -> io::Result<()> {
        let msg = SlotResponse::Output { output };
        self.tx.send(msg).map_err(|_| {
            io::Error::new(io::ErrorKind::BrokenPipe, "slot socket closed")
        })
    }
}

// ============================================================================
// SlotLogWriter - PyO3 class implementing Python file protocol
// ============================================================================

/// A Python file-like object that sends writes to a slot socket.
///
/// Replaces sys.stdout or sys.stderr during predictions. Each write is
/// immediately sent as a SlotResponse::Log message, enabling real-time
/// log streaming.
///
/// Implements the Python file protocol (write, flush, readable, writable, seekable).
#[pyclass]
pub struct SlotLogWriter {
    /// Which stream this captures (stdout or stderr).
    source: LogSource,
    /// Handle for sending to the slot socket.
    sender: Arc<SlotSender>,
    /// Whether writes should be ignored (used after errors).
    #[pyo3(get)]
    closed: bool,
}

#[pymethods]
impl SlotLogWriter {
    /// Write data to the slot socket.
    ///
    /// Returns the number of characters written (always the full string).
    /// Empty writes are ignored (Python sometimes writes empty strings).
    fn write(&self, data: &str) -> PyResult<usize> {
        if self.closed || data.is_empty() {
            return Ok(data.len());
        }

        self.sender.send_log(self.source, data).map_err(|e| {
            pyo3::exceptions::PyIOError::new_err(e.to_string())
        })?;

        Ok(data.len())
    }

    /// Flush the stream.
    ///
    /// This is a no-op because writes are sent immediately.
    fn flush(&self) -> PyResult<()> {
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
    fn isatty(&self) -> bool {
        false
    }

    /// Return the file number (not applicable).
    fn fileno(&self) -> PyResult<i32> {
        Err(pyo3::exceptions::PyOSError::new_err(
            "SlotLogWriter does not have a file descriptor",
        ))
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
        self.closed = true;
        false // Don't suppress exceptions
    }
}

impl SlotLogWriter {
    /// Create a new stdout writer for a slot.
    pub fn stdout(sender: Arc<SlotSender>) -> Self {
        Self {
            source: LogSource::Stdout,
            sender,
            closed: false,
        }
    }

    /// Create a new stderr writer for a slot.
    pub fn stderr(sender: Arc<SlotSender>) -> Self {
        Self {
            source: LogSource::Stderr,
            sender,
            closed: false,
        }
    }
}

// ============================================================================
// SlotLogGuard - RAII guard for stdout/stderr replacement
// ============================================================================

/// RAII guard that replaces sys.stdout/stderr with SlotLogWriters.
///
/// On creation, saves the original stdout/stderr and installs SlotLogWriters.
/// On drop, restores the original streams.
///
/// This ensures Python prints and logging go through the slot socket during
/// prediction, without leaking to the control channel.
pub struct SlotLogGuard {
    original_stdout: Py<PyAny>,
    original_stderr: Py<PyAny>,
}

impl SlotLogGuard {
    /// Install slot log writers, returning a guard that restores originals on drop.
    ///
    /// Returns None if installation fails (shouldn't happen in practice).
    pub fn install(py: Python<'_>, sender: Arc<SlotSender>) -> Option<Self> {
        let sys = py.import("sys").ok()?;

        // Save originals
        let original_stdout = sys.getattr("stdout").ok()?.unbind();
        let original_stderr = sys.getattr("stderr").ok()?.unbind();

        // Create slot writers
        let stdout_writer = SlotLogWriter::stdout(sender.clone());
        let stderr_writer = SlotLogWriter::stderr(sender);

        // Install them
        sys.setattr("stdout", stdout_writer.into_pyobject(py).ok()?)
            .ok()?;
        sys.setattr("stderr", stderr_writer.into_pyobject(py).ok()?)
            .ok()?;

        Some(Self {
            original_stdout,
            original_stderr,
        })
    }

    /// Restore original stdout/stderr.
    ///
    /// Called automatically on drop, but can be called explicitly.
    pub fn restore(self, py: Python<'_>) {
        self.restore_impl(py);
    }

    fn restore_impl(&self, py: Python<'_>) {
        if let Ok(sys) = py.import("sys") {
            let _ = sys.setattr("stdout", self.original_stdout.bind(py));
            let _ = sys.setattr("stderr", self.original_stderr.bind(py));
        }
    }
}

impl Drop for SlotLogGuard {
    fn drop(&mut self) {
        // Try to restore - this requires the GIL
        // In async context, we may not have the GIL on drop
        // Caller should explicitly call restore() when possible
        Python::attach(|py| {
            self.restore_impl(py);
        });
    }
}

// ============================================================================
// Tests
// ============================================================================

#[cfg(test)]
mod tests {
    use super::*;
    use tokio::sync::mpsc;

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
