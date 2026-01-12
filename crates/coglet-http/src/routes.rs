//! HTTP route handlers.

use std::sync::Arc;

use axum::{
    extract::{Path, State},
    http::StatusCode,
    response::{IntoResponse, Json},
    routing::{get, post},
    Router,
};
use serde::{Deserialize, Serialize};

use coglet_core::{Health, PredictionError, PredictionGuard, SetupResult, VersionInfo};

use crate::server::AppState;

/// Health check response.
#[derive(Debug, Serialize)]
pub struct HealthCheckResponse {
    pub status: Health,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub setup: Option<SetupResult>,
    pub version: VersionInfo,
}

/// Prediction request body.
#[derive(Debug, Deserialize)]
pub struct PredictionRequest {
    /// Optional prediction ID (generated if not provided).
    pub id: Option<String>,
    /// Input to the predictor.
    pub input: serde_json::Value,
}

/// Generate a unique prediction ID.
fn generate_prediction_id() -> String {
    use std::time::{SystemTime, UNIX_EPOCH};
    let timestamp = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .unwrap()
        .as_nanos();
    format!("pred_{:x}", timestamp)
}

/// GET /health-check
async fn health_check(State(state): State<Arc<AppState>>) -> Json<HealthCheckResponse> {
    let base_health = *state.health.read().await;
    
    // If we're READY, check if we're actually BUSY (no slots available)
    let health = if base_health == Health::Ready {
        if state.slots.available_permits() == 0 {
            Health::Busy
        } else {
            Health::Ready
        }
    } else {
        base_health
    };

    // Write K8s readiness file if ready and running in Kubernetes
    if health == Health::Ready {
        write_readiness_file();
    }

    let setup = state.get_setup_result().await;
    
    Json(HealthCheckResponse {
        status: health,
        setup,
        version: state.version.clone(),
    })
}

/// Write /var/run/cog/ready for K8s readiness probe.
/// Only writes if KUBERNETES_SERVICE_HOST is set.
fn write_readiness_file() {
    if std::env::var("KUBERNETES_SERVICE_HOST").is_err() {
        return;
    }
    
    let dir = std::path::Path::new("/var/run/cog");
    let file = dir.join("ready");
    
    if file.exists() {
        return;
    }
    
    if let Err(e) = std::fs::create_dir_all(dir) {
        tracing::warn!(error = %e, "Failed to create /var/run/cog directory");
        return;
    }
    
    if let Err(e) = std::fs::write(&file, b"") {
        tracing::warn!(error = %e, "Failed to write readiness file");
    }
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
    let prediction_id = request.id.unwrap_or_else(generate_prediction_id);
    let guard = PredictionGuard::new();
    let cancel_token = guard.cancel_token();
    
    // Register the prediction for cancellation
    state.register_prediction(prediction_id.clone(), cancel_token.clone()).await;

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
    
    // Unregister the prediction now that it's done
    state.unregister_prediction(&prediction_id).await;
    // _permit drops here, releasing the slot

    match result {
        Ok(prediction) => (
            StatusCode::OK,
            Json(serde_json::json!({
                "id": prediction_id,
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
                "id": prediction_id,
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
                "id": prediction_id,
                "error": "Predictor not ready",
                "status": "failed"
            })),
        ),
        Err(PredictionError::Failed(msg)) => (
            StatusCode::INTERNAL_SERVER_ERROR,
            Json(serde_json::json!({
                "id": prediction_id,
                "error": msg,
                "status": "failed",
                "metrics": {
                    "predict_time": metrics.predict_time.map(|d| d.as_secs_f64())
                }
            })),
        ),
        Err(PredictionError::Cancelled) => (
            StatusCode::OK,
            Json(serde_json::json!({
                "id": prediction_id,
                "status": "canceled",
                "metrics": {
                    "predict_time": metrics.predict_time.map(|d| d.as_secs_f64())
                }
            })),
        ),
    }
}

/// POST /predictions/{id}/cancel
async fn cancel_prediction(
    State(state): State<Arc<AppState>>,
    Path(prediction_id): Path<String>,
) -> impl IntoResponse {
    if state.cancel_prediction(&prediction_id).await {
        (
            StatusCode::OK,
            Json(serde_json::json!({
                "id": prediction_id,
                "status": "canceling"
            })),
        )
    } else {
        (
            StatusCode::NOT_FOUND,
            Json(serde_json::json!({
                "error": format!("Prediction {} not found or already completed", prediction_id),
                "status": "failed"
            })),
        )
    }
}

/// Build the router with all routes.
pub fn routes(state: Arc<AppState>) -> Router {
    Router::new()
        .route("/health-check", get(health_check))
        .route("/predictions", post(create_prediction))
        .route("/predictions/{id}/cancel", post(cancel_prediction))
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
    async fn health_check_returns_busy_when_at_capacity() {
        let state = Arc::new(AppState::new(1).with_health(Health::Ready));
        
        // Acquire the only slot
        let _permit = state.slots.acquire().await.unwrap();
        
        let app = routes(Arc::clone(&state));

        let response = app
            .oneshot(Request::get("/health-check").body(Body::empty()).unwrap())
            .await
            .unwrap();

        let json = response_json(response).await;
        assert_eq!(json["status"], "BUSY");
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
