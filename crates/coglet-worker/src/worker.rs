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

use crate::codec::JsonCodec;
use crate::protocol::{ControlRequest, ControlResponse, LogSource, SlotId, SlotOutcome, SlotRequest, SlotResponse};
use crate::transport::{connect_transport, get_transport_info_from_env};

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

        self.tx.send(msg).map_err(|_| {
            io::Error::new(io::ErrorKind::BrokenPipe, "slot channel closed")
        })
    }

    /// Send a streaming output value (for generators).
    pub fn send_output(&self, output: serde_json::Value) -> io::Result<()> {
        let msg = SlotResponse::Output { output };
        self.tx.send(msg).map_err(|_| {
            io::Error::new(io::ErrorKind::BrokenPipe, "slot channel closed")
        })
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

/// Worker configuration.
pub struct WorkerConfig {
    /// Number of concurrent prediction slots.
    pub num_slots: usize,
}

impl Default for WorkerConfig {
    fn default() -> Self {
        Self { num_slots: 1 }
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
        Self { outcome: SlotOutcome::idle(slot) }
    }

    /// Create a completion indicating the slot is poisoned.
    fn poisoned(slot: SlotId, error: impl Into<String>) -> Self {
        Self { outcome: SlotOutcome::poisoned(slot, error) }
    }
}

/// Run the worker event loop.
///
/// Reads control messages from stdin, prediction requests from slot sockets.
/// For sync predictors (num_slots=1), runs predictions inline.
/// For async predictors, spawns tasks for concurrent predictions.
pub async fn run_worker<H: PredictHandler>(handler: Arc<H>, config: WorkerConfig) -> io::Result<()> {
    let num_slots = config.num_slots;

    // Connect to slot sockets (transport info from env, set by parent)
    let child_info = get_transport_info_from_env()?;
    let mut transport = connect_transport(child_info).await?;

    // Control channel
    let mut ctrl_reader = FramedRead::new(stdin(), JsonCodec::<ControlRequest>::new());
    let mut ctrl_writer = FramedWrite::new(stdout(), JsonCodec::<ControlResponse>::new());

    // Generate unique SlotIds for each socket
    let slot_ids: Vec<SlotId> = (0..num_slots).map(|_| SlotId::new()).collect();
    
    // Run setup
    tracing::info!("Worker starting setup");
    if let Err(e) = handler.setup().await {
        tracing::error!(error = %e, "Setup failed");
        // Use first slot ID for setup failure
        let slot = slot_ids.first().copied().unwrap_or_else(SlotId::new);
        let _ = ctrl_writer
            .send(ControlResponse::Failed {
                slot,
                error: format!("Setup failed: {}", e),
            })
            .await;
        return Ok(());
    }

    // Send Ready with slot IDs and schema
    let schema = handler.schema();
    tracing::info!(num_slots, ?slot_ids, "Worker ready");
    ctrl_writer.send(ControlResponse::Ready { slots: slot_ids.clone(), schema }).await?;

    // Channel for slot completions (used by async prediction tasks)
    let (completion_tx, mut completion_rx) = mpsc::channel::<SlotCompletion>(num_slots);

    // Track slot state by SlotId
    let mut slot_busy: HashMap<SlotId, bool> = slot_ids.iter().map(|id| (*id, false)).collect();
    let mut slot_poisoned: HashMap<SlotId, bool> = slot_ids.iter().map(|id| (*id, false)).collect();

    // Drain sockets from transport and split for concurrent read/write
    // Map each socket to its SlotId
    let sockets = transport.drain_sockets();
    let mut slot_readers: HashMap<SlotId, FramedRead<tokio::net::unix::OwnedReadHalf, JsonCodec<SlotRequest>>> = HashMap::new();
    let mut slot_writers: HashMap<SlotId, FramedWrite<tokio::net::unix::OwnedWriteHalf, JsonCodec<SlotResponse>>> = HashMap::new();

    for (slot_id, socket) in slot_ids.iter().zip(sockets) {
        let (read_half, write_half) = socket.into_split();
        slot_readers.insert(*slot_id, FramedRead::new(read_half, JsonCodec::new()));
        slot_writers.insert(*slot_id, FramedWrite::new(write_half, JsonCodec::new()));
    }

    // For sync predictor (single slot), use a simpler inline loop
    if num_slots == 1 {
        let slot_id = slot_ids[0];
        let reader = slot_readers.remove(&slot_id);
        let writer = slot_writers.remove(&slot_id);
        return run_sync_worker(
            handler,
            ctrl_reader,
            ctrl_writer,
            slot_id,
            reader,
            writer,
        ).await;
    }

    // Multi-slot async worker loop
    loop {
        tokio::select! {
            // Control channel messages
            ctrl_msg = ctrl_reader.next() => {
                match ctrl_msg {
                    Some(Ok(ControlRequest::Cancel { slot })) => {
                        tracing::debug!(%slot, "Cancel requested");
                        handler.cancel(slot);
                    }
                    Some(Ok(ControlRequest::Shutdown)) => {
                        tracing::info!("Shutdown requested");
                        let _ = ctrl_writer.send(ControlResponse::ShuttingDown).await;
                        break;
                    }
                    Some(Err(e)) => {
                        tracing::error!(error = %e, "Control channel error");
                        break;
                    }
                    None => {
                        // Parent closed control channel - exit
                        tracing::warn!("Control channel closed (parent died?), exiting");
                        break;
                    }
                }
            }

            // Slot completions from async tasks
            Some(completion) = completion_rx.recv() => {
                let slot = completion.outcome.slot_id();
                slot_busy.insert(slot, false);

                // Update poisoned state and send response
                // SlotOutcome ensures we can't send Idle for a poisoned slot
                if completion.outcome.is_poisoned() {
                    slot_poisoned.insert(slot, true);
                }
                let _ = ctrl_writer.send(completion.outcome.into_control_response()).await;

                // Check if all slots poisoned
                if slot_poisoned.values().all(|&p| p) {
                    tracing::error!("All slots poisoned, exiting");
                    break;
                }
            }
        }

        // TODO: For async multi-slot, need to poll all slot sockets
        // This is deferred - async concurrent predictions are a future task
    }

    tracing::info!("Worker exiting");
    Ok(())
}

/// Run sync worker with single slot (inline prediction).
///
/// For sync predictors, we run predictions inline rather than spawning tasks.
/// This is simpler and avoids the overhead of task spawning.
async fn run_sync_worker<H: PredictHandler>(
    handler: Arc<H>,
    mut ctrl_reader: FramedRead<tokio::io::Stdin, JsonCodec<ControlRequest>>,
    mut ctrl_writer: FramedWrite<tokio::io::Stdout, JsonCodec<ControlResponse>>,
    slot_id: SlotId,
    slot_reader: Option<FramedRead<tokio::net::unix::OwnedReadHalf, JsonCodec<SlotRequest>>>,
    slot_writer: Option<FramedWrite<tokio::net::unix::OwnedWriteHalf, JsonCodec<SlotResponse>>>,
) -> io::Result<()> {
    let mut slot_reader = slot_reader.ok_or_else(|| {
        io::Error::new(io::ErrorKind::NotFound, format!("No slot socket for {}", slot_id))
    })?;
    let mut slot_writer = slot_writer.ok_or_else(|| {
        io::Error::new(io::ErrorKind::NotFound, format!("No slot socket for {}", slot_id))
    })?;

    loop {
        // Wait for either a control message or a prediction request
        tokio::select! {
            biased;  // Prefer control messages

            ctrl_msg = ctrl_reader.next() => {
                match ctrl_msg {
                    Some(Ok(ControlRequest::Cancel { slot })) => {
                        tracing::debug!(%slot, "Cancel requested (sync worker)");
                        handler.cancel(slot);
                    }
                    Some(Ok(ControlRequest::Shutdown)) => {
                        tracing::info!("Shutdown requested");
                        let _ = ctrl_writer.send(ControlResponse::ShuttingDown).await;
                        break;
                    }
                    Some(Err(e)) => {
                        tracing::error!(error = %e, "Control channel error");
                        break;
                    }
                    None => {
                        tracing::warn!("Control channel closed, exiting");
                        break;
                    }
                }
            }

            slot_msg = slot_reader.next() => {
                match slot_msg {
                    Some(Ok(SlotRequest::Predict { id, input })) => {
                        tracing::debug!(id = %id, %slot_id, "Prediction request received");

                        // Create channel for log streaming
                        let (log_tx, mut log_rx) = mpsc::unbounded_channel::<SlotResponse>();
                        let slot_sender = Arc::new(SlotSender::new(log_tx));

                        // Run prediction and forward logs concurrently
                        let handler_clone = Arc::clone(&handler);
                        let predict_fut = handler_clone.predict(slot_id, id.clone(), input, slot_sender);

                        // Drive prediction while forwarding logs
                        let result = tokio::select! {
                            result = predict_fut => result,
                            // This branch drains logs while prediction runs
                            // (won't complete until channel closes)
                            _ = async {
                                while let Some(msg) = log_rx.recv().await {
                                    if let Err(e) = slot_writer.send(msg).await {
                                        tracing::warn!(error = %e, "Failed to send log");
                                    }
                                }
                            } => unreachable!(),
                        };

                        // Drain any remaining logs
                        while let Ok(msg) = log_rx.try_recv() {
                            let _ = slot_writer.send(msg).await;
                        }

                        // Send result on slot socket
                        let response = if result.success {
                            SlotResponse::Done {
                                id,
                                output: Some(result.output),
                                predict_time: result.predict_time,
                            }
                        } else if result.error.as_deref() == Some("Cancelled") {
                            SlotResponse::Cancelled { id }
                        } else {
                            SlotResponse::Failed {
                                id,
                                error: result.error.unwrap_or_else(|| "Unknown error".to_string()),
                            }
                        };

                        if let Err(e) = slot_writer.send(response).await {
                            tracing::error!(error = %e, "Failed to send response");
                            // Socket write failed - slot is poisoned
                            let outcome = SlotOutcome::poisoned(slot_id, format!("Socket write error: {}", e));
                            let _ = ctrl_writer.send(outcome.into_control_response()).await;
                            break;
                        }

                        // Signal slot is idle (prediction completed normally)
                        let outcome = SlotOutcome::idle(slot_id);
                        let _ = ctrl_writer.send(outcome.into_control_response()).await;
                    }
                    Some(Err(e)) => {
                        tracing::error!(error = %e, "Slot socket error");
                        // Socket read failed - slot is poisoned
                        let outcome = SlotOutcome::poisoned(slot_id, format!("Socket read error: {}", e));
                        let _ = ctrl_writer.send(outcome.into_control_response()).await;
                        break;
                    }
                    None => {
                        tracing::warn!("Slot socket closed");
                        break;
                    }
                }
            }
        }
    }

    tracing::info!("Sync worker exiting");
    Ok(())
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
