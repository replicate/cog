//! HTTP route handlers.

use std::sync::Arc;

use axum::{
    extract::State,
    http::StatusCode,
    response::{IntoResponse, Json},
    routing::{get, post},
    Router,
};
use serde::{Deserialize, Serialize};

use coglet_core::{Health, PredictionError, PredictionGuard};

use crate::server::AppState;

/// Health check response.
#[derive(Debug, Serialize)]
pub struct HealthCheckResponse {
    pub status: Health,
}

/// Prediction request body.
#[derive(Debug, Deserialize)]
pub struct PredictionRequest {
    /// Input to the predictor.
    pub input: serde_json::Value,
}

/// GET /health-check
async fn health_check(State(state): State<Arc<AppState>>) -> Json<HealthCheckResponse> {
    let health = *state.health.read().await;
    Json(HealthCheckResponse { status: health })
}

/// POST /predictions
async fn create_prediction(
    State(state): State<Arc<AppState>>,
    Json(request): Json<PredictionRequest>,
) -> impl IntoResponse {
    // Check if predictor is ready
    let health = *state.health.read().await;
    if health != Health::Ready {
        return (
            StatusCode::SERVICE_UNAVAILABLE,
            Json(serde_json::json!({
                "error": "Predictor not ready",
                "status": "failed"
            })),
        );
    }

    // Get the predict function
    let Some(ref predict_fn) = state.predict_fn else {
        return (
            StatusCode::SERVICE_UNAVAILABLE,
            Json(serde_json::json!({
                "error": "No predictor loaded",
                "status": "failed"
            })),
        );
    };

    // Try to acquire a prediction slot (non-blocking)
    // Returns 503 immediately if at capacity rather than queueing
    let Ok(_permit) = state.slots.try_acquire() else {
        return (
            StatusCode::SERVICE_UNAVAILABLE,
            Json(serde_json::json!({
                "error": "At capacity - all prediction slots busy",
                "status": "failed"
            })),
        );
    };

    // Clone the Arc for the blocking task
    let predict_fn = Arc::clone(predict_fn);
    let input = request.input;

    // Run prediction in a blocking task with RAII lifecycle guard
    // PyO3 hands control to Python's embedded interpreter which manages
    // GIL internally - background threads, CUDA, I/O all work normally
    let result = tokio::task::spawn_blocking(move || {
        let guard = PredictionGuard::new();
        let result = predict_fn(input);
        let metrics = guard.finish();
        (result, metrics)
    })
    .await;
    // _permit drops here, releasing the slot

    match result {
        Ok((Ok(prediction), metrics)) => (
            StatusCode::OK,
            Json(serde_json::json!({
                "output": prediction.output,
                "status": "succeeded",
                "metrics": {
                    "predict_time": metrics.predict_time.map(|d| d.as_secs_f64())
                }
            })),
        ),
        Ok((Err(PredictionError::InvalidInput(msg)), metrics)) => (
            StatusCode::UNPROCESSABLE_ENTITY,
            Json(serde_json::json!({
                "error": msg,
                "status": "failed",
                "metrics": {
                    "predict_time": metrics.predict_time.map(|d| d.as_secs_f64())
                }
            })),
        ),
        Ok((Err(PredictionError::NotReady), _)) => (
            StatusCode::SERVICE_UNAVAILABLE,
            Json(serde_json::json!({
                "error": "Predictor not ready",
                "status": "failed"
            })),
        ),
        Ok((Err(PredictionError::Failed(msg)), metrics)) => (
            StatusCode::INTERNAL_SERVER_ERROR,
            Json(serde_json::json!({
                "error": msg,
                "status": "failed",
                "metrics": {
                    "predict_time": metrics.predict_time.map(|d| d.as_secs_f64())
                }
            })),
        ),
        Err(join_err) => (
            StatusCode::INTERNAL_SERVER_ERROR,
            Json(serde_json::json!({
                "error": format!("Prediction task panicked: {}", join_err),
                "status": "failed"
            })),
        ),
    }
}

/// Build the router with all routes.
pub fn routes(state: Arc<AppState>) -> Router {
    Router::new()
        .route("/health-check", get(health_check))
        .route("/predictions", post(create_prediction))
        .with_state(state)
}
