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
use crate::orchestrator::Orchestrator;
use crate::permit::{PermitPool, PredictionSlot};
use crate::prediction::{Prediction, PredictionStatus};
use crate::predictor::{PredictionError, PredictionOutput, PredictionResult};
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
    pub async fn set_orchestrator(&self, pool: Arc<PermitPool>, orchestrator: Arc<dyn Orchestrator>) {
        *self.orchestrator.write().await = Some(OrchestratorState { pool, orchestrator });
    }

    pub async fn has_orchestrator(&self) -> bool {
        self.orchestrator.read().await.is_some()
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

    /// Run a prediction to completion via orchestrator.
    pub async fn predict(
        &self,
        slot: &mut PredictionSlot,
        input: serde_json::Value,
    ) -> Result<PredictionResult, PredictionError> {
        let state = self.orchestrator.read().await.clone();
        let state = state.ok_or_else(|| {
            PredictionError::Failed("No orchestrator configured".to_string())
        })?;

        let prediction_id = slot.id();
        let slot_id = slot.slot_id();

        {
            let prediction = slot.prediction();
            let mut pred = prediction.lock().unwrap();
            pred.set_processing();
        }

        // Register for response routing in event loop
        let prediction_arc = slot.prediction();
        state
            .orchestrator
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
}
