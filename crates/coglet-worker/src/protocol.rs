//! Wire protocol types for parent-worker communication.
//!
//! Uses serde for serialization. Currently JSON for debuggability,
//! can swap to bincode later if performance matters.

use serde::{Deserialize, Serialize};

/// Request from parent to worker.
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(tag = "type", rename_all = "snake_case")]
pub enum WorkerRequest {
    /// Run a prediction.
    Predict {
        /// Unique prediction ID.
        id: String,
        /// Input to the predictor (JSON object).
        input: serde_json::Value,
    },

    /// Cancel an in-flight prediction.
    Cancel {
        /// ID of prediction to cancel.
        id: String,
    },

    /// Graceful shutdown - finish current work and exit.
    Shutdown,
}

/// Response from worker to parent.
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(tag = "type", rename_all = "snake_case")]
pub enum WorkerResponse {
    /// Worker is ready to accept predictions.
    Ready {
        /// OpenAPI schema for the predictor (generated once at setup).
        #[serde(skip_serializing_if = "Option::is_none")]
        schema: Option<serde_json::Value>,
    },

    /// Prediction output (intermediate for streaming, or final).
    Output {
        /// Prediction ID this output belongs to.
        id: String,
        /// The output value.
        output: serde_json::Value,
        /// Status of the prediction.
        status: PredictionStatus,
        /// Captured logs (stdout/stderr).
        #[serde(default, skip_serializing_if = "String::is_empty")]
        logs: String,
        /// Prediction time in seconds (only set when status is terminal).
        #[serde(skip_serializing_if = "Option::is_none")]
        predict_time: Option<f64>,
    },

    /// Prediction was cancelled.
    Cancelled {
        /// Prediction ID that was cancelled.
        id: String,
    },

    /// Prediction failed with error.
    Error {
        /// Prediction ID that failed.
        id: String,
        /// Error message.
        error: String,
        /// Captured logs up to the error.
        #[serde(default, skip_serializing_if = "String::is_empty")]
        logs: String,
    },

    /// Worker is shutting down.
    ShuttingDown,
}

/// Prediction status.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum PredictionStatus {
    /// Still running, more output coming (for generators).
    Processing,
    /// Completed successfully.
    Succeeded,
    /// Failed with error.
    Failed,
    /// Was cancelled.
    Canceled,
}

impl PredictionStatus {
    /// Is this a terminal status?
    pub fn is_terminal(&self) -> bool {
        matches!(self, Self::Succeeded | Self::Failed | Self::Canceled)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    #[test]
    fn request_predict_serializes() {
        let req = WorkerRequest::Predict {
            id: "pred_123".to_string(),
            input: json!({"text": "hello"}),
        };
        insta::assert_json_snapshot!(req);
    }

    #[test]
    fn request_cancel_serializes() {
        let req = WorkerRequest::Cancel {
            id: "pred_123".to_string(),
        };
        insta::assert_json_snapshot!(req);
    }

    #[test]
    fn response_ready_serializes() {
        let resp = WorkerResponse::Ready { schema: None };
        insta::assert_json_snapshot!(resp);
    }

    #[test]
    fn response_ready_with_schema_serializes() {
        let resp = WorkerResponse::Ready {
            schema: Some(json!({
                "openapi": "3.0.2",
                "info": {"title": "Cog", "version": "0.1.0"}
            })),
        };
        insta::assert_json_snapshot!(resp);
    }

    #[test]
    fn response_output_serializes() {
        let resp = WorkerResponse::Output {
            id: "pred_123".to_string(),
            output: json!("hello world"),
            status: PredictionStatus::Succeeded,
            logs: "".to_string(),
            predict_time: Some(0.123),
        };
        insta::assert_json_snapshot!(resp);
    }

    #[test]
    fn response_error_serializes() {
        let resp = WorkerResponse::Error {
            id: "pred_123".to_string(),
            error: "something went wrong".to_string(),
            logs: "traceback here".to_string(),
        };
        insta::assert_json_snapshot!(resp);
    }

    #[test]
    fn prediction_status_terminal() {
        assert!(!PredictionStatus::Processing.is_terminal());
        assert!(PredictionStatus::Succeeded.is_terminal());
        assert!(PredictionStatus::Failed.is_terminal());
        assert!(PredictionStatus::Canceled.is_terminal());
    }
}
