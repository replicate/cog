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
#[cfg(feature = "tracing")]
pub mod trace;
pub mod transport;
pub mod webhook;
pub mod worker;
mod worker_tracing_layer;

#[cfg(feature = "tracing")]
#[macro_export]
macro_rules! cog_span {
    ($level:ident, $($span:tt)*) => {{
        if $crate::trace::is_active() {
            tracing::$level!(target: "coglet::trace", $($span)*)
        } else {
            tracing::Span::none()
        }
    }};
}

#[cfg(not(feature = "tracing"))]
#[macro_export]
macro_rules! cog_span {
    ($level:ident, $($span:tt)*) => {{ tracing::Span::none() }};
}

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

pub fn bounded_attribute_value(value: &str) -> &str {
    let end = value.floor_char_boundary(value.len().min(128));
    &value[..end]
}

#[cfg(test)]
mod tests {
    use super::bounded_attribute_value;

    #[test]
    fn bounds_attributes_without_splitting_utf8() {
        let value = format!("{}é", "x".repeat(127));

        assert_eq!(bounded_attribute_value(&value), "x".repeat(127));
    }
}

/// Install the `ring` TLS crypto provider for `rustls`.
///
/// Must be called once before any `reqwest::Client` is created. Safe to call
/// multiple times — subsequent calls are no-ops (returns `Err` which we ignore).
pub fn install_crypto_provider() {
    let _ = rustls::crypto::ring::default_provider().install_default();
}
