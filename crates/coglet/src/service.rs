//! PredictionService: Transport-agnostic prediction lifecycle management.
//!
//! This service owns:
//! - Slot management (PermitPool for concurrency control)
//! - Health tracking (state, setup result)
//! - Prediction registry (for cancellation)
//! - Shutdown coordination (bidirectional)
//!
//! Transports (HTTP, gRPC, etc.) delegate to this service for prediction handling.

use std::collections::HashMap;
use std::sync::Arc;

use tokio::sync::{Mutex, RwLock, watch};

use crate::bridge::protocol::SlotRequest;
use crate::health::{Health, SetupResult};
use crate::orchestrator::OrchestratorHandle;
use crate::permit::{PermitPool, PredictionSlot};
use crate::prediction::{Prediction, PredictionStatus};
use crate::predictor::{
    AsyncPredictFn, CancellationToken, PredictFn, PredictionError, PredictionOutput,
    PredictionResult,
};
use crate::version::VersionInfo;
use crate::webhook::WebhookSender;

/// Error when creating a prediction.
#[derive(Debug, thiserror::Error)]
pub enum CreatePredictionError {
    #[error("Service not ready")]
    NotReady,
    #[error("At capacity (no slots available)")]
    AtCapacity,
    #[error("Prediction with ID {0} already exists")]
    AlreadyExists(String),
}

/// Snapshot of service health for transports to query.
#[derive(Debug, Clone)]
pub struct HealthSnapshot {
    /// Current health state.
    pub state: Health,
    /// Available prediction slots.
    pub available_slots: usize,
    /// Total prediction slots.
    pub total_slots: usize,
    /// Setup result if available.
    pub setup_result: Option<SetupResult>,
    /// Version information.
    pub version: VersionInfo,
}

impl HealthSnapshot {
    /// Check if the service is ready to accept predictions.
    pub fn is_ready(&self) -> bool {
        self.state == Health::Ready
    }

    /// Check if the service is at capacity (BUSY).
    pub fn is_busy(&self) -> bool {
        self.state == Health::Ready && self.available_slots == 0
    }
}

/// Transport-agnostic prediction service.
///
/// Manages the prediction lifecycle independently of the transport layer.
/// Transports create predictions via `create_prediction()`, run them via
/// `predict()`, and the service handles slot management, health, and cleanup.
///
/// The service can be created in two modes:
/// - `new(pool)`: With a pre-created permit pool (for testing, legacy mode)
/// - `new_no_pool()`: Without a pool, to be set later via `set_pool()` (for orchestrator mode)
pub struct PredictionService {
    /// Sync predict function.
    predict_fn: Option<Arc<PredictFn>>,
    /// Async predict function.
    async_predict_fn: Option<Arc<AsyncPredictFn>>,

    /// Permit pool for slot management (None until worker ready in orchestrator mode).
    pool: RwLock<Option<Arc<PermitPool>>>,

    /// Orchestrator handle (for routing predictions to worker).
    orchestrator: RwLock<Option<Arc<OrchestratorHandle>>>,

    /// Current health state.
    health: RwLock<Health>,
    /// Setup result.
    setup_result: RwLock<Option<SetupResult>>,

    /// In-flight predictions (ID -> CancellationToken).
    predictions: Mutex<HashMap<String, CancellationToken>>,

    /// Shutdown signal sender.
    shutdown_tx: watch::Sender<bool>,
    /// Shutdown signal receiver.
    shutdown_rx: watch::Receiver<bool>,

    /// Version information.
    version: VersionInfo,

    /// OpenAPI schema (cached, generated once at setup).
    schema: RwLock<Option<serde_json::Value>>,
}

impl PredictionService {
    /// Create a new prediction service with a permit pool.
    ///
    /// Use this for testing or legacy mode where the pool is pre-created.
    pub fn new(pool: Arc<PermitPool>) -> Self {
        let (shutdown_tx, shutdown_rx) = watch::channel(false);
        Self {
            predict_fn: None,
            async_predict_fn: None,
            pool: RwLock::new(Some(pool)),
            orchestrator: RwLock::new(None),
            health: RwLock::new(Health::Unknown),
            setup_result: RwLock::new(None),
            predictions: Mutex::new(HashMap::new()),
            shutdown_tx,
            shutdown_rx,
            version: VersionInfo::new(),
            schema: RwLock::new(None),
        }
    }

    /// Create a new prediction service without a permit pool.
    ///
    /// Use this for orchestrator mode where the pool is set after worker ready.
    /// The service starts in Unknown health and won't accept predictions until
    /// `set_pool()` and `set_orchestrator()` are called.
    pub fn new_no_pool() -> Self {
        let (shutdown_tx, shutdown_rx) = watch::channel(false);
        Self {
            predict_fn: None,
            async_predict_fn: None,
            pool: RwLock::new(None),
            orchestrator: RwLock::new(None),
            health: RwLock::new(Health::Unknown),
            setup_result: RwLock::new(None),
            predictions: Mutex::new(HashMap::new()),
            shutdown_tx,
            shutdown_rx,
            version: VersionInfo::new(),
            schema: RwLock::new(None),
        }
    }

    /// Set the permit pool (orchestrator mode).
    ///
    /// Called when worker is ready and pool is populated with slot sockets.
    /// **CRITICAL ORDER**: Call this BEFORE `set_orchestrator()` to avoid race conditions.
    pub async fn set_pool(&self, pool: Arc<PermitPool>) {
        *self.pool.write().await = Some(pool);
    }

    /// Set the orchestrator handle (orchestrator mode).
    ///
    /// Called when worker is ready. Predictions will be routed via orchestrator.
    /// **CRITICAL ORDER**: Call `set_pool()` BEFORE this method.
    pub async fn set_orchestrator(&self, handle: Arc<OrchestratorHandle>) {
        *self.orchestrator.write().await = Some(handle);
    }

    /// Check if this service has an orchestrator.
    pub async fn has_orchestrator(&self) -> bool {
        self.orchestrator.read().await.is_some()
    }

    /// Set the sync predict function.
    pub fn with_predict_fn(mut self, f: Arc<PredictFn>) -> Self {
        self.predict_fn = Some(f);
        self
    }

    /// Set the async predict function.
    pub fn with_async_predict_fn(mut self, f: Arc<AsyncPredictFn>) -> Self {
        self.async_predict_fn = Some(f);
        self
    }

    /// Set the initial health state.
    pub fn with_health(mut self, health: Health) -> Self {
        self.health = RwLock::new(health);
        self
    }

    /// Set version information.
    pub fn with_version(mut self, version: VersionInfo) -> Self {
        self.version = version;
        self
    }

    /// Check if this service uses an async predictor.
    pub fn is_async(&self) -> bool {
        self.async_predict_fn.is_some()
    }

    /// Get the permit pool (if available).
    ///
    /// Returns None if service was created with `new_no_pool()` and pool hasn't been set yet.
    pub async fn pool(&self) -> Option<Arc<PermitPool>> {
        self.pool.read().await.clone()
    }

    /// Get the current health snapshot.
    pub async fn health(&self) -> HealthSnapshot {
        let state = *self.health.read().await;
        let setup_result = self.setup_result.read().await.clone();
        let pool = self.pool.read().await;
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

    /// Set the health state.
    pub async fn set_health(&self, health: Health) {
        *self.health.write().await = health;
    }

    /// Set the setup result.
    pub async fn set_setup_result(&self, result: SetupResult) {
        *self.setup_result.write().await = Some(result);
    }

    /// Set the OpenAPI schema.
    pub async fn set_schema(&self, schema: serde_json::Value) {
        *self.schema.write().await = Some(schema);
    }

    /// Get the OpenAPI schema.
    pub async fn schema(&self) -> Option<serde_json::Value> {
        self.schema.read().await.clone()
    }

    /// Create a new prediction, acquiring a slot permit.
    ///
    /// Returns a `PredictionSlot` that owns both the prediction and the permit.
    /// On drop, the slot sends the terminal webhook and returns the permit to the pool.
    pub async fn create_prediction(
        &self,
        id: String,
        webhook: Option<WebhookSender>,
    ) -> Result<PredictionSlot, CreatePredictionError> {
        // Check health
        let health = *self.health.read().await;
        if health != Health::Ready {
            return Err(CreatePredictionError::NotReady);
        }

        // Check if ID already exists
        {
            let predictions = self.predictions.lock().await;
            if predictions.contains_key(&id) {
                return Err(CreatePredictionError::AlreadyExists(id));
            }
        }

        // Get pool (must exist if health is Ready)
        let pool = self.pool.read().await;
        let pool = pool.as_ref().ok_or(CreatePredictionError::NotReady)?;

        // Try to acquire permit
        let permit = pool
            .try_acquire()
            .ok_or(CreatePredictionError::AtCapacity)?;

        // Create prediction
        let prediction = Prediction::new(id.clone(), webhook);

        // Register for cancellation
        {
            let mut predictions = self.predictions.lock().await;
            predictions.insert(id, prediction.cancel_token());
        }

        Ok(PredictionSlot::new(prediction, permit))
    }

    /// Check if a prediction with the given ID exists.
    pub async fn prediction_exists(&self, id: &str) -> bool {
        self.predictions.lock().await.contains_key(id)
    }

    /// Run a prediction to completion.
    ///
    /// Routes the prediction through either:
    /// - Orchestrator mode: Sends request via slot socket, waits for event loop to complete
    /// - Legacy mode: Calls predict_fn/async_predict_fn directly
    ///
    /// The slot's Drop will handle sending the terminal webhook and releasing the permit.
    pub async fn predict(
        &self,
        slot: &mut PredictionSlot,
        input: serde_json::Value,
    ) -> Result<PredictionResult, PredictionError> {
        // Check if we're in orchestrator mode
        let orchestrator = self.orchestrator.read().await.clone();

        if let Some(orch) = orchestrator {
            // Orchestrator mode: route through slot socket
            self.predict_via_orchestrator(slot, input, &orch).await
        } else {
            // Legacy mode: use predict functions directly
            self.predict_via_function(slot, input).await
        }
    }

    /// Run prediction via orchestrator (slot socket + event loop).
    async fn predict_via_orchestrator(
        &self,
        slot: &mut PredictionSlot,
        input: serde_json::Value,
        orchestrator: &Arc<OrchestratorHandle>,
    ) -> Result<PredictionResult, PredictionError> {
        let prediction_id = slot.id();
        let slot_id = slot.permit().slot_id();

        // Set processing status
        {
            let prediction = slot.prediction();
            let mut pred = prediction.lock().await;
            pred.set_processing();
        }

        // Register prediction with orchestrator's event loop (for response routing)
        let prediction_arc = slot.prediction();
        orchestrator
            .register_prediction(slot_id, Arc::clone(&prediction_arc))
            .await;

        // Send prediction request via slot socket
        let request = SlotRequest::Predict {
            id: prediction_id.clone(),
            input,
        };

        if let Err(e) = slot.permit_mut().send(request).await {
            tracing::error!(%slot_id, error = %e, "Failed to send prediction request");
            let mut pred = prediction_arc.lock().await;
            pred.set_failed(format!("Failed to send request: {}", e));
            // Don't mark idle - slot is poisoned
            return Err(PredictionError::Failed(format!(
                "Failed to send request: {}",
                e
            )));
        }

        // Wait for prediction to complete (event loop will update prediction status)
        // Get completion notifier before waiting
        let completion = {
            let pred = prediction_arc.lock().await;
            pred.completion()
        };
        completion.notified().await;

        // Extract result from prediction
        let (status, output, error, logs, predict_time) = {
            let pred = prediction_arc.lock().await;
            (
                pred.status(),
                pred.output().cloned(),
                pred.error().map(|s| s.to_string()),
                pred.logs().to_string(),
                pred.elapsed(),
            )
        };

        // Mark slot as idle so permit returns to pool on drop
        slot.mark_idle();

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

    /// Run prediction via legacy predict functions.
    async fn predict_via_function(
        &self,
        slot: &mut PredictionSlot,
        input: serde_json::Value,
    ) -> Result<PredictionResult, PredictionError> {
        // Set processing status
        {
            let prediction = slot.prediction();
            let mut pred = prediction.lock().await;
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

        // Update prediction status based on result
        {
            let prediction = slot.prediction();
            let mut pred = prediction.lock().await;
            match &result {
                Ok(r) => pred.set_succeeded(r.output.clone()),
                Err(PredictionError::Cancelled) => pred.set_canceled(),
                Err(e) => pred.set_failed(e.to_string()),
            }
        }

        // Mark slot as idle so permit returns to pool on drop
        slot.mark_idle();

        result
    }

    /// Cancel a prediction by ID.
    ///
    /// Returns true if the prediction was found and cancelled.
    pub async fn cancel(&self, id: &str) -> bool {
        let predictions = self.predictions.lock().await;
        if let Some(token) = predictions.get(id) {
            token.cancel();
            true
        } else {
            false
        }
    }

    /// Unregister a prediction (called when prediction completes).
    ///
    /// This is typically called by the transport after the prediction
    /// result has been sent to the client.
    pub async fn unregister_prediction(&self, id: &str) {
        self.predictions.lock().await.remove(id);
    }

    /// Trigger shutdown.
    pub fn trigger_shutdown(&self) {
        let _ = self.shutdown_tx.send(true);
    }

    /// Get the shutdown signal receiver.
    ///
    /// Transports can select on this to know when to shut down.
    pub fn shutdown_rx(&self) -> watch::Receiver<bool> {
        self.shutdown_rx.clone()
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::prediction::PredictionStatus;
    use crate::predictor::PredictionOutput;
    use crate::bridge::codec::JsonCodec;
    use crate::bridge::protocol::SlotId;
    use serde_json::json;
    use tokio::net::UnixStream;
    use tokio_util::codec::FramedWrite;

    /// Create a test pool with N slots backed by socket pairs.
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
        let svc = PredictionService::new(pool); // Health is Unknown

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
        let pred = prediction.lock().await;
        assert_eq!(pred.id(), "test");
        assert_eq!(pred.status(), PredictionStatus::Starting);
    }

    #[tokio::test]
    async fn create_prediction_fails_at_capacity() {
        let pool = make_test_pool(1).await;
        let svc = PredictionService::new(pool).with_health(Health::Ready);

        // First succeeds
        let _slot1 = svc.create_prediction("p1".to_string(), None).await.unwrap();

        // Second fails - at capacity
        let result = svc.create_prediction("p2".to_string(), None).await;
        assert!(matches!(result, Err(CreatePredictionError::AtCapacity)));
    }

    #[tokio::test]
    async fn create_prediction_fails_duplicate_id() {
        let pool = make_test_pool(2).await;
        let svc = PredictionService::new(pool).with_health(Health::Ready);

        let _slot1 = svc
            .create_prediction("same_id".to_string(), None)
            .await
            .unwrap();

        let result = svc.create_prediction("same_id".to_string(), None).await;
        assert!(matches!(
            result,
            Err(CreatePredictionError::AlreadyExists(_))
        ));
    }

    #[tokio::test]
    async fn prediction_exists_works() {
        let pool = make_test_pool(1).await;
        let svc = PredictionService::new(pool).with_health(Health::Ready);

        assert!(!svc.prediction_exists("test").await);

        let _slot = svc
            .create_prediction("test".to_string(), None)
            .await
            .unwrap();
        assert!(svc.prediction_exists("test").await);
    }

    #[tokio::test]
    async fn cancel_prediction_works() {
        let pool = make_test_pool(1).await;
        let svc = PredictionService::new(pool).with_health(Health::Ready);

        let slot = svc
            .create_prediction("test".to_string(), None)
            .await
            .unwrap();

        {
            let prediction = slot.prediction();
            let pred = prediction.lock().await;
            assert!(!pred.is_canceled());
        }

        assert!(svc.cancel("test").await);

        {
            let prediction = slot.prediction();
            let pred = prediction.lock().await;
            assert!(pred.is_canceled());
        }

        // Cancel non-existent returns false
        assert!(!svc.cancel("nonexistent").await);
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
        let pred = prediction.lock().await;
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
        let pred = prediction.lock().await;
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

            // Permit is held
            assert!(pool_ref.try_acquire().is_none());

            // Run prediction (marks slot idle)
            let _ = svc.predict(&mut slot, json!({})).await;

            // Slot drops here, permit returns to pool
        }

        // Permit should be back
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
    async fn service_set_pool_works() {
        let svc = PredictionService::new_no_pool();

        // Initially no pool
        assert!(svc.pool().await.is_none());

        // Set pool
        let pool = make_test_pool(2).await;
        svc.set_pool(pool).await;

        // Now has pool
        let p = svc.pool().await;
        assert!(p.is_some());
        assert_eq!(p.unwrap().num_slots(), 2);
    }
}
