//! Predictor trait for different backends.

use std::time::{Duration, Instant};

pub use tokio_util::sync::CancellationToken;

/// Result of a prediction.
#[derive(Debug, Clone)]
pub struct PredictionResult {
    /// The output - single value or stream of values.
    pub output: PredictionOutput,
    /// Time taken for the prediction.
    pub predict_time: Option<Duration>,
    /// Captured logs (stdout/stderr) during prediction.
    pub logs: String,
}

/// Output type from a prediction - either single value or stream of values.
#[derive(Debug, Clone, serde::Serialize)]
#[serde(untagged)]
pub enum PredictionOutput {
    /// Single value output (non-generator predict).
    Single(serde_json::Value),
    /// Multiple values streamed (generator predict).
    /// For HTTP, each value is sent as a chunk.
    Stream(Vec<serde_json::Value>),
}

impl PredictionOutput {
    /// Check if this is a streaming (multi-value) output.
    pub fn is_stream(&self) -> bool {
        matches!(self, PredictionOutput::Stream(_))
    }

    /// Get all output values as a vec (single wrapped in vec, stream as-is).
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

/// Metrics collected during a prediction lifecycle.
#[derive(Debug, Clone, Default)]
pub struct PredictionMetrics {
    /// Time spent in the predict() call.
    pub predict_time: Option<Duration>,
}

/// RAII guard for prediction lifecycle.
///
/// Tracks timing and ensures cleanup on drop.
/// Create with `PredictionGuard::new()`, call `finish()` when done.
///
/// Also provides a cancellation token that can be used to cancel the prediction
/// from another task (e.g., via HTTP cancel endpoint or signal handler).
pub struct PredictionGuard {
    start_time: Instant,
    metrics: PredictionMetrics,
    /// Token for cancelling this prediction.
    cancel_token: CancellationToken,
}

impl PredictionGuard {
    /// Start a new prediction, recording the start time.
    pub fn new() -> Self {
        Self {
            start_time: Instant::now(),
            metrics: PredictionMetrics::default(),
            cancel_token: CancellationToken::new(),
        }
    }

    /// Get a clone of the cancellation token for this prediction.
    ///
    /// The token can be used to:
    /// - `cancel()` - trigger cancellation
    /// - `is_cancelled()` - check if cancelled
    /// - `cancelled()` - async wait for cancellation
    pub fn cancel_token(&self) -> CancellationToken {
        self.cancel_token.clone()
    }

    /// Check if this prediction has been cancelled.
    pub fn is_cancelled(&self) -> bool {
        self.cancel_token.is_cancelled()
    }

    /// Cancel this prediction.
    pub fn cancel(&self) {
        self.cancel_token.cancel();
    }

    /// Finish the prediction, computing final metrics.
    pub fn finish(mut self) -> PredictionMetrics {
        self.metrics.predict_time = Some(self.start_time.elapsed());
        self.metrics
    }
}

impl Default for PredictionGuard {
    fn default() -> Self {
        Self::new()
    }
}

/// Errors that can occur during prediction.
#[derive(Debug, thiserror::Error)]
pub enum PredictionError {
    #[error("Prediction failed: {0}")]
    Failed(String),

    #[error("Input validation error: {0}")]
    InvalidInput(String),

    #[error("Predictor not ready")]
    NotReady,

    #[error("Prediction was cancelled")]
    Cancelled,
}

/// A sync predict function trait object that can be stored in AppState.
///
/// Takes JSON input, returns JSON output or error.
/// This is a trait object rather than a Box so it can be wrapped in Arc for cloning.
pub type PredictFn = dyn Fn(serde_json::Value) -> Result<PredictionResult, PredictionError> + Send + Sync;

/// Future type for async predictions.
pub type PredictFuture = std::pin::Pin<
    Box<dyn std::future::Future<Output = Result<PredictionResult, PredictionError>> + Send>,
>;

/// An async predict function that can be stored in AppState.
///
/// Takes JSON input, returns a future that resolves to output or error.
pub type AsyncPredictFn = dyn Fn(serde_json::Value) -> PredictFuture + Send + Sync;

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    #[test]
    fn prediction_output_single_is_not_stream() {
        let output = PredictionOutput::Single(json!("hello"));
        assert!(!output.is_stream());
    }

    #[test]
    fn prediction_output_stream_is_stream() {
        let output = PredictionOutput::Stream(vec![json!("a"), json!("b")]);
        assert!(output.is_stream());
    }

    #[test]
    fn prediction_output_single_into_values() {
        let output = PredictionOutput::Single(json!(42));
        let values = output.into_values();
        assert_eq!(values.len(), 1);
        assert_eq!(values[0], json!(42));
    }

    #[test]
    fn prediction_output_stream_into_values() {
        let output = PredictionOutput::Stream(vec![json!(1), json!(2), json!(3)]);
        let values = output.into_values();
        assert_eq!(values, vec![json!(1), json!(2), json!(3)]);
    }

    #[test]
    fn prediction_output_single_final_value() {
        let output = PredictionOutput::Single(json!("result"));
        assert_eq!(output.final_value(), &json!("result"));
    }

    #[test]
    fn prediction_output_stream_final_value() {
        let output = PredictionOutput::Stream(vec![json!("first"), json!("last")]);
        assert_eq!(output.final_value(), &json!("last"));
    }

    #[test]
    fn prediction_output_empty_stream_final_value_is_null() {
        let output = PredictionOutput::Stream(vec![]);
        assert_eq!(output.final_value(), &serde_json::Value::Null);
    }

    #[test]
    fn prediction_output_serializes_untagged() {
        // Single value serializes as just the value
        let single = PredictionOutput::Single(json!("hello"));
        insta::assert_json_snapshot!("output_single", single);

        // Stream serializes as array
        let stream = PredictionOutput::Stream(vec![json!(1), json!(2)]);
        insta::assert_json_snapshot!("output_stream", stream);
    }

    #[test]
    fn prediction_guard_tracks_time() {
        let guard = PredictionGuard::new();
        std::thread::sleep(std::time::Duration::from_millis(10));
        let metrics = guard.finish();

        assert!(metrics.predict_time.is_some());
        let time = metrics.predict_time.unwrap();
        // Should be at least 10ms
        assert!(time.as_millis() >= 10);
        // But not more than 1 second (sanity check)
        assert!(time.as_secs() < 1);
    }

    #[test]
    fn prediction_error_display() {
        let err = PredictionError::Failed("something broke".to_string());
        assert_eq!(format!("{}", err), "Prediction failed: something broke");

        let err = PredictionError::InvalidInput("bad json".to_string());
        assert_eq!(format!("{}", err), "Input validation error: bad json");

        let err = PredictionError::NotReady;
        assert_eq!(format!("{}", err), "Predictor not ready");
    }
}
