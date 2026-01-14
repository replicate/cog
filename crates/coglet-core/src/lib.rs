//! coglet-core: Core runtime types and traits for coglet.

mod health;
mod prediction;
mod predictor;
mod service;
mod version;
pub mod webhook;

pub use health::{Health, SetupResult, SetupStatus};
pub use prediction::{Prediction, PredictionStatus};
pub use predictor::{
    AsyncPredictFn, CancellationToken, PredictFn, PredictFuture, PredictionError, PredictionGuard,
    PredictionMetrics, PredictionOutput, PredictionResult,
};
pub use service::{CreatePredictionError, HealthSnapshot, PredictionService};
pub use version::{COGLET_VERSION, VersionInfo};
pub use webhook::{WebhookConfig, WebhookEventType, WebhookSender};
