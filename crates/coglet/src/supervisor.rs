//! Prediction supervisor - manages prediction lifecycle.
//!
//! Separates lifecycle management from HTTP handlers, enabling:
//! - Full PredictionResponse for 202/PUT (supervisor has state)
//! - Webhook sends outside Drop (no blocking in destructor)
//! - Lock-free concurrent access via DashMap

use std::collections::HashMap;
use std::sync::Arc;
use std::time::Instant;

use dashmap::DashMap;
use tokio::sync::Notify;

use crate::orchestrator::Orchestrator;
use crate::prediction::{CancellationToken, PredictionStatus};
use crate::webhook::{WebhookEventType, WebhookSender};

/// Prediction state snapshot for API responses.
#[derive(Debug, Clone)]
pub struct PredictionState {
    pub id: String,
    pub status: PredictionStatus,
    pub input: serde_json::Value,
    pub output: Option<serde_json::Value>,
    pub logs: String,
    pub error: Option<String>,
    pub started_at: Instant,
    pub completed_at: Option<Instant>,
    /// User-emitted metrics. Accumulated during prediction, included in all webhook payloads.
    pub metrics: HashMap<String, serde_json::Value>,
}

impl PredictionState {
    pub fn to_response(&self) -> serde_json::Value {
        let mut response = serde_json::json!({
            "id": self.id,
            "status": self.status.as_str(),
            "input": self.input,
            "logs": self.logs,
        });

        if let Some(ref output) = self.output {
            response["output"] = output.clone();
        }

        if let Some(ref error) = self.error {
            response["error"] = serde_json::Value::String(error.clone());
        }

        // Build metrics: user metrics + predict_time (predict_time only on completion).
        // Intermediate webhooks include whatever user metrics have accumulated so far.
        if !self.metrics.is_empty() || self.completed_at.is_some() {
            let mut metrics_obj = serde_json::Map::new();

            // User metrics first
            for (k, v) in &self.metrics {
                metrics_obj.insert(k.clone(), v.clone());
            }

            // predict_time only on terminal (0.16.11: only added in succeeded())
            if let Some(completed_at) = self.completed_at {
                let predict_time = completed_at.duration_since(self.started_at).as_secs_f64();
                metrics_obj.insert("predict_time".to_string(), serde_json::json!(predict_time));
            }

            response["metrics"] = serde_json::Value::Object(metrics_obj);
        }

        response
    }
}

/// Handle to a submitted prediction for waiting, state queries, and cancellation.
pub struct PredictionHandle {
    id: String,
    completion: Arc<Notify>,
    cancel_token: CancellationToken,
    supervisor: Arc<PredictionSupervisor>,
}

impl PredictionHandle {
    pub fn id(&self) -> &str {
        &self.id
    }

    pub async fn wait(&self) {
        self.completion.notified().await;
    }

    pub fn state(&self) -> Option<PredictionState> {
        self.supervisor.get_state(&self.id)
    }

    pub fn cancel(&self) {
        self.cancel_token.cancel();
    }

    pub fn is_complete(&self) -> bool {
        self.supervisor
            .get_state(&self.id)
            .map(|s| s.status.is_terminal())
            .unwrap_or(true)
    }

    /// Create a guard that cancels on drop (for sync predictions).
    ///
    /// On drop (e.g. HTTP connection closed), the guard calls
    /// `supervisor.cancel(id)` which fires the CancellationToken AND
    /// delegates to the orchestrator to cancel the worker subprocess.
    pub fn sync_guard(&self) -> SyncPredictionGuard {
        SyncPredictionGuard::new(self.id.clone(), Arc::clone(&self.supervisor))
    }

    pub fn cancel_token(&self) -> CancellationToken {
        self.cancel_token.clone()
    }
}

/// Guard for sync predictions - cancels on drop unless disarmed.
///
/// When the HTTP connection drops (client disconnect), axum drops the
/// response future which drops this guard. The guard calls
/// `supervisor.cancel(id)` to trigger both the CancellationToken
/// (Rust-side observers) and the orchestrator (worker subprocess cancel).
pub struct SyncPredictionGuard {
    prediction_id: Option<String>,
    supervisor: Arc<PredictionSupervisor>,
}

impl SyncPredictionGuard {
    pub fn new(prediction_id: String, supervisor: Arc<PredictionSupervisor>) -> Self {
        Self {
            prediction_id: Some(prediction_id),
            supervisor,
        }
    }

    pub fn disarm(&mut self) {
        self.prediction_id = None;
    }
}

impl Drop for SyncPredictionGuard {
    fn drop(&mut self) {
        if let Some(ref id) = self.prediction_id {
            self.supervisor.cancel(id);
        }
    }
}

struct PredictionEntry {
    state: PredictionState,
    cancel_token: CancellationToken,
    completion: Arc<Notify>,
    webhook: Option<WebhookSender>,
}

/// Prediction supervisor with lock-free concurrent access.
pub struct PredictionSupervisor {
    predictions: DashMap<String, PredictionEntry>,
    orchestrator: tokio::sync::RwLock<Option<Arc<dyn Orchestrator>>>,
}

impl PredictionSupervisor {
    pub fn new() -> Arc<Self> {
        Arc::new(Self {
            predictions: DashMap::new(),
            orchestrator: tokio::sync::RwLock::new(None),
        })
    }

    /// Set the orchestrator handle for cancel delegation.
    pub async fn set_orchestrator(&self, orchestrator: Arc<dyn Orchestrator>) {
        *self.orchestrator.write().await = Some(orchestrator);
    }

    pub fn submit(
        self: &Arc<Self>,
        id: String,
        input: serde_json::Value,
        webhook: Option<WebhookSender>,
    ) -> PredictionHandle {
        let cancel_token = CancellationToken::new();
        let completion = Arc::new(Notify::new());

        let entry = PredictionEntry {
            state: PredictionState {
                id: id.clone(),
                status: PredictionStatus::Starting,
                input,
                output: None,
                logs: String::new(),
                error: None,
                started_at: Instant::now(),
                completed_at: None,
                metrics: HashMap::new(),
            },
            cancel_token: cancel_token.clone(),
            completion: Arc::clone(&completion),
            webhook,
        };

        self.predictions.insert(id.clone(), entry);

        PredictionHandle {
            id,
            completion,
            cancel_token,
            supervisor: Arc::clone(self),
        }
    }

    pub fn update_status(
        self: &Arc<Self>,
        id: &str,
        status: PredictionStatus,
        output: Option<serde_json::Value>,
        error: Option<String>,
        metrics: Option<HashMap<String, serde_json::Value>>,
    ) {
        if let Some(mut entry) = self.predictions.get_mut(id) {
            entry.state.status = status;
            if let Some(o) = output {
                entry.state.output = Some(o);
            }
            if let Some(e) = error {
                entry.state.error = Some(e);
            }
            if let Some(m) = metrics {
                entry.state.metrics = m;
            }

            if status.is_terminal() {
                entry.state.completed_at = Some(Instant::now());
                entry.completion.notify_waiters();
            }
        }

        if status.is_terminal()
            && let Some((_, mut entry)) = self.predictions.remove(id)
            && let Some(webhook) = entry.webhook.take()
        {
            let response = entry.state.to_response();
            tokio::spawn(async move {
                webhook
                    .send_terminal(WebhookEventType::Completed, &response)
                    .await;
            });
        }
    }

    pub fn append_logs(&self, id: &str, logs: &str) {
        if let Some(mut entry) = self.predictions.get_mut(id) {
            entry.state.logs.push_str(logs);
        }
    }

    /// Update user metrics on a prediction. Called as metrics arrive during prediction.
    pub fn update_metrics(&self, id: &str, metrics: HashMap<String, serde_json::Value>) {
        if let Some(mut entry) = self.predictions.get_mut(id) {
            entry.state.metrics = metrics;
        }
    }

    /// Cancel a prediction by ID.
    ///
    /// Fires the CancellationToken (for Rust-side observers like upload tasks)
    /// and delegates to the orchestrator to send `ControlRequest::Cancel` to the worker.
    pub fn cancel(&self, id: &str) -> bool {
        if let Some(entry) = self.predictions.get(id) {
            entry.cancel_token.cancel();

            // Delegate to orchestrator to actually cancel the worker-side prediction.
            // This must be non-blocking since cancel() is sync, so we spawn a task.
            let id_owned = id.to_string();
            let orchestrator = self
                .orchestrator
                .try_read()
                .ok()
                .and_then(|guard| guard.clone());
            if let Some(orch) = orchestrator {
                tokio::spawn(async move {
                    if let Err(e) = orch.cancel_by_prediction_id(&id_owned).await {
                        tracing::error!(
                            prediction_id = %id_owned,
                            error = %e,
                            "Failed to send cancel to orchestrator"
                        );
                    }
                });
            }
            true
        } else {
            false
        }
    }

    pub fn get_state(&self, id: &str) -> Option<PredictionState> {
        self.predictions.get(id).map(|e| e.state.clone())
    }

    pub fn exists(&self, id: &str) -> bool {
        self.predictions.contains_key(id)
    }

    pub fn remove(&self, id: &str) {
        self.predictions.remove(id);
    }
}

impl Default for PredictionSupervisor {
    fn default() -> Self {
        Self {
            predictions: DashMap::new(),
            orchestrator: tokio::sync::RwLock::new(None),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[tokio::test]
    async fn submit_and_complete() {
        let supervisor = PredictionSupervisor::new();

        let handle = supervisor.submit("test-1".to_string(), serde_json::json!({"x": 1}), None);

        assert_eq!(handle.id(), "test-1");

        let state = handle.state().unwrap();
        assert_eq!(state.status, PredictionStatus::Starting);
        assert_eq!(state.input, serde_json::json!({"x": 1}));

        supervisor.update_status("test-1", PredictionStatus::Processing, None, None, None);

        let state = handle.state().unwrap();
        assert_eq!(state.status, PredictionStatus::Processing);

        supervisor.update_status(
            "test-1",
            PredictionStatus::Succeeded,
            Some(serde_json::json!("result")),
            None,
            None,
        );

        assert!(handle.state().is_none());
        assert!(!supervisor.exists("test-1"));
    }

    #[tokio::test]
    async fn cancel_prediction() {
        let supervisor = PredictionSupervisor::new();

        let handle = supervisor.submit("test-cancel".to_string(), serde_json::json!({}), None);

        let cancelled = supervisor.cancel("test-cancel");
        assert!(cancelled);

        assert!(handle.cancel_token().is_cancelled());
    }

    #[tokio::test]
    async fn exists_check() {
        let supervisor = PredictionSupervisor::new();

        assert!(!supervisor.exists("nonexistent"));

        supervisor.submit("exists-test".to_string(), serde_json::json!({}), None);

        assert!(supervisor.exists("exists-test"));
    }

    #[tokio::test]
    async fn sync_guard_cancels_on_drop() {
        let supervisor = PredictionSupervisor::new();

        let handle = supervisor.submit("test-sync-guard".to_string(), serde_json::json!({}), None);

        let cancel_token = handle.cancel_token();

        {
            let _guard = handle.sync_guard();
            assert!(!cancel_token.is_cancelled());
        }

        assert!(cancel_token.is_cancelled());
    }

    #[tokio::test]
    async fn sync_guard_disarm_prevents_cancel() {
        let supervisor = PredictionSupervisor::new();

        let handle = supervisor.submit("test-disarm".to_string(), serde_json::json!({}), None);

        let cancel_token = handle.cancel_token();

        {
            let mut guard = handle.sync_guard();
            guard.disarm();
        }

        assert!(!cancel_token.is_cancelled());
    }
}
