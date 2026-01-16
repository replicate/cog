//! Worker-side code - runs in the subprocess.
//!
//! Architecture:
//! - Control channel (stdin/stdout): Cancel, Shutdown signals + Ready, Idle responses
//! - Slot sockets: Prediction data + streaming logs
//!
//! Each slot runs predictions independently. Idle sent on control channel when
//! prediction completes.

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

/// Type alias for slot response writers (reduces type complexity).
type SlotWriter =
    Arc<tokio::sync::Mutex<FramedWrite<tokio::net::unix::OwnedWriteHalf, JsonCodec<SlotResponse>>>>;

// ============================================================================
// SlotSender - sends messages on slot socket (for log streaming)
// ============================================================================

/// Handle for sending messages on a slot socket.
///
/// Used by log writers to stream logs during prediction. Thread-safe via
/// tokio mpsc channel - logs are queued and written asynchronously.
#[derive(Clone)]
pub struct SlotSender {
    tx: mpsc::UnboundedSender<SlotResponse>,
}

impl SlotSender {
    /// Create a new slot sender.
    pub fn new(tx: mpsc::UnboundedSender<SlotResponse>) -> Self {
        Self { tx }
    }

    /// Send a log message. Returns error if channel closed.
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

    /// Send a streaming output value (for generators).
    pub fn send_output(&self, output: serde_json::Value) -> io::Result<()> {
        let msg = SlotResponse::Output { output };
        self.tx
            .send(msg)
            .map_err(|_| io::Error::new(io::ErrorKind::BrokenPipe, "slot channel closed"))
    }
}

// ============================================================================
// PredictHandler trait
// ============================================================================

/// Trait for the prediction handler - abstracts the Python integration.
#[async_trait::async_trait]
pub trait PredictHandler: Send + Sync + 'static {
    /// Initialize the predictor (load model, run setup).
    async fn setup(&self) -> Result<(), String>;

    /// Run a prediction.
    ///
    /// Called with slot ID, prediction ID, input, and a sender for streaming logs.
    /// The handler should use `slot_sender` to stream logs during prediction.
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

/// Result of a prediction.
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

/// Callback type for setup log registration.
///
/// The worker calls this before setup() with a sender that routes logs to the control channel.
/// The callback should register the sender globally so SlotLogWriter can use it.
/// Returns a cleanup function that unregisters the sender.
pub type SetupLogHook = Box<
    dyn FnOnce(tokio::sync::mpsc::UnboundedSender<ControlResponse>) -> Box<dyn FnOnce() + Send>
        + Send,
>;

/// Worker configuration.
pub struct WorkerConfig {
    /// Number of concurrent prediction slots.
    pub num_slots: usize,
    /// Optional hook for setup log routing.
    /// If provided, called before setup() to register a sender for logs.
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

/// Completion message from a slot task.
struct SlotCompletion {
    /// The outcome - either Idle (ready for more) or Poisoned (permanently failed).
    outcome: SlotOutcome,
}

impl SlotCompletion {
    /// Create a completion indicating the slot is ready for more work.
    fn idle(slot: SlotId) -> Self {
        Self {
            outcome: SlotOutcome::idle(slot),
        }
    }

    /// Create a completion indicating the slot is poisoned.
    fn poisoned(slot: SlotId, error: impl Into<String>) -> Self {
        Self {
            outcome: SlotOutcome::poisoned(slot, error),
        }
    }
}

/// Run the worker event loop.
///
/// Reads control messages from stdin, prediction requests from slot sockets.
/// For sync predictors (num_slots=1), runs predictions inline.
/// For async predictors, spawns tasks for concurrent predictions.
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

    // Control channel - wrap in Arc<Mutex> for sharing with log forwarder
    let mut ctrl_reader = FramedRead::new(stdin(), JsonCodec::<ControlRequest>::new());
    let ctrl_writer = Arc::new(tokio::sync::Mutex::new(FramedWrite::new(
        stdout(),
        JsonCodec::<ControlResponse>::new(),
    )));

    // Generate unique SlotIds for each socket
    let slot_ids: Vec<SlotId> = (0..num_slots).map(|_| SlotId::new()).collect();

    // Set up log forwarding for setup phase
    let (setup_log_tx, mut setup_log_rx) =
        tokio::sync::mpsc::unbounded_channel::<ControlResponse>();

    // Call the setup log hook if provided (registers the sender globally)
    let setup_cleanup = config.setup_log_hook.map(|hook| hook(setup_log_tx.clone()));

    // Spawn task to forward setup logs to control channel
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
    // IMPORTANT: Must unregister the sender BEFORE waiting for forwarder,
    // because the sender holds a channel clone that keeps forwarder alive
    if let Some(cleanup) = setup_cleanup {
        tracing::trace!("Running cleanup (unregistering setup sender)");
        cleanup(); // Unregister the global sender, drops channel clone
    }
    drop(setup_log_tx); // Drop our copy too
    tracing::trace!("Waiting for log forwarder to finish");
    let _ = log_forwarder.await; // Now forwarder can exit
    tracing::trace!("Log forwarder finished");

    // Handle setup failure
    if let Err(e) = setup_result {
        tracing::error!(error = %e, "Setup failed");
        // Use first slot ID for setup failure
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

    // Channel for slot completions (used by async prediction tasks)
    let (completion_tx, mut completion_rx) = mpsc::channel::<SlotCompletion>(num_slots);

    // Track slot state by SlotId
    let mut slot_busy: HashMap<SlotId, bool> = slot_ids.iter().map(|id| (*id, false)).collect();
    let mut slot_poisoned: HashMap<SlotId, bool> = slot_ids.iter().map(|id| (*id, false)).collect();

    // Drain sockets from transport and split for concurrent read/write
    // Map each socket to its SlotId
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

    // Channel for incoming slot requests (from reader tasks)
    let (request_tx, mut request_rx) = mpsc::channel::<(SlotId, SlotRequest)>(num_slots);

    // Spawn a reader task for each slot
    // Each task reads from its socket and forwards to the shared request channel
    for (slot_id, reader) in slot_readers {
        let tx = request_tx.clone();
        tokio::spawn(async move {
            slot_reader_task(slot_id, reader, tx).await;
        });
    }
    drop(request_tx); // Drop our copy so channel closes when all readers done

    // Wrap writers in Arc<Mutex> for sharing with prediction tasks
    let slot_writers: HashMap<SlotId, SlotWriter> = slot_writers
        .into_iter()
        .map(|(id, w)| (id, Arc::new(tokio::sync::Mutex::new(w))))
        .collect();

    // Main event loop - unified for both sync (1 slot) and async (N slots)
    loop {
        tokio::select! {
            biased; // Prefer control messages, then completions, then new requests

            // Control channel messages (highest priority)
            ctrl_msg = ctrl_reader.next() => {
                match ctrl_msg {
                    Some(Ok(ControlRequest::Init { .. })) => {
                        // Init is handled at startup, not in the event loop
                        // If we receive it here, it's a protocol error
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

            // Slot completions from prediction tasks
            Some(completion) = completion_rx.recv() => {
                let slot = completion.outcome.slot_id();
                slot_busy.insert(slot, false);

                // Update poisoned state and send response
                if completion.outcome.is_poisoned() {
                    slot_poisoned.insert(slot, true);
                }
                {
                    let mut w = ctrl_writer.lock().await;
                    let _ = w.send(completion.outcome.into_control_response()).await;
                }

                // Check if all slots poisoned
                if slot_poisoned.values().all(|&p| p) {
                    tracing::error!("All slots poisoned, exiting");
                    break;
                }
            }

            // New prediction requests from slot sockets
            Some((slot_id, request)) = request_rx.recv() => {
                // Skip if slot is busy or poisoned
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

                        // Get writer for this slot
                        let writer = match slot_writers.get(&slot_id) {
                            Some(w) => Arc::clone(w),
                            None => {
                                tracing::error!(%slot_id, "No writer for slot");
                                continue;
                            }
                        };

                        // Spawn prediction task
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

/// Task that reads from a slot socket and forwards requests to the main loop.
async fn slot_reader_task(
    slot_id: SlotId,
    mut reader: FramedRead<tokio::net::unix::OwnedReadHalf, JsonCodec<SlotRequest>>,
    tx: mpsc::Sender<(SlotId, SlotRequest)>,
) {
    loop {
        match reader.next().await {
            Some(Ok(request)) => {
                if tx.send((slot_id, request)).await.is_err() {
                    // Main loop shut down
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

/// Run a single prediction and return the completion.
async fn run_prediction<H: PredictHandler>(
    slot_id: SlotId,
    prediction_id: String,
    input: serde_json::Value,
    handler: Arc<H>,
    writer: Arc<
        tokio::sync::Mutex<FramedWrite<tokio::net::unix::OwnedWriteHalf, JsonCodec<SlotResponse>>>,
    >,
) -> SlotCompletion {
    tracing::trace!(%slot_id, %prediction_id, "run_prediction starting");

    // Create channel for log streaming
    let (log_tx, mut log_rx) = mpsc::unbounded_channel::<SlotResponse>();
    let slot_sender = Arc::new(SlotSender::new(log_tx));

    // Spawn task to forward logs to slot socket
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

    // Wait for log forwarder to finish (channel closes when SlotSender dropped)
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
