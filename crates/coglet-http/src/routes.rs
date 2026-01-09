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

#[cfg(test)]
mod tests {
    use super::*;
    use axum::body::Body;
    use axum::http::{Request, StatusCode};
    use http_body_util::BodyExt;
    use tower::ServiceExt;

    use coglet_core::{PredictionOutput, PredictionResult};

    async fn response_json(response: axum::response::Response) -> serde_json::Value {
        let body = response.into_body();
        let bytes = body.collect().await.unwrap().to_bytes();
        serde_json::from_slice(&bytes).unwrap()
    }

    #[tokio::test]
    async fn health_check_returns_status_and_version() {
        let state = Arc::new(AppState::new(1).with_health(Health::Ready));
        let app = routes(state);

        let response = app
            .oneshot(Request::get("/health-check").body(Body::empty()).unwrap())
            .await
            .unwrap();

        assert_eq!(response.status(), StatusCode::OK);

        let json = response_json(response).await;
        assert_eq!(json["status"], "READY");
        assert!(json["version"]["coglet"].is_string());
    }

    #[tokio::test]
    async fn health_check_unknown_when_no_predictor() {
        let state = Arc::new(AppState::new(1)); // Default health is Unknown
        let app = routes(state);

        let response = app
            .oneshot(Request::get("/health-check").body(Body::empty()).unwrap())
            .await
            .unwrap();

        let json = response_json(response).await;
        assert_eq!(json["status"], "UNKNOWN");
    }

    #[tokio::test]
    async fn predictions_returns_503_when_not_ready() {
        let state = Arc::new(AppState::new(1)); // Health is Unknown
        let app = routes(state);

        let response = app
            .oneshot(
                Request::post("/predictions")
                    .header("content-type", "application/json")
                    .body(Body::from(r#"{"input":{}}"#))
                    .unwrap(),
            )
            .await
            .unwrap();

        assert_eq!(response.status(), StatusCode::SERVICE_UNAVAILABLE);

        let json = response_json(response).await;
        assert_eq!(json["status"], "failed");
        assert!(json["error"].as_str().unwrap().contains("not ready"));
    }

    #[tokio::test]
    async fn predictions_returns_503_when_no_predictor_loaded() {
        let state = Arc::new(AppState::new(1).with_health(Health::Ready));
        let app = routes(state);

        let response = app
            .oneshot(
                Request::post("/predictions")
                    .header("content-type", "application/json")
                    .body(Body::from(r#"{"input":{}}"#))
                    .unwrap(),
            )
            .await
            .unwrap();

        assert_eq!(response.status(), StatusCode::SERVICE_UNAVAILABLE);

        let json = response_json(response).await;
        assert_eq!(json["status"], "failed");
        assert!(json["error"].as_str().unwrap().contains("No predictor"));
    }

    #[tokio::test]
    async fn predictions_success_with_sync_predictor() {
        let state = Arc::new(
            AppState::new(1)
                .with_health(Health::Ready)
                .with_predict_fn(Arc::new(|input| {
                    let name = input["name"].as_str().unwrap_or("world");
                    Ok(PredictionResult {
                        output: PredictionOutput::Single(serde_json::json!(format!("Hello, {}!", name))),
                        predict_time: None,
                    })
                })),
        );
        let app = routes(state);

        let response = app
            .oneshot(
                Request::post("/predictions")
                    .header("content-type", "application/json")
                    .body(Body::from(r#"{"input":{"name":"test"}}"#))
                    .unwrap(),
            )
            .await
            .unwrap();

        assert_eq!(response.status(), StatusCode::OK);

        let json = response_json(response).await;
        assert_eq!(json["status"], "succeeded");
        assert_eq!(json["output"], "Hello, test!");
        assert!(json["metrics"]["predict_time"].is_number());
    }

    #[tokio::test]
    async fn predictions_returns_error_on_invalid_input() {
        let state = Arc::new(
            AppState::new(1)
                .with_health(Health::Ready)
                .with_predict_fn(Arc::new(|_| {
                    Err(coglet_core::PredictionError::InvalidInput("missing required field".to_string()))
                })),
        );
        let app = routes(state);

        let response = app
            .oneshot(
                Request::post("/predictions")
                    .header("content-type", "application/json")
                    .body(Body::from(r#"{"input":{}}"#))
                    .unwrap(),
            )
            .await
            .unwrap();

        assert_eq!(response.status(), StatusCode::UNPROCESSABLE_ENTITY);

        let json = response_json(response).await;
        assert_eq!(json["status"], "failed");
        assert!(json["error"].as_str().unwrap().contains("missing required"));
    }
}
