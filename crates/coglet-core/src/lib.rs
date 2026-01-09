//! coglet-core: Core runtime types and traits for coglet.

mod health;
mod predictor;
mod version;

pub use health::{Health, SetupStatus};
pub use predictor::{PredictFn, PredictionError, PredictionGuard, PredictionMetrics, PredictionOutput, PredictionResult};
pub use version::{VersionInfo, COGLET_VERSION};
