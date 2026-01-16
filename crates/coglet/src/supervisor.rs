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

use std::collections::HashMap;
use std::sync::Arc;
use std::time::Instant;

use tokio::sync::{mpsc, oneshot, Mutex, Notify, RwLock};

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
    pub async fn state(&self) -> Option<PredictionState> {
        self.supervisor.get_state(&self.id).await
    }

    /// Cancel the prediction.
    pub fn cancel(&self) {
        self.cancel_token.cancel();
    }

    /// Check if prediction is complete.
    pub async fn is_complete(&self) -> bool {
        self.supervisor
            .get_state(&self.id)
            .await
            .map(|s| s.status.is_terminal())
            .unwrap_or(true) // Not found = complete (cleaned up)
    }
}

/// Internal prediction entry.
struct PredictionEntry {
    state: PredictionState,
    cancel_token: CancellationToken,
    completion: Arc<Notify>,
    webhook: Option<WebhookSender>,
}

/// Command sent to supervisor task.
enum SupervisorCommand {
    /// Submit a new prediction.
    Submit {
        id: String,
        input: serde_json::Value,
        webhook: Option<WebhookSender>,
        response: oneshot::Sender<PredictionHandle>,
    },
    /// Update prediction status.
    UpdateStatus {
        id: String,
        status: PredictionStatus,
        output: Option<serde_json::Value>,
        error: Option<String>,
    },
    /// Append logs.
    AppendLogs {
        id: String,
        logs: String,
    },
    /// Cancel a prediction.
    Cancel {
        id: String,
    },
    /// Get state snapshot.
    GetState {
        id: String,
        response: oneshot::Sender<Option<PredictionState>>,
    },
    /// Check if prediction exists.
    Exists {
        id: String,
        response: oneshot::Sender<bool>,
    },
}

/// Prediction supervisor.
///
/// Manages prediction lifecycle, state tracking, and webhook delivery.
pub struct PredictionSupervisor {
    /// Command channel.
    cmd_tx: mpsc::Sender<SupervisorCommand>,
    /// In-flight predictions (for direct access in some cases).
    predictions: RwLock<HashMap<String, Arc<Mutex<PredictionEntry>>>>,
}

impl PredictionSupervisor {
    /// Create a new supervisor.
    pub fn new() -> Arc<Self> {
        let (cmd_tx, cmd_rx) = mpsc::channel(256);
        let supervisor = Arc::new(Self {
            cmd_tx,
            predictions: RwLock::new(HashMap::new()),
        });

        // Spawn the supervisor task
        let supervisor_clone = Arc::clone(&supervisor);
        tokio::spawn(async move {
            supervisor_clone.run(cmd_rx).await;
        });

        supervisor
    }

    /// Run the supervisor event loop.
    async fn run(self: Arc<Self>, mut cmd_rx: mpsc::Receiver<SupervisorCommand>) {
        while let Some(cmd) = cmd_rx.recv().await {
            match cmd {
                SupervisorCommand::Submit {
                    id,
                    input,
                    webhook,
                    response,
                } => {
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

                    let entry = Arc::new(Mutex::new(entry));
                    self.predictions.write().await.insert(id.clone(), entry);

                    let handle = PredictionHandle {
                        id,
                        completion,
                        cancel_token,
                        supervisor: Arc::clone(&self),
                    };

                    let _ = response.send(handle);
                }

                SupervisorCommand::UpdateStatus {
                    id,
                    status,
                    output,
                    error,
                } => {
                    if let Some(entry) = self.predictions.read().await.get(&id) {
                        let mut entry = entry.lock().await;
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

                            // Send terminal webhook
                            if let Some(webhook) = entry.webhook.take() {
                                let response = entry.state.to_response();
                                // Spawn to avoid blocking supervisor
                                tokio::spawn(async move {
                                    webhook
                                        .send_terminal(WebhookEventType::Completed, &response)
                                        .await;
                                });
                            }
                        }
                    }
                }

                SupervisorCommand::AppendLogs { id, logs } => {
                    if let Some(entry) = self.predictions.read().await.get(&id) {
                        let mut entry = entry.lock().await;
                        entry.state.logs.push_str(&logs);
                    }
                }

                SupervisorCommand::Cancel { id } => {
                    if let Some(entry) = self.predictions.read().await.get(&id) {
                        let entry = entry.lock().await;
                        entry.cancel_token.cancel();
                    }
                }

                SupervisorCommand::GetState { id, response } => {
                    let state = if let Some(entry) = self.predictions.read().await.get(&id) {
                        let entry = entry.lock().await;
                        Some(entry.state.clone())
                    } else {
                        None
                    };
                    let _ = response.send(state);
                }

                SupervisorCommand::Exists { id, response } => {
                    let exists = self.predictions.read().await.contains_key(&id);
                    let _ = response.send(exists);
                }
            }
        }
    }

    /// Submit a new prediction.
    pub async fn submit(
        &self,
        id: String,
        input: serde_json::Value,
        webhook: Option<WebhookSender>,
    ) -> PredictionHandle {
        let (tx, rx) = oneshot::channel();
        let _ = self
            .cmd_tx
            .send(SupervisorCommand::Submit {
                id,
                input,
                webhook,
                response: tx,
            })
            .await;
        rx.await.expect("supervisor task died")
    }

    /// Update prediction status.
    pub async fn update_status(
        &self,
        id: &str,
        status: PredictionStatus,
        output: Option<serde_json::Value>,
        error: Option<String>,
    ) {
        let _ = self
            .cmd_tx
            .send(SupervisorCommand::UpdateStatus {
                id: id.to_string(),
                status,
                output,
                error,
            })
            .await;
    }

    /// Append logs to a prediction.
    pub async fn append_logs(&self, id: &str, logs: &str) {
        let _ = self
            .cmd_tx
            .send(SupervisorCommand::AppendLogs {
                id: id.to_string(),
                logs: logs.to_string(),
            })
            .await;
    }

    /// Cancel a prediction.
    pub async fn cancel(&self, id: &str) -> bool {
        let exists = self.exists(id).await;
        if exists {
            let _ = self
                .cmd_tx
                .send(SupervisorCommand::Cancel {
                    id: id.to_string(),
                })
                .await;
        }
        exists
    }

    /// Get state snapshot.
    pub async fn get_state(&self, id: &str) -> Option<PredictionState> {
        let (tx, rx) = oneshot::channel();
        let _ = self
            .cmd_tx
            .send(SupervisorCommand::GetState {
                id: id.to_string(),
                response: tx,
            })
            .await;
        rx.await.ok().flatten()
    }

    /// Check if prediction exists.
    pub async fn exists(&self, id: &str) -> bool {
        let (tx, rx) = oneshot::channel();
        let _ = self
            .cmd_tx
            .send(SupervisorCommand::Exists {
                id: id.to_string(),
                response: tx,
            })
            .await;
        rx.await.unwrap_or(false)
    }

    /// Remove a completed prediction (cleanup).
    pub async fn remove(&self, id: &str) {
        self.predictions.write().await.remove(id);
    }
}

impl Default for PredictionSupervisor {
    fn default() -> Self {
        // This is a bit awkward - we need Arc for the spawned task
        // Users should call PredictionSupervisor::new() instead
        panic!("Use PredictionSupervisor::new() to create a supervisor")
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[tokio::test]
    async fn supervisor_submit_and_complete() {
        let supervisor = PredictionSupervisor::new();

        // Submit prediction
        let handle = supervisor
            .submit(
                "test-1".to_string(),
                serde_json::json!({"x": 1}),
                None,
            )
            .await;

        assert_eq!(handle.id(), "test-1");

        // Check initial state
        let state = handle.state().await.unwrap();
        assert_eq!(state.status, PredictionStatus::Starting);
        assert_eq!(state.input, serde_json::json!({"x": 1}));

        // Update to processing
        supervisor
            .update_status("test-1", PredictionStatus::Processing, None, None)
            .await;

        let state = handle.state().await.unwrap();
        assert_eq!(state.status, PredictionStatus::Processing);

        // Complete
        supervisor
            .update_status(
                "test-1",
                PredictionStatus::Succeeded,
                Some(serde_json::json!("result")),
                None,
            )
            .await;

        // Give the notify a moment
        tokio::time::sleep(tokio::time::Duration::from_millis(10)).await;

        let state = handle.state().await.unwrap();
        assert_eq!(state.status, PredictionStatus::Succeeded);
        assert_eq!(state.output, Some(serde_json::json!("result")));
        assert!(state.completed_at.is_some());
    }

    #[tokio::test]
    async fn supervisor_cancel() {
        let supervisor = PredictionSupervisor::new();

        let handle = supervisor
            .submit("test-cancel".to_string(), serde_json::json!({}), None)
            .await;

        // Cancel via supervisor
        let cancelled = supervisor.cancel("test-cancel").await;
        assert!(cancelled);

        // Cancel token should be triggered
        // (In real code, the worker would check this)
    }

    #[tokio::test]
    async fn supervisor_exists() {
        let supervisor = PredictionSupervisor::new();

        assert!(!supervisor.exists("nonexistent").await);

        supervisor
            .submit("exists-test".to_string(), serde_json::json!({}), None)
            .await;

        assert!(supervisor.exists("exists-test").await);
    }
}
