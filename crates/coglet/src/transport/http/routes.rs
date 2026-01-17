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

use crate::health::{Health, SetupResult};
use crate::prediction::PredictionStatus;
use crate::predictor::PredictionError;
use crate::service::{CreatePredictionError, HealthSnapshot, PredictionService};
use crate::version::VersionInfo;
use crate::webhook::{TraceContext, WebhookConfig, WebhookEventType, WebhookSender};

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

#[derive(Debug, Deserialize)]
pub struct PredictionRequest {
    pub id: Option<String>,
    #[serde(
        default = "default_empty_input",
        deserialize_with = "deserialize_input"
    )]
    pub input: serde_json::Value,
    pub webhook: Option<String>,
    #[serde(default = "default_webhook_events_filter")]
    pub webhook_events_filter: Vec<WebhookEventType>,
}

fn default_empty_input() -> serde_json::Value {
    serde_json::json!({})
}

fn deserialize_input<'de, D>(deserializer: D) -> Result<serde_json::Value, D::Error>
where
    D: serde::Deserializer<'de>,
{
    let value = serde_json::Value::deserialize(deserializer)?;
    Ok(if value.is_null() {
        serde_json::json!({})
    } else {
        value
    })
}

fn default_webhook_events_filter() -> Vec<WebhookEventType> {
    vec![
        WebhookEventType::Start,
        WebhookEventType::Output,
        WebhookEventType::Logs,
        WebhookEventType::Completed,
    ]
}

fn generate_prediction_id() -> String {
    use std::time::{SystemTime, UNIX_EPOCH};
    let timestamp = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .unwrap()
        .as_nanos();
    format!("pred_{:x}", timestamp)
}

async fn health_check(State(service): State<Arc<PredictionService>>) -> Json<HealthCheckResponse> {
    let snapshot = service.health().await;

    if snapshot.is_ready() && !snapshot.is_busy() {
        write_readiness_file();
    }

    Json(snapshot.into())
}

/// Write /var/run/cog/ready for K8s readiness probe.
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

fn should_respond_async(headers: &HeaderMap) -> bool {
    headers
        .get("prefer")
        .and_then(|v| v.to_str().ok())
        .map(|v| v == "respond-async")
        .unwrap_or(false)
}

fn extract_trace_context(headers: &HeaderMap) -> TraceContext {
    TraceContext {
        traceparent: headers
            .get("traceparent")
            .and_then(|v| v.to_str().ok())
            .map(|s| s.to_string()),
        tracestate: headers
            .get("tracestate")
            .and_then(|v| v.to_str().ok())
            .map(|s| s.to_string()),
    }
}

async fn create_prediction(
    State(service): State<Arc<PredictionService>>,
    headers: HeaderMap,
    body: Option<Json<PredictionRequest>>,
) -> impl IntoResponse {
    let request = body.map(|Json(r)| r).unwrap_or_else(|| PredictionRequest {
        id: None,
        input: serde_json::json!({}),
        webhook: None,
        webhook_events_filter: default_webhook_events_filter(),
    });
    let prediction_id = request.id.unwrap_or_else(generate_prediction_id);
    let respond_async = should_respond_async(&headers);
    let trace_context = extract_trace_context(&headers);
    create_prediction_with_id(
        service,
        prediction_id,
        request.input,
        request.webhook,
        request.webhook_events_filter,
        respond_async,
        trace_context,
    )
    .await
}

async fn create_prediction_idempotent(
    State(service): State<Arc<PredictionService>>,
    Path(prediction_id): Path<String>,
    headers: HeaderMap,
    body: Option<Json<PredictionRequest>>,
) -> impl IntoResponse {
    let request = body.map(|Json(r)| r).unwrap_or_else(|| PredictionRequest {
        id: None,
        input: serde_json::json!({}),
        webhook: None,
        webhook_events_filter: default_webhook_events_filter(),
    });

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
    let supervisor = service.supervisor();
    if let Some(state) = supervisor.get_state(&prediction_id) {
        return (StatusCode::ACCEPTED, Json(state.to_response()));
    }

    let respond_async = should_respond_async(&headers);
    let trace_context = extract_trace_context(&headers);
    create_prediction_with_id(
        service,
        prediction_id,
        request.input,
        request.webhook,
        request.webhook_events_filter,
        respond_async,
        trace_context,
    )
    .await
}

fn build_webhook_sender(
    webhook: Option<String>,
    events_filter: Vec<WebhookEventType>,
    trace_context: TraceContext,
) -> Option<WebhookSender> {
    let webhook_url = webhook?;
    let events: std::collections::HashSet<_> = events_filter.into_iter().collect();

    Some(WebhookSender::with_trace_context(
        webhook_url,
        WebhookConfig {
            events_filter: events,
            ..Default::default()
        },
        trace_context,
    ))
}

async fn create_prediction_with_id(
    service: Arc<PredictionService>,
    prediction_id: String,
    input: serde_json::Value,
    webhook: Option<String>,
    webhook_events_filter: Vec<WebhookEventType>,
    respond_async: bool,
    trace_context: TraceContext,
) -> (StatusCode, Json<serde_json::Value>) {
    let webhook_sender = build_webhook_sender(
        webhook.clone(),
        webhook_events_filter.clone(),
        trace_context.clone(),
    );

    // Submit to supervisor (tracks lifecycle, owns webhook)
    let supervisor = service.supervisor();
    let handle = supervisor.submit(prediction_id.clone(), input.clone(), webhook_sender);

    // Try to create prediction slot (acquires permit, checks health)
    let mut slot = match service.create_prediction(prediction_id.clone(), None).await {
        Ok(p) => p,
        Err(CreatePredictionError::NotReady) => {
            supervisor.update_status(
                &prediction_id,
                PredictionStatus::Failed,
                None,
                Some("Predictor not ready".to_string()),
            );
            return (
                StatusCode::SERVICE_UNAVAILABLE,
                Json(serde_json::json!({
                    "error": "Predictor not ready",
                    "status": "failed"
                })),
            );
        }
        Err(CreatePredictionError::AtCapacity) => {
            supervisor.update_status(
                &prediction_id,
                PredictionStatus::Failed,
                None,
                Some("At capacity".to_string()),
            );
            return (
                StatusCode::CONFLICT,
                Json(serde_json::json!({
                    "error": "At capacity - all prediction slots busy",
                    "status": "failed"
                })),
            );
        }
    };

    supervisor.update_status(&prediction_id, PredictionStatus::Processing, None, None);

    // Async mode: spawn background task, return immediately
    if respond_async {
        let service_clone = Arc::clone(&service);
        let supervisor_clone = Arc::clone(supervisor);
        let id_for_cleanup = prediction_id.clone();
        tokio::spawn(async move {
            let result = service_clone.predict(&mut slot, input).await;

            match result {
                Ok(r) => {
                    supervisor_clone.update_status(
                        &id_for_cleanup,
                        PredictionStatus::Succeeded,
                        Some(serde_json::json!(r.output)),
                        None,
                    );
                }
                Err(PredictionError::Cancelled) => {
                    supervisor_clone.update_status(
                        &id_for_cleanup,
                        PredictionStatus::Canceled,
                        None,
                        None,
                    );
                }
                Err(e) => {
                    supervisor_clone.update_status(
                        &id_for_cleanup,
                        PredictionStatus::Failed,
                        None,
                        Some(e.to_string()),
                    );
                }
            }

            service_clone.unregister_prediction(&id_for_cleanup);
        });

        return (
            StatusCode::ACCEPTED,
            Json(serde_json::json!({
                "id": prediction_id,
                "status": "starting"
            })),
        );
    }

    // Sync mode: use sync guard for connection-drop cancellation
    let mut sync_guard = handle.sync_guard();

    let result = service.predict(&mut slot, input).await;
    let predict_time = slot.elapsed().as_secs_f64();

    // Disarm guard - prediction completed normally
    sync_guard.disarm();

    match &result {
        Ok(r) => {
            supervisor.update_status(
                &prediction_id,
                PredictionStatus::Succeeded,
                Some(serde_json::json!(r.output)),
                None,
            );
        }
        Err(PredictionError::Cancelled) => {
            supervisor.update_status(&prediction_id, PredictionStatus::Canceled, None, None);
        }
        Err(e) => {
            supervisor.update_status(
                &prediction_id,
                PredictionStatus::Failed,
                None,
                Some(e.to_string()),
            );
        }
    }

    service.unregister_prediction(&prediction_id);

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
            // 200 for parity with Python - prediction failure is data, not HTTP error
            StatusCode::OK,
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

async fn cancel_prediction(
    State(service): State<Arc<PredictionService>>,
    Path(prediction_id): Path<String>,
) -> impl IntoResponse {
    let cancelled = service.cancel(&prediction_id);

    if cancelled {
        (StatusCode::OK, Json(serde_json::json!({})))
    } else {
        (StatusCode::NOT_FOUND, Json(serde_json::json!({})))
    }
}

async fn shutdown(State(service): State<Arc<PredictionService>>) -> impl IntoResponse {
    tracing::info!("Shutdown requested via HTTP");
    service.trigger_shutdown();
    (StatusCode::OK, Json(serde_json::json!({})))
}

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

// Training routes - bug-for-bug compatibility with cog mainline
// In cog, training routes actually call predict(), not train()

async fn create_training(
    State(service): State<Arc<PredictionService>>,
    headers: HeaderMap,
    body: Option<Json<PredictionRequest>>,
) -> impl IntoResponse {
    create_prediction(State(service), headers, body).await
}

async fn create_training_idempotent(
    State(service): State<Arc<PredictionService>>,
    Path(training_id): Path<String>,
    headers: HeaderMap,
    body: Option<Json<PredictionRequest>>,
) -> impl IntoResponse {
    create_prediction_idempotent(State(service), Path(training_id), headers, body).await
}

async fn cancel_training(
    State(service): State<Arc<PredictionService>>,
    Path(training_id): Path<String>,
) -> impl IntoResponse {
    cancel_prediction(State(service), Path(training_id)).await
}

pub fn routes(service: Arc<PredictionService>) -> Router {
    Router::new()
        .route("/health-check", get(health_check))
        .route("/openapi.json", get(openapi_schema))
        .route("/shutdown", post(shutdown))
        .route("/predictions", post(create_prediction))
        .route("/predictions/{id}", put(create_prediction_idempotent))
        .route("/predictions/{id}/cancel", post(cancel_prediction))
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
    use crate::permit::PermitPool;
    use crate::predictor::{PredictionOutput, PredictionResult};
    use tokio::net::UnixStream;
    use tokio_util::codec::FramedWrite;

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
        let service = Arc::new(PredictionService::new(pool));
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
        let service = Arc::new(PredictionService::new(pool));
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
