//! RAII Prediction struct that owns resources and guarantees cleanup.
//!
//! The `Prediction` struct ensures:
//! - Terminal webhook is sent before resources are released
//! - Slot permit is released only after webhook completes
//! - Cleanup happens even on panic (via Drop)

use std::time::Instant;

use tokio::sync::OwnedSemaphorePermit;

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

/// RAII prediction that owns resources and guarantees cleanup.
///
/// When dropped:
/// 1. Sends terminal webhook (if configured) via `send_terminal_sync`
/// 2. Releases slot permit (after webhook completes)
///
/// This ensures the platform receives the terminal webhook before the slot
/// becomes available for new predictions.
pub struct Prediction {
    /// Prediction ID.
    id: String,
    
    /// Slot permit (released on drop, after webhook).
    #[allow(dead_code)]
    slot_permit: OwnedSemaphorePermit,
    
    /// Cancellation token for this prediction.
    cancel_token: CancellationToken,
    
    /// When the prediction started.
    started_at: Instant,
    
    /// Current status.
    status: PredictionStatus,
    
    /// Output (set when prediction succeeds).
    output: Option<PredictionOutput>,
    
    /// Error message (set when prediction fails).
    error: Option<String>,
    
    /// Webhook sender (consumed on drop).
    webhook: Option<WebhookSender>,
}

impl Prediction {
    /// Create a new prediction.
    ///
    /// The prediction starts in `Starting` status.
    pub fn new(
        id: String,
        slot_permit: OwnedSemaphorePermit,
        webhook: Option<WebhookSender>,
    ) -> Self {
        Self {
            id,
            slot_permit,
            cancel_token: CancellationToken::new(),
            started_at: Instant::now(),
            status: PredictionStatus::Starting,
            output: None,
            error: None,
            webhook,
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
    }
    
    /// Mark the prediction as failed with error.
    pub fn set_failed(&mut self, error: String) {
        self.status = PredictionStatus::Failed;
        self.error = Some(error);
    }
    
    /// Mark the prediction as canceled.
    pub fn set_canceled(&mut self) {
        self.status = PredictionStatus::Canceled;
    }
    
    /// Get elapsed time since prediction started.
    pub fn elapsed(&self) -> std::time::Duration {
        self.started_at.elapsed()
    }
    
    /// Build the terminal webhook response payload.
    fn build_terminal_response(&self) -> serde_json::Value {
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
            // Non-terminal statuses shouldn't reach Drop with webhook,
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

impl Drop for Prediction {
    fn drop(&mut self) {
        // Send terminal webhook if configured
        if let Some(webhook) = self.webhook.take() {
            let response = self.build_terminal_response();
            
            // Spawn a thread to send the webhook synchronously.
            // This blocks Drop (and thus slot release) until webhook completes,
            // but doesn't block the tokio runtime.
            let handle = std::thread::spawn(move || {
                webhook.send_terminal_sync(&response);
            });
            
            // Wait for webhook to complete before releasing slot
            if let Err(e) = handle.join() {
                tracing::error!("Terminal webhook thread panicked: {:?}", e);
            }
        }
        // OwnedSemaphorePermit drops here → slot released
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::sync::Arc;
    use tokio::sync::Semaphore;
    
    fn make_permit() -> OwnedSemaphorePermit {
        let sem = Arc::new(Semaphore::new(1));
        sem.try_acquire_owned().unwrap()
    }
    
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
        let pred = Prediction::new("test".to_string(), make_permit(), None);
        assert_eq!(pred.status(), PredictionStatus::Starting);
        assert_eq!(pred.id(), "test");
    }
    
    #[test]
    fn prediction_set_succeeded() {
        let mut pred = Prediction::new("test".to_string(), make_permit(), None);
        pred.set_succeeded(PredictionOutput::Single(serde_json::json!("hello")));
        assert_eq!(pred.status(), PredictionStatus::Succeeded);
    }
    
    #[test]
    fn prediction_set_failed() {
        let mut pred = Prediction::new("test".to_string(), make_permit(), None);
        pred.set_failed("something went wrong".to_string());
        assert_eq!(pred.status(), PredictionStatus::Failed);
    }
    
    #[test]
    fn prediction_set_canceled() {
        let mut pred = Prediction::new("test".to_string(), make_permit(), None);
        pred.set_canceled();
        assert_eq!(pred.status(), PredictionStatus::Canceled);
    }
    
    #[test]
    fn prediction_cancel_token_works() {
        let pred = Prediction::new("test".to_string(), make_permit(), None);
        let token = pred.cancel_token();
        
        assert!(!pred.is_canceled());
        token.cancel();
        assert!(pred.is_canceled());
    }
    
    #[test]
    fn prediction_elapsed_time_increases() {
        let pred = Prediction::new("test".to_string(), make_permit(), None);
        let t1 = pred.elapsed();
        std::thread::sleep(std::time::Duration::from_millis(10));
        let t2 = pred.elapsed();
        assert!(t2 > t1);
    }
    
    #[test]
    fn prediction_drop_releases_slot_without_webhook() {
        let sem = Arc::new(Semaphore::new(1));
        let permit = sem.clone().try_acquire_owned().unwrap();
        
        // Slot is taken
        assert_eq!(sem.available_permits(), 0);
        
        {
            let _pred = Prediction::new("test".to_string(), permit, None);
            // Still taken
            assert_eq!(sem.available_permits(), 0);
        }
        
        // Slot released after drop
        assert_eq!(sem.available_permits(), 1);
    }
    
    #[tokio::test]
    async fn prediction_drop_sends_webhook_before_releasing_slot() {
        use wiremock::matchers::{method, path};
        use wiremock::{Mock, MockServer, ResponseTemplate};
        use crate::webhook::{WebhookConfig, WebhookSender};
        
        let server = MockServer::start().await;
        
        Mock::given(method("POST"))
            .and(path("/webhook"))
            .respond_with(ResponseTemplate::new(200))
            .expect(1)
            .mount(&server)
            .await;
        
        let sem = Arc::new(Semaphore::new(1));
        let permit = sem.clone().try_acquire_owned().unwrap();
        
        let webhook = WebhookSender::new(
            format!("{}/webhook", server.uri()),
            WebhookConfig::default(),
        );
        
        {
            let mut pred = Prediction::new("test_123".to_string(), permit, Some(webhook));
            pred.set_succeeded(PredictionOutput::Single(serde_json::json!("result")));
            // Drop happens here, webhook is sent, then slot released
        }
        
        // Slot should be released after webhook
        assert_eq!(sem.available_permits(), 1);
        
        // wiremock verifies the webhook was called via expect(1)
    }
    
    #[tokio::test]
    async fn prediction_drop_sends_webhook_on_failure() {
        use wiremock::matchers::{method, path};
        use wiremock::{Mock, MockServer, ResponseTemplate};
        use crate::webhook::{WebhookConfig, WebhookSender};
        
        let server = MockServer::start().await;
        
        Mock::given(method("POST"))
            .and(path("/webhook"))
            .respond_with(ResponseTemplate::new(200))
            .expect(1)
            .mount(&server)
            .await;
        
        let sem = Arc::new(Semaphore::new(1));
        let permit = sem.clone().try_acquire_owned().unwrap();
        
        let webhook = WebhookSender::new(
            format!("{}/webhook", server.uri()),
            WebhookConfig::default(),
        );
        
        {
            let mut pred = Prediction::new("test_456".to_string(), permit, Some(webhook));
            pred.set_failed("something broke".to_string());
        }
        
        assert_eq!(sem.available_permits(), 1);
    }
}
