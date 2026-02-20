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
use std::path::PathBuf;
use std::sync::Arc;
use std::sync::OnceLock;
use std::sync::atomic::{AtomicUsize, Ordering};

use futures::{SinkExt, StreamExt};
use tokio::sync::mpsc;
use tokio_util::codec::{FramedRead, FramedWrite};

use crate::bridge::protocol::truncate_worker_log;

// ============================================================================
// Dropped log tracking
// ============================================================================

/// Counter for logs dropped due to channel backpressure during setup.
static DROPPED_SETUP_LOG_COUNT: AtomicUsize = AtomicUsize::new(0);

/// Increment the dropped log counter.
/// Called by ControlChannelLogSender in coglet-python when try_send fails.
pub fn increment_dropped_log_count() {
    DROPPED_SETUP_LOG_COUNT.fetch_add(1, Ordering::Relaxed);
}

/// Report and reset dropped log count.
/// Returns the number of logs dropped since last call.
fn report_dropped_logs(tx: &mpsc::Sender<ControlResponse>, interval_millis: u64) {
    let dropped = DROPPED_SETUP_LOG_COUNT.swap(0, Ordering::Relaxed);
    if dropped > 0 {
        let _ = tx.try_send(ControlResponse::DroppedLogs {
            count: dropped,
            interval_millis,
        });
    }
}

// ============================================================================
// Fatal worker shutdown
// ============================================================================

struct FatalContext {
    tx: mpsc::Sender<ControlResponse>,
}

static FATAL_CONTEXT: OnceLock<FatalContext> = OnceLock::new();

fn init_fatal_context(tx: mpsc::Sender<ControlResponse>) {
    let _ = FATAL_CONTEXT.set(FatalContext { tx });
}

/// Install a panic hook that sends a Fatal IPC message and aborts.
///
/// Any panic in the worker is an invariant violation. The hook sends a best-effort
/// `ControlResponse::Fatal` so the parent can poison all slots, then aborts.
/// This means `.expect()` / `panic!()` at any call site automatically gets
/// the correct fatal behavior — no special helpers needed.
fn install_panic_hook() {
    let prev = std::panic::take_hook();
    std::panic::set_hook(Box::new(move |info| {
        // Run the default hook first (prints to stderr).
        prev(info);

        let msg = if let Some(s) = info.payload().downcast_ref::<&str>() {
            (*s).to_string()
        } else if let Some(s) = info.payload().downcast_ref::<String>() {
            s.clone()
        } else {
            "<unknown>".to_string()
        };

        let reason = match info.location() {
            Some(loc) => format!("panic at {}:{}: {}", loc.file(), loc.line(), msg),
            None => format!("panic: {}", msg),
        };

        if let Some(ctx) = FATAL_CONTEXT.get() {
            let _ = ctx.tx.try_send(ControlResponse::Fatal { reason });
        }

        // If panic=abort is not set, abort explicitly.
        std::process::abort();
    }));
}

// ============================================================================
// Tracing initialization
// ============================================================================

fn init_worker_tracing(tx: mpsc::Sender<ControlResponse>) {
    use tracing_subscriber::{EnvFilter, layer::SubscriberExt, util::SubscriberInitExt};

    let filter = if std::env::var("RUST_LOG").is_ok() {
        EnvFilter::from_default_env()
    } else {
        let base_level = match std::env::var("COG_LOG_LEVEL").as_deref() {
            Ok("debug") => "debug",
            Ok("warn") | Ok("warning") => "warn",
            Ok("error") => "error",
            _ => "info",
        };

        let filter_str = format!(
            "coglet={level},coglet::setup=info,coglet::user=info,coglet_worker={level},coglet_worker::schema=off,coglet_worker::protocol=off",
            level = base_level
        );

        EnvFilter::new(filter_str)
    };

    let worker_layer = WorkerTracingLayer::new(tx);

    let subscriber = tracing_subscriber::registry()
        .with(filter)
        .with(worker_layer);

    let _ = subscriber.try_init();
}

use crate::bridge::codec::JsonCodec;
use crate::bridge::protocol::{
    ControlRequest, ControlResponse, FileOutputKind, LogSource, SlotId, SlotOutcome, SlotRequest,
    SlotResponse,
};
use crate::bridge::transport::{ChildTransportInfo, connect_transport};
use crate::orchestrator::HealthcheckResult;
use crate::worker_tracing_layer::WorkerTracingLayer;

type SlotWriter =
    Arc<tokio::sync::Mutex<FramedWrite<tokio::net::unix::OwnedWriteHalf, JsonCodec<SlotResponse>>>>;

/// Handle for sending messages on a slot socket.
///
/// Used by log writers to stream logs during prediction. Thread-safe via
/// tokio mpsc channel - logs are queued and written asynchronously.
#[derive(Clone)]
pub struct SlotSender {
    tx: mpsc::UnboundedSender<SlotResponse>,
    output_dir: PathBuf,
    file_counter: Arc<AtomicUsize>,
}

impl SlotSender {
    pub fn new(tx: mpsc::UnboundedSender<SlotResponse>, output_dir: PathBuf) -> Self {
        Self {
            tx,
            output_dir,
            file_counter: Arc::new(AtomicUsize::new(0)),
        }
    }

    /// Generate a unique filename in the output dir.
    fn next_output_path(&self, extension: &str) -> PathBuf {
        let n = self.file_counter.fetch_add(1, Ordering::Relaxed);
        self.output_dir.join(format!("{n}.{extension}"))
    }

    pub fn send_log(&self, source: LogSource, data: &str) -> io::Result<()> {
        if data.is_empty() {
            return Ok(());
        }

        let msg = SlotResponse::Log {
            source,
            data: truncate_worker_log(data.to_string()),
        };

        self.tx
            .send(msg)
            .map_err(|_| io::Error::new(io::ErrorKind::BrokenPipe, "slot channel closed"))
    }

    /// Write raw bytes to a file in the output dir and send as FileOutput.
    ///
    /// Used by FFI workers (Python, Node, etc.) to hand off file data without
    /// needing language-specific file I/O — SlotSender owns the write.
    pub fn write_file_output(
        &self,
        data: &[u8],
        extension: &str,
        mime_type: Option<String>,
    ) -> io::Result<()> {
        let path = self.next_output_path(extension);
        std::fs::write(&path, data)?;
        self.send_file_output(path, mime_type)
    }

    /// Send a file-typed output (e.g. Path, File return types).
    ///
    /// The file is already on disk at `path` — we just send the path reference.
    /// `mime_type` is an explicit MIME type; when None the parent guesses from extension.
    pub fn send_file_output(&self, path: PathBuf, mime_type: Option<String>) -> io::Result<()> {
        let filename = path
            .to_str()
            .ok_or_else(|| io::Error::new(io::ErrorKind::InvalidData, "non-UTF-8 path"))?
            .to_string();
        let msg = SlotResponse::FileOutput {
            filename,
            kind: FileOutputKind::FileType,
            mime_type,
        };
        self.tx
            .send(msg)
            .map_err(|_| io::Error::new(io::ErrorKind::BrokenPipe, "slot channel closed"))
    }

    /// Send prediction output, either inline or spilled to disk if too large.
    pub fn send_output(&self, output: serde_json::Value) -> io::Result<()> {
        let msg = build_output_message(&self.output_dir, output)?;
        self.tx
            .send(msg)
            .map_err(|_| io::Error::new(io::ErrorKind::BrokenPipe, "slot channel closed"))
    }
}

const MAX_INLINE_OUTPUT_SIZE: usize = 1024 * 1024 * 6; // 6MiB

/// Build an output message, spilling to disk if larger than the IPC frame limit.
fn build_output_message(
    output_dir: &std::path::Path,
    output: serde_json::Value,
) -> io::Result<SlotResponse> {
    let serialized =
        serde_json::to_vec(&output).map_err(|e| io::Error::new(io::ErrorKind::InvalidData, e))?;

    if serialized.len() > MAX_INLINE_OUTPUT_SIZE {
        let path = output_dir.join(format!("spill_{}.json", uuid::Uuid::new_v4()));
        std::fs::write(&path, &serialized)?;
        let filename = path
            .to_str()
            .ok_or_else(|| io::Error::new(io::ErrorKind::InvalidData, "non-UTF-8 path"))?
            .to_string();
        Ok(SlotResponse::FileOutput {
            filename,
            kind: FileOutputKind::Oversized,
            mime_type: None,
        })
    } else {
        Ok(SlotResponse::Output { output })
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

    /// Run user-defined healthcheck. Default: healthy.
    async fn healthcheck(&self) -> HealthcheckResult {
        HealthcheckResult::healthy()
    }
}

/// The outcome of a prediction
#[derive(Debug, Clone, PartialEq)]
pub enum PredictionOutcome {
    /// Prediction completed successfully
    Success {
        output: serde_json::Value,
        predict_time: f64,
    },
    /// Prediction failed with an error
    Failed { error: String, predict_time: f64 },
    /// Prediction was cancelled
    Cancelled { predict_time: f64 },
}

#[derive(Debug)]
pub struct PredictResult {
    pub outcome: PredictionOutcome,
}

impl PredictResult {
    pub fn success(output: serde_json::Value, predict_time: f64) -> Self {
        Self {
            outcome: PredictionOutcome::Success {
                output,
                predict_time,
            },
        }
    }

    pub fn failed(error: String, predict_time: f64) -> Self {
        Self {
            outcome: PredictionOutcome::Failed {
                error,
                predict_time,
            },
        }
    }

    pub fn cancelled(predict_time: f64) -> Self {
        Self {
            outcome: PredictionOutcome::Cancelled { predict_time },
        }
    }
}

/// Callback for setup log routing.
///
/// Called before setup() with a sender for routing logs to the control channel.
/// Returns a cleanup function that unregisters the sender.
pub type SetupLogHook =
    Box<dyn FnOnce(mpsc::Sender<ControlResponse>) -> Box<dyn FnOnce() + Send> + Send>;

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
    transport_info: ChildTransportInfo,
) -> io::Result<()> {
    let num_slots = config.num_slots;

    let (setup_log_tx, mut setup_log_rx) = mpsc::channel::<ControlResponse>(5000);

    init_worker_tracing(setup_log_tx.clone());

    // CRITICAL: Redirect fds BEFORE any FFI initialization to prevent subprocesses
    // from polluting the control channel
    let control_fds =
        crate::fd_redirect::redirect_fds_for_subprocess_isolation(setup_log_tx.clone())?;

    // Connect to slot sockets (transport info from Init message)
    tracing::trace!(?transport_info, "Connecting to slot transport");
    let mut transport = connect_transport(transport_info).await?;
    tracing::info!(num_slots, "Connected to slot transport");

    // Control channel via redirected fds (not stdin/stdout)
    let ctrl_stdin = tokio::fs::File::from_std(control_fds.stdin_fd.into());
    let ctrl_stdout = tokio::fs::File::from_std(control_fds.stdout_fd.into());

    let mut ctrl_reader = FramedRead::new(ctrl_stdin, JsonCodec::<ControlRequest>::new());
    let ctrl_writer = Arc::new(tokio::sync::Mutex::new(FramedWrite::new(
        ctrl_stdout,
        JsonCodec::<ControlResponse>::new(),
    )));

    // Generate unique SlotIds for each socket
    let slot_ids: Vec<SlotId> = (0..num_slots).map(|_| SlotId::new()).collect();

    init_fatal_context(setup_log_tx.clone());
    install_panic_hook();

    let setup_cleanup = config.setup_log_hook.map(|hook| hook(setup_log_tx.clone()));

    // Forward logs to control channel (runs for entire worker lifetime)
    // Receives logs from both Python (during setup) and fd_redirect capture threads (always)
    let ctrl_writer_for_logs = Arc::clone(&ctrl_writer);
    let _log_forwarder = tokio::spawn(async move {
        let mut log_count = 0;
        let mut total_bytes = 0;
        while let Some(msg) = setup_log_rx.recv().await {
            if let ControlResponse::Log { ref data, .. } = msg {
                let msg_size = data.len();
                log_count += 1;
                total_bytes += msg_size;
                tracing::trace!(
                    log_number = log_count,
                    msg_size_bytes = msg_size,
                    total_bytes,
                    "Forwarding log"
                );
            }
            let mut w = ctrl_writer_for_logs.lock().await;
            if let Err(e) = w.send(msg).await {
                tracing::warn!(
                    error = %e,
                    log_count,
                    total_bytes,
                    "Failed to forward log"
                );
                break;
            }
        }
        tracing::debug!(
            total_logs = log_count,
            total_bytes,
            total_kb = total_bytes / 1024,
            "Log forwarder exiting"
        );
    });

    // Periodic reporter for dropped logs (runs for entire worker lifetime)
    let dropped_log_tx = setup_log_tx.clone();
    let _dropped_log_reporter = tokio::spawn(async move {
        let mut interval = tokio::time::interval(std::time::Duration::from_millis(5000));
        loop {
            interval.tick().await;
            report_dropped_logs(&dropped_log_tx, 5000);
        }
    });

    // Run setup
    tracing::info!("Worker starting setup");
    let setup_result = handler.setup().await;
    tracing::trace!("Setup handler returned");

    // Unregister Python's setup sender, but keep log_forwarder running
    // The fd_redirect capture threads will continue sending subprocess logs
    if let Some(cleanup) = setup_cleanup {
        tracing::trace!("Running cleanup (unregistering Python setup sender)");
        cleanup();
    }
    // Note: We DON'T drop setup_log_tx or wait for log_forwarder
    // The log_forwarder continues running to forward subprocess output throughout worker lifetime

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
    if let Some(ref s) = schema {
        let schema_json = serde_json::to_string(s).unwrap_or_else(|_| "{}".to_string());
        let schema_size = schema_json.len();
        tracing::info!(
            schema_size_bytes = schema_size,
            schema_size_kb = schema_size / 1024,
            "Schema generated"
        );
        if schema_size > 1024 * 1024 {
            // Log first 500 chars if schema is >1MB
            tracing::warn!(
                schema_preview = &schema_json[..500.min(schema_json.len())],
                "Large schema detected"
            );
        }
    }
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
                    Some(Ok(ControlRequest::Healthcheck { id })) => {
                        tracing::debug!(%id, "Healthcheck requested");
                        let result = handler.healthcheck().await;
                        let mut w = ctrl_writer.lock().await;
                        let _ = w.send(ControlResponse::HealthcheckResult {
                            id,
                            status: result.status,
                            error: result.error,
                        }).await;
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
                    SlotRequest::Predict { id, input, output_dir } => {
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
                                PathBuf::from(output_dir),
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
    output_dir: PathBuf,
    handler: Arc<H>,
    writer: SlotWriter,
) -> SlotCompletion {
    tracing::trace!(%slot_id, %prediction_id, "run_prediction starting");

    // Create channel for log streaming
    let (log_tx, mut log_rx) = mpsc::unbounded_channel::<SlotResponse>();
    let slot_sender = Arc::new(SlotSender::new(log_tx, output_dir.clone()));

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

    // Run prediction — slot_sender is moved in, dropped when predict returns,
    // which closes the log channel and lets the log forwarder exit.
    let result = handler
        .predict(slot_id, prediction_id.clone(), input, slot_sender)
        .await;
    tracing::trace!(%slot_id, %prediction_id, "handler.predict returned");

    // Wait for log forwarder
    tracing::trace!(%slot_id, %prediction_id, "Waiting for log forwarder");
    let _ = log_forwarder.await;
    tracing::trace!(%slot_id, %prediction_id, "Log forwarder done");

    // Send result on slot socket.
    // Output is always sent separately from Done so that large values get
    // spilled to disk and never exceed the IPC frame limit.
    let mut w = writer.lock().await;
    let response = match result.outcome {
        PredictionOutcome::Success {
            output,
            predict_time,
        } => {
            // Send output as a separate message (handles spilling for large values).
            // Skip if null or empty array — those mean "already streamed" (generators).
            if !output.is_null() && output != serde_json::Value::Array(vec![]) {
                let output_msg = match build_output_message(&output_dir, output) {
                    Ok(msg) => msg,
                    Err(e) => {
                        tracing::error!(error = %e, "Failed to build output message");
                        return SlotCompletion::poisoned(
                            slot_id,
                            format!("Output spill error: {}", e),
                        );
                    }
                };
                if let Err(e) = w.send(output_msg).await {
                    tracing::error!(error = %e, "Failed to send prediction output");
                    return SlotCompletion::poisoned(slot_id, format!("Socket write error: {}", e));
                }
            }
            SlotResponse::Done {
                id: prediction_id.clone(),
                output: None,
                predict_time,
            }
        }
        PredictionOutcome::Cancelled { .. } => SlotResponse::Cancelled {
            id: prediction_id.clone(),
        },
        PredictionOutcome::Failed { error, .. } => SlotResponse::Failed {
            id: prediction_id.clone(),
            error,
        },
    };

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
        assert!(matches!(r.outcome, PredictionOutcome::Success { .. }));
    }

    #[test]
    fn predict_result_failed() {
        let r = PredictResult::failed("oops".into(), 0.5);
        assert!(matches!(
            r.outcome,
            PredictionOutcome::Failed { ref error, .. } if error == "oops"
        ));
    }

    #[test]
    fn predict_result_cancelled() {
        let r = PredictResult::cancelled(0.5);
        assert!(matches!(r.outcome, PredictionOutcome::Cancelled { .. }));
    }

    #[test]
    fn worker_config_default() {
        let config = WorkerConfig::default();
        assert_eq!(config.num_slots, 1);
    }
}
