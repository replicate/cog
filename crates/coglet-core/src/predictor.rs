//! Predictor trait for different backends.

/// Result of a prediction.
#[derive(Debug, Clone)]
pub struct PredictionResult {
    /// The output value as JSON.
    pub output: serde_json::Value,
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
