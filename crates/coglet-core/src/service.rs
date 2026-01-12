//! PredictionService: Transport-agnostic prediction lifecycle management.
//!
//! This service owns:
//! - Slot management (semaphore for concurrency control)
//! - Health tracking (state, setup result)
//! - Prediction registry (for cancellation)
//! - Shutdown coordination (bidirectional)
//!
//! Transports (HTTP, gRPC, etc.) delegate to this service for prediction handling.

use std::collections::HashMap;
use std::sync::Arc;

use tokio::sync::{watch, Mutex, RwLock, Semaphore};

use crate::health::{Health, SetupResult};
use crate::prediction::Prediction;
use crate::predictor::{
    AsyncPredictFn, CancellationToken, PredictFn, PredictionError, PredictionResult,
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
pub struct PredictionService {
    /// Sync predict function.
    predict_fn: Option<Arc<PredictFn>>,
    /// Async predict function.
    async_predict_fn: Option<Arc<AsyncPredictFn>>,
    
    /// Semaphore for slot management.
    slots: Arc<Semaphore>,
    /// Total number of slots.
    max_slots: usize,
    
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
}

impl PredictionService {
    /// Create a new prediction service.
    pub fn new(max_slots: usize) -> Self {
        let (shutdown_tx, shutdown_rx) = watch::channel(false);
        Self {
            predict_fn: None,
            async_predict_fn: None,
            slots: Arc::new(Semaphore::new(max_slots)),
            max_slots,
            health: RwLock::new(Health::Unknown),
            setup_result: RwLock::new(None),
            predictions: Mutex::new(HashMap::new()),
            shutdown_tx,
            shutdown_rx,
            version: VersionInfo::new(),
        }
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
    
    /// Get the current health snapshot.
    pub async fn health(&self) -> HealthSnapshot {
        let state = *self.health.read().await;
        let setup_result = self.setup_result.read().await.clone();
        let available_slots = self.slots.available_permits();
        
        HealthSnapshot {
            state,
            available_slots,
            total_slots: self.max_slots,
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
    
    /// Create a new prediction, acquiring a slot.
    ///
    /// Returns a `Prediction` that owns the slot permit and will send
    /// terminal webhook on drop.
    pub async fn create_prediction(
        &self,
        id: String,
        webhook: Option<WebhookSender>,
    ) -> Result<Prediction, CreatePredictionError> {
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
        
        // Try to acquire slot
        let permit = self.slots.clone().try_acquire_owned()
            .map_err(|_| CreatePredictionError::AtCapacity)?;
        
        // Create prediction
        let prediction = Prediction::new(id.clone(), permit, webhook);
        
        // Register for cancellation
        {
            let mut predictions = self.predictions.lock().await;
            predictions.insert(id, prediction.cancel_token());
        }
        
        Ok(prediction)
    }
    
    /// Check if a prediction with the given ID exists.
    pub async fn prediction_exists(&self, id: &str) -> bool {
        self.predictions.lock().await.contains_key(id)
    }
    
    /// Run a prediction to completion.
    ///
    /// This runs the predictor function (sync or async) and updates the
    /// prediction's status and output. The prediction's Drop will handle
    /// sending the terminal webhook and releasing the slot.
    pub async fn predict(
        &self,
        prediction: &mut Prediction,
        input: serde_json::Value,
    ) -> Result<PredictionResult, PredictionError> {
        prediction.set_processing();
        
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
        match &result {
            Ok(r) => prediction.set_succeeded(r.output.clone()),
            Err(PredictionError::Cancelled) => prediction.set_canceled(),
            Err(e) => prediction.set_failed(e.to_string()),
        }
        
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
    use serde_json::json;
    
    #[tokio::test]
    async fn service_new_defaults() {
        let svc = PredictionService::new(1);
        let health = svc.health().await;
        
        assert_eq!(health.state, Health::Unknown);
        assert_eq!(health.total_slots, 1);
        assert_eq!(health.available_slots, 1);
        assert!(!svc.is_async());
    }
    
    #[tokio::test]
    async fn service_with_builders() {
        let svc = PredictionService::new(4)
            .with_health(Health::Ready)
            .with_predict_fn(Arc::new(|_| Ok(PredictionResult {
                output: PredictionOutput::Single(json!("test")),
                predict_time: None,
            })));
        
        let health = svc.health().await;
        assert_eq!(health.state, Health::Ready);
        assert_eq!(health.total_slots, 4);
        assert!(!svc.is_async());
    }
    
    #[tokio::test]
    async fn service_is_async_with_async_fn() {
        let svc = PredictionService::new(1)
            .with_async_predict_fn(Arc::new(|_| {
                Box::pin(async { Ok(PredictionResult {
                    output: PredictionOutput::Single(json!("test")),
                    predict_time: None,
                })})
            }));
        
        assert!(svc.is_async());
    }
    
    #[tokio::test]
    async fn create_prediction_fails_when_not_ready() {
        let svc = PredictionService::new(1); // Health is Unknown
        
        let result = svc.create_prediction("test".to_string(), None).await;
        assert!(matches!(result, Err(CreatePredictionError::NotReady)));
    }
    
    #[tokio::test]
    async fn create_prediction_succeeds_when_ready() {
        let svc = PredictionService::new(1).with_health(Health::Ready);
        
        let result = svc.create_prediction("test".to_string(), None).await;
        assert!(result.is_ok());
        
        let pred = result.unwrap();
        assert_eq!(pred.id(), "test");
        assert_eq!(pred.status(), PredictionStatus::Starting);
    }
    
    #[tokio::test]
    async fn create_prediction_fails_at_capacity() {
        let svc = PredictionService::new(1).with_health(Health::Ready);
        
        // First succeeds
        let _pred1 = svc.create_prediction("p1".to_string(), None).await.unwrap();
        
        // Second fails - at capacity
        let result = svc.create_prediction("p2".to_string(), None).await;
        assert!(matches!(result, Err(CreatePredictionError::AtCapacity)));
    }
    
    #[tokio::test]
    async fn create_prediction_fails_duplicate_id() {
        let svc = PredictionService::new(2).with_health(Health::Ready);
        
        let _pred1 = svc.create_prediction("same_id".to_string(), None).await.unwrap();
        
        let result = svc.create_prediction("same_id".to_string(), None).await;
        assert!(matches!(result, Err(CreatePredictionError::AlreadyExists(_))));
    }
    
    #[tokio::test]
    async fn prediction_exists_works() {
        let svc = PredictionService::new(1).with_health(Health::Ready);
        
        assert!(!svc.prediction_exists("test").await);
        
        let _pred = svc.create_prediction("test".to_string(), None).await.unwrap();
        assert!(svc.prediction_exists("test").await);
    }
    
    #[tokio::test]
    async fn cancel_prediction_works() {
        let svc = PredictionService::new(1).with_health(Health::Ready);
        
        let pred = svc.create_prediction("test".to_string(), None).await.unwrap();
        assert!(!pred.is_canceled());
        
        assert!(svc.cancel("test").await);
        assert!(pred.is_canceled());
        
        // Cancel non-existent returns false
        assert!(!svc.cancel("nonexistent").await);
    }
    
    #[tokio::test]
    async fn predict_with_sync_fn() {
        let svc = PredictionService::new(1)
            .with_health(Health::Ready)
            .with_predict_fn(Arc::new(|input| {
                let name = input["name"].as_str().unwrap_or("world");
                Ok(PredictionResult {
                    output: PredictionOutput::Single(json!(format!("Hello, {}!", name))),
                    predict_time: None,
                })
            }));
        
        let mut pred = svc.create_prediction("test".to_string(), None).await.unwrap();
        let result = svc.predict(&mut pred, json!({"name": "Rust"})).await;
        
        assert!(result.is_ok());
        let r = result.unwrap();
        assert_eq!(r.output.final_value(), &json!("Hello, Rust!"));
        assert_eq!(pred.status(), PredictionStatus::Succeeded);
    }
    
    #[tokio::test]
    async fn predict_with_async_fn() {
        let svc = PredictionService::new(1)
            .with_health(Health::Ready)
            .with_async_predict_fn(Arc::new(|input| {
                Box::pin(async move {
                    let x = input["x"].as_i64().unwrap_or(0);
                    Ok(PredictionResult {
                        output: PredictionOutput::Single(json!(x * 2)),
                        predict_time: None,
                    })
                })
            }));
        
        let mut pred = svc.create_prediction("test".to_string(), None).await.unwrap();
        let result = svc.predict(&mut pred, json!({"x": 21})).await;
        
        assert!(result.is_ok());
        assert_eq!(result.unwrap().output.final_value(), &json!(42));
    }
    
    #[tokio::test]
    async fn predict_sets_failed_status_on_error() {
        let svc = PredictionService::new(1)
            .with_health(Health::Ready)
            .with_predict_fn(Arc::new(|_| {
                Err(PredictionError::Failed("oops".to_string()))
            }));
        
        let mut pred = svc.create_prediction("test".to_string(), None).await.unwrap();
        let result = svc.predict(&mut pred, json!({})).await;
        
        assert!(result.is_err());
        assert_eq!(pred.status(), PredictionStatus::Failed);
    }
    
    #[tokio::test]
    async fn health_snapshot_is_busy() {
        let svc = PredictionService::new(1).with_health(Health::Ready);
        
        let health = svc.health().await;
        assert!(!health.is_busy());
        
        // Take the slot
        let _pred = svc.create_prediction("test".to_string(), None).await.unwrap();
        
        let health = svc.health().await;
        assert!(health.is_busy());
    }
    
    #[tokio::test]
    async fn shutdown_signal_works() {
        let svc = PredictionService::new(1);
        let mut rx = svc.shutdown_rx();
        
        assert!(!*rx.borrow());
        
        svc.trigger_shutdown();
        rx.changed().await.unwrap();
        
        assert!(*rx.borrow());
    }
}
