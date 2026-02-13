//! Prediction state tracking.

use std::sync::Arc;
use std::time::Instant;

use tokio::sync::Notify;
pub use tokio_util::sync::CancellationToken;

use crate::webhook::WebhookSender;

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

/// Prediction output - single value or streamed chunks.
#[derive(Debug, Clone, serde::Serialize)]
#[serde(untagged)]
pub enum PredictionOutput {
    Single(serde_json::Value),
    Stream(Vec<serde_json::Value>),
}

impl PredictionOutput {
    pub fn is_stream(&self) -> bool {
        matches!(self, PredictionOutput::Stream(_))
    }

    pub fn into_values(self) -> Vec<serde_json::Value> {
        match self {
            PredictionOutput::Single(v) => vec![v],
            PredictionOutput::Stream(v) => v,
        }
    }

    /// Get the final/only output value (last for stream, the value for single).
    pub fn final_value(&self) -> &serde_json::Value {
        match self {
            PredictionOutput::Single(v) => v,
            PredictionOutput::Stream(v) => v.last().unwrap_or(&serde_json::Value::Null),
        }
    }
}

/// Prediction lifecycle state.
pub struct Prediction {
    id: String,
    cancel_token: CancellationToken,
    started_at: Instant,
    status: PredictionStatus,
    logs: String,
    outputs: Vec<serde_json::Value>,
    output: Option<PredictionOutput>,
    error: Option<String>,
    webhook: Option<WebhookSender>,
    completion: Arc<Notify>,
}

impl Prediction {
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

    pub fn id(&self) -> &str {
        &self.id
    }

    pub fn cancel_token(&self) -> CancellationToken {
        self.cancel_token.clone()
    }

    pub fn is_canceled(&self) -> bool {
        self.cancel_token.is_cancelled()
    }

    pub fn status(&self) -> PredictionStatus {
        self.status
    }

    pub fn is_terminal(&self) -> bool {
        self.status.is_terminal()
    }

    pub fn set_processing(&mut self) {
        self.status = PredictionStatus::Processing;
    }

    pub fn set_succeeded(&mut self, output: PredictionOutput) {
        self.status = PredictionStatus::Succeeded;
        self.output = Some(output);
        self.completion.notify_waiters();
    }

    pub fn set_failed(&mut self, error: String) {
        self.status = PredictionStatus::Failed;
        self.error = Some(error);
        self.completion.notify_waiters();
    }

    pub fn set_canceled(&mut self) {
        self.status = PredictionStatus::Canceled;
        self.completion.notify_waiters();
    }

    pub fn elapsed(&self) -> std::time::Duration {
        self.started_at.elapsed()
    }

    pub fn append_log(&mut self, data: &str) {
        self.logs.push_str(data);
    }

    pub fn logs(&self) -> &str {
        &self.logs
    }

    pub fn append_output(&mut self, output: serde_json::Value) {
        self.outputs.push(output);
    }

    pub fn outputs(&self) -> &[serde_json::Value] {
        &self.outputs
    }

    pub fn output(&self) -> Option<&PredictionOutput> {
        self.output.as_ref()
    }

    pub fn error(&self) -> Option<&str> {
        self.error.as_deref()
    }

    pub async fn wait(&self) {
        if self.status.is_terminal() {
            return;
        }
        self.completion.notified().await;
    }

    pub fn completion(&self) -> Arc<Notify> {
        Arc::clone(&self.completion)
    }

    /// Take the webhook sender (for sending on drop).
    pub fn take_webhook(&mut self) -> Option<WebhookSender> {
        self.webhook.take()
    }

    pub fn build_terminal_response(&self) -> serde_json::Value {
        let predict_time = self.elapsed().as_secs_f64();

        match self.status {
            PredictionStatus::Succeeded => {
                serde_json::json!({
                    "id": self.id,
                    "status": "succeeded",
                    "output": self.output,
                    "metrics": { "predict_time": predict_time }
                })
            }
            PredictionStatus::Failed => {
                serde_json::json!({
                    "id": self.id,
                    "status": "failed",
                    "error": self.error,
                    "metrics": { "predict_time": predict_time }
                })
            }
            PredictionStatus::Canceled => {
                serde_json::json!({
                    "id": self.id,
                    "status": "canceled",
                    "metrics": { "predict_time": predict_time }
                })
            }
            _ => {
                serde_json::json!({
                    "id": self.id,
                    "status": "failed",
                    "error": "Prediction in non-terminal state",
                    "metrics": { "predict_time": predict_time }
                })
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn status_is_terminal() {
        assert!(!PredictionStatus::Starting.is_terminal());
        assert!(!PredictionStatus::Processing.is_terminal());
        assert!(PredictionStatus::Succeeded.is_terminal());
        assert!(PredictionStatus::Failed.is_terminal());
        assert!(PredictionStatus::Canceled.is_terminal());
    }

    #[test]
    fn new_starts_in_starting_status() {
        let pred = Prediction::new("test".to_string(), None);
        assert_eq!(pred.status(), PredictionStatus::Starting);
        assert_eq!(pred.id(), "test");
    }

    #[test]
    fn set_succeeded() {
        let mut pred = Prediction::new("test".to_string(), None);
        pred.set_succeeded(PredictionOutput::Single(serde_json::json!("hello")));
        assert_eq!(pred.status(), PredictionStatus::Succeeded);
    }

    #[test]
    fn set_failed() {
        let mut pred = Prediction::new("test".to_string(), None);
        pred.set_failed("something went wrong".to_string());
        assert_eq!(pred.status(), PredictionStatus::Failed);
    }

    #[test]
    fn set_canceled() {
        let mut pred = Prediction::new("test".to_string(), None);
        pred.set_canceled();
        assert_eq!(pred.status(), PredictionStatus::Canceled);
    }

    #[test]
    fn cancel_token_works() {
        let pred = Prediction::new("test".to_string(), None);
        let token = pred.cancel_token();

        assert!(!pred.is_canceled());
        token.cancel();
        assert!(pred.is_canceled());
    }

    #[test]
    fn elapsed_time_increases() {
        let pred = Prediction::new("test".to_string(), None);
        let t1 = pred.elapsed();
        std::thread::sleep(std::time::Duration::from_millis(10));
        let t2 = pred.elapsed();
        assert!(t2 > t1);
    }

    #[test]
    fn append_log() {
        let mut pred = Prediction::new("test".to_string(), None);
        pred.append_log("line 1\n");
        pred.append_log("line 2\n");
        assert_eq!(pred.logs(), "line 1\nline 2\n");
    }

    #[test]
    fn append_output() {
        let mut pred = Prediction::new("test".to_string(), None);
        pred.append_output(serde_json::json!("chunk1"));
        pred.append_output(serde_json::json!("chunk2"));
        assert_eq!(pred.outputs().len(), 2);
    }

    #[tokio::test]
    async fn wait_returns_immediately_if_terminal() {
        let mut pred = Prediction::new("test".to_string(), None);
        pred.set_succeeded(PredictionOutput::Single(serde_json::json!("done")));

        pred.wait().await;
        assert_eq!(pred.status(), PredictionStatus::Succeeded);
    }

    #[test]
    fn prediction_output_single() {
        let output = PredictionOutput::Single(serde_json::json!("hello"));
        assert!(!output.is_stream());
        assert_eq!(output.into_values(), vec![serde_json::json!("hello")]);
    }

    #[test]
    fn prediction_output_stream() {
        let output = PredictionOutput::Stream(vec![serde_json::json!("a"), serde_json::json!("b")]);
        assert!(output.is_stream());
    }
}
