//! coglet: Rust execution engine for cog models.

mod health;
pub mod input_validation;
mod prediction;
mod predictor;
mod version;

pub mod bridge;
mod fd_redirect;
pub mod orchestrator;
pub mod permit;
pub mod service;
mod setup_log_accumulator;
pub mod transport;
pub mod webhook;
pub mod worker;
mod worker_tracing_layer;

pub use orchestrator::Orchestrator;

pub use service::{PredictionHandle, SyncPredictionGuard};

pub use health::{Health, SetupResult, SetupStatus};
pub use input_validation::InputValidator;
pub use prediction::{CancellationToken, Prediction, PredictionOutput, PredictionStatus};
pub use predictor::{PredictionError, PredictionGuard, PredictionMetrics, PredictionResult};
pub use service::{CreatePredictionError, HealthSnapshot, PredictionService};
pub use setup_log_accumulator::{SetupLogAccumulator, drain_accumulated_logs};
pub use version::{COGLET_VERSION, VersionInfo};
pub use worker::{
    PredictHandler, PredictResult, SetupError, SetupLogHook, SlotSender, WorkerConfig, run_worker,
};
