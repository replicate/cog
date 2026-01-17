//! coglet: Rust execution engine for cog models.

mod health;
mod prediction;
mod predictor;
mod supervisor;
mod version;

pub mod bridge;
pub mod orchestrator;
pub mod permit;
pub mod service;
pub mod transport;
pub mod webhook;

pub use supervisor::{PredictionHandle, PredictionState, PredictionSupervisor, SyncPredictionGuard};

pub use health::{Health, SetupResult, SetupStatus};
pub use prediction::{CancellationToken, Prediction, PredictionOutput, PredictionStatus};
pub use predictor::{
    AsyncPredictFn, PredictFn, PredictFuture, PredictionError, PredictionGuard, PredictionMetrics,
    PredictionResult,
};
pub use version::{VersionInfo, COGLET_VERSION};
