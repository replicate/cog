//! PredictionService: Transport-agnostic prediction lifecycle management.
//!
//! This service owns:
//! - Slot management (PermitPool for concurrency control)
//! - Health tracking (state, setup result)
//! - Shutdown coordination (bidirectional)
//!
//! Prediction lifecycle (state, cancellation, webhooks) is managed by PredictionSupervisor.
//! Transports (HTTP, gRPC, etc.) delegate to this service for prediction handling.

use std::sync::Arc;

use tokio::sync::{RwLock, watch};

use crate::bridge::protocol::SlotRequest;
use crate::health::{Health, SetupResult};
use crate::orchestrator::OrchestratorHandle;
use crate::permit::{PermitPool, PredictionSlot};
use crate::prediction::{Prediction, PredictionStatus};
use crate::predictor::{
    AsyncPredictFn, PredictFn, PredictionError, PredictionOutput, PredictionResult,
};
use crate::supervisor::PredictionSupervisor;
use crate::version::VersionInfo;
use crate::webhook::WebhookSender;

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

    /// BUSY state: ready but no slots available.
    pub fn is_busy(&self) -> bool {
        self.state == Health::Ready && self.available_slots == 0
    }
}

/// Transport-agnostic prediction service.
///
/// Two creation modes:
/// - `new(pool)`: Legacy mode with pre-created pool (for testing)
/// - `new_no_pool()` + `set_orchestrator()`: Late configuration for early HTTP start
pub struct PredictionService {
    predict_fn: Option<Arc<PredictFn>>,
    async_predict_fn: Option<Arc<AsyncPredictFn>>,

    /// Orchestrator state (pool + handle together).
    /// Using a single field makes invalid states (pool without handle) impossible.
    orchestrator: RwLock<Option<OrchestratorState>>,

    /// Legacy pool (non-orchestrator mode, for testing).
    legacy_pool: RwLock<Option<Arc<PermitPool>>>,

    health: RwLock<Health>,
    setup_result: RwLock<Option<SetupResult>>,

    supervisor: Arc<PredictionSupervisor>,

    shutdown_tx: watch::Sender<bool>,
    shutdown_rx: watch::Receiver<bool>,

    version: VersionInfo,

    schema: RwLock<Option<serde_json::Value>>,
}

/// Orchestrator runtime state - pool and handle together.
///
/// Ensures pool and orchestrator are always set atomically.
#[derive(Clone)]
pub struct OrchestratorState {
    pub pool: Arc<PermitPool>,
    pub handle: Arc<OrchestratorHandle>,
}

impl PredictionService {
    /// Create with a permit pool (legacy/test mode).
    pub fn new(pool: Arc<PermitPool>) -> Self {
        let (shutdown_tx, shutdown_rx) = watch::channel(false);
        Self {
            predict_fn: None,
            async_predict_fn: None,
            orchestrator: RwLock::new(None),
            legacy_pool: RwLock::new(Some(pool)),
            health: RwLock::new(Health::Unknown),
            setup_result: RwLock::new(None),
            supervisor: PredictionSupervisor::new(),
            shutdown_tx,
            shutdown_rx,
            version: VersionInfo::new(),
            schema: RwLock::new(None),
        }
    }

    /// Create without configuration (for early HTTP start).
    ///
    /// Serves 503 until `set_orchestrator()` is called.
    pub fn new_no_pool() -> Self {
        let (shutdown_tx, shutdown_rx) = watch::channel(false);
        Self {
            predict_fn: None,
            async_predict_fn: None,
            orchestrator: RwLock::new(None),
            legacy_pool: RwLock::new(None),
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
    pub async fn set_orchestrator(&self, pool: Arc<PermitPool>, handle: Arc<OrchestratorHandle>) {
        *self.orchestrator.write().await = Some(OrchestratorState { pool, handle });
    }

    pub async fn has_orchestrator(&self) -> bool {
        self.orchestrator.read().await.is_some()
    }

    pub fn supervisor(&self) -> &Arc<PredictionSupervisor> {
        &self.supervisor
    }

    pub fn with_predict_fn(mut self, f: Arc<PredictFn>) -> Self {
        self.predict_fn = Some(f);
        self
    }

    pub fn with_async_predict_fn(mut self, f: Arc<AsyncPredictFn>) -> Self {
        self.async_predict_fn = Some(f);
        self
    }

    pub fn with_health(mut self, health: Health) -> Self {
        self.health = RwLock::new(health);
        self
    }

    pub fn with_version(mut self, version: VersionInfo) -> Self {
        self.version = version;
        self
    }

    pub fn is_async(&self) -> bool {
        self.async_predict_fn.is_some()
    }

    /// Get the permit pool (from orchestrator or legacy mode).
    pub async fn pool(&self) -> Option<Arc<PermitPool>> {
        if let Some(ref config) = *self.orchestrator.read().await {
            Some(Arc::clone(&config.pool))
        } else {
            self.legacy_pool.read().await.clone()
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

    pub async fn set_health(&self, health: Health) {
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

    /// Create a new prediction, acquiring a slot permit.
    ///
    /// Caller should check for duplicates via supervisor first.
    pub async fn create_prediction(
        &self,
        id: String,
        webhook: Option<WebhookSender>,
    ) -> Result<PredictionSlot, CreatePredictionError> {
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
        Ok(PredictionSlot::new(prediction, permit))
    }

    pub fn prediction_exists(&self, id: &str) -> bool {
        self.supervisor.exists(id)
    }

    /// Run a prediction to completion.
    ///
    /// Routes through orchestrator (slot socket) or legacy predict functions.
    pub async fn predict(
        &self,
        slot: &mut PredictionSlot,
        input: serde_json::Value,
    ) -> Result<PredictionResult, PredictionError> {
        let config = self.orchestrator.read().await.clone();

        if let Some(config) = config {
            self.predict_via_orchestrator(slot, input, &config.handle)
                .await
        } else {
            self.predict_via_function(slot, input).await
        }
    }

    async fn predict_via_orchestrator(
        &self,
        slot: &mut PredictionSlot,
        input: serde_json::Value,
        orchestrator: &Arc<OrchestratorHandle>,
    ) -> Result<PredictionResult, PredictionError> {
        let prediction_id = slot.id();
        let slot_id = slot.slot_id();

        {
            let prediction = slot.prediction();
            let mut pred = prediction.lock().unwrap();
            pred.set_processing();
        }

        // Register for response routing in event loop
        let prediction_arc = slot.prediction();
        orchestrator
            .register_prediction(slot_id, Arc::clone(&prediction_arc))
            .await;

        let request = SlotRequest::Predict {
            id: prediction_id.clone(),
            input,
        };

        // permit_mut returns None if permit isn't InUse (shouldn't happen here)
        let permit = slot
            .permit_mut()
            .ok_or_else(|| PredictionError::Failed("Permit not in use".to_string()))?;

        if let Err(e) = permit.send(request).await {
            tracing::error!(%slot_id, error = %e, "Failed to send prediction request");
            let mut pred = prediction_arc.lock().unwrap();
            pred.set_failed(format!("Failed to send request: {}", e));
            // Slot is poisoned - don't mark idle
            return Err(PredictionError::Failed(format!(
                "Failed to send request: {}",
                e
            )));
        }

        // Get notifier before waiting so we don't miss completion
        let completion = {
            let pred = prediction_arc.lock().unwrap();
            pred.completion()
        };
        completion.notified().await;

        let (status, output, error, logs, predict_time, slot_poisoned) = {
            let pred = prediction_arc.lock().unwrap();
            (
                pred.status(),
                pred.output().cloned(),
                pred.error().map(|s| s.to_string()),
                pred.logs().to_string(),
                pred.elapsed(),
                pred.is_slot_poisoned(),
            )
        };

        if slot_poisoned {
            slot.into_poisoned();
        } else {
            let _idle_token = slot.into_idle();
        }

        match status {
            PredictionStatus::Succeeded => Ok(PredictionResult {
                output: output.unwrap_or(PredictionOutput::Single(serde_json::Value::Null)),
                predict_time: Some(predict_time),
                logs,
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

    async fn predict_via_function(
        &self,
        slot: &mut PredictionSlot,
        input: serde_json::Value,
    ) -> Result<PredictionResult, PredictionError> {
        {
            let prediction = slot.prediction();
            let mut pred = prediction.lock().unwrap();
            pred.set_processing();
        }

        let result = if let Some(ref async_fn) = self.async_predict_fn {
            let f = Arc::clone(async_fn);
            f(input).await
        } else if let Some(ref sync_fn) = self.predict_fn {
            let f = Arc::clone(sync_fn);
            tokio::task::spawn_blocking(move || f(input))
                .await
                .map_err(|e| PredictionError::Failed(format!("Task panicked: {}", e)))?
        } else {
            return Err(PredictionError::NotReady);
        };

        {
            let prediction = slot.prediction();
            let mut pred = prediction.lock().unwrap();
            match &result {
                Ok(r) => pred.set_succeeded(r.output.clone()),
                Err(PredictionError::Cancelled) => pred.set_canceled(),
                Err(e) => pred.set_failed(e.to_string()),
            }
        }

        // Return permit to pool
        let _idle_token = slot.into_idle();

        result
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
    use crate::bridge::codec::JsonCodec;
    use crate::bridge::protocol::SlotId;
    use crate::prediction::PredictionStatus;
    use crate::predictor::PredictionOutput;
    use serde_json::json;
    use tokio::net::UnixStream;
    use tokio_util::codec::FramedWrite;

    async fn make_test_pool(n: usize) -> Arc<PermitPool> {
        let pool = Arc::new(PermitPool::new(n));
        for _ in 0..n {
            let (a, _b) = UnixStream::pair().unwrap();
            let (_, write) = a.into_split();
            pool.add_permit(SlotId::new(), FramedWrite::new(write, JsonCodec::new()));
        }
        pool
    }

    #[tokio::test]
    async fn service_new_defaults() {
        let pool = make_test_pool(1).await;
        let svc = PredictionService::new(pool);
        let health = svc.health().await;

        assert_eq!(health.state, Health::Unknown);
        assert_eq!(health.total_slots, 1);
        assert!(!svc.is_async());
    }

    #[tokio::test]
    async fn service_with_builders() {
        let pool = make_test_pool(4).await;
        let svc = PredictionService::new(pool)
            .with_health(Health::Ready)
            .with_predict_fn(Arc::new(|_| {
                Ok(PredictionResult {
                    output: PredictionOutput::Single(json!("test")),
                    predict_time: None,
                    logs: String::new(),
                })
            }));

        let health = svc.health().await;
        assert_eq!(health.state, Health::Ready);
        assert_eq!(health.total_slots, 4);
        assert!(!svc.is_async());
    }

    #[tokio::test]
    async fn service_is_async_with_async_fn() {
        let pool = make_test_pool(1).await;
        let svc = PredictionService::new(pool).with_async_predict_fn(Arc::new(|_| {
            Box::pin(async {
                Ok(PredictionResult {
                    output: PredictionOutput::Single(json!("test")),
                    predict_time: None,
                    logs: String::new(),
                })
            })
        }));

        assert!(svc.is_async());
    }

    #[tokio::test]
    async fn create_prediction_fails_when_not_ready() {
        let pool = make_test_pool(1).await;
        let svc = PredictionService::new(pool);

        let result = svc.create_prediction("test".to_string(), None).await;
        assert!(matches!(result, Err(CreatePredictionError::NotReady)));
    }

    #[tokio::test]
    async fn create_prediction_succeeds_when_ready() {
        let pool = make_test_pool(1).await;
        let svc = PredictionService::new(pool).with_health(Health::Ready);

        let result = svc.create_prediction("test".to_string(), None).await;
        assert!(result.is_ok());

        let slot = result.unwrap();
        let prediction = slot.prediction();
        let pred = prediction.lock().unwrap();
        assert_eq!(pred.id(), "test");
        assert_eq!(pred.status(), PredictionStatus::Starting);
    }

    #[tokio::test]
    async fn create_prediction_fails_at_capacity() {
        let pool = make_test_pool(1).await;
        let svc = PredictionService::new(pool).with_health(Health::Ready);

        let _slot1 = svc.create_prediction("p1".to_string(), None).await.unwrap();

        let result = svc.create_prediction("p2".to_string(), None).await;
        assert!(matches!(result, Err(CreatePredictionError::AtCapacity)));
    }

    #[tokio::test]
    async fn supervisor_detects_duplicate_id() {
        let pool = make_test_pool(2).await;
        let svc = PredictionService::new(pool).with_health(Health::Ready);

        let _handle = svc.supervisor().submit("same_id".to_string(), json!({}), None);
        assert!(svc.supervisor().exists("same_id"));

        // Service doesn't block duplicate - caller (routes) should check supervisor first
        let _slot1 = svc
            .create_prediction("same_id".to_string(), None)
            .await
            .unwrap();

        // Real duplicate detection happens at route level via supervisor.get_state()
        let _slot2 = svc
            .create_prediction("same_id".to_string(), None)
            .await
            .unwrap();
    }

    #[tokio::test]
    async fn prediction_exists_works() {
        let pool = make_test_pool(1).await;
        let svc = PredictionService::new(pool).with_health(Health::Ready);

        assert!(!svc.prediction_exists("test"));

        let _handle = svc.supervisor().submit("test".to_string(), json!({}), None);

        let _slot = svc
            .create_prediction("test".to_string(), None)
            .await
            .unwrap();
        assert!(svc.prediction_exists("test"));
    }

    #[tokio::test]
    async fn cancel_prediction_works() {
        let pool = make_test_pool(1).await;
        let svc = PredictionService::new(pool).with_health(Health::Ready);

        let _handle = svc.supervisor().submit("test".to_string(), json!({}), None);

        let _slot = svc
            .create_prediction("test".to_string(), None)
            .await
            .unwrap();

        assert!(svc.cancel("test"));

        let state = svc.supervisor().get_state("test");
        assert!(state.is_some());
        // cancel() triggers the token but doesn't update supervisor status directly;
        // the actual status update happens when the prediction flow detects cancellation

        assert!(!svc.cancel("nonexistent"));
    }

    #[tokio::test]
    async fn predict_with_sync_fn() {
        let pool = make_test_pool(1).await;
        let svc = PredictionService::new(pool)
            .with_health(Health::Ready)
            .with_predict_fn(Arc::new(|input| {
                let name = input["name"].as_str().unwrap_or("world");
                Ok(PredictionResult {
                    output: PredictionOutput::Single(json!(format!("Hello, {}!", name))),
                    predict_time: None,
                    logs: String::new(),
                })
            }));

        let mut slot = svc
            .create_prediction("test".to_string(), None)
            .await
            .unwrap();
        let result = svc.predict(&mut slot, json!({"name": "Rust"})).await;

        assert!(result.is_ok());
        let r = result.unwrap();
        assert_eq!(r.output.final_value(), &json!("Hello, Rust!"));

        let prediction = slot.prediction();
        let pred = prediction.lock().unwrap();
        assert_eq!(pred.status(), PredictionStatus::Succeeded);
    }

    #[tokio::test]
    async fn predict_with_async_fn() {
        let pool = make_test_pool(1).await;
        let svc = PredictionService::new(pool)
            .with_health(Health::Ready)
            .with_async_predict_fn(Arc::new(|input| {
                Box::pin(async move {
                    let x = input["x"].as_i64().unwrap_or(0);
                    Ok(PredictionResult {
                        output: PredictionOutput::Single(json!(x * 2)),
                        predict_time: None,
                        logs: String::new(),
                    })
                })
            }));

        let mut slot = svc
            .create_prediction("test".to_string(), None)
            .await
            .unwrap();
        let result = svc.predict(&mut slot, json!({"x": 21})).await;

        assert!(result.is_ok());
        assert_eq!(result.unwrap().output.final_value(), &json!(42));
    }

    #[tokio::test]
    async fn predict_sets_failed_status_on_error() {
        let pool = make_test_pool(1).await;
        let svc = PredictionService::new(pool)
            .with_health(Health::Ready)
            .with_predict_fn(Arc::new(|_| {
                Err(PredictionError::Failed("oops".to_string()))
            }));

        let mut slot = svc
            .create_prediction("test".to_string(), None)
            .await
            .unwrap();
        let result = svc.predict(&mut slot, json!({})).await;

        assert!(result.is_err());

        let prediction = slot.prediction();
        let pred = prediction.lock().unwrap();
        assert_eq!(pred.status(), PredictionStatus::Failed);
    }

    #[tokio::test]
    async fn shutdown_signal_works() {
        let pool = make_test_pool(1).await;
        let svc = PredictionService::new(pool);
        let mut rx = svc.shutdown_rx();

        assert!(!*rx.borrow());

        svc.trigger_shutdown();
        rx.changed().await.unwrap();

        assert!(*rx.borrow());
    }

    #[tokio::test]
    async fn slot_returns_permit_after_predict() {
        let pool = make_test_pool(1).await;
        let pool_ref = Arc::clone(&pool);
        let svc = PredictionService::new(pool)
            .with_health(Health::Ready)
            .with_predict_fn(Arc::new(|_| {
                Ok(PredictionResult {
                    output: PredictionOutput::Single(json!("done")),
                    predict_time: None,
                    logs: String::new(),
                })
            }));

        {
            let mut slot = svc
                .create_prediction("test".to_string(), None)
                .await
                .unwrap();

            assert!(pool_ref.try_acquire().is_none());

            let _ = svc.predict(&mut slot, json!({})).await;
        }

        assert!(pool_ref.try_acquire().is_some());
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
    async fn service_legacy_pool_works() {
        let pool = make_test_pool(2).await;
        let svc = PredictionService::new(pool);

        let p = svc.pool().await;
        assert!(p.is_some());
        assert_eq!(p.unwrap().num_slots(), 2);

        assert!(!svc.has_orchestrator().await);
    }
}
