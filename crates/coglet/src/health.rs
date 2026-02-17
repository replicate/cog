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

/// Response-only health status (includes transient states like UNHEALTHY).
/// Used in HTTP responses but not stored as internal state.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "SCREAMING_SNAKE_CASE")]
pub enum HealthResponse {
    Unknown,
    Starting,
    Ready,
    Busy,
    SetupFailed,
    Defunct,
    /// User-defined healthcheck failed (transient - not stored)
    Unhealthy,
}

impl From<Health> for HealthResponse {
    fn from(health: Health) -> Self {
        match health {
            Health::Unknown => HealthResponse::Unknown,
            Health::Starting => HealthResponse::Starting,
            Health::Ready => HealthResponse::Ready,
            Health::Busy => HealthResponse::Busy,
            Health::SetupFailed => HealthResponse::SetupFailed,
            Health::Defunct => HealthResponse::Defunct,
        }
    }
}

/// Status of the setup phase.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum SetupStatus {
    Starting,
    Succeeded,
    Failed,
}

/// Result of the setup phase.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct SetupResult {
    /// When setup started (ISO 8601 format).
    pub started_at: String,
    /// When setup completed (ISO 8601 format), if finished.
    #[serde(skip_serializing_if = "Option::is_none")]
    pub completed_at: Option<String>,
    /// Status of setup.
    #[serde(skip_serializing_if = "Option::is_none")]
    pub status: Option<SetupStatus>,
    /// Captured logs during setup.
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub logs: String,
}

impl SetupResult {
    /// Create a new SetupResult with the current time as started_at.
    pub fn starting() -> Self {
        Self {
            started_at: chrono::Utc::now().to_rfc3339(),
            completed_at: None,
            status: Some(SetupStatus::Starting),
            logs: String::new(),
        }
    }

    /// Mark setup as succeeded with accumulated logs.
    pub fn succeeded(mut self, logs: String) -> Self {
        self.completed_at = Some(chrono::Utc::now().to_rfc3339());
        self.status = Some(SetupStatus::Succeeded);
        self.logs = logs;
        self
    }

    /// Mark setup as failed with error logs.
    pub fn failed(mut self, logs: String) -> Self {
        self.completed_at = Some(chrono::Utc::now().to_rfc3339());
        self.status = Some(SetupStatus::Failed);
        self.logs = logs;
        self
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn health_default_is_unknown() {
        assert_eq!(Health::default(), Health::Unknown);
    }

    #[test]
    fn health_serializes_screaming_snake_case() {
        insta::assert_json_snapshot!(
            "health_all_variants",
            [
                Health::Unknown,
                Health::Starting,
                Health::Ready,
                Health::Busy,
                Health::SetupFailed,
                Health::Defunct,
            ]
        );
    }

    #[test]
    fn health_response_serializes_screaming_snake_case() {
        insta::assert_json_snapshot!(
            "health_response_all_variants",
            [
                HealthResponse::Unknown,
                HealthResponse::Starting,
                HealthResponse::Ready,
                HealthResponse::Busy,
                HealthResponse::SetupFailed,
                HealthResponse::Defunct,
                HealthResponse::Unhealthy,
            ]
        );
    }

    #[test]
    fn health_deserializes_screaming_snake_case() {
        assert_eq!(
            serde_json::from_str::<Health>("\"READY\"").unwrap(),
            Health::Ready
        );
        assert_eq!(
            serde_json::from_str::<Health>("\"SETUP_FAILED\"").unwrap(),
            Health::SetupFailed
        );
    }

    #[test]
    fn setup_status_serializes_lowercase() {
        insta::assert_json_snapshot!(
            "setup_status_all_variants",
            [
                SetupStatus::Starting,
                SetupStatus::Succeeded,
                SetupStatus::Failed,
            ]
        );
    }

    #[test]
    fn setup_status_deserializes_lowercase() {
        assert_eq!(
            serde_json::from_str::<SetupStatus>("\"succeeded\"").unwrap(),
            SetupStatus::Succeeded
        );
    }
}
