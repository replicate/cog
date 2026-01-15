//! Orchestrator - manages worker subprocess lifecycle and event loop.
//!
//! The orchestrator:
//! 1. Spawns worker subprocess
//! 2. Sends Init message, waits for Ready
//! 3. Populates PermitPool with slot sockets
//! 4. Runs event loop routing responses to predictions
//! 5. On worker crash: fails all predictions, shuts down

use std::collections::HashMap;
use std::process::Stdio;
use std::sync::Arc;
use std::time::Duration;

use futures::{SinkExt, StreamExt};
use tokio::process::{Child, Command};
use tokio::sync::{mpsc, Mutex};
use tokio_util::codec::{FramedRead, FramedWrite};

use crate::bridge::codec::JsonCodec;
use crate::bridge::protocol::{ControlRequest, ControlResponse, SlotId, SlotRequest, SlotResponse};
use crate::bridge::transport::create_transport;
use crate::permit::PermitPool;
use crate::prediction::Prediction;
use crate::PredictionOutput;

// ============================================================================
// WorkerSpawner trait - extension point for different spawn strategies
// ============================================================================

/// Configuration for spawning a worker subprocess.
#[derive(Debug, Clone)]
pub struct WorkerSpawnConfig {
    /// Number of concurrent prediction slots
    pub num_slots: usize,
}

/// Error from spawning a worker.
#[derive(Debug, thiserror::Error)]
pub enum SpawnError {
    #[error("failed to spawn process: {0}")]
    Spawn(#[from] std::io::Error),
    #[error("spawn failed: {0}")]
    Other(String),
}

/// Trait for worker subprocess spawning strategies.
///
/// Implementations can customize how worker subprocesses are created:
/// - `SimpleSpawner`: Basic subprocess via `python -c "import coglet; coglet._run_worker()"`
/// - Future: `SandboxedSpawner`: Fork + seccomp + privilege drop
/// - Future: `SnapshotSpawner`: CRIU restore + NVIDIA attach for fast cold starts
pub trait WorkerSpawner: Send + Sync {
    /// Spawn a worker subprocess.
    ///
    /// Returns a Child with stdin/stdout captured for protocol communication.
    /// The child should be ready to receive an Init message on stdin.
    fn spawn(&self, config: &WorkerSpawnConfig) -> Result<Child, SpawnError>;
}

/// Simple spawner using Python subprocess.
///
/// Spawns: `python -c "import coglet; coglet._run_worker()"`
pub struct SimpleSpawner;

impl WorkerSpawner for SimpleSpawner {
    fn spawn(&self, _config: &WorkerSpawnConfig) -> Result<Child, SpawnError> {
        let child = Command::new("python")
            .args(["-c", "import coglet; coglet._run_worker()"])
            .stdin(Stdio::piped())
            .stdout(Stdio::piped())
            .stderr(Stdio::inherit()) // Worker logs go to our stderr
            .spawn()?;
        Ok(child)
    }
}

// Future spawner variants (stubs for documentation):
//
// pub struct SandboxedSpawner {
//     /// Seccomp filter to apply after fork
//     pub seccomp_filter: Option<SeccompFilter>,
//     /// User/group to drop privileges to
//     pub drop_privileges: Option<(uid_t, gid_t)>,
// }
//
// pub struct SnapshotSpawner {
//     /// Path to CRIU checkpoint directory
//     pub checkpoint_dir: PathBuf,
//     /// NVIDIA GPU UUIDs to attach
//     pub gpu_uuids: Vec<String>,
// }

/// Configuration for the orchestrator.
pub struct OrchestratorConfig {
    /// Predictor reference (e.g., "predict.py:Predictor")
    pub predictor_ref: String,
    /// Number of concurrent prediction slots
    pub num_slots: usize,
    /// Whether this is training mode
    pub is_train: bool,
    /// Whether predictor is async
    pub is_async: bool,
    /// Timeout for worker setup
    pub setup_timeout: Duration,
    /// Custom spawner (defaults to SimpleSpawner)
    pub spawner: Arc<dyn WorkerSpawner>,
}

impl OrchestratorConfig {
    pub fn new(predictor_ref: impl Into<String>) -> Self {
        Self {
            predictor_ref: predictor_ref.into(),
            num_slots: 1,
            is_train: false,
            is_async: false,
            setup_timeout: Duration::from_secs(300), // 5 min default
            spawner: Arc::new(SimpleSpawner),
        }
    }

    pub fn with_num_slots(mut self, n: usize) -> Self {
        self.num_slots = n;
        self
    }

    pub fn with_train(mut self, is_train: bool) -> Self {
        self.is_train = is_train;
        self
    }

    pub fn with_async(mut self, is_async: bool) -> Self {
        self.is_async = is_async;
        self
    }

    pub fn with_setup_timeout(mut self, timeout: Duration) -> Self {
        self.setup_timeout = timeout;
        self
    }

    /// Set a custom worker spawner.
    ///
    /// By default uses `SimpleSpawner` which spawns Python directly.
    /// Use this to customize how workers are created (e.g., sandboxed, from snapshot).
    pub fn with_spawner(mut self, spawner: Arc<dyn WorkerSpawner>) -> Self {
        self.spawner = spawner;
        self
    }
}

/// Result of orchestrator initialization.
pub struct OrchestratorReady {
    /// The permit pool populated with slot sockets
    pub pool: Arc<PermitPool>,
    /// OpenAPI schema from predictor (if available)
    pub schema: Option<serde_json::Value>,
    /// Handle to the running orchestrator (for event loop)
    pub handle: OrchestratorHandle,
}

/// Handle to a running orchestrator.
pub struct OrchestratorHandle {
    /// Worker child process
    child: Child,
    /// Control channel writer (for Cancel, Shutdown)
    ctrl_writer: Arc<Mutex<FramedWrite<tokio::process::ChildStdin, JsonCodec<ControlRequest>>>>,
    /// Channel to register predictions with event loop
    register_tx: mpsc::Sender<(SlotId, Arc<Mutex<Prediction>>)>,
    /// Slot IDs from worker
    slot_ids: Vec<SlotId>,
}

impl OrchestratorHandle {
    /// Register a prediction with the event loop.
    ///
    /// Called after acquiring a permit and creating a prediction.
    pub async fn register_prediction(&self, slot_id: SlotId, prediction: Arc<Mutex<Prediction>>) {
        let _ = self.register_tx.send((slot_id, prediction)).await;
    }

    /// Send cancel request to worker.
    pub async fn cancel(&self, slot_id: SlotId) -> Result<(), OrchestratorError> {
        let mut writer = self.ctrl_writer.lock().await;
        writer
            .send(ControlRequest::Cancel { slot: slot_id })
            .await
            .map_err(|e| OrchestratorError::Protocol(format!("failed to send cancel: {}", e)))
    }

    /// Request graceful shutdown.
    pub async fn shutdown(&self) -> Result<(), OrchestratorError> {
        let mut writer = self.ctrl_writer.lock().await;
        writer
            .send(ControlRequest::Shutdown)
            .await
            .map_err(|e| OrchestratorError::Protocol(format!("failed to send shutdown: {}", e)))
    }

    /// Get the slot IDs.
    pub fn slot_ids(&self) -> &[SlotId] {
        &self.slot_ids
    }

    /// Wait for worker process to exit.
    pub async fn wait(&mut self) -> Result<(), OrchestratorError> {
        self.child
            .wait()
            .await
            .map_err(|e| OrchestratorError::Protocol(format!("failed to wait for worker: {}", e)))?;
        Ok(())
    }
}

/// Orchestrator errors.
#[derive(Debug, thiserror::Error)]
pub enum OrchestratorError {
    #[error("failed to spawn worker: {0}")]
    Spawn(String),
    #[error("worker setup failed: {0}")]
    Setup(String),
    #[error("worker setup timed out")]
    SetupTimeout,
    #[error("protocol error: {0}")]
    Protocol(String),
    #[error("worker crashed")]
    WorkerCrashed,
}

/// Spawn worker and initialize orchestrator.
///
/// Returns when worker is ready (setup complete).
pub async fn spawn_worker(config: OrchestratorConfig) -> Result<OrchestratorReady, OrchestratorError> {
    let num_slots = config.num_slots;

    // Create slot transport (Unix sockets)
    tracing::info!(num_slots, "Creating slot transport");
    let (mut transport, child_transport_info) = create_transport(num_slots)
        .await
        .map_err(|e| OrchestratorError::Spawn(format!("failed to create transport: {}", e)))?;

    tracing::info!("Spawning worker subprocess");

    // Use configured spawner (defaults to SimpleSpawner)
    let spawn_config = WorkerSpawnConfig { num_slots };
    let mut child = config
        .spawner
        .spawn(&spawn_config)
        .map_err(|e| OrchestratorError::Spawn(format!("spawner failed: {}", e)))?;

    let stdin = child.stdin.take().expect("stdin not captured");
    let stdout = child.stdout.take().expect("stdout not captured");

    // Wrap in framed codec
    let mut ctrl_writer = FramedWrite::new(stdin, JsonCodec::<ControlRequest>::new());
    let mut ctrl_reader = FramedRead::new(stdout, JsonCodec::<ControlResponse>::new());

    // Send Init message
    tracing::debug!("Sending Init to worker");
    ctrl_writer
        .send(ControlRequest::Init {
            predictor_ref: config.predictor_ref.clone(),
            num_slots,
            transport_info: child_transport_info,
            is_train: config.is_train,
            is_async: config.is_async,
        })
        .await
        .map_err(|e| OrchestratorError::Protocol(format!("failed to send Init: {}", e)))?;

    // Accept slot socket connections (worker connects after receiving Init)
    tracing::debug!("Waiting for worker to connect to slot sockets");
    transport
        .accept_connections(num_slots)
        .await
        .map_err(|e| OrchestratorError::Spawn(format!("failed to accept connections: {}", e)))?;

    // Wait for Ready (with timeout)
    tracing::debug!("Waiting for Ready from worker");
    let ready_result = tokio::time::timeout(config.setup_timeout, async {
        loop {
            match ctrl_reader.next().await {
                Some(Ok(ControlResponse::Ready { slots, schema })) => {
                    return Ok((slots, schema));
                }
                Some(Ok(ControlResponse::Log { source, data })) => {
                    // Setup logs - forward to our logging
                    for line in data.lines() {
                        tracing::info!(target: "coglet::setup", source = ?source, "{}", line);
                    }
                }
                Some(Ok(ControlResponse::Failed { slot, error })) => {
                    return Err(OrchestratorError::Setup(format!(
                        "worker setup failed (slot {}): {}",
                        slot, error
                    )));
                }
                Some(Ok(other)) => {
                    tracing::warn!(?other, "Unexpected message during setup");
                }
                Some(Err(e)) => {
                    return Err(OrchestratorError::Protocol(format!(
                        "control channel error: {}",
                        e
                    )));
                }
                None => {
                    return Err(OrchestratorError::WorkerCrashed);
                }
            }
        }
    })
    .await;

    let (slot_ids, schema) = match ready_result {
        Ok(Ok((slots, schema))) => (slots, schema),
        Ok(Err(e)) => return Err(e),
        Err(_) => return Err(OrchestratorError::SetupTimeout),
    };

    tracing::info!(num_slots = slot_ids.len(), "Worker ready");

    // Create permit pool and populate with slot sockets
    let pool = Arc::new(PermitPool::new(num_slots));
    let sockets = transport.drain_sockets();

    // Split sockets: write halves to PermitPool, read halves to event loop
    let mut slot_readers = Vec::with_capacity(num_slots);
    for (slot_id, socket) in slot_ids.iter().zip(sockets) {
        let (read_half, write_half) = socket.into_split();

        // Write half goes to permit pool
        let writer = FramedWrite::new(write_half, JsonCodec::<SlotRequest>::new());
        pool.add_permit(*slot_id, writer);

        // Read half goes to event loop
        let reader = FramedRead::new(read_half, JsonCodec::<SlotResponse>::new());
        slot_readers.push((*slot_id, reader));
    }

    // Create registration channel for event loop
    let (register_tx, register_rx) = mpsc::channel(num_slots);

    let ctrl_writer = Arc::new(Mutex::new(ctrl_writer));

    let handle = OrchestratorHandle {
        child,
        ctrl_writer: Arc::clone(&ctrl_writer),
        register_tx,
        slot_ids: slot_ids.clone(),
    };

    // Spawn event loop task
    let pool_for_loop = Arc::clone(&pool);
    tokio::spawn(async move {
        run_event_loop(ctrl_reader, slot_readers, register_rx, pool_for_loop).await;
    });

    Ok(OrchestratorReady {
        pool,
        schema,
        handle,
    })
}

/// Event loop - routes worker responses to predictions.
///
/// Runs until control channel closes (worker exit/crash).
async fn run_event_loop(
    mut ctrl_reader: FramedRead<tokio::process::ChildStdout, JsonCodec<ControlResponse>>,
    slot_readers: Vec<(SlotId, FramedRead<tokio::net::unix::OwnedReadHalf, JsonCodec<SlotResponse>>)>,
    mut register_rx: mpsc::Receiver<(SlotId, Arc<Mutex<Prediction>>)>,
    _pool: Arc<PermitPool>, // Kept for future use (permit return coordination)
) {
    // Active predictions by slot
    let mut predictions: HashMap<SlotId, Arc<Mutex<Prediction>>> = HashMap::new();

    // Channel for slot reader messages (aggregated from all slots)
    let (slot_msg_tx, mut slot_msg_rx) =
        mpsc::channel::<(SlotId, Result<SlotResponse, std::io::Error>)>(100);

    // Spawn a reader task for each slot
    for (slot_id, mut reader) in slot_readers {
        let tx = slot_msg_tx.clone();
        tokio::spawn(async move {
            loop {
                let msg = reader.next().await;
                match msg {
                    Some(Ok(response)) => {
                        if tx.send((slot_id, Ok(response))).await.is_err() {
                            break; // Event loop shut down
                        }
                    }
                    Some(Err(e)) => {
                        let _ = tx.send((slot_id, Err(e.into()))).await;
                        break;
                    }
                    None => {
                        // Socket closed
                        break;
                    }
                }
            }
            tracing::debug!(%slot_id, "Slot reader task exiting");
        });
    }
    drop(slot_msg_tx); // Drop our copy so channel closes when all readers done

    loop {
        tokio::select! {
            biased;

            // Control channel messages (highest priority)
            ctrl_msg = ctrl_reader.next() => {
                match ctrl_msg {
                    Some(Ok(ControlResponse::Idle { slot })) => {
                        tracing::debug!(%slot, "Slot idle");
                        // Don't remove prediction here - Done message on slot socket handles that
                        // This just signals the slot is ready for more work
                    }
                    Some(Ok(ControlResponse::Cancelled { slot })) => {
                        tracing::debug!(%slot, "Slot cancelled (control channel)");
                        // Cancellation is handled via slot socket Cancelled response
                        // This is just a control signal
                    }
                    Some(Ok(ControlResponse::Failed { slot, error })) => {
                        tracing::warn!(%slot, %error, "Slot poisoned");
                        if let Some(pred) = predictions.remove(&slot) {
                            let mut p = pred.lock().await;
                            p.set_slot_poisoned();
                            p.set_failed(error);
                        }
                    }
                    Some(Ok(ControlResponse::Ready { .. })) => {
                        tracing::warn!("Unexpected Ready in event loop");
                    }
                    Some(Ok(ControlResponse::Log { source, data })) => {
                        tracing::debug!(?source, "Late setup log: {}", data.trim());
                    }
                    Some(Ok(ControlResponse::ShuttingDown)) => {
                        tracing::info!("Worker shutting down");
                        break;
                    }
                    Some(Err(e)) => {
                        tracing::error!(error = %e, "Control channel error");
                        break;
                    }
                    None => {
                        tracing::warn!("Control channel closed (worker crashed?)");
                        for (slot, pred) in predictions.drain() {
                            tracing::warn!(%slot, "Failing prediction due to worker crash");
                            let mut p = pred.lock().await;
                            p.set_failed("Worker crashed".to_string());
                        }
                        break;
                    }
                }
            }

            // New prediction registrations
            Some((slot_id, prediction)) = register_rx.recv() => {
                tracing::debug!(%slot_id, "Registered prediction");
                predictions.insert(slot_id, prediction);
            }

            // Slot socket messages
            Some((slot_id, result)) = slot_msg_rx.recv() => {
                match result {
                    Ok(SlotResponse::Log { source, data }) => {
                        if let Some(pred) = predictions.get(&slot_id) {
                            let mut p = pred.lock().await;
                            p.append_log(&data);
                        }
                        tracing::debug!(target: "coglet::prediction", %slot_id, ?source, "{}", data.trim());
                    }
                    Ok(SlotResponse::Output { output }) => {
                        if let Some(pred) = predictions.get(&slot_id) {
                            let mut p = pred.lock().await;
                            p.append_output(output);
                        }
                    }
                    Ok(SlotResponse::Done { id, output, predict_time }) => {
                        tracing::debug!(%slot_id, %id, predict_time, "Prediction done");
                        // Remove prediction from registry and notify waiters
                        if let Some(pred) = predictions.remove(&slot_id) {
                            let mut p = pred.lock().await;
                            let pred_output = output
                                .map(PredictionOutput::Single)
                                .unwrap_or(PredictionOutput::Single(serde_json::Value::Null));
                            p.set_succeeded(pred_output);
                        } else {
                            tracing::warn!(%slot_id, %id, "Prediction not found for Done message");
                        }
                    }
                    Ok(SlotResponse::Failed { id, error }) => {
                        tracing::warn!(%slot_id, %id, %error, "Prediction failed");
                        if let Some(pred) = predictions.remove(&slot_id) {
                            let mut p = pred.lock().await;
                            p.set_failed(error);
                        }
                    }
                    Ok(SlotResponse::Cancelled { id }) => {
                        tracing::info!(%slot_id, %id, "Prediction cancelled");
                        if let Some(pred) = predictions.remove(&slot_id) {
                            let mut p = pred.lock().await;
                            p.set_canceled();
                        }
                    }
                    Err(e) => {
                        tracing::error!(%slot_id, error = %e, "Slot socket error");
                        if let Some(pred) = predictions.remove(&slot_id) {
                            let mut p = pred.lock().await;
                            p.set_failed(format!("Slot socket error: {}", e));
                        }
                    }
                }
            }
        }
    }

    tracing::info!("Event loop exiting");
}
