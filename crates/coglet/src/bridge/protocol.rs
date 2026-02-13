//! Wire protocol types for parent-worker communication.
//!
//! Two channels:
//! - **Control channel** (stdin/stdout): Init, Cancel, Shutdown, Ready, Idle
//! - **Slot sockets**: Prediction data, streaming logs (per-slot to avoid HOL blocking)

use serde::{Deserialize, Serialize};

use super::transport::ChildTransportInfo;

/// Unique identifier for a prediction slot.
///
/// UUID v4 avoids confusion with array indices and prevents accidental reuse.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Serialize, Deserialize)]
#[serde(transparent)]
pub struct SlotId(uuid::Uuid);

impl SlotId {
    pub fn new() -> Self {
        Self(uuid::Uuid::new_v4())
    }

    pub fn as_uuid(&self) -> &uuid::Uuid {
        &self.0
    }

    pub fn parse(s: &str) -> Result<Self, uuid::Error> {
        let uuid = uuid::Uuid::parse_str(s)?;
        Ok(Self(uuid))
    }
}

impl Default for SlotId {
    fn default() -> Self {
        Self::new()
    }
}

impl std::fmt::Display for SlotId {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "{}", self.0)
    }
}

/// Control messages from parent to worker.
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(tag = "type", rename_all = "snake_case")]
pub enum ControlRequest {
    /// Initial configuration sent immediately after spawn (must be first message).
    Init {
        predictor_ref: String,
        num_slots: usize,
        transport_info: ChildTransportInfo,
        is_train: bool,
        is_async: bool,
    },

    Cancel {
        slot: SlotId,
    },

    /// Request user-defined healthcheck execution.
    Healthcheck {
        id: String,
    },

    Shutdown,
}

/// Control messages from worker to parent.
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(tag = "type", rename_all = "snake_case")]
pub enum ControlResponse {
    Ready {
        /// Slot IDs in socket order - parent uses these for all subsequent communication.
        slots: Vec<SlotId>,
        #[serde(skip_serializing_if = "Option::is_none")]
        schema: Option<serde_json::Value>,
    },

    /// Setup-phase logs (before slots are active).
    Log {
        source: LogSource,
        data: String,
    },

    /// Worker tracing log (Rust structured logging).
    WorkerLog {
        target: String,
        level: String,
        message: String,
    },

    /// Slot completed and is ready for next prediction.
    Idle {
        slot: SlotId,
    },

    Cancelled {
        slot: SlotId,
    },

    /// Slot is poisoned and will not accept more predictions.
    Failed {
        slot: SlotId,
        error: String,
    },

    /// Worker unrecoverable error - parent should poison all slots and fail all
    /// in-flight predictions. The worker will abort immediately after sending this.
    ///
    /// Reason explains *why* (e.g. "slots mutex poisoned: cannot guarantee slot isolation").
    Fatal {
        reason: String,
    },

    /// System diagnostic: logs dropped due to backpressure.
    DroppedLogs {
        count: usize,
        interval_millis: u64,
    },

    /// Result of user-defined healthcheck execution.
    HealthcheckResult {
        id: String,
        status: HealthcheckStatus,
        #[serde(skip_serializing_if = "Option::is_none")]
        error: Option<String>,
    },

    ShuttingDown,
}

/// Status of a user-defined healthcheck.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum HealthcheckStatus {
    /// Healthcheck passed (returned True or no healthcheck defined).
    Healthy,
    /// Healthcheck failed (returned False, raised exception, or timed out).
    Unhealthy,
}

/// Type-safe slot completion - ensures poisoned slots produce Failed, not Idle.
#[derive(Debug)]
pub enum SlotOutcome {
    Idle(SlotId),
    Poisoned { slot: SlotId, error: String },
}

impl SlotOutcome {
    pub fn idle(slot: SlotId) -> Self {
        Self::Idle(slot)
    }

    pub fn poisoned(slot: SlotId, error: impl Into<String>) -> Self {
        Self::Poisoned {
            slot,
            error: error.into(),
        }
    }

    pub fn slot_id(&self) -> SlotId {
        match self {
            Self::Idle(slot) => *slot,
            Self::Poisoned { slot, .. } => *slot,
        }
    }

    pub fn is_poisoned(&self) -> bool {
        matches!(self, Self::Poisoned { .. })
    }

    pub fn into_control_response(self) -> ControlResponse {
        match self {
            Self::Idle(slot) => ControlResponse::Idle { slot },
            Self::Poisoned { slot, error } => ControlResponse::Failed { slot, error },
        }
    }
}

/// Messages from parent to worker on slot socket.
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(tag = "type", rename_all = "snake_case")]
pub enum SlotRequest {
    Predict {
        id: String,
        input: serde_json::Value,
    },
}

/// Messages from worker to parent on slot socket.
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(tag = "type", rename_all = "snake_case")]
pub enum SlotResponse {
    Log {
        source: LogSource,
        data: String,
    },

    /// Streaming output chunk (for generators).
    Output {
        output: serde_json::Value,
    },

    Done {
        id: String,
        #[serde(skip_serializing_if = "Option::is_none")]
        output: Option<serde_json::Value>,
        predict_time: f64,
    },

    Failed {
        id: String,
        error: String,
    },

    Cancelled {
        id: String,
    },
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum LogSource {
    Stdout,
    Stderr,
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;
    use std::path::PathBuf;

    fn test_slot_id() -> SlotId {
        SlotId(uuid::Uuid::parse_str("550e8400-e29b-41d4-a716-446655440000").unwrap())
    }

    #[test]
    fn control_init_serializes() {
        let req = ControlRequest::Init {
            predictor_ref: "predict.py:Predictor".to_string(),
            num_slots: 2,
            transport_info: ChildTransportInfo::NamedSockets {
                dir: PathBuf::from("/tmp/coglet-123"),
                num_slots: 2,
            },
            is_train: false,
            is_async: true,
        };
        insta::assert_json_snapshot!(req);
    }

    #[test]
    fn control_cancel_serializes() {
        let req = ControlRequest::Cancel {
            slot: test_slot_id(),
        };
        insta::assert_json_snapshot!(req);
    }

    #[test]
    fn control_shutdown_serializes() {
        let req = ControlRequest::Shutdown;
        insta::assert_json_snapshot!(req);
    }

    #[test]
    fn control_healthcheck_serializes() {
        let req = ControlRequest::Healthcheck {
            id: "hc_123".to_string(),
        };
        insta::assert_json_snapshot!(req);
    }

    #[test]
    fn control_healthcheck_result_healthy_serializes() {
        let resp = ControlResponse::HealthcheckResult {
            id: "hc_123".to_string(),
            status: HealthcheckStatus::Healthy,
            error: None,
        };
        insta::assert_json_snapshot!(resp);
    }

    #[test]
    fn control_healthcheck_result_unhealthy_serializes() {
        let resp = ControlResponse::HealthcheckResult {
            id: "hc_123".to_string(),
            status: HealthcheckStatus::Unhealthy,
            error: Some("user healthcheck returned False".to_string()),
        };
        insta::assert_json_snapshot!(resp);
    }

    #[test]
    fn control_ready_serializes() {
        let resp = ControlResponse::Ready {
            slots: vec![test_slot_id()],
            schema: None,
        };
        insta::assert_json_snapshot!(resp);
    }

    #[test]
    fn control_ready_with_schema_serializes() {
        let resp = ControlResponse::Ready {
            slots: vec![test_slot_id()],
            schema: Some(json!({
                "openapi": "3.0.2",
                "info": {"title": "Cog", "version": "0.1.0"}
            })),
        };
        insta::assert_json_snapshot!(resp);
    }

    #[test]
    fn control_idle_serializes() {
        let resp = ControlResponse::Idle {
            slot: test_slot_id(),
        };
        insta::assert_json_snapshot!(resp);
    }

    #[test]
    fn control_cancelled_serializes() {
        let resp = ControlResponse::Cancelled {
            slot: test_slot_id(),
        };
        insta::assert_json_snapshot!(resp);
    }

    #[test]
    fn control_failed_serializes() {
        let resp = ControlResponse::Failed {
            slot: test_slot_id(),
            error: "segfault".to_string(),
        };
        insta::assert_json_snapshot!(resp);
    }

    #[test]
    fn slot_predict_serializes() {
        let req = SlotRequest::Predict {
            id: "pred_123".to_string(),
            input: json!({"text": "hello"}),
        };
        insta::assert_json_snapshot!(req);
    }

    #[test]
    fn slot_log_serializes() {
        let resp = SlotResponse::Log {
            source: LogSource::Stdout,
            data: "Processing...".to_string(),
        };
        insta::assert_json_snapshot!(resp);
    }

    #[test]
    fn slot_output_serializes() {
        let resp = SlotResponse::Output {
            output: json!("chunk 1"),
        };
        insta::assert_json_snapshot!(resp);
    }

    #[test]
    fn slot_done_serializes() {
        let resp = SlotResponse::Done {
            id: "pred_123".to_string(),
            output: Some(json!("final result")),
            predict_time: 1.234,
        };
        insta::assert_json_snapshot!(resp);
    }

    #[test]
    fn slot_failed_serializes() {
        let resp = SlotResponse::Failed {
            id: "pred_123".to_string(),
            error: "ValueError: invalid input".to_string(),
        };
        insta::assert_json_snapshot!(resp);
    }

    #[test]
    fn slot_cancelled_serializes() {
        let resp = SlotResponse::Cancelled {
            id: "pred_123".to_string(),
        };
        insta::assert_json_snapshot!(resp);
    }
}
