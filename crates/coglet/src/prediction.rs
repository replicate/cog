//! Prediction struct for tracking prediction state.
//!
//! The `Prediction` struct holds:
//! - Status and timing information
//! - Accumulated logs and outputs
//! - Completion notification
//!
//! Note: Prediction does NOT own the slot permit. The permit lives in
//! `PredictionSlot` alongside the prediction, allowing the idle flag
//! to be set without holding the prediction lock.

use std::sync::Arc;
use std::time::Instant;

use tokio::sync::Notify;

use crate::predictor::{CancellationToken, PredictionOutput};
use crate::webhook::WebhookSender;

/// Status of a prediction.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum PredictionStatus {
    /// Prediction is starting.
    Starting,
    /// Prediction is running.
    Processing,
    /// Prediction succeeded.
    Succeeded,
    /// Prediction failed.
    Failed,
    /// Prediction was canceled.
    Canceled,
}

impl PredictionStatus {
    /// Check if this is a terminal status.
    pub fn is_terminal(&self) -> bool {
        matches!(self, Self::Succeeded | Self::Failed | Self::Canceled)
    }

    /// Get the status string for API responses.
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

/// Prediction state and accumulated results.
///
/// Tracks the lifecycle of a prediction including logs, outputs, and status.
/// Does NOT own the slot permit - that's managed separately by `PredictionSlot`.
pub struct Prediction {
    /// Prediction ID.
    id: String,

    /// Cancellation token for this prediction.
    cancel_token: CancellationToken,

    /// When the prediction started.
    started_at: Instant,

    /// Current status.
    status: PredictionStatus,

    /// Accumulated logs during prediction.
    logs: String,

    /// Streaming outputs (for generators).
    outputs: Vec<serde_json::Value>,

    /// Final output (set when prediction succeeds).
    output: Option<PredictionOutput>,

    /// Error message (set when prediction fails).
    error: Option<String>,

    /// Webhook sender (consumed on completion).
    webhook: Option<WebhookSender>,

    /// Notifies waiters when prediction completes.
    completion: Arc<Notify>,
}

impl Prediction {
    /// Create a new prediction.
    ///
    /// The prediction starts in `Starting` status.
    pub fn new(id: String, webhook: Option<WebhookSender>) -> Self {
        Self {
            id,
            cancel_token: CancellationToken::new(),
            started_at: Instant::now(),
            status: PredictionStatus::Starting,
            logs: String::new(),
            outputs: Vec::new(),
            output: None,
            error: None,
            webhook,
            completion: Arc::new(Notify::new()),
        }
    }

    /// Get the prediction ID.
    pub fn id(&self) -> &str {
        &self.id
    }

    /// Get the cancellation token.
    pub fn cancel_token(&self) -> CancellationToken {
        self.cancel_token.clone()
    }

    /// Check if this prediction has been canceled.
    pub fn is_canceled(&self) -> bool {
        self.cancel_token.is_cancelled()
    }

    /// Get the current status.
    pub fn status(&self) -> PredictionStatus {
        self.status
    }

    /// Mark the prediction as processing.
    pub fn set_processing(&mut self) {
        self.status = PredictionStatus::Processing;
    }

    /// Mark the prediction as succeeded with output.
    pub fn set_succeeded(&mut self, output: PredictionOutput) {
        self.status = PredictionStatus::Succeeded;
        self.output = Some(output);
        self.completion.notify_waiters();
    }

    /// Mark the prediction as failed with error.
    pub fn set_failed(&mut self, error: String) {
        self.status = PredictionStatus::Failed;
        self.error = Some(error);
        self.completion.notify_waiters();
    }

    /// Mark the prediction as canceled.
    pub fn set_canceled(&mut self) {
        self.status = PredictionStatus::Canceled;
        self.completion.notify_waiters();
    }

    /// Get elapsed time since prediction started.
    pub fn elapsed(&self) -> std::time::Duration {
        self.started_at.elapsed()
    }

    /// Append log data.
    pub fn append_log(&mut self, data: &str) {
        self.logs.push_str(data);
    }

    /// Get accumulated logs.
    pub fn logs(&self) -> &str {
        &self.logs
    }

    /// Append a streaming output value (for generators).
    pub fn append_output(&mut self, output: serde_json::Value) {
        self.outputs.push(output);
    }

    /// Get streaming outputs.
    pub fn outputs(&self) -> &[serde_json::Value] {
        &self.outputs
    }

    /// Get the final output.
    pub fn output(&self) -> Option<&PredictionOutput> {
        self.output.as_ref()
    }

    /// Get the error message.
    pub fn error(&self) -> Option<&str> {
        self.error.as_deref()
    }

    /// Wait for prediction to complete.
    pub async fn wait(&self) {
        if self.status.is_terminal() {
            return;
        }
        self.completion.notified().await;
    }

    /// Get the completion notifier (for sharing).
    pub fn completion(&self) -> Arc<Notify> {
        Arc::clone(&self.completion)
    }

    /// Take the webhook sender (for sending on drop).
    pub fn take_webhook(&mut self) -> Option<WebhookSender> {
        self.webhook.take()
    }

    /// Build the terminal webhook response payload.
    pub fn build_terminal_response(&self) -> serde_json::Value {
        let predict_time = self.elapsed().as_secs_f64();

        match self.status {
            PredictionStatus::Succeeded => {
                serde_json::json!({
                    "id": self.id,
                    "status": "succeeded",
                    "output": self.output,
                    "metrics": {
                        "predict_time": predict_time
                    }
                })
            }
            PredictionStatus::Failed => {
                serde_json::json!({
                    "id": self.id,
                    "status": "failed",
                    "error": self.error,
                    "metrics": {
                        "predict_time": predict_time
                    }
                })
            }
            PredictionStatus::Canceled => {
                serde_json::json!({
                    "id": self.id,
                    "status": "canceled",
                    "metrics": {
                        "predict_time": predict_time
                    }
                })
            }
            // Non-terminal statuses shouldn't reach here,
            // but handle gracefully
            _ => {
                serde_json::json!({
                    "id": self.id,
                    "status": "failed",
                    "error": "Prediction dropped in non-terminal state",
                    "metrics": {
                        "predict_time": predict_time
                    }
                })
            }
        }
    }
}

// Note: Prediction does NOT implement Drop. RAII cleanup (webhook, permit return)
// is handled by PredictionSlot which owns both the Prediction and Permit.

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn prediction_status_is_terminal() {
        assert!(!PredictionStatus::Starting.is_terminal());
        assert!(!PredictionStatus::Processing.is_terminal());
        assert!(PredictionStatus::Succeeded.is_terminal());
        assert!(PredictionStatus::Failed.is_terminal());
        assert!(PredictionStatus::Canceled.is_terminal());
    }

    #[test]
    fn prediction_new_starts_in_starting_status() {
        let pred = Prediction::new("test".to_string(), None);
        assert_eq!(pred.status(), PredictionStatus::Starting);
        assert_eq!(pred.id(), "test");
    }

    #[test]
    fn prediction_set_succeeded() {
        let mut pred = Prediction::new("test".to_string(), None);
        pred.set_succeeded(PredictionOutput::Single(serde_json::json!("hello")));
        assert_eq!(pred.status(), PredictionStatus::Succeeded);
    }

    #[test]
    fn prediction_set_failed() {
        let mut pred = Prediction::new("test".to_string(), None);
        pred.set_failed("something went wrong".to_string());
        assert_eq!(pred.status(), PredictionStatus::Failed);
    }

    #[test]
    fn prediction_set_canceled() {
        let mut pred = Prediction::new("test".to_string(), None);
        pred.set_canceled();
        assert_eq!(pred.status(), PredictionStatus::Canceled);
    }

    #[test]
    fn prediction_cancel_token_works() {
        let pred = Prediction::new("test".to_string(), None);
        let token = pred.cancel_token();

        assert!(!pred.is_canceled());
        token.cancel();
        assert!(pred.is_canceled());
    }

    #[test]
    fn prediction_elapsed_time_increases() {
        let pred = Prediction::new("test".to_string(), None);
        let t1 = pred.elapsed();
        std::thread::sleep(std::time::Duration::from_millis(10));
        let t2 = pred.elapsed();
        assert!(t2 > t1);
    }

    #[test]
    fn prediction_append_log() {
        let mut pred = Prediction::new("test".to_string(), None);
        pred.append_log("line 1\n");
        pred.append_log("line 2\n");
        assert_eq!(pred.logs(), "line 1\nline 2\n");
    }

    #[test]
    fn prediction_append_output() {
        let mut pred = Prediction::new("test".to_string(), None);
        pred.append_output(serde_json::json!("chunk1"));
        pred.append_output(serde_json::json!("chunk2"));
        assert_eq!(pred.outputs().len(), 2);
    }

    #[tokio::test]
    async fn prediction_wait_returns_immediately_if_terminal() {
        let mut pred = Prediction::new("test".to_string(), None);
        pred.set_succeeded(PredictionOutput::Single(serde_json::json!("done")));
        
        // Should return immediately, not block
        pred.wait().await;
        assert_eq!(pred.status(), PredictionStatus::Succeeded);
    }
}
