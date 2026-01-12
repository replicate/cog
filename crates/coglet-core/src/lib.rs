//! coglet-core: Core runtime types and traits for coglet.

mod health;
mod prediction;
mod predictor;
mod version;
pub mod webhook;

pub use health::{Health, SetupResult, SetupStatus};
pub use prediction::{Prediction, PredictionStatus};
pub use predictor::{
    AsyncPredictFn, CancellationToken, PredictFn, PredictFuture, PredictionError, PredictionGuard,
    PredictionMetrics, PredictionOutput, PredictionResult,
};
pub use version::{VersionInfo, COGLET_VERSION};
pub use webhook::{WebhookConfig, WebhookEventType, WebhookSender};
