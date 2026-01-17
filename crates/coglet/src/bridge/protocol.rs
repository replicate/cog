//! Wire protocol types for parent-worker communication.
//!
//! Two channels:
//! - **Control channel** (stdin/stdout): ControlRequest/ControlResponse
//!   - Cancel, Shutdown, Ready, Idle signals
//! - **Slot sockets**: SlotRequest/SlotResponse
//!   - Prediction data, streaming logs (per-slot, avoids HOL blocking)

use serde::{Deserialize, Serialize};

// ============================================================================
// SlotId - unique identifier for prediction slots
// ============================================================================

/// Unique identifier for a prediction slot.
///
/// Uses UUID v4 for guaranteed uniqueness. Impossible to confuse with array
/// indices or accidentally reuse. Generated once per slot at worker startup.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Serialize, Deserialize)]
#[serde(transparent)]
pub struct SlotId(uuid::Uuid);

impl SlotId {
    /// Generate a new unique slot ID.
    pub fn new() -> Self {
        Self(uuid::Uuid::new_v4())
    }

    /// Get the underlying UUID.
    pub fn as_uuid(&self) -> &uuid::Uuid {
        &self.0
    }

    /// Parse a SlotId from string (UUID format).
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

// ============================================================================
// Control channel protocol (stdin/stdout)
// ============================================================================

/// Control messages from parent to worker.
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(tag = "type", rename_all = "snake_case")]
pub enum ControlRequest {
    /// Cancel prediction on a slot.
    Cancel {
        /// Unique slot ID to cancel.
        slot: SlotId,
    },

    /// Graceful shutdown - finish current work and exit.
    Shutdown,
}

/// Control messages from worker to parent.
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(tag = "type", rename_all = "snake_case")]
pub enum ControlResponse {
    /// Worker is ready to accept predictions.
    Ready {
        /// Slot IDs for each socket (index 0 = first socket, etc).
        /// Parent uses these IDs for all subsequent slot communication.
        slots: Vec<SlotId>,
        /// OpenAPI schema for the predictor.
        #[serde(skip_serializing_if = "Option::is_none")]
        schema: Option<serde_json::Value>,
    },

    /// Log message (used during setup before slots are active).
    Log {
        /// Log source (stdout or stderr).
        source: LogSource,
        /// Log data.
        data: String,
    },

    /// Slot is now idle (prediction completed, ready for next).
    Idle {
        /// Unique slot ID that became idle.
        slot: SlotId,
    },

    /// Slot prediction was cancelled.
    Cancelled {
        /// Unique slot ID that was cancelled.
        slot: SlotId,
    },

    /// Slot failed (poisoned, will not accept more predictions).
    Failed {
        /// Unique slot ID that failed.
        slot: SlotId,
        /// Error message.
        error: String,
    },

    /// Worker is shutting down.
    ShuttingDown,
}

// ============================================================================
// SlotOutcome - type-safe completion status (prevents Idle if poisoned)
// ============================================================================

/// Outcome of a slot operation - enforces that poisoned slots produce Failed.
///
/// This type makes it impossible to accidentally send `Idle` for a poisoned slot.
/// Use `into_control_response()` to get the appropriate `ControlResponse`.
#[derive(Debug)]
pub enum SlotOutcome {
    /// Slot completed normally, ready for more work.
    Idle(SlotId),
    /// Slot is poisoned, will not accept more predictions.
    Poisoned { slot: SlotId, error: String },
}

impl SlotOutcome {
    /// Create an idle outcome (slot ready for more work).
    pub fn idle(slot: SlotId) -> Self {
        Self::Idle(slot)
    }

    /// Create a poisoned outcome (slot permanently failed).
    pub fn poisoned(slot: SlotId, error: impl Into<String>) -> Self {
        Self::Poisoned {
            slot,
            error: error.into(),
        }
    }

    /// Get the slot ID.
    pub fn slot_id(&self) -> SlotId {
        match self {
            Self::Idle(slot) => *slot,
            Self::Poisoned { slot, .. } => *slot,
        }
    }

    /// Check if this outcome indicates the slot is poisoned.
    pub fn is_poisoned(&self) -> bool {
        matches!(self, Self::Poisoned { .. })
    }

    /// Convert to the appropriate ControlResponse.
    ///
    /// This is the ONLY way to create Idle/Failed responses from a completion,
    /// ensuring poisoned slots always produce Failed.
    pub fn into_control_response(self) -> ControlResponse {
        match self {
            Self::Idle(slot) => ControlResponse::Idle { slot },
            Self::Poisoned { slot, error } => ControlResponse::Failed { slot, error },
        }
    }
}

// ============================================================================
// Slot socket protocol (per-slot data channel)
// ============================================================================

/// Messages from parent to worker on slot socket.
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(tag = "type", rename_all = "snake_case")]
pub enum SlotRequest {
    /// Run a prediction.
    Predict {
        /// Unique prediction ID.
        id: String,
        /// Input to the predictor (JSON object).
        input: serde_json::Value,
    },
}

/// Messages from worker to parent on slot socket.
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(tag = "type", rename_all = "snake_case")]
pub enum SlotResponse {
    /// Log output during prediction (streaming).
    Log {
        /// Log source.
        source: LogSource,
        /// Log data.
        data: String,
    },

    /// Streaming output (for generators).
    Output {
        /// The output value.
        output: serde_json::Value,
    },

    /// Prediction completed successfully.
    Done {
        /// Prediction ID.
        id: String,
        /// Final output (for non-generators, or None if already streamed).
        #[serde(skip_serializing_if = "Option::is_none")]
        output: Option<serde_json::Value>,
        /// Prediction time in seconds.
        predict_time: f64,
    },

    /// Prediction failed.
    Failed {
        /// Prediction ID.
        id: String,
        /// Error message.
        error: String,
    },

    /// Prediction was cancelled.
    Cancelled {
        /// Prediction ID.
        id: String,
    },
}

/// Log source for streaming logs.
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

    // Fixed slot ID for deterministic tests
    fn test_slot_id() -> SlotId {
        SlotId(uuid::Uuid::parse_str("550e8400-e29b-41d4-a716-446655440000").unwrap())
    }

    // Control channel tests
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

    // Slot socket tests
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
