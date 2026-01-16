//! Prediction supervisor - manages prediction lifecycle.
//!
//! The supervisor owns in-flight predictions and handles:
//! - Prediction state tracking (for full response queries)
//! - Webhook terminal sends (moved out of Drop)
//! - Cancellation propagation
//!
//! This separates lifecycle management from the HTTP handler, enabling:
//! - Full PredictionResponse for 202/PUT (supervisor has state)
//! - Webhook sends outside Drop (no blocking in destructor)
//! - Clean async patterns
//!
//! Uses DashMap for lock-free concurrent access, eliminating deadlock risks.

use std::sync::Arc;
use std::time::Instant;

use dashmap::DashMap;
use tokio::sync::Notify;

use crate::predictor::CancellationToken;
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
}

impl PredictionState {
    /// Build a full PredictionResponse JSON.
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

        // Add metrics if completed
        if let Some(completed_at) = self.completed_at {
            let predict_time = completed_at.duration_since(self.started_at).as_secs_f64();
            response["metrics"] = serde_json::json!({
                "predict_time": predict_time
            });
        }

        response
    }
}

/// Prediction status.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum PredictionStatus {
    Starting,
    Processing,
    Succeeded,
    Failed,
    Canceled,
}

impl PredictionStatus {
    pub fn is_terminal(&self) -> bool {
        matches!(self, Self::Succeeded | Self::Failed | Self::Canceled)
    }

    pub fn as_str(&self) -> &'static str {
        match self {
            Self::Starting => "starting",
            Self::Processing => "processing",
            Self::Succeeded => "succeeded",
            Self::Failed => "failed",
            Self::Canceled => "canceled",
        }
    }
}

/// A handle to a submitted prediction.
///
/// Allows the handler to:
/// - Wait for completion
/// - Get current state (for idempotent PUT)
/// - Cancel the prediction
pub struct PredictionHandle {
    /// Prediction ID.
    id: String,
    /// Completion notification.
    completion: Arc<Notify>,
    /// Cancellation token.
    cancel_token: CancellationToken,
    /// Reference to supervisor for state queries.
    supervisor: Arc<PredictionSupervisor>,
}

/// Guard for sync predictions that cancels on drop.
///
/// For sync predictions, if the HTTP connection drops before completion,
/// we should cancel the prediction. This guard ensures that behavior
/// by cancelling on drop unless explicitly disarmed.
pub struct SyncPredictionGuard {
    cancel_token: Option<CancellationToken>,
}

impl SyncPredictionGuard {
    /// Create a new guard that will cancel on drop.
    pub fn new(cancel_token: CancellationToken) -> Self {
        Self {
            cancel_token: Some(cancel_token),
        }
    }

    /// Disarm the guard (prediction completed normally).
    pub fn disarm(&mut self) {
        self.cancel_token = None;
    }
}

impl Drop for SyncPredictionGuard {
    fn drop(&mut self) {
        if let Some(ref token) = self.cancel_token {
            token.cancel();
        }
    }
}

impl PredictionHandle {
    /// Get the prediction ID.
    pub fn id(&self) -> &str {
        &self.id
    }

    /// Wait for prediction to complete.
    pub async fn wait(&self) {
        self.completion.notified().await;
    }

    /// Get current state snapshot.
    pub fn state(&self) -> Option<PredictionState> {
        self.supervisor.get_state(&self.id)
    }

    /// Cancel the prediction.
    pub fn cancel(&self) {
        self.cancel_token.cancel();
    }

    /// Check if prediction is complete.
    pub fn is_complete(&self) -> bool {
        self.supervisor
            .get_state(&self.id)
            .map(|s| s.status.is_terminal())
            .unwrap_or(true) // Not found = complete (cleaned up)
    }

    /// Create a sync guard that cancels on drop.
    ///
    /// For sync predictions, the handler should hold this guard.
    /// If the connection drops (handler returns early), the guard
    /// will cancel the prediction, matching cog mainline behavior.
    pub fn sync_guard(&self) -> SyncPredictionGuard {
        SyncPredictionGuard::new(self.cancel_token.clone())
    }

    /// Get the cancellation token (for advanced use).
    pub fn cancel_token(&self) -> CancellationToken {
        self.cancel_token.clone()
    }
}

/// Internal prediction entry.
struct PredictionEntry {
    state: PredictionState,
    cancel_token: CancellationToken,
    completion: Arc<Notify>,
    webhook: Option<WebhookSender>,
}

/// Prediction supervisor.
///
/// Manages prediction lifecycle, state tracking, and webhook delivery.
/// Uses DashMap for lock-free concurrent access - no deadlocks possible.
pub struct PredictionSupervisor {
    /// In-flight predictions. DashMap provides safe concurrent access.
    predictions: DashMap<String, PredictionEntry>,
}

impl PredictionSupervisor {
    /// Create a new supervisor.
    pub fn new() -> Arc<Self> {
        Arc::new(Self {
            predictions: DashMap::new(),
        })
    }

    /// Submit a new prediction.
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

    /// Update prediction status.
    ///
    /// If status is terminal, sends webhook and cleans up entry.
    pub fn update_status(
        self: &Arc<Self>,
        id: &str,
        status: PredictionStatus,
        output: Option<serde_json::Value>,
        error: Option<String>,
    ) {
        // Use entry API for atomic update-then-remove
        if let Some(mut entry) = self.predictions.get_mut(id) {
            entry.state.status = status;
            if let Some(o) = output {
                entry.state.output = Some(o);
            }
            if let Some(e) = error {
                entry.state.error = Some(e);
            }

            if status.is_terminal() {
                entry.state.completed_at = Some(Instant::now());
                entry.completion.notify_waiters();
            }
        }

        // Handle terminal cleanup outside the entry reference
        if status.is_terminal() {
            // Remove and get ownership for webhook handling
            if let Some((_, mut entry)) = self.predictions.remove(id)
                && let Some(webhook) = entry.webhook.take()
            {
                let response = entry.state.to_response();
                // Spawn webhook send - don't block
                tokio::spawn(async move {
                    webhook
                        .send_terminal(WebhookEventType::Completed, &response)
                        .await;
                });
            }
        }
    }

    /// Append logs to a prediction.
    pub fn append_logs(&self, id: &str, logs: &str) {
        if let Some(mut entry) = self.predictions.get_mut(id) {
            entry.state.logs.push_str(logs);
        }
    }

    /// Cancel a prediction.
    pub fn cancel(&self, id: &str) -> bool {
        if let Some(entry) = self.predictions.get(id) {
            entry.cancel_token.cancel();
            true
        } else {
            false
        }
    }

    /// Get state snapshot.
    pub fn get_state(&self, id: &str) -> Option<PredictionState> {
        self.predictions.get(id).map(|e| e.state.clone())
    }

    /// Check if prediction exists.
    pub fn exists(&self, id: &str) -> bool {
        self.predictions.contains_key(id)
    }

    /// Remove a prediction entry (for manual cleanup if needed).
    pub fn remove(&self, id: &str) {
        self.predictions.remove(id);
    }
}

impl Default for PredictionSupervisor {
    fn default() -> Self {
        Self {
            predictions: DashMap::new(),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[tokio::test]
    async fn supervisor_submit_and_complete() {
        let supervisor = PredictionSupervisor::new();

        // Submit prediction
        let handle = supervisor.submit("test-1".to_string(), serde_json::json!({"x": 1}), None);

        assert_eq!(handle.id(), "test-1");

        // Check initial state
        let state = handle.state().unwrap();
        assert_eq!(state.status, PredictionStatus::Starting);
        assert_eq!(state.input, serde_json::json!({"x": 1}));

        // Update to processing
        supervisor.update_status("test-1", PredictionStatus::Processing, None, None);

        let state = handle.state().unwrap();
        assert_eq!(state.status, PredictionStatus::Processing);

        // Complete - with no webhook, entry is removed immediately
        supervisor.update_status(
            "test-1",
            PredictionStatus::Succeeded,
            Some(serde_json::json!("result")),
            None,
        );

        // After terminal + cleanup, state should be gone
        assert!(handle.state().is_none());
        assert!(!supervisor.exists("test-1"));
    }

    #[tokio::test]
    async fn supervisor_cancel() {
        let supervisor = PredictionSupervisor::new();

        let handle = supervisor.submit("test-cancel".to_string(), serde_json::json!({}), None);

        // Cancel via supervisor
        let cancelled = supervisor.cancel("test-cancel");
        assert!(cancelled);

        // Cancel token should be triggered
        assert!(handle.cancel_token().is_cancelled());
    }

    #[tokio::test]
    async fn supervisor_exists() {
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

        // Create guard and drop it (simulates connection drop)
        {
            let _guard = handle.sync_guard();
            assert!(!cancel_token.is_cancelled());
            // guard drops here
        }

        // Token should be cancelled
        assert!(cancel_token.is_cancelled());
    }

    #[tokio::test]
    async fn sync_guard_disarm_prevents_cancel() {
        let supervisor = PredictionSupervisor::new();

        let handle = supervisor.submit("test-disarm".to_string(), serde_json::json!({}), None);

        let cancel_token = handle.cancel_token();

        // Create guard, disarm it, then drop
        {
            let mut guard = handle.sync_guard();
            guard.disarm();
            // guard drops here
        }

        // Token should NOT be cancelled
        assert!(!cancel_token.is_cancelled());
    }
}
