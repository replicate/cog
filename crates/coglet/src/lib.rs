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
pub mod worker;

pub use orchestrator::Orchestrator;

pub use supervisor::{PredictionHandle, PredictionState, PredictionSupervisor, SyncPredictionGuard};

pub use health::{Health, SetupResult, SetupStatus};
pub use prediction::{CancellationToken, Prediction, PredictionOutput, PredictionStatus};
pub use predictor::{PredictionError, PredictionGuard, PredictionMetrics, PredictionResult};
pub use version::{VersionInfo, COGLET_VERSION};
pub use worker::{PredictHandler, PredictResult, SetupError, SetupLogHook, SlotSender, WorkerConfig, run_worker};
pub use service::{CreatePredictionError, HealthSnapshot, PredictionService};
