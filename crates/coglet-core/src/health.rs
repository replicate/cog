//! Health status types for coglet runtime.

use serde::{Deserialize, Serialize};

/// Health status of the coglet runtime.
#[derive(Debug, Clone, Copy, Default, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "SCREAMING_SNAKE_CASE")]
pub enum Health {
    /// Just started, status unknown
    #[default]
    Unknown,
    /// Running setup()
    Starting,
    /// Ready to accept predictions
    Ready,
    /// At capacity (all slots in use)
    Busy,
    /// setup() failed
    SetupFailed,
    /// Unrecoverable error
    Defunct,
}

/// Status of the setup phase.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum SetupStatus {
    Starting,
    Succeeded,
    Failed,
}
