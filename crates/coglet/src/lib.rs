//! coglet: Rust execution engine for cog models.

mod health;
mod prediction;
mod predictor;
mod version;

pub mod bridge;

pub use health::{Health, SetupResult, SetupStatus};
pub use prediction::{CancellationToken, Prediction, PredictionOutput, PredictionStatus};
pub use predictor::{
    AsyncPredictFn, PredictFn, PredictFuture, PredictionError, PredictionGuard, PredictionMetrics,
    PredictionResult,
};
pub use version::{VersionInfo, COGLET_VERSION};
