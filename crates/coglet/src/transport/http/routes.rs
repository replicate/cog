//! HTTP route handlers.

use std::sync::Arc;

use axum::{
    Router,
    extract::{Path, State},
    http::{HeaderMap, StatusCode},
    response::{IntoResponse, Json},
    routing::{get, post, put},
};
use serde::{Deserialize, Serialize};

use crate::{
    CreatePredictionError, Health, HealthSnapshot, PredictionError, PredictionService, SetupResult,
    VersionInfo, WebhookConfig, WebhookEventType, WebhookSender,
};

/// Health check response.
#[derive(Debug, Serialize)]
pub struct HealthCheckResponse {
    pub status: Health,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub setup: Option<SetupResult>,
    pub version: VersionInfo,
}

impl From<HealthSnapshot> for HealthCheckResponse {
    fn from(snapshot: HealthSnapshot) -> Self {
        // If ready but at capacity, report BUSY
        let status = if snapshot.is_busy() {
            Health::Busy
        } else {
            snapshot.state
        };

        Self {
            status,
            setup: snapshot.setup_result,
            version: snapshot.version,
        }
    }
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
async fn health_check(State(service): State<Arc<PredictionService>>) -> Json<HealthCheckResponse> {
    let snapshot = service.health().await;

    // Write K8s readiness file if ready
    if snapshot.is_ready() && !snapshot.is_busy() {
        write_readiness_file();
    }

    Json(snapshot.into())
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
    State(service): State<Arc<PredictionService>>,
    headers: HeaderMap,
    Json(request): Json<PredictionRequest>,
) -> impl IntoResponse {
    let prediction_id = request.id.unwrap_or_else(generate_prediction_id);
    let respond_async = should_respond_async(&headers);
    create_prediction_with_id(
        service,
        prediction_id,
        request.input,
        request.webhook,
        request.webhook_events_filter,
        respond_async,
    )
    .await
}

/// PUT /predictions/{id} - idempotent prediction creation
async fn create_prediction_idempotent(
    State(service): State<Arc<PredictionService>>,
    Path(prediction_id): Path<String>,
    headers: HeaderMap,
    Json(request): Json<PredictionRequest>,
) -> impl IntoResponse {
    // If request has ID, it must match URL
    if let Some(ref req_id) = request.id
        && req_id != &prediction_id
    {
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

    // Check if prediction with this ID is already in-flight
    if service.prediction_exists(&prediction_id).await {
        return (
            StatusCode::ACCEPTED,
            Json(serde_json::json!({
                "id": prediction_id,
                "status": "processing"
            })),
        );
    }

    // Not running, create new prediction with the specified ID
    let respond_async = should_respond_async(&headers);
    create_prediction_with_id(
        service,
        prediction_id,
        request.input,
        request.webhook,
        request.webhook_events_filter,
        respond_async,
    )
    .await
}

/// Build a webhook sender if webhook URL is provided.
fn build_webhook_sender(
    webhook: Option<String>,
    events_filter: Vec<WebhookEventType>,
) -> Option<WebhookSender> {
    let webhook_url = webhook?;
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
    service: Arc<PredictionService>,
    prediction_id: String,
    input: serde_json::Value,
    webhook: Option<String>,
    webhook_events_filter: Vec<WebhookEventType>,
    respond_async: bool,
) -> (StatusCode, Json<serde_json::Value>) {
    // Build webhook sender for async start notification
    let start_webhook_sender = build_webhook_sender(webhook.clone(), webhook_events_filter.clone());

    // Build webhook sender for the prediction (will be used in Drop)
    let prediction_webhook = build_webhook_sender(webhook, webhook_events_filter);

    // Try to create prediction (acquires slot, checks health)
    let mut prediction = match service
        .create_prediction(prediction_id.clone(), prediction_webhook)
        .await
    {
        Ok(p) => p,
        Err(CreatePredictionError::NotReady) => {
            return (
                StatusCode::SERVICE_UNAVAILABLE,
                Json(serde_json::json!({
                    "error": "Predictor not ready",
                    "status": "failed"
                })),
            );
        }
        Err(CreatePredictionError::AtCapacity) => {
            // 409 for parity with Python
            // Python response: {"detail": "Already running a prediction"}
            return (
                StatusCode::CONFLICT,
                Json(serde_json::json!({
                    "error": "At capacity - all prediction slots busy",
                    "status": "failed"
                })),
            );
        }
        Err(CreatePredictionError::AlreadyExists(id)) => {
            return (
                StatusCode::CONFLICT,
                Json(serde_json::json!({
                    "error": format!("Prediction {} already exists", id),
                    "status": "failed"
                })),
            );
        }
    };

    // If respond_async, spawn background task and return immediately
    if respond_async {
        // Send start webhook
        if let Some(ref ws) = start_webhook_sender {
            ws.send(
                WebhookEventType::Start,
                &serde_json::json!({
                    "id": prediction_id,
                    "status": "starting",
                    "input": input,
                }),
            );
        }

        let service_clone = Arc::clone(&service);
        let id_for_unregister = prediction_id.clone();
        tokio::spawn(async move {
            let _ = service_clone.predict(&mut prediction, input).await;
            service_clone.unregister_prediction(&id_for_unregister).await;
            // prediction drops here, sending terminal webhook
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
    let result = service.predict(&mut prediction, input).await;
    let predict_time = prediction.elapsed().as_secs_f64();
    service.unregister_prediction(&prediction_id).await;

    match result {
        Ok(r) => (
            StatusCode::OK,
            Json(serde_json::json!({
                "id": prediction_id,
                "output": r.output,
                "logs": r.logs,
                "status": "succeeded",
                "metrics": { "predict_time": predict_time }
            })),
        ),
        Err(PredictionError::InvalidInput(msg)) => (
            StatusCode::UNPROCESSABLE_ENTITY,
            Json(serde_json::json!({
                "id": prediction_id,
                "error": msg,
                "logs": "",
                "status": "failed",
                "metrics": { "predict_time": predict_time }
            })),
        ),
        Err(PredictionError::NotReady) => (
            StatusCode::SERVICE_UNAVAILABLE,
            Json(serde_json::json!({
                "id": prediction_id,
                "error": "Predictor not ready",
                "logs": "",
                "status": "failed"
            })),
        ),
        Err(PredictionError::Failed(msg)) => (
            StatusCode::INTERNAL_SERVER_ERROR,
            Json(serde_json::json!({
                "id": prediction_id,
                "error": msg,
                "logs": "",
                "status": "failed",
                "metrics": { "predict_time": predict_time }
            })),
        ),
        Err(PredictionError::Cancelled) => (
            StatusCode::OK,
            Json(serde_json::json!({
                "id": prediction_id,
                "logs": "",
                "status": "canceled",
                "metrics": { "predict_time": predict_time }
            })),
        ),
    }
}

/// POST /predictions/{id}/cancel
async fn cancel_prediction(
    State(service): State<Arc<PredictionService>>,
    Path(prediction_id): Path<String>,
) -> impl IntoResponse {
    if service.cancel(&prediction_id).await {
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
async fn shutdown(State(service): State<Arc<PredictionService>>) -> impl IntoResponse {
    tracing::info!("Shutdown requested via HTTP");
    service.trigger_shutdown();
    (StatusCode::OK, Json(serde_json::json!({})))
}

/// GET /openapi.json
async fn openapi_schema(State(service): State<Arc<PredictionService>>) -> impl IntoResponse {
    match service.schema().await {
        Some(schema) => (StatusCode::OK, Json(schema)),
        None => (
            StatusCode::SERVICE_UNAVAILABLE,
            Json(serde_json::json!({
                "error": "OpenAPI schema not available"
            })),
        ),
    }
}

// =============================================================================
// Training routes
//
// BUG-FOR-BUG COMPATIBILITY: In cog mainline, training routes use the same
// worker/service that was created for predictions with is_train=false. This
// means training routes actually call predict() instead of train(). We
// replicate this bug by routing /trainings to the same handlers as /predictions.
// =============================================================================

/// POST /trainings - same as POST /predictions (bug-for-bug)
async fn create_training(
    State(service): State<Arc<PredictionService>>,
    headers: HeaderMap,
    Json(request): Json<PredictionRequest>,
) -> impl IntoResponse {
    // BUG: This calls predict(), not train(), matching cog mainline behavior
    create_prediction(State(service), headers, Json(request)).await
}

/// PUT /trainings/{id} - same as PUT /predictions/{id} (bug-for-bug)
async fn create_training_idempotent(
    State(service): State<Arc<PredictionService>>,
    Path(training_id): Path<String>,
    headers: HeaderMap,
    Json(request): Json<PredictionRequest>,
) -> impl IntoResponse {
    // BUG: This calls predict(), not train(), matching cog mainline behavior
    create_prediction_idempotent(State(service), Path(training_id), headers, Json(request)).await
}

/// POST /trainings/{id}/cancel - same as POST /predictions/{id}/cancel
async fn cancel_training(
    State(service): State<Arc<PredictionService>>,
    Path(training_id): Path<String>,
) -> impl IntoResponse {
    cancel_prediction(State(service), Path(training_id)).await
}

/// Build the router with all routes.
pub fn routes(service: Arc<PredictionService>) -> Router {
    Router::new()
        .route("/health-check", get(health_check))
        .route("/openapi.json", get(openapi_schema))
        .route("/shutdown", post(shutdown))
        // Prediction routes
        .route("/predictions", post(create_prediction))
        .route("/predictions/{id}", put(create_prediction_idempotent))
        .route("/predictions/{id}/cancel", post(cancel_prediction))
        // Training routes (BUG: these call predict(), not train(), matching cog mainline)
        .route("/trainings", post(create_training))
        .route("/trainings/{id}", put(create_training_idempotent))
        .route("/trainings/{id}/cancel", post(cancel_training))
        .with_state(service)
}

#[cfg(test)]
mod tests {
    use super::*;
    use axum::body::Body;
    use axum::http::{Request, StatusCode};
    use http_body_util::BodyExt;
    use tower::ServiceExt;

    use crate::bridge::codec::JsonCodec;
    use crate::bridge::protocol::SlotId;
    use crate::{PermitPool, PredictionOutput, PredictionResult};
    use tokio::net::UnixStream;
    use tokio_util::codec::FramedWrite;

    /// Create a test pool with N slots backed by socket pairs.
    async fn make_test_pool(n: usize) -> Arc<PermitPool> {
        let pool = Arc::new(PermitPool::new(n));
        for _ in 0..n {
            let (a, _b) = UnixStream::pair().unwrap();
            let (_, write) = a.into_split();
            pool.add_permit(SlotId::new(), FramedWrite::new(write, JsonCodec::new()));
        }
        pool
    }

    async fn response_json(response: axum::response::Response) -> serde_json::Value {
        let body = response.into_body();
        let bytes = body.collect().await.unwrap().to_bytes();
        serde_json::from_slice(&bytes).unwrap()
    }

    #[tokio::test]
    async fn health_check_returns_status_and_version() {
        let pool = make_test_pool(1).await;
        let service = Arc::new(PredictionService::new(pool).with_health(Health::Ready));
        let app = routes(service);

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
        let pool = make_test_pool(1).await;
        let service = Arc::new(PredictionService::new(pool)); // Default health is Unknown
        let app = routes(service);

        let response = app
            .oneshot(Request::get("/health-check").body(Body::empty()).unwrap())
            .await
            .unwrap();

        let json = response_json(response).await;
        assert_eq!(json["status"], "UNKNOWN");
    }

    #[tokio::test]
    async fn health_check_returns_busy_when_at_capacity() {
        let pool = make_test_pool(1).await;
        let service = Arc::new(PredictionService::new(pool).with_health(Health::Ready));

        // Take the only slot
        let _pred = service
            .create_prediction("busy".to_string(), None)
            .await
            .unwrap();

        let app = routes(Arc::clone(&service));

        let response = app
            .oneshot(Request::get("/health-check").body(Body::empty()).unwrap())
            .await
            .unwrap();

        let json = response_json(response).await;
        assert_eq!(json["status"], "BUSY");
    }

    #[tokio::test]
    async fn predictions_returns_503_when_not_ready() {
        let pool = make_test_pool(1).await;
        let service = Arc::new(PredictionService::new(pool)); // Health is Unknown
        let app = routes(service);

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
        let pool = make_test_pool(1).await;
        let service = Arc::new(PredictionService::new(pool).with_health(Health::Ready));
        let app = routes(service);

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
    async fn predictions_success_with_sync_predictor() {
        let pool = make_test_pool(1).await;
        let service = Arc::new(
            PredictionService::new(pool)
                .with_health(Health::Ready)
                .with_predict_fn(Arc::new(|input| {
                    let name = input["name"].as_str().unwrap_or("world");
                    Ok(PredictionResult {
                        output: PredictionOutput::Single(serde_json::json!(format!(
                            "Hello, {}!",
                            name
                        ))),
                        predict_time: None,
                        logs: String::new(),
                    })
                })),
        );
        let app = routes(service);

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
        let pool = make_test_pool(1).await;
        let service = Arc::new(
            PredictionService::new(pool)
                .with_health(Health::Ready)
                .with_predict_fn(Arc::new(|_| {
                    Err(PredictionError::InvalidInput(
                        "missing required field".to_string(),
                    ))
                })),
        );
        let app = routes(service);

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

    #[tokio::test]
    async fn openapi_returns_503_when_schema_not_available() {
        let pool = make_test_pool(1).await;
        let service = Arc::new(PredictionService::new(pool));
        let app = routes(service);

        let response = app
            .oneshot(Request::get("/openapi.json").body(Body::empty()).unwrap())
            .await
            .unwrap();

        assert_eq!(response.status(), StatusCode::SERVICE_UNAVAILABLE);

        let json = response_json(response).await;
        assert!(json["error"].as_str().unwrap().contains("not available"));
    }

    #[tokio::test]
    async fn openapi_returns_schema_when_available() {
        let pool = make_test_pool(1).await;
        let service = Arc::new(PredictionService::new(pool));
        service
            .set_schema(serde_json::json!({
                "openapi": "3.0.2",
                "info": {"title": "Cog", "version": "0.1.0"}
            }))
            .await;
        let app = routes(service);

        let response = app
            .oneshot(Request::get("/openapi.json").body(Body::empty()).unwrap())
            .await
            .unwrap();

        assert_eq!(response.status(), StatusCode::OK);

        let json = response_json(response).await;
        assert_eq!(json["openapi"], "3.0.2");
        assert_eq!(json["info"]["title"], "Cog");
    }
}
