//! Orchestrator - manages worker subprocess lifecycle and event loop.
//!
//! Flow:
//! 1. Spawn worker subprocess
//! 2. Send Init message, wait for Ready
//! 3. Populate PermitPool with slot sockets
//! 4. Run event loop routing responses to predictions
//! 5. On worker crash: fail all predictions, shut down

use std::collections::HashMap;
use std::process::Stdio;
use std::sync::Arc;
use std::sync::Mutex as StdMutex;
use std::time::Duration;

use async_trait::async_trait;
use futures::{SinkExt, StreamExt};
use tokio::process::{Child, Command};
use tokio::sync::mpsc;
use tokio_util::codec::{FramedRead, FramedWrite};

use crate::PredictionOutput;
use crate::bridge::codec::JsonCodec;
use crate::bridge::protocol::{
    ControlRequest, ControlResponse, FileOutputKind, HealthcheckStatus, SlotId, SlotRequest,
    SlotResponse,
};
use crate::bridge::transport::create_transport;
use crate::permit::{InactiveSlotIdleToken, PermitPool, SlotIdleToken};
use crate::prediction::Prediction;

/// Try to lock a prediction mutex.
/// On poison: logs error, recovers to fail the prediction, returns None.
/// Caller should remove the prediction from tracking if None is returned.
fn try_lock_prediction(
    pred: &Arc<StdMutex<Prediction>>,
) -> Option<std::sync::MutexGuard<'_, Prediction>> {
    match pred.lock() {
        Ok(guard) => Some(guard),
        Err(poisoned) => {
            tracing::error!("Prediction mutex poisoned - failing prediction");
            let mut guard = poisoned.into_inner();
            if !guard.is_terminal() {
                guard.set_failed("Internal error: mutex poisoned".to_string());
            }
            None
        }
    }
}

fn emit_worker_log(target: &str, level: &str, msg: &str) {
    use std::collections::HashMap;
    use std::sync::OnceLock;
    use tracing::{
        Level, Metadata,
        callsite::{Callsite, Identifier},
        field::FieldSet,
    };

    struct DummyCallsite;
    impl Callsite for DummyCallsite {
        fn set_interest(&self, _: tracing::subscriber::Interest) {}
        fn metadata(&self) -> &Metadata<'static> {
            unreachable!()
        }
    }

    static DUMMY: DummyCallsite = DummyCallsite;
    static CALLSITES: OnceLock<
        std::sync::Mutex<HashMap<(&'static str, Level), Metadata<'static>>>,
    > = OnceLock::new();
    static FIELDS: &[&str] = &["message"];

    let lvl = match level {
        "error" => Level::ERROR,
        "warn" => Level::WARN,
        "info" => Level::INFO,
        "debug" => Level::DEBUG,
        "trace" => Level::TRACE,
        _ => Level::INFO,
    };

    let target_static: &'static str = Box::leak(target.to_string().into_boxed_str());

    let callsites = CALLSITES.get_or_init(|| std::sync::Mutex::new(HashMap::new()));
    let mut map = match callsites.lock() {
        Ok(guard) => guard,
        Err(_poisoned) => {
            tracing::error!("Worker log callsite cache poisoned");
            return;
        }
    };

    let meta = map.entry((target_static, lvl)).or_insert_with(|| {
        Metadata::new(
            "worker_log",
            target_static,
            lvl,
            Some(file!()),
            Some(line!()),
            Some(module_path!()),
            FieldSet::new(FIELDS, Identifier(&DUMMY)),
            tracing::metadata::Kind::EVENT,
        )
    });

    let meta_ref = meta as *const Metadata<'static>;
    drop(map);

    let meta = unsafe { &*meta_ref };

    tracing::dispatcher::get_default(|dispatch| {
        if dispatch.enabled(meta) {
            let fields = meta.fields();
            if let Some(field) = fields.field("message") {
                let value_array = [(&field, Some(&msg as &dyn tracing::Value))];
                let values = fields.value_set(&value_array);
                dispatch.event(&tracing::Event::new(meta, &values));
            }
        }
    });
}

/// Result of a user-defined healthcheck.
#[derive(Debug, Clone)]
pub struct HealthcheckResult {
    pub status: HealthcheckStatus,
    pub error: Option<String>,
}

impl HealthcheckResult {
    pub fn healthy() -> Self {
        Self {
            status: HealthcheckStatus::Healthy,
            error: None,
        }
    }

    pub fn unhealthy(error: impl Into<String>) -> Self {
        Self {
            status: HealthcheckStatus::Unhealthy,
            error: Some(error.into()),
        }
    }

    pub fn is_healthy(&self) -> bool {
        self.status == HealthcheckStatus::Healthy
    }
}

/// Trait for prediction registration with the orchestrator.
///
/// This abstraction enables testing the service layer without a real worker subprocess.
/// The service only needs to register predictions for response routing - all other
/// orchestrator operations happen outside the predict path.
#[async_trait]
pub trait Orchestrator: Send + Sync {
    /// Register a prediction for response routing in the event loop.
    async fn register_prediction(
        &self,
        slot_id: SlotId,
        prediction: Arc<StdMutex<Prediction>>,
        idle_sender: tokio::sync::oneshot::Sender<SlotIdleToken>,
    );

    /// Run user-defined healthcheck if available.
    async fn healthcheck(&self) -> Result<HealthcheckResult, OrchestratorError>;

    /// Shutdown the orchestrator and worker gracefully.
    async fn shutdown(&self) -> Result<(), OrchestratorError>;
}

#[derive(Debug, Clone)]
pub struct WorkerSpawnConfig {
    pub num_slots: usize,
}

#[derive(Debug, thiserror::Error)]
pub enum SpawnError {
    #[error("failed to spawn process: {0}")]
    Spawn(#[from] std::io::Error),
    #[error("spawn failed: {0}")]
    Other(String),
}

/// Extension point for different worker spawn strategies.
pub trait WorkerSpawner: Send + Sync {
    fn spawn(&self, config: &WorkerSpawnConfig) -> Result<Child, SpawnError>;
}

/// Simple spawner using Python subprocess.
pub struct SimpleSpawner;

impl WorkerSpawner for SimpleSpawner {
    fn spawn(&self, _config: &WorkerSpawnConfig) -> Result<Child, SpawnError> {
        let child = Command::new("python")
            .args(["-c", "import coglet; coglet.server._run_worker()"])
            .stdin(Stdio::piped())
            .stdout(Stdio::piped())
            .stderr(Stdio::inherit())
            .spawn()?;
        Ok(child)
    }
}

pub struct OrchestratorConfig {
    pub predictor_ref: String,
    pub num_slots: usize,
    pub is_train: bool,
    pub is_async: bool,
    pub setup_timeout: Duration,
    pub spawner: Arc<dyn WorkerSpawner>,
}

impl OrchestratorConfig {
    pub fn new(predictor_ref: impl Into<String>) -> Self {
        Self {
            predictor_ref: predictor_ref.into(),
            num_slots: 1,
            is_train: false,
            is_async: false,
            setup_timeout: Duration::from_secs(300),
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

    pub fn with_spawner(mut self, spawner: Arc<dyn WorkerSpawner>) -> Self {
        self.spawner = spawner;
        self
    }
}

pub struct OrchestratorReady {
    pub pool: Arc<PermitPool>,
    pub schema: Option<serde_json::Value>,
    pub handle: OrchestratorHandle,
    pub setup_logs: String,
}

pub struct OrchestratorHandle {
    child: Child,
    ctrl_writer:
        Arc<tokio::sync::Mutex<FramedWrite<tokio::process::ChildStdin, JsonCodec<ControlRequest>>>>,
    register_tx: mpsc::Sender<(
        SlotId,
        Arc<StdMutex<Prediction>>,
        tokio::sync::oneshot::Sender<SlotIdleToken>,
    )>,
    healthcheck_tx: mpsc::Sender<tokio::sync::oneshot::Sender<HealthcheckResult>>,
    slot_ids: Vec<SlotId>,
}

#[async_trait]
impl Orchestrator for OrchestratorHandle {
    async fn register_prediction(
        &self,
        slot_id: SlotId,
        prediction: Arc<StdMutex<Prediction>>,
        idle_sender: tokio::sync::oneshot::Sender<SlotIdleToken>,
    ) {
        let _ = self
            .register_tx
            .send((slot_id, prediction, idle_sender))
            .await;
    }

    async fn healthcheck(&self) -> Result<HealthcheckResult, OrchestratorError> {
        let (response_tx, response_rx) = tokio::sync::oneshot::channel();

        // Send our channel to the event loop. If a healthcheck is already
        // in-flight, the event loop coalesces — we get the same result as
        // all other waiters when it comes back.
        self.healthcheck_tx
            .send(response_tx)
            .await
            .map_err(|_| OrchestratorError::Protocol("healthcheck channel closed".to_string()))?;

        // Wait for the response with a timeout (worker has 5s, we give 10s total).
        // If we time out, the healthcheck keeps running — our sender just gets a
        // silent failure when the event loop eventually broadcasts.
        match tokio::time::timeout(Duration::from_secs(10), response_rx).await {
            Ok(Ok(result)) => Ok(result),
            Ok(Err(_)) => Err(OrchestratorError::Protocol(
                "healthcheck response channel dropped".to_string(),
            )),
            Err(_) => Ok(HealthcheckResult::unhealthy("healthcheck timed out")),
        }
    }

    async fn shutdown(&self) -> Result<(), OrchestratorError> {
        let mut writer = self.ctrl_writer.lock().await;
        writer
            .send(ControlRequest::Shutdown)
            .await
            .map_err(|e| OrchestratorError::Protocol(format!("failed to send shutdown: {}", e)))
    }
}

impl OrchestratorHandle {
    pub async fn cancel(&self, slot_id: SlotId) -> Result<(), OrchestratorError> {
        let mut writer = self.ctrl_writer.lock().await;
        writer
            .send(ControlRequest::Cancel { slot: slot_id })
            .await
            .map_err(|e| OrchestratorError::Protocol(format!("failed to send cancel: {}", e)))
    }

    pub fn slot_ids(&self) -> &[SlotId] {
        &self.slot_ids
    }

    pub async fn wait(&mut self) -> Result<(), OrchestratorError> {
        self.child.wait().await.map_err(|e| {
            OrchestratorError::Protocol(format!("failed to wait for worker: {}", e))
        })?;
        Ok(())
    }
}

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

pub async fn spawn_worker(
    config: OrchestratorConfig,
    setup_log_rx: &mut tokio::sync::mpsc::UnboundedReceiver<String>,
) -> Result<OrchestratorReady, OrchestratorError> {
    let num_slots = config.num_slots;

    tracing::info!(num_slots, "Creating slot transport");
    let (mut transport, child_transport_info) = create_transport(num_slots)
        .await
        .map_err(|e| OrchestratorError::Spawn(format!("failed to create transport: {}", e)))?;

    tracing::info!("Spawning worker subprocess");

    let spawn_config = WorkerSpawnConfig { num_slots };
    let mut child = config
        .spawner
        .spawn(&spawn_config)
        .map_err(|e| OrchestratorError::Spawn(format!("spawner failed: {}", e)))?;

    let stdin = child
        .stdin
        .take()
        .ok_or_else(|| OrchestratorError::Spawn("stdin not captured".to_string()))?;
    let stdout = child
        .stdout
        .take()
        .ok_or_else(|| OrchestratorError::Spawn("stdout not captured".to_string()))?;

    let mut ctrl_writer = FramedWrite::new(stdin, JsonCodec::<ControlRequest>::new());
    let mut ctrl_reader = FramedRead::new(stdout, JsonCodec::<ControlResponse>::new());

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

    tracing::debug!("Waiting for worker to connect to slot sockets");
    transport
        .accept_connections(num_slots)
        .await
        .map_err(|e| OrchestratorError::Spawn(format!("failed to accept connections: {}", e)))?;

    tracing::debug!("Waiting for Ready from worker");
    let ready_result = tokio::time::timeout(config.setup_timeout, async {
        loop {
            match ctrl_reader.next().await {
                Some(Ok(ControlResponse::Ready { slots, schema })) => {
                    return Ok((slots, schema));
                }
                Some(Ok(ControlResponse::Log { source, data })) => {
                    for line in data.lines() {
                        tracing::info!(target: "coglet::setup", source = ?source, "{}", line);
                    }
                }
                Some(Ok(ControlResponse::WorkerLog { target, level, message })) => {
                    emit_worker_log(&target, &level, &message);
                }
                Some(Ok(ControlResponse::DroppedLogs { count, interval_millis })) => {
                    tracing::trace!(count, interval_millis, "Received DroppedLogs during setup");
                    let interval_secs = interval_millis as f64 / 1000.0;
                    tracing::warn!(
                        "Log production exceeds consumption rate during setup. {} logs dropped in last {:.1}s",
                        count, interval_secs
                    );
                }
                Some(Ok(ControlResponse::Failed { slot, error })) => {
                    return Err(OrchestratorError::Setup(format!(
                        "worker setup failed (slot {}): {}",
                        slot, error
                    )));
                }
                Some(Ok(ControlResponse::Fatal { reason })) => {
                    return Err(OrchestratorError::Setup(format!(
                        "worker fatal: {}",
                        reason
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

    let setup_logs = crate::setup_log_accumulator::drain_accumulated_logs(setup_log_rx);

    tracing::debug!(num_slots = slot_ids.len(), "Worker ready");

    if let Some(ref s) = schema
        && let Ok(json) = serde_json::to_string_pretty(s)
    {
        tracing::trace!(target: "coglet::schema", schema = %json, "OpenAPI schema");
    }

    let pool = Arc::new(PermitPool::new(num_slots));
    let sockets = transport.drain_sockets();

    let mut slot_readers = Vec::with_capacity(num_slots);
    for (slot_id, socket) in slot_ids.iter().zip(sockets) {
        let (read_half, write_half) = socket.into_split();

        let writer = FramedWrite::new(write_half, JsonCodec::<SlotRequest>::new());
        pool.add_permit(*slot_id, writer);

        let reader = FramedRead::new(read_half, JsonCodec::<SlotResponse>::new());
        slot_readers.push((*slot_id, reader));
    }

    let (register_tx, register_rx) = mpsc::channel(num_slots);
    let (healthcheck_tx, healthcheck_rx) = mpsc::channel(1);

    let ctrl_writer = Arc::new(tokio::sync::Mutex::new(ctrl_writer));

    let handle = OrchestratorHandle {
        child,
        ctrl_writer: Arc::clone(&ctrl_writer),
        register_tx,
        healthcheck_tx,
        slot_ids: slot_ids.clone(),
    };

    let pool_for_loop = Arc::clone(&pool);
    let ctrl_writer_for_loop = Arc::clone(&ctrl_writer);
    tokio::spawn(async move {
        run_event_loop(
            ctrl_reader,
            ctrl_writer_for_loop,
            slot_readers,
            register_rx,
            healthcheck_rx,
            pool_for_loop,
        )
        .await;
    });

    Ok(OrchestratorReady {
        pool,
        schema,
        handle,
        setup_logs,
    })
}

async fn run_event_loop(
    mut ctrl_reader: FramedRead<tokio::process::ChildStdout, JsonCodec<ControlResponse>>,
    ctrl_writer: Arc<
        tokio::sync::Mutex<FramedWrite<tokio::process::ChildStdin, JsonCodec<ControlRequest>>>,
    >,
    slot_readers: Vec<(
        SlotId,
        FramedRead<tokio::net::unix::OwnedReadHalf, JsonCodec<SlotResponse>>,
    )>,
    mut register_rx: mpsc::Receiver<(
        SlotId,
        Arc<StdMutex<Prediction>>,
        tokio::sync::oneshot::Sender<SlotIdleToken>,
    )>,
    mut healthcheck_rx: mpsc::Receiver<tokio::sync::oneshot::Sender<HealthcheckResult>>,
    pool: Arc<PermitPool>,
) {
    let mut predictions: HashMap<SlotId, Arc<StdMutex<Prediction>>> = HashMap::new();
    let mut idle_senders: HashMap<SlotId, tokio::sync::oneshot::Sender<SlotIdleToken>> =
        HashMap::new();
    let mut pending_healthchecks: Vec<tokio::sync::oneshot::Sender<HealthcheckResult>> = Vec::new();
    let mut healthcheck_counter: u64 = 0;

    let (slot_msg_tx, mut slot_msg_rx) =
        mpsc::channel::<(SlotId, Result<SlotResponse, std::io::Error>)>(100);

    for (slot_id, mut reader) in slot_readers {
        let tx = slot_msg_tx.clone();
        tokio::spawn(async move {
            loop {
                let msg = reader.next().await;
                match msg {
                    Some(Ok(response)) => {
                        if tx.send((slot_id, Ok(response))).await.is_err() {
                            break;
                        }
                    }
                    Some(Err(e)) => {
                        let _ = tx.send((slot_id, Err(e))).await;
                        break;
                    }
                    None => {
                        break;
                    }
                }
            }
            tracing::debug!(%slot_id, "Slot reader task exiting");
        });
    }
    drop(slot_msg_tx);

    loop {
        tokio::select! {
            biased;

            ctrl_msg = ctrl_reader.next() => {
                match ctrl_msg {
                    Some(Ok(ControlResponse::Idle { slot })) => {
                        tracing::debug!(%slot, "Slot idle notification received (control channel)");
                        match idle_senders.remove(&slot) {
                            Some(sender) => {
                                let token = InactiveSlotIdleToken::new(slot);
                                if sender.send(token.activate()).is_err() {
                                    tracing::warn!(%slot, "Idle token receiver dropped before idle confirmation");
                                }
                            }
                            None => {
                                tracing::warn!(%slot, "Received Idle for slot with no pending idle confirmation");
                            }

                        }
                    }
                    Some(Ok(ControlResponse::Cancelled { slot })) => {
                        tracing::debug!(%slot, "Slot cancelled (control channel)");
                    }
                    Some(Ok(ControlResponse::Failed { slot, error })) => {
                        tracing::warn!(%slot, %error, "Slot poisoned");
                        pool.poison(slot);
                        if let Some(pred) = predictions.remove(&slot)
                            && let Some(mut p) = try_lock_prediction(&pred)
                            && !p.is_terminal()
                        {
                            p.set_failed(error);
                        }
                    }
                    Some(Ok(ControlResponse::Fatal { reason })) => {
                        tracing::error!(%reason, "Worker fatal");
                        for (slot, pred) in predictions.drain() {
                            tracing::warn!(%slot, "Failing prediction due to worker fatal error");
                            pool.poison(slot);
                            if let Some(mut p) = try_lock_prediction(&pred)
                                && !p.is_terminal()
                            {
                                p.set_failed(reason.clone());
                            }
                        }
                        let result = HealthcheckResult::unhealthy(&reason);
                        for tx in pending_healthchecks.drain(..) {
                            let _ = tx.send(result.clone());
                        }
                        break;
                    }
                    Some(Ok(ControlResponse::Ready { .. })) => {
                        tracing::warn!("Unexpected Ready in event loop");
                    }
                    Some(Ok(ControlResponse::Log { source: _, data })) => {
                        for line in data.lines() {
                            tracing::info!(target: "coglet::user", "{}", line);
                        }
                    }
                    Some(Ok(ControlResponse::WorkerLog { target, level, message })) => {
                        emit_worker_log(&target, &level, &message);
                    }
                    Some(Ok(ControlResponse::DroppedLogs { count, interval_millis })) => {
                        tracing::trace!(count, interval_millis, "Received DroppedLogs message");
                        let interval_secs = interval_millis as f64 / 1000.0;
                        tracing::warn!(
                            "Log production exceeds consumption rate. {} logs dropped in last {:.1}s",
                            count, interval_secs
                        );
                    }
                    Some(Ok(ControlResponse::HealthcheckResult { id: _, status, error })) => {
                        if pending_healthchecks.is_empty() {
                            tracing::warn!("Received healthcheck result but no pending requests");
                        } else {
                            let result = match status {
                                HealthcheckStatus::Healthy => HealthcheckResult::healthy(),
                                HealthcheckStatus::Unhealthy => {
                                    HealthcheckResult::unhealthy(error.unwrap_or_else(|| "unhealthy".to_string()))
                                }
                            };
                            for tx in pending_healthchecks.drain(..) {
                                let _ = tx.send(result.clone());
                            }
                        }
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
                            if let Some(mut p) = try_lock_prediction(&pred) {
                                p.set_failed("Worker crashed".to_string());
                            }
                        }
                        // Fail any pending healthchecks
                        for tx in pending_healthchecks.drain(..) {
                            let _ = tx.send(HealthcheckResult::unhealthy("Worker crashed"));
                        }
                        break;
                    }
                }
            }

            Some(response_tx) = healthcheck_rx.recv() => {
                let in_flight = !pending_healthchecks.is_empty();
                pending_healthchecks.push(response_tx);

                // Only send to worker if no healthcheck is already in-flight.
                // Otherwise this caller just waits for the same result.
                if !in_flight {
                    healthcheck_counter += 1;
                    let hc_id = format!("hc_{}", healthcheck_counter);

                    let mut writer = ctrl_writer.lock().await;
                    if let Err(e) = writer.send(ControlRequest::Healthcheck { id: hc_id }).await {
                        tracing::error!(error = %e, "Failed to send healthcheck request");
                        let result = HealthcheckResult::unhealthy(format!("Failed to send: {}", e));
                        for tx in pending_healthchecks.drain(..) {
                            let _ = tx.send(result.clone());
                        }
                    }
                }
            }

            Some((slot_id, prediction, idle_sender)) = register_rx.recv() => {
                let prediction_id = match try_lock_prediction(&prediction) {
                    Some(p) => p.id().to_string(),
                    None => {
                        // Mutex poisoned during registration - prediction already failed
                        tracing::error!(%slot_id, "Prediction mutex poisoned during registration");
                        continue;
                    }
                };
                // NOTE: we insert the idle sender, and idle senders are only removed on consumption of the
                // `tokio::sync::oneshot::Sender`, this means the only time we'll leak memory here is if the
                // slot is poisoned or otherwise in a bad state. It is intentional that we don't remove idle
                // senders in any other case.
                idle_senders.insert(slot_id, idle_sender);
                tracing::info!(
                    target: "coglet::prediction",
                    %prediction_id,
                    "Starting prediction"
                );
                tracing::debug!(%slot_id, %prediction_id, "Registered prediction");
                predictions.insert(slot_id, prediction);
            }

            Some((slot_id, result)) = slot_msg_rx.recv() => {
                match result {
                    Ok(SlotResponse::Log { source, data }) => {
                        let (prediction_id, poisoned) = if let Some(pred) = predictions.get(&slot_id) {
                            if let Some(mut p) = try_lock_prediction(pred) {
                                p.append_log(&data);
                                (Some(p.id().to_string()), false)
                            } else {
                                (None, true)
                            }
                        } else {
                            (None, false)
                        };
                        // Remove poisoned predictions outside the borrow
                        if poisoned {
                            predictions.remove(&slot_id);
                        }

                        let trimmed = data.trim();
                        if !trimmed.is_empty() {
                            if let Some(id) = prediction_id {
                                tracing::info!(
                                    target: "coglet::prediction",
                                    prediction_id = %id,
                                    source = ?source,
                                    "{}",
                                    trimmed
                                );
                            } else {
                                tracing::warn!(
                                    target: "coglet::prediction",
                                    prediction_id = "NO_ACTIVE_PREDICTION",
                                    source = ?source,
                                    "{}",
                                    trimmed
                                );
                            }
                        }
                    }
                    Ok(SlotResponse::Output { output }) => {
                        let poisoned = if let Some(pred) = predictions.get(&slot_id) {
                            if let Some(mut p) = try_lock_prediction(pred) {
                                p.append_output(output);
                                false
                            } else {
                                true
                            }
                        } else {
                            false
                        };
                        // Remove poisoned predictions outside the borrow
                        if poisoned {
                            predictions.remove(&slot_id);
                        }
                    }
                    Ok(SlotResponse::FileOutput { filename, kind }) => {
                        tracing::debug!(%slot_id, %filename, ?kind, "FileOutput received");
                        let output = match std::fs::read(&filename) {
                            Ok(bytes) => match kind {
                                FileOutputKind::Oversized => {
                                    match serde_json::from_slice(&bytes) {
                                        Ok(val) => val,
                                        Err(e) => {
                                            tracing::error!(%slot_id, %filename, error = %e, "Failed to parse oversized JSON");
                                            continue;
                                        }
                                    }
                                }
                                FileOutputKind::FileType => {
                                    // Binary file — base64-encode as data URI
                                    // TODO: upload to signed endpoint when upload_url is set
                                    let mime = mime_guess::from_path(&filename)
                                        .first_or_octet_stream()
                                        .to_string();
                                    use base64::Engine;
                                    let encoded =
                                        base64::engine::general_purpose::STANDARD.encode(&bytes);
                                    serde_json::Value::String(format!(
                                        "data:{mime};base64,{encoded}"
                                    ))
                                }
                            },
                            Err(e) => {
                                tracing::error!(%slot_id, %filename, error = %e, "Failed to read FileOutput");
                                continue;
                            }
                        };
                        let poisoned = if let Some(pred) = predictions.get(&slot_id) {
                            if let Some(mut p) = try_lock_prediction(pred) {
                                p.append_output(output);
                                false
                            } else {
                                true
                            }
                        } else {
                            false
                        };
                        if poisoned {
                            predictions.remove(&slot_id);
                        }
                    }
                    Ok(SlotResponse::Done { id, output, predict_time }) => {
                        tracing::info!(
                            target: "coglet::prediction",
                            prediction_id = %id,
                            predict_time,
                            "Prediction succeeded"
                        );
                        if let Some(pred) = predictions.remove(&slot_id) {
                            if let Some(mut p) = try_lock_prediction(&pred) {
                                let pred_output = output
                                    .map(PredictionOutput::Single)
                                    .unwrap_or(PredictionOutput::Single(serde_json::Value::Null));
                                p.set_succeeded(pred_output);
                            }
                            // On mutex poison, prediction already failed - nothing more to do
                        } else {
                            tracing::warn!(%slot_id, %id, "Prediction not found for Done message");
                        }
                    }
                    Ok(SlotResponse::Failed { id, error }) => {
                        tracing::info!(
                            target: "coglet::prediction",
                            prediction_id = %id,
                            %error,
                            "Prediction failed"
                        );
                        if let Some(pred) = predictions.remove(&slot_id)
                            && let Some(mut p) = try_lock_prediction(&pred)
                        {
                            p.set_failed(error);
                        }
                    }
                    Ok(SlotResponse::Cancelled { id }) => {
                        tracing::info!(
                            target: "coglet::prediction",
                            prediction_id = %id,
                            "Prediction cancelled"
                        );
                        if let Some(pred) = predictions.remove(&slot_id)
                            && let Some(mut p) = try_lock_prediction(&pred)
                        {
                            p.set_canceled();
                        }
                    }
                    Err(e) => {
                        tracing::error!(%slot_id, error = %e, "Slot socket error");
                        if let Some(pred) = predictions.remove(&slot_id)
                            && let Some(mut p) = try_lock_prediction(&pred)
                        {
                            p.set_failed(format!("Slot socket error: {}", e));
                        }
                    }
                }
            }
        }
    }

    tracing::info!("Event loop exiting");
}
