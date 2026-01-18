//! Worker subprocess - runs inside the Python subprocess.
//!
//! This module provides the child-side of the worker subprocess protocol.
//! The parent side (spawning, message routing) is in orchestrator.rs.
//!
//! Architecture:
//! - Control channel (stdin/stdout): Cancel, Shutdown signals + Ready, Idle responses
//! - Slot sockets: Prediction data + streaming logs
//!
//! Each slot runs predictions independently.

use std::collections::HashMap;
use std::io;
use std::sync::Arc;

use futures::{SinkExt, StreamExt};
use tokio::io::{stdin, stdout};
use tokio::sync::mpsc;
use tokio_util::codec::{FramedRead, FramedWrite};

use crate::bridge::codec::JsonCodec;
use crate::bridge::protocol::{
    ControlRequest, ControlResponse, LogSource, SlotId, SlotOutcome, SlotRequest, SlotResponse,
};
use crate::bridge::transport::{connect_transport, get_transport_info_from_env};

type SlotWriter =
    Arc<tokio::sync::Mutex<FramedWrite<tokio::net::unix::OwnedWriteHalf, JsonCodec<SlotResponse>>>>;

/// Handle for sending messages on a slot socket.
///
/// Used by log writers to stream logs during prediction. Thread-safe via
/// tokio mpsc channel - logs are queued and written asynchronously.
#[derive(Clone)]
pub struct SlotSender {
    tx: mpsc::UnboundedSender<SlotResponse>,
}

impl SlotSender {
    pub fn new(tx: mpsc::UnboundedSender<SlotResponse>) -> Self {
        Self { tx }
    }

    pub fn send_log(&self, source: LogSource, data: &str) -> io::Result<()> {
        if data.is_empty() {
            return Ok(());
        }

        let msg = SlotResponse::Log {
            source,
            data: data.to_string(),
        };

        self.tx
            .send(msg)
            .map_err(|_| io::Error::new(io::ErrorKind::BrokenPipe, "slot channel closed"))
    }

    pub fn send_output(&self, output: serde_json::Value) -> io::Result<()> {
        let msg = SlotResponse::Output { output };
        self.tx
            .send(msg)
            .map_err(|_| io::Error::new(io::ErrorKind::BrokenPipe, "slot channel closed"))
    }
}

/// Setup phase errors.
///
/// These errors occur during predictor loading and setup, before predictions
/// can run. They affect health status (SETUP_FAILED) rather than HTTP status.
#[derive(Debug, thiserror::Error)]
pub enum SetupError {
    /// Failed to import or instantiate the predictor class.
    #[error("failed to load predictor: {message}")]
    Load { message: String },

    /// The setup() method raised an exception.
    #[error("setup failed: {message}")]
    Setup { message: String },

    /// Internal error (e.g., GIL acquisition failed).
    #[error("internal error: {message}")]
    Internal { message: String },
}

impl SetupError {
    pub fn load(message: impl Into<String>) -> Self {
        Self::Load {
            message: message.into(),
        }
    }

    pub fn setup(message: impl Into<String>) -> Self {
        Self::Setup {
            message: message.into(),
        }
    }

    pub fn internal(message: impl Into<String>) -> Self {
        Self::Internal {
            message: message.into(),
        }
    }
}

/// Trait for the prediction handler - abstracts the Python integration.
#[async_trait::async_trait]
pub trait PredictHandler: Send + Sync + 'static {
    /// Initialize the predictor (load model, run setup).
    async fn setup(&self) -> Result<(), SetupError>;

    /// Run a prediction.
    async fn predict(
        &self,
        slot: SlotId,
        id: String,
        input: serde_json::Value,
        slot_sender: Arc<SlotSender>,
    ) -> PredictResult;

    /// Request cancellation of prediction on a slot.
    fn cancel(&self, slot: SlotId);

    /// Get OpenAPI schema for the predictor.
    fn schema(&self) -> Option<serde_json::Value> {
        None
    }
}

#[derive(Debug)]
pub struct PredictResult {
    pub output: serde_json::Value,
    pub success: bool,
    pub error: Option<String>,
    pub predict_time: f64,
}

impl PredictResult {
    pub fn success(output: serde_json::Value, predict_time: f64) -> Self {
        Self {
            output,
            success: true,
            error: None,
            predict_time,
        }
    }

    pub fn failed(error: String, predict_time: f64) -> Self {
        Self {
            output: serde_json::Value::Null,
            success: false,
            error: Some(error),
            predict_time,
        }
    }

    pub fn cancelled(predict_time: f64) -> Self {
        Self {
            output: serde_json::Value::Null,
            success: false,
            error: Some("Cancelled".to_string()),
            predict_time,
        }
    }
}

/// Callback for setup log routing.
///
/// Called before setup() with a sender for routing logs to the control channel.
/// Returns a cleanup function that unregisters the sender.
pub type SetupLogHook =
    Box<dyn FnOnce(mpsc::UnboundedSender<ControlResponse>) -> Box<dyn FnOnce() + Send> + Send>;

pub struct WorkerConfig {
    pub num_slots: usize,
    /// Hook for setup log routing. Called before setup() to register a log sender.
    pub setup_log_hook: Option<SetupLogHook>,
}

impl Default for WorkerConfig {
    fn default() -> Self {
        Self {
            num_slots: 1,
            setup_log_hook: None,
        }
    }
}

struct SlotCompletion {
    outcome: SlotOutcome,
}

impl SlotCompletion {
    fn idle(slot: SlotId) -> Self {
        Self {
            outcome: SlotOutcome::idle(slot),
        }
    }

    fn poisoned(slot: SlotId, error: impl Into<String>) -> Self {
        Self {
            outcome: SlotOutcome::poisoned(slot, error),
        }
    }
}

/// Run the worker event loop.
///
/// Connects to slot sockets, runs setup, then processes predictions.
/// Reads control messages from stdin, prediction requests from slot sockets.
pub async fn run_worker<H: PredictHandler>(
    handler: Arc<H>,
    config: WorkerConfig,
) -> io::Result<()> {
    let num_slots = config.num_slots;

    // Connect to slot sockets (transport info from env, set by parent)
    let child_info = get_transport_info_from_env()?;
    tracing::trace!(?child_info, "Connecting to slot transport");
    let mut transport = connect_transport(child_info).await?;
    tracing::info!(num_slots, "Connected to slot transport");

    // Control channel via stdin/stdout
    let mut ctrl_reader = FramedRead::new(stdin(), JsonCodec::<ControlRequest>::new());
    let ctrl_writer = Arc::new(tokio::sync::Mutex::new(FramedWrite::new(
        stdout(),
        JsonCodec::<ControlResponse>::new(),
    )));

    // Generate unique SlotIds for each socket
    let slot_ids: Vec<SlotId> = (0..num_slots).map(|_| SlotId::new()).collect();

    // Set up log forwarding for setup phase
    let (setup_log_tx, mut setup_log_rx) = mpsc::unbounded_channel::<ControlResponse>();

    let setup_cleanup = config.setup_log_hook.map(|hook| hook(setup_log_tx.clone()));

    // Forward setup logs to control channel
    let ctrl_writer_for_logs = Arc::clone(&ctrl_writer);
    let log_forwarder = tokio::spawn(async move {
        while let Some(msg) = setup_log_rx.recv().await {
            let mut w = ctrl_writer_for_logs.lock().await;
            if let Err(e) = w.send(msg).await {
                tracing::warn!(error = %e, "Failed to forward setup log");
                break;
            }
        }
    });

    // Run setup
    tracing::info!("Worker starting setup");
    let setup_result = handler.setup().await;
    tracing::trace!("Setup handler returned");

    // Clean up setup log forwarding
    // Must unregister sender BEFORE waiting for forwarder (sender keeps channel alive)
    if let Some(cleanup) = setup_cleanup {
        tracing::trace!("Running cleanup (unregistering setup sender)");
        cleanup();
    }
    drop(setup_log_tx);
    tracing::trace!("Waiting for log forwarder to finish");
    let _ = log_forwarder.await;
    tracing::trace!("Log forwarder finished");

    // Handle setup failure
    if let Err(e) = setup_result {
        tracing::error!(error = %e, "Setup failed");
        let slot = slot_ids.first().copied().unwrap_or_else(SlotId::new);
        let mut w = ctrl_writer.lock().await;
        let _ = w
            .send(ControlResponse::Failed {
                slot,
                error: format!("Setup failed: {}", e),
            })
            .await;
        return Ok(());
    }

    // Send Ready with slot IDs and schema
    let schema = handler.schema();
    tracing::trace!(num_slots, ?slot_ids, "Sending Ready to parent");
    {
        let mut w = ctrl_writer.lock().await;
        w.send(ControlResponse::Ready {
            slots: slot_ids.clone(),
            schema,
        })
        .await?;
    }

    // Channel for slot completions
    let (completion_tx, mut completion_rx) = mpsc::channel::<SlotCompletion>(num_slots);

    // Track slot state
    let mut slot_busy: HashMap<SlotId, bool> = slot_ids.iter().map(|id| (*id, false)).collect();
    let mut slot_poisoned: HashMap<SlotId, bool> = slot_ids.iter().map(|id| (*id, false)).collect();

    // Set up slot socket readers/writers
    let sockets = transport.drain_sockets();
    let mut slot_readers: HashMap<
        SlotId,
        FramedRead<tokio::net::unix::OwnedReadHalf, JsonCodec<SlotRequest>>,
    > = HashMap::new();
    let mut slot_writers: HashMap<
        SlotId,
        FramedWrite<tokio::net::unix::OwnedWriteHalf, JsonCodec<SlotResponse>>,
    > = HashMap::new();

    for (slot_id, socket) in slot_ids.iter().zip(sockets) {
        let (read_half, write_half) = socket.into_split();
        slot_readers.insert(*slot_id, FramedRead::new(read_half, JsonCodec::new()));
        slot_writers.insert(*slot_id, FramedWrite::new(write_half, JsonCodec::new()));
    }

    // Channel for incoming slot requests
    let (request_tx, mut request_rx) = mpsc::channel::<(SlotId, SlotRequest)>(num_slots);

    // Spawn reader task for each slot
    for (slot_id, reader) in slot_readers {
        let tx = request_tx.clone();
        tokio::spawn(async move {
            slot_reader_task(slot_id, reader, tx).await;
        });
    }
    drop(request_tx);

    // Wrap writers for sharing with prediction tasks
    let slot_writers: HashMap<SlotId, SlotWriter> = slot_writers
        .into_iter()
        .map(|(id, w)| (id, Arc::new(tokio::sync::Mutex::new(w))))
        .collect();

    // Main event loop
    loop {
        tokio::select! {
            biased;

            ctrl_msg = ctrl_reader.next() => {
                match ctrl_msg {
                    Some(Ok(ControlRequest::Init { .. })) => {
                        tracing::warn!("Received Init in event loop (should be at startup)");
                    }
                    Some(Ok(ControlRequest::Cancel { slot })) => {
                        tracing::trace!(%slot, "Cancel requested");
                        handler.cancel(slot);
                    }
                    Some(Ok(ControlRequest::Shutdown)) => {
                        tracing::info!("Shutdown requested");
                        let mut w = ctrl_writer.lock().await;
                        let _ = w.send(ControlResponse::ShuttingDown).await;
                        break;
                    }
                    Some(Err(e)) => {
                        tracing::error!(error = %e, "Control channel error");
                        break;
                    }
                    None => {
                        tracing::error!("Control channel closed (parent died?), exiting");
                        break;
                    }
                }
            }

            Some(completion) = completion_rx.recv() => {
                let slot = completion.outcome.slot_id();
                slot_busy.insert(slot, false);

                if completion.outcome.is_poisoned() {
                    slot_poisoned.insert(slot, true);
                }
                {
                    let mut w = ctrl_writer.lock().await;
                    let _ = w.send(completion.outcome.into_control_response()).await;
                }

                if slot_poisoned.values().all(|&p| p) {
                    tracing::error!("All slots poisoned, exiting");
                    break;
                }
            }

            Some((slot_id, request)) = request_rx.recv() => {
                if slot_busy.get(&slot_id).copied().unwrap_or(false) {
                    tracing::warn!(%slot_id, "Request received for busy slot, ignoring");
                    continue;
                }
                if slot_poisoned.get(&slot_id).copied().unwrap_or(false) {
                    tracing::warn!(%slot_id, "Request received for poisoned slot, ignoring");
                    continue;
                }

                match request {
                    SlotRequest::Predict { id, input } => {
                        tracing::trace!(%slot_id, %id, "Prediction request received");
                        slot_busy.insert(slot_id, true);

                        let writer = match slot_writers.get(&slot_id) {
                            Some(w) => Arc::clone(w),
                            None => {
                                tracing::error!(%slot_id, "No writer for slot");
                                continue;
                            }
                        };

                        let handler = Arc::clone(&handler);
                        let completion_tx = completion_tx.clone();
                        tokio::spawn(async move {
                            let completion = run_prediction(
                                slot_id,
                                id,
                                input,
                                handler,
                                writer,
                            ).await;
                            let _ = completion_tx.send(completion).await;
                        });
                    }
                }
            }
        }
    }

    tracing::info!("Worker exiting");
    Ok(())
}

async fn slot_reader_task(
    slot_id: SlotId,
    mut reader: FramedRead<tokio::net::unix::OwnedReadHalf, JsonCodec<SlotRequest>>,
    tx: mpsc::Sender<(SlotId, SlotRequest)>,
) {
    loop {
        match reader.next().await {
            Some(Ok(request)) => {
                if tx.send((slot_id, request)).await.is_err() {
                    break;
                }
            }
            Some(Err(e)) => {
                tracing::error!(%slot_id, error = %e, "Slot reader error");
                break;
            }
            None => {
                tracing::trace!(%slot_id, "Slot socket closed");
                break;
            }
        }
    }
}

async fn run_prediction<H: PredictHandler>(
    slot_id: SlotId,
    prediction_id: String,
    input: serde_json::Value,
    handler: Arc<H>,
    writer: SlotWriter,
) -> SlotCompletion {
    tracing::trace!(%slot_id, %prediction_id, "run_prediction starting");

    // Create channel for log streaming
    let (log_tx, mut log_rx) = mpsc::unbounded_channel::<SlotResponse>();
    let slot_sender = Arc::new(SlotSender::new(log_tx));

    // Forward logs to slot socket
    let writer_for_logs = Arc::clone(&writer);
    let log_forwarder = tokio::spawn(async move {
        while let Some(msg) = log_rx.recv().await {
            let mut w = writer_for_logs.lock().await;
            if let Err(e) = w.send(msg).await {
                tracing::warn!(error = %e, "Failed to forward log");
                break;
            }
        }
        tracing::trace!("Prediction log forwarder exiting");
    });

    // Run prediction
    let result = handler
        .predict(slot_id, prediction_id.clone(), input, slot_sender)
        .await;
    tracing::trace!(%slot_id, %prediction_id, success = result.success, "handler.predict returned");

    // Wait for log forwarder
    tracing::trace!(%slot_id, %prediction_id, "Waiting for log forwarder");
    let _ = log_forwarder.await;
    tracing::trace!(%slot_id, %prediction_id, "Log forwarder done");

    // Send result on slot socket
    let response = if result.success {
        SlotResponse::Done {
            id: prediction_id.clone(),
            output: Some(result.output),
            predict_time: result.predict_time,
        }
    } else if result.error.as_deref() == Some("Cancelled") {
        SlotResponse::Cancelled {
            id: prediction_id.clone(),
        }
    } else {
        SlotResponse::Failed {
            id: prediction_id.clone(),
            error: result
                .error
                .clone()
                .unwrap_or_else(|| "Unknown error".to_string()),
        }
    };

    let mut w = writer.lock().await;
    if let Err(e) = w.send(response).await {
        tracing::error!(error = %e, "Failed to send prediction response");
        return SlotCompletion::poisoned(slot_id, format!("Socket write error: {}", e));
    }

    SlotCompletion::idle(slot_id)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn predict_result_success() {
        let r = PredictResult::success(serde_json::json!("hello"), 0.5);
        assert!(r.success);
        assert!(r.error.is_none());
    }

    #[test]
    fn predict_result_failed() {
        let r = PredictResult::failed("oops".into(), 0.5);
        assert!(!r.success);
        assert_eq!(r.error, Some("oops".to_string()));
    }

    #[test]
    fn predict_result_cancelled() {
        let r = PredictResult::cancelled(0.5);
        assert!(!r.success);
    }

    #[test]
    fn worker_config_default() {
        let config = WorkerConfig::default();
        assert_eq!(config.num_slots, 1);
    }
}
