//! Wire protocol types for parent-worker communication.
//!
//! Two channels:
//! - **Control channel** (stdin/stdout): ControlRequest/ControlResponse
//!   - Cancel, Shutdown, Ready, Idle signals
//! - **Slot sockets**: SlotRequest/SlotResponse
//!   - Prediction data, streaming logs (per-slot, avoids HOL blocking)
//!
//! Uses serde JSON for debuggability. Can swap to bincode if needed.

use serde::{Deserialize, Serialize};

// ============================================================================
// Control channel protocol (stdin/stdout)
// ============================================================================

/// Control messages from parent to worker.
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(tag = "type", rename_all = "snake_case")]
pub enum ControlRequest {
    /// Cancel prediction on a slot.
    Cancel {
        /// Slot index to cancel.
        slot: usize,
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
        /// OpenAPI schema for the predictor.
        #[serde(skip_serializing_if = "Option::is_none")]
        schema: Option<serde_json::Value>,
    },

    /// Slot is now idle (prediction completed, ready for next).
    Idle {
        /// Slot index that became idle.
        slot: usize,
    },

    /// Slot prediction was cancelled.
    Cancelled {
        /// Slot index that was cancelled.
        slot: usize,
    },

    /// Slot failed (poisoned, will not accept more predictions).
    Failed {
        /// Slot index that failed.
        slot: usize,
        /// Error message.
        error: String,
    },

    /// Worker is shutting down.
    ShuttingDown,
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

    // Control channel tests
    #[test]
    fn control_cancel_serializes() {
        let req = ControlRequest::Cancel { slot: 2 };
        insta::assert_json_snapshot!(req);
    }

    #[test]
    fn control_shutdown_serializes() {
        let req = ControlRequest::Shutdown;
        insta::assert_json_snapshot!(req);
    }

    #[test]
    fn control_ready_serializes() {
        let resp = ControlResponse::Ready { schema: None };
        insta::assert_json_snapshot!(resp);
    }

    #[test]
    fn control_ready_with_schema_serializes() {
        let resp = ControlResponse::Ready {
            schema: Some(json!({
                "openapi": "3.0.2",
                "info": {"title": "Cog", "version": "0.1.0"}
            })),
        };
        insta::assert_json_snapshot!(resp);
    }

    #[test]
    fn control_idle_serializes() {
        let resp = ControlResponse::Idle { slot: 1 };
        insta::assert_json_snapshot!(resp);
    }

    #[test]
    fn control_cancelled_serializes() {
        let resp = ControlResponse::Cancelled { slot: 0 };
        insta::assert_json_snapshot!(resp);
    }

    #[test]
    fn control_failed_serializes() {
        let resp = ControlResponse::Failed {
            slot: 3,
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
