//! HTTP route handlers.

use std::sync::Arc;

use axum::{
    extract::{Path, State},
    http::{HeaderMap, StatusCode},
    response::{IntoResponse, Json},
    routing::{get, post, put},
    Router,
};
use serde::{Deserialize, Serialize};

use coglet_core::{
    Health, PredictionError, PredictionGuard, SetupResult, VersionInfo,
    WebhookConfig, WebhookEventType, WebhookSender,
};

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
    /// Webhook URL to send prediction updates to.
    pub webhook: Option<String>,
    /// Filter for which webhook events to send.
    /// Defaults to all events: ["start", "output", "logs", "completed"]
    #[serde(default = "default_webhook_events_filter")]
    pub webhook_events_filter: Vec<WebhookEventType>,
}

fn default_webhook_events_filter() -> Vec<WebhookEventType> {
    vec![
        WebhookEventType::Start,
        WebhookEventType::Output,
        WebhookEventType::Logs,
        WebhookEventType::Completed,
    ]
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

/// Check if the request should be handled asynchronously.
fn should_respond_async(headers: &HeaderMap) -> bool {
    headers
        .get("prefer")
        .and_then(|v| v.to_str().ok())
        .map(|v| v == "respond-async")
        .unwrap_or(false)
}

/// POST /predictions
async fn create_prediction(
    State(state): State<Arc<AppState>>,
    headers: HeaderMap,
    Json(request): Json<PredictionRequest>,
) -> impl IntoResponse {
    let prediction_id = request.id.unwrap_or_else(generate_prediction_id);
    let respond_async = should_respond_async(&headers);
    create_prediction_with_id(state, prediction_id, request.input, request.webhook, request.webhook_events_filter, respond_async).await
}

/// PUT /predictions/{id} - idempotent prediction creation
async fn create_prediction_idempotent(
    State(state): State<Arc<AppState>>,
    Path(prediction_id): Path<String>,
    headers: HeaderMap,
    Json(request): Json<PredictionRequest>,
) -> impl IntoResponse {
    // If request has ID, it must match URL
    if let Some(ref req_id) = request.id {
        if req_id != &prediction_id {
            return (
                StatusCode::UNPROCESSABLE_ENTITY,
                Json(serde_json::json!({
                    "detail": [{
                        "loc": ["body", "id"],
                        "msg": "prediction ID must match the ID supplied in the URL",
                        "type": "value_error"
                    }]
                })),
            );
        }
    }

    // Check if prediction with this ID is already in-flight
    {
        let predictions = state.predictions.lock().await;
        if predictions.contains_key(&prediction_id) {
            // Already running - return 202 with current state
            return (
                StatusCode::ACCEPTED,
                Json(serde_json::json!({
                    "id": prediction_id,
                    "status": "processing"
                })),
            );
        }
    }

    // Not running, create new prediction with the specified ID
    let respond_async = should_respond_async(&headers);
    create_prediction_with_id(state, prediction_id, request.input, request.webhook, request.webhook_events_filter, respond_async).await
}

/// Build a webhook sender if webhook URL is provided.
fn build_webhook_sender(
    webhook: Option<String>,
    events_filter: Vec<WebhookEventType>,
) -> Option<WebhookSender> {
    let webhook_url = webhook?;
    
    // Convert filter to HashSet for O(1) lookup
    let events: std::collections::HashSet<_> = events_filter.into_iter().collect();
    
    Some(WebhookSender::new(
        webhook_url,
        WebhookConfig {
            events_filter: events,
            ..Default::default()
        },
    ))
}

/// Shared logic for creating predictions (used by both POST and PUT)
async fn create_prediction_with_id(
    state: Arc<AppState>,
    prediction_id: String,
    input: serde_json::Value,
    webhook: Option<String>,
    webhook_events_filter: Vec<WebhookEventType>,
    respond_async: bool,
) -> (StatusCode, Json<serde_json::Value>) {
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

    // Try to acquire a prediction slot
    let permit = match Arc::clone(&state.slots).try_acquire_owned() {
        Ok(p) => p,
        Err(_) => {
            return (
                StatusCode::SERVICE_UNAVAILABLE,
                Json(serde_json::json!({
                    "error": "At capacity - all prediction slots busy",
                    "status": "failed"
                })),
            );
        }
    };

    let guard = PredictionGuard::new();
    let cancel_token = guard.cancel_token();
    
    state.register_prediction(prediction_id.clone(), cancel_token.clone()).await;

    // Build webhook sender if webhook URL provided
    let webhook_sender = build_webhook_sender(webhook, webhook_events_filter);

    // If respond_async, return immediately and run prediction in background
    if respond_async {
        let state_clone = Arc::clone(&state);
        let prediction_id_clone = prediction_id.clone();
        
        // Send start webhook if configured
        if let Some(ref ws) = webhook_sender {
            ws.send(WebhookEventType::Start, &serde_json::json!({
                "id": prediction_id,
                "status": "starting",
                "input": input,
            }));
        }
        
        tokio::spawn(async move {
            run_prediction_with_webhook(
                state_clone,
                prediction_id_clone,
                input,
                guard,
                webhook_sender,
                permit,
            ).await;
        });
        
        return (
            StatusCode::ACCEPTED,
            Json(serde_json::json!({
                "id": prediction_id,
                "status": "starting"
            })),
        );
    }

    // Synchronous mode - run and wait for result
    run_prediction_sync(state, prediction_id, input, guard, permit).await
}

/// Run prediction synchronously and return result.
async fn run_prediction_sync(
    state: Arc<AppState>,
    prediction_id: String,
    input: serde_json::Value,
    guard: PredictionGuard,
    _permit: tokio::sync::OwnedSemaphorePermit,
) -> (StatusCode, Json<serde_json::Value>) {
    let result = if let Some(ref async_predict_fn) = state.async_predict_fn {
        let predict_fn = Arc::clone(async_predict_fn);
        predict_fn(input).await
    } else if let Some(ref predict_fn) = state.predict_fn {
        let predict_fn = Arc::clone(predict_fn);
        match tokio::task::spawn_blocking(move || predict_fn(input)).await {
            Ok(result) => result,
            Err(join_err) => {
                state.unregister_prediction(&prediction_id).await;
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
        state.unregister_prediction(&prediction_id).await;
        return (
            StatusCode::SERVICE_UNAVAILABLE,
            Json(serde_json::json!({
                "error": "No predictor loaded",
                "status": "failed"
            })),
        );
    };

    let metrics = guard.finish();
    state.unregister_prediction(&prediction_id).await;

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

/// Run prediction with webhook notifications (for async mode).
async fn run_prediction_with_webhook(
    state: Arc<AppState>,
    prediction_id: String,
    input: serde_json::Value,
    guard: PredictionGuard,
    webhook_sender: Option<WebhookSender>,
    _permit: tokio::sync::OwnedSemaphorePermit,
) {
    let result = if let Some(ref async_predict_fn) = state.async_predict_fn {
        let predict_fn = Arc::clone(async_predict_fn);
        predict_fn(input.clone()).await
    } else if let Some(ref predict_fn) = state.predict_fn {
        let predict_fn = Arc::clone(predict_fn);
        match tokio::task::spawn_blocking(move || predict_fn(input.clone())).await {
            Ok(result) => result,
            Err(join_err) => {
                let response = serde_json::json!({
                    "id": prediction_id,
                    "error": format!("Prediction task panicked: {}", join_err),
                    "status": "failed"
                });
                if let Some(ref ws) = webhook_sender {
                    ws.send_terminal(WebhookEventType::Completed, &response).await;
                }
                state.unregister_prediction(&prediction_id).await;
                return;
            }
        }
    } else {
        let response = serde_json::json!({
            "id": prediction_id,
            "error": "No predictor loaded",
            "status": "failed"
        });
        if let Some(ref ws) = webhook_sender {
            ws.send_terminal(WebhookEventType::Completed, &response).await;
        }
        state.unregister_prediction(&prediction_id).await;
        return;
    };

    let metrics = guard.finish();
    state.unregister_prediction(&prediction_id).await;

    let response = match result {
        Ok(prediction) => serde_json::json!({
            "id": prediction_id,
            "output": prediction.output,
            "status": "succeeded",
            "metrics": {
                "predict_time": metrics.predict_time.map(|d| d.as_secs_f64())
            }
        }),
        Err(PredictionError::InvalidInput(msg)) => serde_json::json!({
            "id": prediction_id,
            "error": msg,
            "status": "failed",
            "metrics": {
                "predict_time": metrics.predict_time.map(|d| d.as_secs_f64())
            }
        }),
        Err(PredictionError::NotReady) => serde_json::json!({
            "id": prediction_id,
            "error": "Predictor not ready",
            "status": "failed"
        }),
        Err(PredictionError::Failed(msg)) => serde_json::json!({
            "id": prediction_id,
            "error": msg,
            "status": "failed",
            "metrics": {
                "predict_time": metrics.predict_time.map(|d| d.as_secs_f64())
            }
        }),
        Err(PredictionError::Cancelled) => serde_json::json!({
            "id": prediction_id,
            "status": "canceled",
            "metrics": {
                "predict_time": metrics.predict_time.map(|d| d.as_secs_f64())
            }
        }),
    };

    // Send completed webhook (with retries for terminal state)
    if let Some(ref ws) = webhook_sender {
        ws.send_terminal(WebhookEventType::Completed, &response).await;
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

/// POST /shutdown
async fn shutdown(State(state): State<Arc<AppState>>) -> impl IntoResponse {
    tracing::info!("Shutdown requested via HTTP");
    state.trigger_shutdown();
    (StatusCode::OK, Json(serde_json::json!({})))
}

/// Build the router with all routes.
pub fn routes(state: Arc<AppState>) -> Router {
    Router::new()
        .route("/health-check", get(health_check))
        .route("/shutdown", post(shutdown))
        .route("/predictions", post(create_prediction))
        .route("/predictions/{id}", put(create_prediction_idempotent))
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
