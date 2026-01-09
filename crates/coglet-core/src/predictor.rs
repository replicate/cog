//! Predictor trait for different backends.

use std::time::{Duration, Instant};

/// Result of a prediction.
#[derive(Debug, Clone)]
pub struct PredictionResult {
    /// The output value as JSON.
    pub output: serde_json::Value,
    /// Time taken for the prediction.
    pub predict_time: Option<Duration>,
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
pub struct PredictionGuard {
    start_time: Instant,
    metrics: PredictionMetrics,
}

impl PredictionGuard {
    /// Start a new prediction, recording the start time.
    pub fn new() -> Self {
        Self {
            start_time: Instant::now(),
            metrics: PredictionMetrics::default(),
        }
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
}

/// A predict function trait object that can be stored in AppState.
///
/// Takes JSON input, returns JSON output or error.
/// This is a trait object rather than a Box so it can be wrapped in Arc for cloning.
pub type PredictFn = dyn Fn(serde_json::Value) -> Result<PredictionResult, PredictionError> + Send + Sync;
