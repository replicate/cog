//! coglet-core: Core runtime types and traits for coglet.

mod health;
mod predictor;

pub use health::{Health, SetupStatus};
pub use predictor::{PredictFn, PredictionError, PredictionResult};
