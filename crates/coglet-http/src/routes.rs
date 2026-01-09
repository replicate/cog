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

use coglet_core::{Health, PredictionError, PredictionGuard, VersionInfo};

use crate::server::AppState;

/// Health check response.
#[derive(Debug, Serialize)]
pub struct HealthCheckResponse {
    pub status: Health,
    pub version: VersionInfo,
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
    Json(HealthCheckResponse {
        status: health,
        version: state.version.clone(),
    })
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

    let input = request.input;
    let guard = PredictionGuard::new();

    // Run prediction - async or sync path
    let result = if let Some(ref async_predict_fn) = state.async_predict_fn {
        // Async predictor: run directly in tokio (no spawn_blocking)
        // This enables true concurrency - while one prediction awaits I/O,
        // another can run
        let predict_fn = Arc::clone(async_predict_fn);
        predict_fn(input).await
    } else if let Some(ref predict_fn) = state.predict_fn {
        // Sync predictor: run in spawn_blocking to not block tokio
        let predict_fn = Arc::clone(predict_fn);
        match tokio::task::spawn_blocking(move || predict_fn(input)).await {
            Ok(result) => result,
            Err(join_err) => {
                return (
                    StatusCode::INTERNAL_SERVER_ERROR,
                    Json(serde_json::json!({
                        "error": format!("Prediction task panicked: {}", join_err),
                        "status": "failed"
                    })),
                );
            }
        }
    } else {
        return (
            StatusCode::SERVICE_UNAVAILABLE,
            Json(serde_json::json!({
                "error": "No predictor loaded",
                "status": "failed"
            })),
        );
    };

    let metrics = guard.finish();
    // _permit drops here, releasing the slot

    match result {
        Ok(prediction) => (
            StatusCode::OK,
            Json(serde_json::json!({
                "output": prediction.output,
                "status": "succeeded",
                "metrics": {
                    "predict_time": metrics.predict_time.map(|d| d.as_secs_f64())
                }
            })),
        ),
        Err(PredictionError::InvalidInput(msg)) => (
            StatusCode::UNPROCESSABLE_ENTITY,
            Json(serde_json::json!({
                "error": msg,
                "status": "failed",
                "metrics": {
                    "predict_time": metrics.predict_time.map(|d| d.as_secs_f64())
                }
            })),
        ),
        Err(PredictionError::NotReady) => (
            StatusCode::SERVICE_UNAVAILABLE,
            Json(serde_json::json!({
                "error": "Predictor not ready",
                "status": "failed"
            })),
        ),
        Err(PredictionError::Failed(msg)) => (
            StatusCode::INTERNAL_SERVER_ERROR,
            Json(serde_json::json!({
                "error": msg,
                "status": "failed",
                "metrics": {
                    "predict_time": metrics.predict_time.map(|d| d.as_secs_f64())
                }
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
