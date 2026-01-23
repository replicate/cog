//! coglet: Rust execution engine for cog models.

mod health;
mod prediction;
mod predictor;
mod supervisor;
mod version;

pub mod bridge;
mod fd_redirect;
pub mod orchestrator;
pub mod permit;
pub mod service;
pub mod transport;
pub mod webhook;
pub mod worker;
mod worker_tracing_layer;

pub use orchestrator::Orchestrator;

pub use supervisor::{
    PredictionHandle, PredictionState, PredictionSupervisor, SyncPredictionGuard,
};

pub use health::{Health, SetupResult, SetupStatus};
pub use prediction::{CancellationToken, Prediction, PredictionOutput, PredictionStatus};
pub use predictor::{PredictionError, PredictionGuard, PredictionMetrics, PredictionResult};
pub use service::{CreatePredictionError, HealthSnapshot, PredictionService};
pub use version::{COGLET_VERSION, VersionInfo};
pub use worker::{
    PredictHandler, PredictResult, SetupError, SetupLogHook, SlotSender, WorkerConfig, run_worker,
};
