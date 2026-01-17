//! Predictor traits and prediction lifecycle types.

use std::time::{Duration, Instant};

pub use crate::prediction::{CancellationToken, PredictionOutput};

/// Result of a completed prediction.
#[derive(Debug, Clone)]
pub struct PredictionResult {
    pub output: PredictionOutput,
    pub predict_time: Option<Duration>,
    pub logs: String,
}

/// Metrics collected during prediction.
#[derive(Debug, Clone, Default)]
pub struct PredictionMetrics {
    pub predict_time: Option<Duration>,
}

/// RAII guard for prediction lifecycle timing.
pub struct PredictionGuard {
    start_time: Instant,
    metrics: PredictionMetrics,
    cancel_token: CancellationToken,
}

impl PredictionGuard {
    pub fn new() -> Self {
        Self {
            start_time: Instant::now(),
            metrics: PredictionMetrics::default(),
            cancel_token: CancellationToken::new(),
        }
    }

    pub fn cancel_token(&self) -> CancellationToken {
        self.cancel_token.clone()
    }

    pub fn is_cancelled(&self) -> bool {
        self.cancel_token.is_cancelled()
    }

    pub fn cancel(&self) {
        self.cancel_token.cancel();
    }

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

/// Sync predict function - takes JSON input, returns result or error.
pub type PredictFn =
    dyn Fn(serde_json::Value) -> Result<PredictionResult, PredictionError> + Send + Sync;

/// Future type for async predictions.
pub type PredictFuture = std::pin::Pin<
    Box<dyn std::future::Future<Output = Result<PredictionResult, PredictionError>> + Send>,
>;

/// Async predict function - takes JSON input, returns future resolving to result.
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
    fn prediction_output_serializes_untagged() {
        let single = PredictionOutput::Single(json!("hello"));
        insta::assert_json_snapshot!("output_single", single);

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
        assert!(time.as_millis() >= 10);
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
