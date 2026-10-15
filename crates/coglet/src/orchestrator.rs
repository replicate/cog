//! Orchestrator - manages worker subprocess lifecycle and event loop.
//!
//! Flow:
//! 1. Spawn worker subprocess
//! 2. Send Init message, wait for Ready
//! 3. Populate PermitPool with slot sockets
//! 4. Run event loop routing responses to predictions
//! 5. On worker crash: fail all predictions, shut down

use std::collections::{HashMap, HashSet};
use std::process::Stdio;
use std::sync::Arc;
use std::sync::Mutex as StdMutex;
use std::time::Duration;

use async_trait::async_trait;
use futures::{SinkExt, StreamExt};
use tokio::process::{Child, Command};
use tokio::sync::mpsc;
use tokio_util::codec::{FramedRead, FramedWrite};
use tokio_util::task::AbortOnDropHandle;
use tracing::Instrument as _;

use crate::PredictionOutput;
use crate::bridge::codec::JsonCodec;
use crate::bridge::protocol::{
    ControlRequest, ControlResponse, FileOutputKind, HealthcheckStatus, SlotId, SlotRequest,
    SlotResponse,
};
use crate::bridge::transport::create_transport;
use crate::permit::{InactiveSlotIdleToken, PermitPool, SlotIdleToken};
use crate::prediction::Prediction;

const MAX_PENDING_CANCELLATIONS: usize = 1000;

/// Upload a file to a signed endpoint, returning the final URL.
///
/// Matches the behavior of Python cog's `put_file_to_signed_endpoint`:
/// PUT to `{endpoint}{filename}` with Content-Type header, then extract
/// the final URL from the Location header (falling back to response URL),
/// stripping query parameters. Follows redirects automatically.
async fn upload_file(
    endpoint: &str,
    filename: &str,
    data: &[u8],
    content_type: &str,
    prediction_id: &str,
) -> Result<String, String> {
    let url = format!("{endpoint}{filename}");
    let client = reqwest::Client::new();
    let resp = client
        .put(&url)
        .header("Content-Type", content_type)
        .header("X-Prediction-Id", prediction_id)
        .body(data.to_vec())
        .timeout(std::time::Duration::from_secs(25))
        .send()
        .await
        .map_err(|e| {
            if e.is_timeout() {
                "upload request timed out".to_string()
            } else {
                format!("upload request failed: {}", e.without_url())
            }
        })?;

    if !resp.status().is_success() {
        return Err(format!("upload returned status {}", resp.status()));
    }

    // Prefer Location header, fall back to final request URL (after redirects)
    let final_url = resp
        .headers()
        .get("location")
        .and_then(|v| v.to_str().ok())
        .map(|s| s.to_string())
        .unwrap_or_else(|| resp.url().to_string());

    // Strip query parameters (signing gubbins)
    match reqwest::Url::parse(&final_url) {
        Ok(mut parsed) => {
            parsed.set_query(None);
            Ok(parsed.to_string())
        }
        Err(_) => Ok(final_url),
    }
}

fn ensure_trailing_slash(s: &str) -> String {
    if s.ends_with('/') {
        s.to_string()
    } else {
        format!("{s}/")
    }
}

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

/// Wrap collected output items into the correct `PredictionOutput` variant.
///
/// Priority:
/// 1. Schema says `"type": "array"` (`output_is_array = true`) → always `Stream`
/// 2. Predictor signals `is_stream` (list/generator return) → always `Stream`
/// 3. Otherwise → `Single` for one item, `Stream` for multiple
///
/// This ensures `List[Path]` with a single element returns `["url"]` not `"url"`.
fn wrap_outputs(
    outputs: Vec<serde_json::Value>,
    output_is_array: bool,
    is_stream: bool,
) -> PredictionOutput {
    let should_stream = output_is_array || is_stream;

    match outputs.as_slice() {
        [] => {
            if should_stream {
                PredictionOutput::Stream(vec![])
            } else {
                PredictionOutput::Single(serde_json::Value::Null)
            }
        }
        _ if should_stream => PredictionOutput::Stream(outputs),
        [single] => PredictionOutput::Single(single.clone()),
        _ => PredictionOutput::Stream(outputs),
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

    /// Cancel a prediction by its prediction ID.
    ///
    /// The orchestrator resolves the prediction ID to a slot ID and sends
    /// a cancel request to the worker over the control socket.
    async fn cancel_by_prediction_id(&self, prediction_id: &str) -> Result<(), OrchestratorError>;

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
    pub setup_timeout: Option<Duration>,
    pub spawner: Arc<dyn WorkerSpawner>,
    /// Upload URL prefix for file outputs (from --upload-url CLI arg).
    pub upload_url: Option<String>,
}

impl OrchestratorConfig {
    pub fn new(predictor_ref: impl Into<String>) -> Self {
        Self {
            predictor_ref: predictor_ref.into(),
            num_slots: 1,
            is_train: false,
            is_async: false,
            setup_timeout: None,
            spawner: Arc::new(SimpleSpawner),
            upload_url: None,
        }
    }

    pub fn with_upload_url(mut self, upload_url: Option<String>) -> Self {
        self.upload_url = upload_url;
        self
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

    pub fn with_setup_timeout(mut self, timeout: Option<Duration>) -> Self {
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

struct RegisterPredictionMessage {
    slot_id: SlotId,
    prediction: Arc<StdMutex<Prediction>>,
    idle_sender: tokio::sync::oneshot::Sender<SlotIdleToken>,
    registered_ack: tokio::sync::oneshot::Sender<()>,
}

pub struct OrchestratorHandle {
    child: tokio::sync::Mutex<Option<Child>>,
    ctrl_writer:
        Arc<tokio::sync::Mutex<FramedWrite<tokio::process::ChildStdin, JsonCodec<ControlRequest>>>>,
    register_tx: mpsc::Sender<RegisterPredictionMessage>,
    healthcheck_tx: mpsc::Sender<tokio::sync::oneshot::Sender<HealthcheckResult>>,
    cancel_tx: mpsc::Sender<String>,
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
        let (ack_tx, ack_rx) = tokio::sync::oneshot::channel();
        let _ = self
            .register_tx
            .send(RegisterPredictionMessage {
                slot_id,
                prediction,
                idle_sender,
                registered_ack: ack_tx,
            })
            .await;
        let _ = ack_rx.await;
    }

    async fn cancel_by_prediction_id(&self, prediction_id: &str) -> Result<(), OrchestratorError> {
        self.cancel_tx
            .send(prediction_id.to_string())
            .await
            .map_err(|_| OrchestratorError::Protocol("cancel channel closed".to_string()))
    }

    async fn healthcheck(&self) -> Result<HealthcheckResult, OrchestratorError> {
        tracing::trace!("Healthcheck requested via orchestrator handle");
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
            Ok(Ok(result)) => {
                tracing::trace!(healthy = result.is_healthy(), "Healthcheck completed");
                Ok(result)
            }
            Ok(Err(_)) => {
                tracing::debug!("Healthcheck response channel dropped");
                Err(OrchestratorError::Protocol(
                    "healthcheck response channel dropped".to_string(),
                ))
            }
            Err(_) => {
                tracing::debug!("Healthcheck timed out after 10s");
                Ok(HealthcheckResult::unhealthy("healthcheck timed out"))
            }
        }
    }

    async fn shutdown(&self) -> Result<(), OrchestratorError> {
        {
            let mut writer = self.ctrl_writer.lock().await;
            writer.send(ControlRequest::Shutdown).await.map_err(|e| {
                OrchestratorError::Protocol(format!("failed to send shutdown: {}", e))
            })?;
        }

        let mut child_guard = self.child.lock().await;
        let Some(child) = child_guard.as_mut() else {
            return Ok(());
        };
        let wait_result = tokio::time::timeout(Duration::from_secs(10), child.wait()).await;
        if let Ok(Err(error)) = wait_result {
            return Err(OrchestratorError::Protocol(format!(
                "failed to wait for worker: {error}"
            )));
        }
        if wait_result.is_err() {
            child.start_kill().map_err(|error| {
                OrchestratorError::Protocol(format!("failed to kill worker: {error}"))
            })?;
            child.wait().await.map_err(|error| {
                OrchestratorError::Protocol(format!("failed to reap worker: {error}"))
            })?;
        }
        *child_guard = None;
        Ok(())
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
        let mut child = self.child.lock().await;
        if let Some(child) = child.as_mut() {
            child.wait().await.map_err(|e| {
                OrchestratorError::Protocol(format!("failed to wait for worker: {}", e))
            })?;
        }
        *child = None;
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
            trace: {
                #[cfg(feature = "tracing")]
                {
                    crate::trace::carrier_from_span(&tracing::Span::current())
                }
                #[cfg(not(feature = "tracing"))]
                {
                    None
                }
            },
        })
        .await
        .map_err(|e| OrchestratorError::Protocol(format!("failed to send Init: {}", e)))?;

    tracing::debug!("Waiting for worker to connect to slot sockets");
    transport
        .accept_connections(num_slots)
        .await
        .map_err(|e| OrchestratorError::Spawn(format!("failed to accept connections: {}", e)))?;

    tracing::debug!("Waiting for Ready from worker");
    let setup_fut = async {
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
                Some(Ok(ControlResponse::WorkerLog {
                    target,
                    level,
                    message,
                })) => {
                    emit_worker_log(&target, &level, &message);
                }
                Some(Ok(ControlResponse::DroppedLogs {
                    count,
                    interval_millis,
                })) => {
                    tracing::trace!(count, interval_millis, "Received DroppedLogs during setup");
                    let interval_secs = interval_millis as f64 / 1000.0;
                    tracing::warn!(
                        "Log production exceeds consumption rate during setup. {} logs dropped in last {:.1}s",
                        count,
                        interval_secs
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
    };

    let (slot_ids, schema) = match config.setup_timeout {
        Some(timeout) => {
            tracing::debug!(
                timeout_secs = timeout.as_secs(),
                "Waiting for setup with timeout"
            );
            match tokio::time::timeout(timeout, setup_fut).await {
                Ok(Ok((slots, schema))) => {
                    tracing::debug!(num_slots = slots.len(), "Setup completed within timeout");
                    (slots, schema)
                }
                Ok(Err(e)) => {
                    tracing::debug!(error = %e, "Setup failed");
                    return Err(e);
                }
                Err(_) => {
                    tracing::debug!(timeout_secs = timeout.as_secs(), "Setup timed out");
                    return Err(OrchestratorError::SetupTimeout);
                }
            }
        }
        None => {
            tracing::debug!("Waiting for setup with no timeout");
            setup_fut.await?
        }
    };

    let setup_logs = crate::setup_log_accumulator::drain_accumulated_logs(setup_log_rx);
    tracing::debug!(
        setup_logs_len = setup_logs.len(),
        "Drained accumulated setup logs"
    );

    tracing::debug!(num_slots = slot_ids.len(), "Worker ready");

    if let Some(ref s) = schema
        && let Ok(json) = serde_json::to_string_pretty(s)
    {
        tracing::trace!(target: "coglet::schema", schema = %json, "OpenAPI schema");
    }

    // Determine whether the output type is an array from the schema so the
    // event loop can correctly wrap single-element list returns as Stream
    // instead of collapsing them to Single.
    let output_is_array = schema
        .as_ref()
        .and_then(|s| s.get("components"))
        .and_then(|c| c.get("schemas"))
        .and_then(|schemas| {
            let key = if config.is_train {
                "TrainingOutput"
            } else {
                "Output"
            };
            schemas.get(key)
        })
        .and_then(|output| output.get("type"))
        .and_then(|t| t.as_str())
        .is_some_and(|t| t == "array");

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
    let (cancel_tx, cancel_rx) = mpsc::channel(16);

    let ctrl_writer = Arc::new(tokio::sync::Mutex::new(ctrl_writer));

    let handle = OrchestratorHandle {
        child: tokio::sync::Mutex::new(Some(child)),
        ctrl_writer: Arc::clone(&ctrl_writer),
        register_tx,
        healthcheck_tx,
        cancel_tx,
        slot_ids: slot_ids.clone(),
    };

    let pool_for_loop = Arc::clone(&pool);
    let ctrl_writer_for_loop = Arc::clone(&ctrl_writer);
    let upload_url = config.upload_url.clone();
    tokio::spawn(async move {
        run_event_loop(
            ctrl_reader,
            ctrl_writer_for_loop,
            slot_readers,
            register_rx,
            healthcheck_rx,
            cancel_rx,
            pool_for_loop,
            upload_url,
            output_is_array,
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

fn record_pending_cancellation(pending_cancellations: &mut HashSet<String>, prediction_id: String) {
    if pending_cancellations.len() >= MAX_PENDING_CANCELLATIONS {
        tracing::warn!(
            prediction_id = %prediction_id,
            cap = MAX_PENDING_CANCELLATIONS,
            "Dropping pending cancellation because the pending cancellation buffer is full"
        );
        return;
    }
    pending_cancellations.insert(prediction_id);
}

#[allow(clippy::too_many_arguments)]
async fn run_event_loop(
    mut ctrl_reader: FramedRead<tokio::process::ChildStdout, JsonCodec<ControlResponse>>,
    ctrl_writer: Arc<
        tokio::sync::Mutex<FramedWrite<tokio::process::ChildStdin, JsonCodec<ControlRequest>>>,
    >,
    slot_readers: Vec<(
        SlotId,
        FramedRead<tokio::net::unix::OwnedReadHalf, JsonCodec<SlotResponse>>,
    )>,
    mut register_rx: mpsc::Receiver<RegisterPredictionMessage>,
    mut healthcheck_rx: mpsc::Receiver<tokio::sync::oneshot::Sender<HealthcheckResult>>,
    mut cancel_rx: mpsc::Receiver<String>,
    pool: Arc<PermitPool>,
    upload_url: Option<String>,
    // Schema says Output is "type": "array" — always wrap as Stream.
    // When false, the schema was unavailable or Output type is Any; fall
    // back to the predictor's is_stream flag on the Done message.
    output_is_array: bool,
) {
    let mut predictions: HashMap<SlotId, Arc<StdMutex<Prediction>>> = HashMap::new();
    let mut idle_senders: HashMap<SlotId, tokio::sync::oneshot::Sender<SlotIdleToken>> =
        HashMap::new();
    let mut pending_healthchecks: Vec<tokio::sync::oneshot::Sender<HealthcheckResult>> = Vec::new();
    let mut healthcheck_counter: u64 = 0;
    let mut pending_uploads: HashMap<SlotId, Vec<AbortOnDropHandle<Result<(), String>>>> =
        HashMap::new();
    let mut pending_cancellations: HashSet<String> = HashSet::new();

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
                        tracing::trace!(
                            ?status,
                            ?error,
                            pending_count = pending_healthchecks.len(),
                            "Received healthcheck result from worker"
                        );
                        if pending_healthchecks.is_empty() {
                            tracing::warn!("Received healthcheck result but no pending requests");
                        } else {
                            let result = match status {
                                HealthcheckStatus::Healthy => HealthcheckResult::healthy(),
                                HealthcheckStatus::Unhealthy => {
                                    HealthcheckResult::unhealthy(error.unwrap_or_else(|| "unhealthy".to_string()))
                                }
                            };
                            tracing::trace!(
                                pending_count = pending_healthchecks.len(),
                                "Distributing healthcheck result to pending callers"
                            );
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
                    tracing::trace!(%hc_id, "Sending healthcheck request to worker");

                    let mut writer = ctrl_writer.lock().await;
                    if let Err(e) = writer.send(ControlRequest::Healthcheck { id: hc_id }).await {
                        tracing::error!(error = %e, "Failed to send healthcheck request");
                        let result = HealthcheckResult::unhealthy(format!("Failed to send: {}", e));
                        for tx in pending_healthchecks.drain(..) {
                            let _ = tx.send(result.clone());
                        }
                    }
                } else {
                    tracing::trace!(
                        pending_count = pending_healthchecks.len(),
                        "Healthcheck already in-flight, coalescing request"
                    );
                }
            }

            Some(prediction_id) = cancel_rx.recv() => {
                // Resolve prediction_id → slot_id by iterating (fine for small concurrency)
                let slot = predictions.iter().find_map(|(sid, pred)| {
                    try_lock_prediction(pred)
                        .filter(|p| p.id() == prediction_id)
                        .map(|_| *sid)
                });
                match slot {
                    Some(slot_id) => {
                        tracing::info!(
                            target: "coglet::prediction",
                            %prediction_id,
                            %slot_id,
                            "Cancelling prediction"
                        );
                        let mut writer = ctrl_writer.lock().await;
                        if let Err(e) = writer.send(ControlRequest::Cancel { slot: slot_id }).await {
                            tracing::error!(
                                %slot_id,
                                error = %e,
                                "Failed to send cancel request to worker"
                            );
                        }
                        // Also abort any pending upload tasks for this slot
                        if let Some(handles) = pending_uploads.remove(&slot_id) {
                            for h in handles { h.abort(); }
                        }
                    }
                    None => {
                        tracing::debug!(%prediction_id, "Cancel requested for unknown prediction; storing pending cancellation");
                        record_pending_cancellation(&mut pending_cancellations, prediction_id);
                    }
                }
            }

            Some(RegisterPredictionMessage { slot_id, prediction, idle_sender, registered_ack }) = register_rx.recv() => {
                let prediction_id = match try_lock_prediction(&prediction) {
                    Some(p) => p.id().to_string(),
                    None => {
                        // Mutex poisoned during registration - prediction already failed
                        tracing::error!(%slot_id, "Prediction mutex poisoned during registration");
                        let _ = registered_ack.send(());
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
                let pending_cancel = pending_cancellations.remove(&prediction_id);
                let _ = registered_ack.send(());
                if pending_cancel {
                    tracing::info!(
                        target: "coglet::prediction",
                        %prediction_id,
                        %slot_id,
                        "Applying pending cancellation"
                    );
                    let mut writer = ctrl_writer.lock().await;
                    if let Err(e) = writer.send(ControlRequest::Cancel { slot: slot_id }).await {
                        tracing::error!(
                            %slot_id,
                            error = %e,
                            "Failed to send pending cancel request to worker"
                        );
                    }
                }
            }

            Some((slot_id, result)) = slot_msg_rx.recv() => {
                match result {
                    Ok(SlotResponse::ProtocolVersion { version }) => {
                        if version != crate::bridge::protocol::SLOT_RESPONSE_PROTOCOL_VERSION {
                            tracing::warn!(
                                %slot_id,
                                version,
                                expected = crate::bridge::protocol::SLOT_RESPONSE_PROTOCOL_VERSION,
                                "Worker reported unexpected slot response protocol version"
                            );
                        }
                    }
                    Ok(SlotResponse::LogLine { source, data }) => {
                        let (prediction_id, poisoned) = if let Some(pred) = predictions.get(&slot_id) {
                            if let Some(mut p) = try_lock_prediction(pred) {
                                p.append_log_source(source, &data);
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
                    Ok(SlotResponse::Metric { name, value, mode }) => {
                        let poisoned = if let Some(pred) = predictions.get(&slot_id) {
                            if let Some(mut p) = try_lock_prediction(pred) {
                                p.set_metric(name, value, mode);
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
                    Ok(SlotResponse::OutputChunk { output, index }) => {
                        let poisoned = if let Some(pred) = predictions.get(&slot_id) {
                            if let Some(mut p) = try_lock_prediction(pred) {
                                p.append_output_chunk(output, index);
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
                    Ok(SlotResponse::FileOutput { filename, kind, mime_type }) => {
                        tracing::debug!(%slot_id, %filename, ?kind, "FileOutput received");
                        let bytes = match std::fs::read(&filename) {
                            Ok(b) => b,
                            Err(e) => {
                                tracing::error!(%slot_id, %filename, error = %e, "Failed to read FileOutput");
                                continue;
                            }
                        };
                        match kind {
                            FileOutputKind::Oversized => {
                                let output: serde_json::Value = match serde_json::from_slice(&bytes) {
                                    Ok(val) => val,
                                    Err(e) => {
                                        tracing::error!(%slot_id, %filename, error = %e, "Failed to parse oversized JSON");
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
                            FileOutputKind::FileType => {
                                let mime = mime_type.unwrap_or_else(|| {
                                    mime_guess::from_path(&filename)
                                        .first_or_octet_stream()
                                        .to_string()
                                });
                                if let Some(ref url) = upload_url {
                                    // Spawn upload task so we don't block the event loop
                                    let Some(pred) = predictions.get(&slot_id).cloned() else {
                                        continue;
                                    };
                                    let Some(prediction_id) = try_lock_prediction(&pred).map(|p| p.id().to_string()) else {
                                        predictions.remove(&slot_id);
                                        continue;
                                    };
                                    #[cfg(feature = "tracing")]
                                    let trace = try_lock_prediction(&pred).and_then(|p| p.trace_carrier());
                                    let endpoint = ensure_trailing_slash(url);
                                    let basename = std::path::Path::new(&filename)
                                        .file_name()
                                        .and_then(|n| n.to_str())
                                        .unwrap_or("output")
                                        .to_string();
                                    let upload_span = crate::cog_span!(
                                        info_span,
                                        "cog.prediction.upload_output",
                                        "cog.output.bytes" = bytes.len() as u64,
                                        "cog.output.mime_type" = %mime,
                                        "otel.kind" = "client"
                                    );
                                    #[cfg(feature = "tracing")]
                                    if let Some(trace) = trace.as_ref() {
                                        crate::trace::set_parent_from_carrier(&upload_span, trace);
                                    }
                                    let handle = AbortOnDropHandle::new(tokio::spawn(async move {
                                        match upload_file(&endpoint, &basename, &bytes, &mime, &prediction_id).await {
                                            Ok(url) => {
                                                if let Some(mut p) = try_lock_prediction(&pred) {
                                                    p.append_output(serde_json::Value::String(url));
                                                }
                                                Ok(())
                                            }
                                            Err(e) => {
                                                tracing::error!(%prediction_id, error = %e, "Failed to upload file output");
                                                Err(e)
                                            }
                                        }
                                    }.instrument(upload_span)));
                                    pending_uploads.entry(slot_id).or_default().push(handle);
                                } else {
                                    // No upload URL — base64-encode as data URI
                                    use base64::Engine;
                                    let encoded = base64::engine::general_purpose::STANDARD
                                        .encode(&bytes);
                                    let output = serde_json::Value::String(format!(
                                        "data:{mime};base64,{encoded}"
                                    ));
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
                            }
                        }
                    }
                    Ok(SlotResponse::Done { id, output: _, predict_time, is_stream }) => {
                        tracing::info!(
                            target: "coglet::prediction",
                            prediction_id = %id,
                            predict_time,
                            is_stream,
                            output_is_array,
                            "Prediction worker completed"
                        );
                        let uploads = pending_uploads.remove(&slot_id).unwrap_or_default();
                        if let Some(pred) = predictions.remove(&slot_id) {
                            if uploads.is_empty() {
                                // No pending uploads — complete synchronously to avoid
                                // a race between tokio::spawn and Notify::notified() in
                                // service.rs.  notify_waiters() only wakes already-
                                // registered waiters; spawning a task can fire the
                                // notification before the service registers its waiter.
                                if let Some(mut p) = try_lock_prediction(&pred) {
                                    if p.is_canceled() {
                                        p.set_canceled();
                                    } else {
                                        let pred_output = wrap_outputs(
                                            p.take_outputs(),
                                            output_is_array,
                                            is_stream,
                                        );
                                        p.set_succeeded(pred_output);
                                    }
                                }
                            } else {
                                // Wait for uploads before making the prediction terminal.
                                // Dropping their handles aborts them on cancellation.
                                let Some(cancel_token) = try_lock_prediction(&pred).map(|p| p.cancel_token()) else {
                                    continue;
                                };
                                tokio::spawn(async move {
                                    let results = tokio::select! {
                                        biased;
                                        _ = cancel_token.cancelled() => {
                                            tracing::info!(
                                                target: "coglet::prediction",
                                                prediction_id = %id,
                                                "Aborting in-flight uploads due to cancellation"
                                            );
                                            if let Some(mut p) = try_lock_prediction(&pred) {
                                                p.set_canceled();
                                            }
                                            return;
                                        }
                                        results = futures::future::join_all(uploads) => results,
                                    };
                                    let error = results.into_iter().find_map(|result| match result {
                                        Ok(Ok(())) => None,
                                        Ok(Err(error)) => Some(error),
                                        Err(error) => Some(format!("upload task failed: {error}")),
                                    });
                                    if let Some(mut p) = try_lock_prediction(&pred) {
                                        if p.is_canceled() {
                                            p.set_canceled();
                                        } else if let Some(error) = error {
                                            p.set_failed(format!("Failed to upload output: {error}"));
                                        } else {
                                            let pred_output = wrap_outputs(
                                                p.take_outputs(),
                                                output_is_array,
                                                is_stream,
                                            );
                                            p.set_succeeded(pred_output);
                                        }
                                    }
                                });
                            }
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
                        // Abort any pending uploads — prediction is terminal
                        if let Some(handles) = pending_uploads.remove(&slot_id) {
                            for h in handles { h.abort(); }
                        }
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
                        // Abort any pending uploads — prediction is terminal
                        if let Some(handles) = pending_uploads.remove(&slot_id) {
                            for h in handles { h.abort(); }
                        }
                        if let Some(pred) = predictions.remove(&slot_id)
                            && let Some(mut p) = try_lock_prediction(&pred)
                        {
                            p.set_canceled();
                        }
                    }
                    Err(e) => {
                        tracing::error!(%slot_id, error = %e, "Slot socket error");
                        if let Some(handles) = pending_uploads.remove(&slot_id) {
                            for h in handles { h.abort(); }
                        }
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

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;
    use wiremock::matchers::{header, method, path};
    use wiremock::{Mock, MockServer, ResponseTemplate};

    struct TestPrediction {
        prediction: Arc<StdMutex<Prediction>>,
        writer: FramedWrite<tokio::net::unix::OwnedWriteHalf, JsonCodec<SlotResponse>>,
        event_loop: tokio::task::JoinHandle<()>,
        worker: Child,
        _idle_receiver: tokio::sync::oneshot::Receiver<SlotIdleToken>,
    }

    impl TestPrediction {
        async fn new(upload_url: String, output_is_array: bool) -> Self {
            let _ = rustls::crypto::ring::default_provider().install_default();
            // Keep the control pipe open without requiring a Python worker.
            let mut worker = Command::new("cat")
                .stdin(Stdio::piped())
                .stdout(Stdio::piped())
                .stderr(Stdio::null())
                .kill_on_drop(true)
                .spawn()
                .unwrap();
            let ctrl_reader = FramedRead::new(worker.stdout.take().unwrap(), JsonCodec::new());
            let ctrl_writer = Arc::new(tokio::sync::Mutex::new(FramedWrite::new(
                worker.stdin.take().unwrap(),
                JsonCodec::new(),
            )));
            let (parent, worker_socket) = tokio::net::UnixStream::pair().unwrap();
            let (reader, _) = parent.into_split();
            let (_, writer) = worker_socket.into_split();
            let slot_id = SlotId::new();
            let (register_tx, register_rx) = mpsc::channel(1);
            let (_, healthcheck_rx) = mpsc::channel(1);
            let (_, cancel_rx) = mpsc::channel(1);
            let event_loop = tokio::spawn(run_event_loop(
                ctrl_reader,
                ctrl_writer,
                vec![(slot_id, FramedRead::new(reader, JsonCodec::new()))],
                register_rx,
                healthcheck_rx,
                cancel_rx,
                Arc::new(PermitPool::new(1)),
                Some(upload_url),
                output_is_array,
            ));
            let prediction = Arc::new(StdMutex::new(Prediction::new("upload-test".into(), None)));
            prediction.lock().unwrap().set_processing();
            let (idle_sender, idle_receiver) = tokio::sync::oneshot::channel();
            let (registered_ack, registered_rx) = tokio::sync::oneshot::channel();
            register_tx
                .send(RegisterPredictionMessage {
                    slot_id,
                    prediction: Arc::clone(&prediction),
                    idle_sender,
                    registered_ack,
                })
                .await
                .unwrap();
            registered_rx.await.unwrap();
            Self {
                prediction,
                writer: FramedWrite::new(writer, JsonCodec::new()),
                event_loop,
                worker,
                _idle_receiver: idle_receiver,
            }
        }

        async fn file_output(&mut self, file: &std::path::Path) {
            self.writer
                .send(SlotResponse::FileOutput {
                    filename: file.to_str().unwrap().to_string(),
                    kind: FileOutputKind::FileType,
                    mime_type: Some("application/octet-stream".into()),
                })
                .await
                .unwrap();
        }

        async fn finish(&mut self, is_stream: bool) -> serde_json::Value {
            let completion = self.prediction.lock().unwrap().completion();
            self.writer
                .send(SlotResponse::Done {
                    id: "upload-test".into(),
                    output: None,
                    predict_time: 0.1,
                    is_stream,
                })
                .await
                .unwrap();
            tokio::time::timeout(Duration::from_secs(30), completion.notified())
                .await
                .expect("prediction did not complete");
            let response = self.prediction.lock().unwrap().build_state_snapshot();
            self.worker.kill().await.unwrap();
            self.event_loop.abort();
            let _ = (&mut self.event_loop).await;
            response
        }
    }

    impl Drop for TestPrediction {
        fn drop(&mut self) {
            self.event_loop.abort();
        }
    }

    #[tokio::test]
    async fn rejected_output_upload_fails_scalar_prediction() {
        let server = MockServer::start().await;
        Mock::given(method("PUT"))
            .respond_with(ResponseTemplate::new(503))
            .expect(1)
            .mount(&server)
            .await;
        let file = tempfile::NamedTempFile::new().unwrap();
        let mut prediction = TestPrediction::new(server.uri(), false).await;
        prediction.file_output(file.path()).await;

        let response = prediction.finish(false).await;

        assert_eq!(response["status"], "failed", "{response}");
        assert_eq!(
            response["error"],
            "Failed to upload output: upload returned status 503 Service Unavailable"
        );
        assert!(response["output"].is_null());
    }

    #[tokio::test]
    async fn rejected_output_upload_fails_prediction_with_partial_list_output() {
        let server = MockServer::start().await;
        Mock::given(method("PUT"))
            .respond_with(ResponseTemplate::new(503))
            .expect(1)
            .mount(&server)
            .await;
        let file = tempfile::NamedTempFile::new().unwrap();
        let mut prediction = TestPrediction::new(server.uri(), true).await;
        prediction
            .writer
            .send(SlotResponse::OutputChunk {
                output: json!("https://example.com/previous-output"),
                index: 0,
            })
            .await
            .unwrap();
        prediction.file_output(file.path()).await;

        let response = prediction.finish(true).await;

        assert_eq!(response["status"], "failed", "{response}");
        assert!(response["error"].as_str().unwrap().contains("503"));
    }

    #[tokio::test]
    async fn timed_out_output_upload_fails_prediction() {
        let server = MockServer::start().await;
        Mock::given(method("PUT"))
            .respond_with(ResponseTemplate::new(201).set_delay(Duration::from_secs(35)))
            .mount(&server)
            .await;
        let file = tempfile::NamedTempFile::new().unwrap();
        let mut prediction = TestPrediction::new(server.uri(), false).await;
        prediction.file_output(file.path()).await;

        let response = prediction.finish(false).await;

        assert_eq!(response["status"], "failed", "{response}");
        assert_eq!(
            response["error"],
            "Failed to upload output: upload request timed out"
        );
    }

    #[tokio::test]
    async fn output_upload_sends_prediction_id_and_preserves_output_url() {
        let server = MockServer::start().await;
        Mock::given(method("PUT"))
            .and(path("/output.bin"))
            .and(header("X-Prediction-Id", "upload-test"))
            .and(header("Content-Type", "application/octet-stream"))
            .respond_with(ResponseTemplate::new(202).insert_header(
                "Location",
                "https://example.com/result.bin?secret=signing-token",
            ))
            .expect(1)
            .mount(&server)
            .await;
        let directory = tempfile::tempdir().unwrap();
        let file = directory.path().join("output.bin");
        std::fs::write(&file, b"prediction output").unwrap();
        let mut prediction = TestPrediction::new(server.uri(), false).await;
        prediction.file_output(&file).await;

        let response = prediction.finish(false).await;

        assert_eq!(response["status"], "succeeded", "{response}");
        assert_eq!(response["output"], "https://example.com/result.bin");
        assert!(response["error"].is_null());
        assert_eq!(
            server.received_requests().await.unwrap()[0].body,
            b"prediction output"
        );
    }

    #[tokio::test]
    async fn output_upload_follows_redirects_and_strips_signing_query() {
        let server = MockServer::start().await;
        Mock::given(method("PUT"))
            .and(path("/output.bin"))
            .respond_with(ResponseTemplate::new(307).insert_header(
                "Location",
                format!("{}/signed/output.bin?token=secret", server.uri()),
            ))
            .expect(1)
            .mount(&server)
            .await;
        Mock::given(method("PUT"))
            .and(path("/signed/output.bin"))
            .and(wiremock::matchers::body_bytes(b"prediction output"))
            .respond_with(ResponseTemplate::new(201))
            .expect(1)
            .mount(&server)
            .await;
        let directory = tempfile::tempdir().unwrap();
        let file = directory.path().join("output.bin");
        std::fs::write(&file, b"prediction output").unwrap();
        let mut prediction = TestPrediction::new(server.uri(), false).await;
        prediction.file_output(&file).await;

        let response = prediction.finish(false).await;

        assert_eq!(response["status"], "succeeded", "{response}");
        assert_eq!(
            response["output"],
            format!("{}/signed/output.bin", server.uri())
        );
    }

    #[tokio::test]
    async fn output_upload_connection_error_fails_prediction_without_leaking_url() {
        let listener = std::net::TcpListener::bind("127.0.0.1:0").unwrap();
        let address = listener.local_addr().unwrap();
        drop(listener);
        let file = tempfile::NamedTempFile::new().unwrap();
        let mut prediction = TestPrediction::new(
            format!("http://{address}/?secret=upload-token&filename="),
            false,
        )
        .await;
        prediction.file_output(file.path()).await;

        let response = prediction.finish(false).await;

        assert_eq!(response["status"], "failed", "{response}");
        let error = response["error"].as_str().unwrap();
        assert!(error.starts_with("Failed to upload output: upload request failed:"));
        assert!(!error.contains("upload-token"), "{error}");
        assert!(!error.contains(&address.to_string()), "{error}");
    }

    #[tokio::test]
    async fn canceling_prediction_closes_pending_upload_connection() {
        use tokio::io::AsyncReadExt;

        let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
        let file = tempfile::NamedTempFile::new().unwrap();
        let mut prediction =
            TestPrediction::new(format!("http://{}", listener.local_addr().unwrap()), false).await;
        prediction.file_output(file.path()).await;
        let cancel_token = prediction.prediction.lock().unwrap().cancel_token();
        let server = async {
            let mut connection = tokio::time::timeout(Duration::from_secs(5), async {
                let (mut connection, _) = listener.accept().await.unwrap();
                // The file is empty, so the headers are the entire request.
                let mut request = Vec::new();
                while !request.ends_with(b"\r\n\r\n") {
                    request.push(connection.read_u8().await.unwrap());
                }
                connection
            })
            .await
            .expect("upload did not start");

            // Keep the response pending so only cancellation can close the connection.
            cancel_token.cancel();
            let mut buffer = [0];
            let bytes_read =
                tokio::time::timeout(Duration::from_secs(5), connection.read(&mut buffer))
                    .await
                    .expect("pending upload was not aborted")
                    .unwrap();
            assert_eq!(
                bytes_read, 0,
                "upload connection should close on cancellation"
            );
        };

        let (response, ()) = tokio::join!(prediction.finish(false), server);

        assert_eq!(response["status"], "canceled", "{response}");
        assert!(response["output"].is_null());
        assert!(response["error"].is_null());
    }

    #[tokio::test]
    async fn canceled_prediction_does_not_succeed_after_upload_failure() {
        let server = MockServer::start().await;
        Mock::given(method("PUT"))
            .respond_with(ResponseTemplate::new(503).set_delay(Duration::from_secs(35)))
            .mount(&server)
            .await;
        let file = tempfile::NamedTempFile::new().unwrap();
        let mut prediction = TestPrediction::new(server.uri(), false).await;
        prediction.file_output(file.path()).await;
        let cancel_token = prediction.prediction.lock().unwrap().cancel_token();
        let cancel_upload = async {
            tokio::time::timeout(Duration::from_secs(5), async {
                while server.received_requests().await.unwrap().is_empty() {
                    tokio::time::sleep(Duration::from_millis(1)).await;
                }
            })
            .await
            .expect("upload did not start");
            cancel_token.cancel();
        };

        let (response, ()) = tokio::join!(prediction.finish(false), cancel_upload);

        assert_eq!(response["status"], "canceled", "{response}");
        assert!(response["error"].is_null());
    }

    // ── wrap_outputs: schema says array (output_is_array = true) ──

    #[test]
    fn wrap_outputs_schema_array_empty() {
        // List[Path] that returned no items → empty array
        let result = wrap_outputs(vec![], true, true);
        assert!(result.is_stream());
        assert_eq!(result.into_values(), Vec::<serde_json::Value>::new());
    }

    #[test]
    fn record_pending_cancellation_caps_stored_ids() {
        let mut pending = HashSet::new();
        for index in 0..MAX_PENDING_CANCELLATIONS {
            record_pending_cancellation(&mut pending, format!("pred-{index}"));
        }

        record_pending_cancellation(&mut pending, "overflow".to_string());

        assert_eq!(pending.len(), MAX_PENDING_CANCELLATIONS);
        assert!(!pending.contains("overflow"));
    }

    #[test]
    fn wrap_outputs_schema_array_single_item() {
        // List[Path] with num_outputs=1 → ["url"] not "url"
        let result = wrap_outputs(vec![json!("https://example.com/img.png")], true, true);
        assert!(result.is_stream());
        assert_eq!(
            result.into_values(),
            vec![json!("https://example.com/img.png")]
        );
    }

    #[test]
    fn wrap_outputs_schema_array_multiple_items() {
        // List[Path] with num_outputs=4
        let items = vec![
            json!("https://example.com/1.png"),
            json!("https://example.com/2.png"),
            json!("https://example.com/3.png"),
            json!("https://example.com/4.png"),
        ];
        let result = wrap_outputs(items.clone(), true, true);
        assert!(result.is_stream());
        assert_eq!(result.into_values(), items);
    }

    #[test]
    fn wrap_outputs_schema_array_overrides_is_stream_false() {
        // Schema says array but predictor didn't set is_stream (shouldn't happen,
        // but schema is authoritative)
        let result = wrap_outputs(vec![json!("url")], true, false);
        assert!(result.is_stream());
    }

    // ── wrap_outputs: predictor signal (is_stream = true, no schema) ──

    #[test]
    fn wrap_outputs_predictor_stream_empty() {
        // Generator that yielded nothing, no schema
        let result = wrap_outputs(vec![], false, true);
        assert!(result.is_stream());
        assert_eq!(result.into_values(), Vec::<serde_json::Value>::new());
    }

    #[test]
    fn wrap_outputs_predictor_stream_single_item() {
        // Any-typed list with one element, no schema
        let result = wrap_outputs(vec![json!("only_item")], false, true);
        assert!(result.is_stream());
        assert_eq!(result.into_values(), vec![json!("only_item")]);
    }

    #[test]
    fn wrap_outputs_predictor_stream_multiple_items() {
        // Generator yielding multiple, no schema
        let items = vec![json!("a"), json!("b"), json!("c")];
        let result = wrap_outputs(items.clone(), false, true);
        assert!(result.is_stream());
        assert_eq!(result.into_values(), items);
    }

    // ── wrap_outputs: scalar output (neither schema array nor predictor stream) ──

    #[test]
    fn wrap_outputs_scalar_empty() {
        // Single output that was null (e.g. Path sent via FileOutput, not yet resolved?)
        let result = wrap_outputs(vec![], false, false);
        assert!(!result.is_stream());
        assert_eq!(result.final_value(), &json!(null));
    }

    #[test]
    fn wrap_outputs_scalar_single() {
        // return Path("output.png") → single string
        let result = wrap_outputs(vec![json!("https://example.com/output.png")], false, false);
        assert!(!result.is_stream());
        assert_eq!(
            result.final_value(),
            &json!("https://example.com/output.png")
        );
    }

    #[test]
    fn wrap_outputs_scalar_multiple_falls_back_to_stream() {
        // Shouldn't happen for scalar returns, but if multiple items arrive
        // with neither flag set, Stream is the safe choice
        let items = vec![json!("a"), json!("b")];
        let result = wrap_outputs(items.clone(), false, false);
        assert!(result.is_stream());
        assert_eq!(result.into_values(), items);
    }

    // ── Serialization: is_stream field on Done message ──

    #[test]
    fn done_is_stream_false_omitted_from_json() {
        let msg = SlotResponse::Done {
            id: "p1".into(),
            output: None,
            predict_time: 1.0,
            is_stream: false,
        };
        let json = serde_json::to_value(&msg).unwrap();
        assert!(
            json.get("is_stream").is_none(),
            "is_stream=false should be omitted"
        );
    }

    #[test]
    fn done_is_stream_true_present_in_json() {
        let msg = SlotResponse::Done {
            id: "p1".into(),
            output: None,
            predict_time: 1.0,
            is_stream: true,
        };
        let json = serde_json::to_value(&msg).unwrap();
        assert_eq!(json.get("is_stream"), Some(&json!(true)));
    }

    #[test]
    fn done_without_is_stream_deserializes_as_false() {
        // Backward compat: old workers won't send is_stream
        let json = json!({
            "type": "done",
            "id": "p1",
            "predict_time": 1.0
        });
        let msg: SlotResponse = serde_json::from_value(json).unwrap();
        match msg {
            SlotResponse::Done { is_stream, .. } => assert!(!is_stream),
            _ => panic!("wrong variant"),
        }
    }
}
