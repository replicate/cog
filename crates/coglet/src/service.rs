//! PredictionService: Transport-agnostic prediction lifecycle management.
//!
//! This service owns:
//! - Slot management (PermitPool for concurrency control)
//! - Health tracking (state, setup result)
//! - Shutdown coordination (bidirectional)
//!
//! Prediction lifecycle (state, cancellation, webhooks) is managed by PredictionSupervisor.
//! Transports (HTTP, gRPC, etc.) delegate to this service for prediction handling.

use std::sync::{Arc, Mutex as StdMutex};

use tokio::sync::{RwLock, watch};

use crate::bridge::protocol::SlotRequest;
use crate::health::{Health, SetupResult};
use crate::orchestrator::{HealthcheckResult, Orchestrator};
use crate::permit::{PermitPool, PredictionSlot, UnregisteredPredictionSlot};
use crate::prediction::{Prediction, PredictionStatus};
use crate::predictor::{PredictionError, PredictionOutput, PredictionResult};
use crate::supervisor::PredictionSupervisor;
use crate::version::VersionInfo;
use crate::webhook::WebhookSender;

/// Try to lock a prediction mutex. On poison, fail the prediction and return None.
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

#[derive(Debug, thiserror::Error)]
pub enum CreatePredictionError {
    #[error("Service not ready")]
    NotReady,
    #[error("At capacity (no slots available)")]
    AtCapacity,
}

/// Snapshot of service health for transports to query.
#[derive(Debug, Clone)]
pub struct HealthSnapshot {
    pub state: Health,
    pub available_slots: usize,
    pub total_slots: usize,
    pub setup_result: Option<SetupResult>,
    pub version: VersionInfo,
}

impl HealthSnapshot {
    pub fn is_ready(&self) -> bool {
        self.state == Health::Ready
    }

    /// BUSY state: ready but all slots in use.
    pub fn is_busy(&self) -> bool {
        self.state == Health::Ready && self.available_slots == 0
    }
}

/// Transport-agnostic prediction service.
///
/// Created with `new_no_pool()`, then configured with `set_orchestrator()` once
/// the worker subprocess is ready.
pub struct PredictionService {
    /// Orchestrator state (pool + handle together).
    orchestrator: RwLock<Option<OrchestratorState>>,

    health: RwLock<Health>,
    setup_result: RwLock<Option<SetupResult>>,

    supervisor: Arc<PredictionSupervisor>,

    shutdown_tx: watch::Sender<bool>,
    shutdown_rx: watch::Receiver<bool>,

    version: VersionInfo,

    schema: RwLock<Option<serde_json::Value>>,
}

/// Orchestrator runtime state - pool and orchestrator together.
///
/// Ensures pool and orchestrator are always set atomically.
pub struct OrchestratorState {
    pub pool: Arc<PermitPool>,
    pub orchestrator: Arc<dyn Orchestrator>,
}

impl Clone for OrchestratorState {
    fn clone(&self) -> Self {
        Self {
            pool: Arc::clone(&self.pool),
            orchestrator: Arc::clone(&self.orchestrator),
        }
    }
}

impl PredictionService {
    /// Create without configuration (for early HTTP start).
    ///
    /// Health check returns STARTING until `set_orchestrator()` is called.
    pub fn new_no_pool() -> Self {
        let (shutdown_tx, shutdown_rx) = watch::channel(false);
        Self {
            orchestrator: RwLock::new(None),
            health: RwLock::new(Health::Unknown),
            setup_result: RwLock::new(None),
            supervisor: PredictionSupervisor::new(),
            shutdown_tx,
            shutdown_rx,
            version: VersionInfo::new(),
            schema: RwLock::new(None),
        }
    }

    /// Configure orchestrator mode atomically.
    ///
    /// Also sets the orchestrator on the supervisor so cancellation can be
    /// delegated from supervisor → orchestrator → worker.
    pub async fn set_orchestrator(
        &self,
        pool: Arc<PermitPool>,
        orchestrator: Arc<dyn Orchestrator>,
    ) {
        self.supervisor
            .set_orchestrator(Arc::clone(&orchestrator))
            .await;
        *self.orchestrator.write().await = Some(OrchestratorState { pool, orchestrator });
    }

    pub async fn has_orchestrator(&self) -> bool {
        self.orchestrator.read().await.is_some()
    }

    /// Shutdown the orchestrator gracefully.
    ///
    /// Sends a shutdown message to the worker process and waits for it to exit.
    /// If no orchestrator is configured, this is a no-op.
    pub async fn shutdown(&self) {
        if let Some(ref state) = *self.orchestrator.read().await
            && let Err(e) = state.orchestrator.shutdown().await
        {
            tracing::warn!(error = %e, "Error during orchestrator shutdown");
        }
    }

    pub fn supervisor(&self) -> &Arc<PredictionSupervisor> {
        &self.supervisor
    }

    /// Set initial health state (for non-Ready states only).
    ///
    /// READY requires an orchestrator, so use `set_health()` after `set_orchestrator()`.
    /// Silently ignores attempts to set READY here.
    pub fn with_health(mut self, health: Health) -> Self {
        if health != Health::Ready {
            self.health = RwLock::new(health);
        }
        self
    }

    pub fn with_version(mut self, version: VersionInfo) -> Self {
        self.version = version;
        self
    }

    /// Get the permit pool from orchestrator.
    pub async fn pool(&self) -> Option<Arc<PermitPool>> {
        if let Some(ref state) = *self.orchestrator.read().await {
            Some(Arc::clone(&state.pool))
        } else {
            None
        }
    }

    pub async fn health(&self) -> HealthSnapshot {
        let state = *self.health.read().await;
        let setup_result = self.setup_result.read().await.clone();
        let pool = self.pool().await;
        let (available_slots, total_slots) = match pool.as_ref() {
            Some(p) => (p.available(), p.num_slots()),
            None => (0, 0),
        };

        HealthSnapshot {
            state,
            available_slots,
            total_slots,
            setup_result,
            version: self.version.clone(),
        }
    }

    /// Set health state. Setting READY requires orchestrator to be configured.
    ///
    /// Silently ignores attempts to set READY without orchestrator.
    pub async fn set_health(&self, health: Health) {
        if health == Health::Ready && self.orchestrator.read().await.is_none() {
            tracing::warn!("Attempted to set READY without orchestrator, ignoring");
            return;
        }
        *self.health.write().await = health;
    }

    pub async fn set_setup_result(&self, result: SetupResult) {
        *self.setup_result.write().await = Some(result);
    }

    pub async fn set_schema(&self, schema: serde_json::Value) {
        *self.schema.write().await = Some(schema);
    }

    pub async fn schema(&self) -> Option<serde_json::Value> {
        self.schema.read().await.clone()
    }

    /// Run user-defined healthcheck via orchestrator.
    ///
    /// Returns healthy if no orchestrator is configured (not ready yet).
    pub async fn healthcheck(
        &self,
    ) -> Result<HealthcheckResult, crate::orchestrator::OrchestratorError> {
        if let Some(ref state) = *self.orchestrator.read().await {
            state.orchestrator.healthcheck().await
        } else {
            // No orchestrator = not ready, return healthy (healthcheck not applicable)
            Ok(HealthcheckResult::healthy())
        }
    }

    /// Create a new prediction, acquiring a slot permit.
    ///
    /// Caller should check for duplicates via supervisor first.
    pub async fn create_prediction(
        &self,
        id: String,
        webhook: Option<WebhookSender>,
    ) -> Result<UnregisteredPredictionSlot, CreatePredictionError> {
        let health = *self.health.read().await;
        if health != Health::Ready {
            return Err(CreatePredictionError::NotReady);
        }

        // Pool must exist if health is Ready
        let pool = self.pool().await;
        let pool = pool.as_ref().ok_or(CreatePredictionError::NotReady)?;

        let permit = pool
            .try_acquire()
            .ok_or(CreatePredictionError::AtCapacity)?;

        let prediction = Prediction::new(id, webhook);
        let (idle_tx, idle_rx) = tokio::sync::oneshot::channel();
        Ok(UnregisteredPredictionSlot::new(
            PredictionSlot::new(prediction, permit, idle_rx),
            idle_tx,
        ))
    }

    pub fn prediction_exists(&self, id: &str) -> bool {
        self.supervisor.exists(id)
    }

    /// Run a prediction to completion via orchestrator.
    pub async fn predict(
        &self,
        unregistered_slot: UnregisteredPredictionSlot,
        input: serde_json::Value,
    ) -> Result<PredictionResult, PredictionError> {
        let state = self.orchestrator.read().await.clone();
        let state = state
            .ok_or_else(|| PredictionError::Failed("No orchestrator configured".to_string()))?;

        let (idle_tx, mut slot) = unregistered_slot.into_parts();
        let prediction_id = slot.id();
        let slot_id = slot.slot_id();

        {
            let prediction = slot.prediction();
            let Some(mut pred) = try_lock_prediction(&prediction) else {
                return Err(PredictionError::Failed(
                    "Prediction mutex poisoned".to_string(),
                ));
            };
            pred.set_processing();
        }

        // Register for response routing in event loop
        let prediction_arc = slot.prediction();
        state
            .orchestrator
            .register_prediction(slot_id, Arc::clone(&prediction_arc), idle_tx)
            .await;

        // Create per-prediction output dir for file-based outputs
        let output_dir = std::path::PathBuf::from("/tmp/coglet/outputs").join(&prediction_id);
        std::fs::create_dir_all(&output_dir)
            .map_err(|e| PredictionError::Failed(format!("Failed to create output dir: {}", e)))?;

        let request = SlotRequest::Predict {
            id: prediction_id.clone(),
            input,
            output_dir: output_dir
                .to_str()
                .expect("output dir path is valid UTF-8")
                .to_string(),
        };

        // permit_mut returns None if permit isn't InUse (shouldn't happen here)
        let permit = slot
            .permit_mut()
            .ok_or_else(|| PredictionError::Failed("Permit not in use".to_string()))?;

        if let Err(e) = permit.send(request).await {
            tracing::error!(%slot_id, error = %e, "Failed to send prediction request");
            // Broken socket means the slot is dead — poison it at the pool level.
            state.pool.poison(slot_id);
            if let Some(mut pred) = try_lock_prediction(&prediction_arc) {
                pred.set_failed(format!("Failed to send request: {}", e));
            }
            return Err(PredictionError::Failed(format!(
                "Failed to send request: {}",
                e
            )));
        }

        // Wait for prediction to complete
        // Check if already terminal first to avoid race with fast completions
        let (already_terminal, completion) = {
            let Some(pred) = try_lock_prediction(&prediction_arc) else {
                return Err(PredictionError::Failed(
                    "Prediction mutex poisoned".to_string(),
                ));
            };
            (pred.is_terminal(), pred.completion())
        };
        if !already_terminal {
            completion.notified().await;
        }

        let (status, output, error, logs, predict_time, metrics) = {
            let Some(pred) = try_lock_prediction(&prediction_arc) else {
                return Err(PredictionError::Failed(
                    "Prediction mutex poisoned".to_string(),
                ));
            };
            (
                pred.status(),
                pred.output().cloned(),
                pred.error().map(|s| s.to_string()),
                pred.logs().to_string(),
                pred.elapsed(),
                pred.metrics().clone(),
            )
        };

        // If `into_idle()` fails, it does not necessarily mean the prediction failed,
        // so we return the result if available, but log the error and poison the slot to prevent reuse.
        // This is performed asynchronously to avoid blocking the prediction response to the caller.
        tokio::spawn(async move {
            if let Err(e) = slot.into_idle().await {
                tracing::error!(%slot_id, error = %e, "Failed to transition slot to idle, poisoning slot");
                state.pool.poison(slot_id);
            }
        });

        match status {
            PredictionStatus::Succeeded => Ok(PredictionResult {
                output: output.unwrap_or(PredictionOutput::Single(serde_json::Value::Null)),
                predict_time: Some(predict_time),
                logs,
                metrics,
            }),
            PredictionStatus::Failed => Err(PredictionError::Failed(
                error.unwrap_or_else(|| "Unknown error".to_string()),
            )),
            PredictionStatus::Canceled => Err(PredictionError::Cancelled),
            _ => Err(PredictionError::Failed(format!(
                "Prediction ended in unexpected state: {:?}",
                status
            ))),
        }
    }

    /// Cancel a prediction by ID. Returns true if found and cancelled.
    pub fn cancel(&self, id: &str) -> bool {
        self.supervisor.cancel(id)
    }

    /// Unregister a prediction after completion.
    pub fn unregister_prediction(&self, id: &str) {
        self.supervisor.remove(id);
    }

    pub fn trigger_shutdown(&self) {
        let _ = self.shutdown_tx.send(true);
    }

    pub fn shutdown_rx(&self) -> watch::Receiver<bool> {
        self.shutdown_rx.clone()
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::bridge::protocol::SlotId;
    use crate::permit::{InactiveSlotIdleToken, SlotIdleToken};
    use std::sync::atomic::{AtomicUsize, Ordering};
    use std::time::Duration;

    /// Mock orchestrator that immediately completes predictions.
    struct MockOrchestrator {
        register_count: AtomicUsize,
        complete_immediately: bool,
        send_idle_ack: bool,
    }

    impl MockOrchestrator {
        fn new() -> Self {
            Self {
                register_count: AtomicUsize::new(0),
                complete_immediately: true,
                send_idle_ack: false,
            }
        }

        fn register_count(&self) -> usize {
            self.register_count.load(Ordering::SeqCst)
        }

        fn with_idle_ack(mut self) -> Self {
            self.send_idle_ack = true;
            self
        }
    }

    #[async_trait::async_trait]
    impl Orchestrator for MockOrchestrator {
        async fn register_prediction(
            &self,
            slot_id: SlotId,
            prediction: Arc<std::sync::Mutex<crate::prediction::Prediction>>,
            idle_sender: tokio::sync::oneshot::Sender<SlotIdleToken>,
        ) {
            self.register_count.fetch_add(1, Ordering::SeqCst);
            if self.complete_immediately {
                let mut pred = prediction.lock().unwrap();
                pred.set_succeeded(crate::PredictionOutput::Single(serde_json::json!(
                    "mock result"
                )));
            }
            if self.send_idle_ack {
                let _ = idle_sender.send(InactiveSlotIdleToken::new(slot_id).activate());
            }
        }

        async fn cancel_by_prediction_id(
            &self,
            _prediction_id: &str,
        ) -> Result<(), crate::orchestrator::OrchestratorError> {
            Ok(())
        }

        async fn healthcheck(
            &self,
        ) -> Result<HealthcheckResult, crate::orchestrator::OrchestratorError> {
            Ok(HealthcheckResult::healthy())
        }

        async fn shutdown(&self) -> Result<(), crate::orchestrator::OrchestratorError> {
            Ok(())
        }
    }

    async fn create_test_pool(num_slots: usize) -> Arc<PermitPool> {
        use crate::bridge::codec::JsonCodec;
        use crate::bridge::protocol::SlotRequest;
        use futures::StreamExt;
        use tokio::net::UnixStream;

        let pool = Arc::new(PermitPool::new(num_slots));
        for _ in 0..num_slots {
            let (a, b) = UnixStream::pair().unwrap();
            let (_read_a, write_a) = a.into_split();
            let (read_b, _write_b) = b.into_split();

            // Spawn a task to consume messages from the socket (prevents broken pipe)
            let mut reader =
                tokio_util::codec::FramedRead::new(read_b, JsonCodec::<SlotRequest>::new());
            tokio::spawn(async move { while reader.next().await.is_some() {} });

            let writer =
                tokio_util::codec::FramedWrite::new(write_a, JsonCodec::<SlotRequest>::new());
            pool.add_permit(SlotId::new(), writer);
        }
        pool
    }

    async fn create_test_pool_with_slots(num_slots: usize) -> (Arc<PermitPool>, Vec<SlotId>) {
        use crate::bridge::codec::JsonCodec;
        use crate::bridge::protocol::SlotRequest;
        use futures::StreamExt;
        use tokio::net::UnixStream;

        let pool = Arc::new(PermitPool::new(num_slots));
        let mut slot_ids = Vec::with_capacity(num_slots);
        for _ in 0..num_slots {
            let (a, b) = UnixStream::pair().unwrap();
            let (_read_a, write_a) = a.into_split();
            let (read_b, _write_b) = b.into_split();

            let mut reader =
                tokio_util::codec::FramedRead::new(read_b, JsonCodec::<SlotRequest>::new());
            tokio::spawn(async move { while reader.next().await.is_some() {} });

            let writer =
                tokio_util::codec::FramedWrite::new(write_a, JsonCodec::<SlotRequest>::new());
            let slot_id = SlotId::new();
            pool.add_permit(slot_id, writer);
            slot_ids.push(slot_id);
        }
        (pool, slot_ids)
    }

    async fn create_broken_test_pool() -> (Arc<PermitPool>, SlotId) {
        use crate::bridge::codec::JsonCodec;
        use crate::bridge::protocol::SlotRequest;
        use tokio::net::UnixStream;

        let pool = Arc::new(PermitPool::new(1));
        let (a, b) = UnixStream::pair().unwrap();
        let (_read_a, write_a) = a.into_split();
        drop(b);

        let writer = tokio_util::codec::FramedWrite::new(write_a, JsonCodec::<SlotRequest>::new());
        let slot_id = SlotId::new();
        pool.add_permit(slot_id, writer);
        (pool, slot_id)
    }

    #[tokio::test]
    async fn service_new_no_pool_works() {
        let svc = PredictionService::new_no_pool();
        let health = svc.health().await;

        assert_eq!(health.state, Health::Unknown);
        assert_eq!(health.total_slots, 0);
        assert_eq!(health.available_slots, 0);
        assert!(svc.pool().await.is_none());
    }

    #[tokio::test]
    async fn service_no_pool_initially() {
        let svc = PredictionService::new_no_pool();

        assert!(svc.pool().await.is_none());
        assert!(!svc.has_orchestrator().await);
    }

    #[tokio::test]
    async fn shutdown_signal_works() {
        let svc = PredictionService::new_no_pool();
        let mut rx = svc.shutdown_rx();

        assert!(!*rx.borrow());

        svc.trigger_shutdown();
        rx.changed().await.unwrap();

        assert!(*rx.borrow());
    }

    #[tokio::test]
    async fn create_prediction_fails_when_not_ready() {
        let svc = PredictionService::new_no_pool();

        let result = svc.create_prediction("test".to_string(), None).await;
        assert!(matches!(result, Err(CreatePredictionError::NotReady)));
    }

    #[tokio::test]
    async fn cannot_set_ready_without_orchestrator() {
        let svc = PredictionService::new_no_pool();

        // with_health silently ignores READY
        let svc2 = PredictionService::new_no_pool().with_health(Health::Ready);
        assert_eq!(svc2.health().await.state, Health::Unknown);

        // set_health also ignores READY without orchestrator
        svc.set_health(Health::Ready).await;
        assert_eq!(svc.health().await.state, Health::Unknown);
    }

    #[tokio::test]
    async fn set_orchestrator_enables_ready_health() {
        let svc = PredictionService::new_no_pool();
        let pool = create_test_pool(2).await;
        let orchestrator = Arc::new(MockOrchestrator::new());

        svc.set_orchestrator(pool, orchestrator).await;
        assert!(svc.has_orchestrator().await);

        // Now we can set READY
        svc.set_health(Health::Ready).await;
        let health = svc.health().await;
        assert_eq!(health.state, Health::Ready);
        assert_eq!(health.total_slots, 2);
        assert_eq!(health.available_slots, 2);
    }

    #[tokio::test]
    async fn create_prediction_succeeds_when_ready() {
        let svc = PredictionService::new_no_pool();
        let pool = create_test_pool(1).await;
        let orchestrator = Arc::new(MockOrchestrator::new());

        svc.set_orchestrator(pool, orchestrator).await;
        svc.set_health(Health::Ready).await;

        let unregistered_slot = svc.create_prediction("test-1".to_string(), None).await;
        assert!(unregistered_slot.is_ok());
        let (_idle_rx, slot) = unregistered_slot.unwrap().into_parts();

        assert_eq!(slot.id(), "test-1");
    }

    #[tokio::test]
    async fn create_prediction_returns_at_capacity_when_no_slots() {
        let svc = PredictionService::new_no_pool();
        let pool = create_test_pool(1).await;
        let orchestrator = Arc::new(MockOrchestrator::new());

        svc.set_orchestrator(pool, orchestrator).await;
        svc.set_health(Health::Ready).await;

        // First prediction takes the only slot
        let _slot1 = svc
            .create_prediction("test-1".to_string(), None)
            .await
            .unwrap();

        // Second should fail with AtCapacity
        let result = svc.create_prediction("test-2".to_string(), None).await;
        assert!(matches!(result, Err(CreatePredictionError::AtCapacity)));
    }

    #[tokio::test]
    async fn predict_calls_orchestrator_register() {
        let svc = PredictionService::new_no_pool();
        let pool = create_test_pool(1).await;
        let orchestrator = Arc::new(MockOrchestrator::new());
        let orch_ref = Arc::clone(&orchestrator);

        svc.set_orchestrator(pool, orchestrator).await;
        svc.set_health(Health::Ready).await;

        let unregistered_slot = svc
            .create_prediction("test-1".to_string(), None)
            .await
            .unwrap();
        let input = serde_json::json!({"prompt": "hello"});

        let result = svc.predict(unregistered_slot, input).await;

        // MockOrchestrator completes immediately with success
        assert!(result.is_ok(), "predict failed: {:?}", result.err());
        assert_eq!(orch_ref.register_count(), 1);
    }

    #[tokio::test]
    async fn health_shows_busy_when_all_slots_used() {
        let svc = PredictionService::new_no_pool();
        let pool = create_test_pool(1).await;
        let orchestrator = Arc::new(MockOrchestrator::new());

        svc.set_orchestrator(pool, orchestrator).await;
        svc.set_health(Health::Ready).await;

        // Before acquiring slot
        let health = svc.health().await;
        assert!(!health.is_busy());
        assert_eq!(health.available_slots, 1);

        // After acquiring slot
        let _slot = svc
            .create_prediction("test-1".to_string(), None)
            .await
            .unwrap();
        let health = svc.health().await;
        assert!(health.is_busy());
        assert_eq!(health.available_slots, 0);
    }

    #[tokio::test]
    async fn predict_idle_channel_closed_poison_slot_async() {
        let svc = PredictionService::new_no_pool();
        let (pool, slot_ids) = create_test_pool_with_slots(1).await;
        let orchestrator = Arc::new(MockOrchestrator::new());
        let slot_id = slot_ids[0];

        svc.set_orchestrator(Arc::clone(&pool), orchestrator).await;
        svc.set_health(Health::Ready).await;

        let unregistered_slot = svc
            .create_prediction("test-1".to_string(), None)
            .await
            .unwrap();
        let input = serde_json::json!({"prompt": "hello"});

        let result = svc.predict(unregistered_slot, input).await;
        assert!(result.is_ok(), "predict failed: {:?}", result.err());

        tokio::time::timeout(Duration::from_secs(1), async {
            loop {
                if pool.is_poisoned(slot_id) {
                    break;
                }
                tokio::time::sleep(Duration::from_millis(10)).await;
            }
        })
        .await
        .expect("slot was not poisoned after idle token channel closed");
    }

    #[tokio::test]
    async fn predict_idle_ack_returns_capacity_async() {
        let svc = PredictionService::new_no_pool();
        let pool = create_test_pool(1).await;
        let orchestrator = Arc::new(MockOrchestrator::new().with_idle_ack());

        svc.set_orchestrator(Arc::clone(&pool), orchestrator).await;
        svc.set_health(Health::Ready).await;

        let unregistered_slot = svc
            .create_prediction("test-1".to_string(), None)
            .await
            .unwrap();
        let input = serde_json::json!({"prompt": "hello"});

        let result = svc.predict(unregistered_slot, input).await;
        assert!(result.is_ok(), "predict failed: {:?}", result.err());

        tokio::time::timeout(Duration::from_secs(1), async {
            loop {
                if pool.available() == 1 {
                    break;
                }
                tokio::time::sleep(Duration::from_millis(10)).await;
            }
        })
        .await
        .expect("slot capacity was not returned after idle acknowledgement");
    }

    #[tokio::test]
    async fn predict_send_failure_poison_slot() {
        let svc = PredictionService::new_no_pool();
        let (pool, slot_id) = create_broken_test_pool().await;
        let orchestrator = Arc::new(MockOrchestrator::new());

        svc.set_orchestrator(Arc::clone(&pool), orchestrator).await;
        svc.set_health(Health::Ready).await;

        let unregistered_slot = svc
            .create_prediction("test-1".to_string(), None)
            .await
            .unwrap();
        let input = serde_json::json!({"prompt": "hello"});

        let result = svc.predict(unregistered_slot, input).await;
        assert!(matches!(result, Err(PredictionError::Failed(_))));
        assert!(pool.is_poisoned(slot_id));
        assert!(pool.try_acquire().is_none());
    }
}
