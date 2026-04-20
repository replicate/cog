//! HTTP route handlers.

use std::sync::Arc;

use axum::{
    Router,
    extract::{DefaultBodyLimit, Path, State},
    http::{HeaderMap, StatusCode},
    response::{IntoResponse, Json},
    routing::{get, post, put},
};
use serde::{Deserialize, Serialize};

#[cfg(test)]
use crate::health::Health;
use crate::health::{HealthResponse, SetupResult};
use crate::predictor::PredictionError;
use crate::service::{CreatePredictionError, HealthSnapshot, PredictionService};
use crate::version::VersionInfo;
use crate::webhook::{TraceContext, WebhookConfig, WebhookEventType, WebhookSender};

#[derive(Debug, Serialize)]
pub struct HealthCheckResponse {
    pub status: HealthResponse,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub setup: Option<SetupResult>,
    pub version: VersionInfo,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub user_healthcheck_error: Option<String>,
}

impl HealthCheckResponse {
    pub fn from_snapshot(snapshot: HealthSnapshot, user_healthcheck_error: Option<String>) -> Self {
        // Determine response status
        let status = if user_healthcheck_error.is_some() {
            HealthResponse::Unhealthy
        } else if snapshot.is_busy() {
            HealthResponse::Busy
        } else {
            snapshot.state.into()
        };

        Self {
            status,
            setup: snapshot.setup_result,
            version: snapshot.version,
            user_healthcheck_error,
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
    /// Per-prediction context made available to predictors via `current_scope().context`.
    #[serde(default)]
    pub context: std::collections::HashMap<String, String>,
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
    // SAFETY: SystemTime::now() is always after UNIX_EPOCH on any reasonable system.
    // This cannot fail unless the system clock is set before 1970.
    let timestamp = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .expect("system clock is before 1970")
        .as_nanos();
    format!("pred_{:x}", timestamp)
}

/// Root discovery endpoint — returns a map of available API endpoints.
///
/// Restores the `GET /` endpoint from cog <= 0.16.x for service discovery.
/// `cog_version` reports the Python SDK version when available (matching the
/// old Python server behaviour), falling back to the coglet runtime version.
async fn root(State(service): State<Arc<PredictionService>>) -> Json<serde_json::Value> {
    let version = service.version();
    let cog_version = version.python_sdk.as_deref().unwrap_or(version.coglet);
    let mut doc = serde_json::json!({
        "cog_version": cog_version,
        "docs_url": "/docs",
        "openapi_url": "/openapi.json",
        "shutdown_url": "/shutdown",
        "healthcheck_url": "/health-check",
        "predictions_url": "/predictions",
        "predictions_idempotent_url": "/predictions/{prediction_id}",
        "predictions_cancel_url": "/predictions/{prediction_id}/cancel",
    });

    if service.supports_training().await {
        let obj = doc.as_object_mut().expect("doc is an object");
        obj.insert("trainings_url".to_string(), serde_json::json!("/trainings"));
        obj.insert(
            "trainings_idempotent_url".to_string(),
            serde_json::json!("/trainings/{training_id}"),
        );
        obj.insert(
            "trainings_cancel_url".to_string(),
            serde_json::json!("/trainings/{training_id}/cancel"),
        );
    }

    Json(doc)
}

async fn health_check(State(service): State<Arc<PredictionService>>) -> Json<HealthCheckResponse> {
    tracing::trace!("Health check endpoint called");
    let snapshot = service.health().await;
    tracing::trace!(
        state = ?snapshot.state,
        available_slots = snapshot.available_slots,
        total_slots = snapshot.total_slots,
        has_setup_result = snapshot.setup_result.is_some(),
        "Health snapshot retrieved"
    );

    // Run user healthcheck if ready (even when busy — healthcheck health
    // and slot availability are orthogonal concerns).
    let user_healthcheck_error = if snapshot.is_ready() {
        write_readiness_file();

        // Run user-defined healthcheck
        tracing::trace!("Running user-defined healthcheck");
        match service.healthcheck().await {
            Ok(result) if result.is_healthy() => {
                tracing::trace!("User healthcheck passed");
                None
            }
            Ok(result) => {
                tracing::debug!(error = ?result.error, "User healthcheck reported unhealthy");
                result.error
            }
            Err(e) => {
                tracing::debug!(error = %e, "User healthcheck returned error");
                Some(format!("Healthcheck error: {}", e))
            }
        }
    } else {
        tracing::trace!(state = ?snapshot.state, "Skipping user healthcheck (not ready)");
        None
    };

    let response = HealthCheckResponse::from_snapshot(snapshot, user_healthcheck_error);
    tracing::trace!(status = ?response.status, "Health check response");
    Json(response)
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
        context: Default::default(),
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
        request.context,
        request.webhook,
        request.webhook_events_filter,
        respond_async,
        trace_context,
        false,
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
        context: Default::default(),
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
    if let Some(response) = service.get_prediction_response(&prediction_id) {
        return (StatusCode::ACCEPTED, Json(response));
    }

    let respond_async = should_respond_async(&headers);
    let trace_context = extract_trace_context(&headers);
    create_prediction_with_id(
        service,
        prediction_id,
        request.input,
        request.context,
        request.webhook,
        request.webhook_events_filter,
        respond_async,
        trace_context,
        false,
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

    match WebhookSender::with_trace_context(
        webhook_url.clone(),
        WebhookConfig {
            events_filter: events,
            ..Default::default()
        },
        trace_context,
    ) {
        Ok(sender) => Some(sender),
        Err(e) => {
            tracing::error!(url = %webhook_url, error = %e, "Failed to create webhook sender");
            None
        }
    }
}

#[allow(clippy::too_many_arguments)]
async fn create_prediction_with_id(
    service: Arc<PredictionService>,
    prediction_id: String,
    mut input: serde_json::Value,
    context: std::collections::HashMap<String, String>,
    webhook: Option<String>,
    webhook_events_filter: Vec<WebhookEventType>,
    respond_async: bool,
    trace_context: TraceContext,
    is_training: bool,
) -> (StatusCode, Json<serde_json::Value>) {
    // Strip unknown fields and validate in one pass. Unknown inputs are
    // silently dropped to match Replicate's historical API behavior.
    let (stripped, validation_result) = if is_training {
        service.strip_and_validate_train_input(&mut input).await
    } else {
        service.strip_and_validate_input(&mut input).await
    };
    if !stripped.is_empty() {
        tracing::warn!(
            prediction_id = %prediction_id,
            fields = ?stripped,
            "Stripped unknown input fields"
        );
    }
    if let Err(errors) = validation_result {
        let detail: Vec<serde_json::Value> = errors
            .into_iter()
            .map(|e| {
                serde_json::json!({
                    "loc": ["body", "input", e.field],
                    "msg": e.msg,
                    "type": e.error_type
                })
            })
            .collect();
        return (
            StatusCode::UNPROCESSABLE_ENTITY,
            Json(serde_json::json!({ "detail": detail })),
        );
    }

    let webhook_sender = build_webhook_sender(
        webhook.clone(),
        webhook_events_filter.clone(),
        trace_context.clone(),
    );

    // Submit prediction: creates Prediction, acquires slot, registers in service
    let (handle, unregistered_slot) = match service
        .submit_prediction(prediction_id.clone(), input.clone(), webhook_sender)
        .await
    {
        Ok(r) => r,
        Err(CreatePredictionError::NotReady) => {
            let msg = PredictionError::NotReady.to_string();
            return (
                StatusCode::SERVICE_UNAVAILABLE,
                Json(serde_json::json!({
                    "error": msg,
                    "status": "failed"
                })),
            );
        }
        Err(CreatePredictionError::AtCapacity) => {
            return (
                StatusCode::CONFLICT,
                Json(serde_json::json!({
                    "error": "At capacity - all prediction slots busy",
                    "status": "failed"
                })),
            );
        }
    };

    let prediction = unregistered_slot.prediction();

    // Async mode: spawn background task, return immediately
    if respond_async {
        let service_clone = Arc::clone(&service);
        let id_for_cleanup = prediction_id.clone();
        let context_async = context.clone();
        tokio::spawn(async move {
            let _result = service_clone
                .predict(unregistered_slot, input, context_async)
                .await;
            // Prediction state is already updated by predict() internally
            // (set_succeeded/set_failed/set_canceled fire webhooks automatically)
            service_clone.remove_prediction(&id_for_cleanup);
        });

        return (
            StatusCode::ACCEPTED,
            Json(serde_json::json!({
                "id": prediction_id,
                "status": "starting"
            })),
        );
    }

    // Sync mode: spawn prediction into a background task so the slot lifetime
    // is NOT tied to the HTTP connection. If the client disconnects, the
    // SyncPredictionGuard fires cancel, but the slot/permit stays alive in the
    // spawned task until the worker acknowledges the cancel.
    let mut sync_guard = handle.sync_guard(Arc::clone(&service));

    let service_bg = Arc::clone(&service);
    let id_bg = prediction_id.clone();
    let result_rx = {
        let (tx, rx) = tokio::sync::oneshot::channel();
        tokio::spawn(async move {
            let result = service_bg.predict(unregistered_slot, input, context).await;
            // Prediction state is already updated by predict() internally
            service_bg.remove_prediction(&id_bg);
            let _ = tx.send(result);
        });
        rx
    };

    // Wait for the prediction to complete. If the connection drops, axum
    // cancels this future, dropping sync_guard which fires cancel.
    let result = match result_rx.await {
        Ok(r) => r,
        Err(_) => {
            // Background task panicked or was cancelled
            Err(PredictionError::Failed("prediction task lost".to_string()))
        }
    };

    // Extract predict_time and user metrics from the Prediction mutex.
    // User metrics are read here so they are available for all response paths
    // (success, failure, and cancellation), not just the success path.
    let (predict_time, user_metrics) = prediction
        .try_lock()
        .map(|p| (p.elapsed().as_secs_f64(), p.metrics().clone()))
        .unwrap_or_default();

    // Disarm guard - prediction completed normally (connection still alive)
    sync_guard.disarm();

    // Build metrics object: user metrics + predict_time
    let build_metrics = |user_metrics: &std::collections::HashMap<String, serde_json::Value>| {
        let mut m = serde_json::Map::new();
        for (k, v) in user_metrics {
            m.insert(k.clone(), v.clone());
        }
        m.insert("predict_time".to_string(), serde_json::json!(predict_time));
        serde_json::Value::Object(m)
    };

    match result {
        Ok(r) => {
            let metrics = build_metrics(&r.metrics);
            (
                StatusCode::OK,
                Json(serde_json::json!({
                    "id": prediction_id,
                    "output": r.output,
                    "logs": r.logs,
                    "status": "succeeded",
                    "metrics": metrics
                })),
            )
        }
        Err(PredictionError::InvalidInput(msg)) => {
            let metrics = build_metrics(&user_metrics);
            (
                StatusCode::UNPROCESSABLE_ENTITY,
                Json(serde_json::json!({
                    "id": prediction_id,
                    "error": msg,
                    "logs": "",
                    "status": "failed",
                    "metrics": metrics
                })),
            )
        }
        Err(PredictionError::NotReady) => {
            let msg = PredictionError::NotReady.to_string();
            (
                StatusCode::SERVICE_UNAVAILABLE,
                Json(serde_json::json!({
                    "id": prediction_id,
                    "error": msg,
                    "logs": "",
                    "status": "failed"
                })),
            )
        }
        Err(PredictionError::Failed(msg)) => {
            let metrics = build_metrics(&user_metrics);
            (
                // 200 for parity with Python - prediction failure is data, not HTTP error
                StatusCode::OK,
                Json(serde_json::json!({
                    "id": prediction_id,
                    "error": msg,
                    "logs": "",
                    "status": "failed",
                    "metrics": metrics
                })),
            )
        }
        Err(PredictionError::Cancelled) => {
            let metrics = build_metrics(&user_metrics);
            (
                StatusCode::OK,
                Json(serde_json::json!({
                    "id": prediction_id,
                    "logs": "",
                    "status": "canceled",
                    "metrics": metrics
                })),
            )
        }
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

// Training routes — same dispatch as predictions but validated against
// TrainingInput schema instead of Input.

async fn create_training(
    State(service): State<Arc<PredictionService>>,
    headers: HeaderMap,
    body: Option<Json<PredictionRequest>>,
) -> impl IntoResponse {
    let request = body.map(|Json(r)| r).unwrap_or_else(|| PredictionRequest {
        id: None,
        input: serde_json::json!({}),
        context: Default::default(),
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
        request.context,
        request.webhook,
        request.webhook_events_filter,
        respond_async,
        trace_context,
        true,
    )
    .await
}

async fn create_training_idempotent(
    State(service): State<Arc<PredictionService>>,
    Path(training_id): Path<String>,
    headers: HeaderMap,
    body: Option<Json<PredictionRequest>>,
) -> impl IntoResponse {
    let request = body.map(|Json(r)| r).unwrap_or_else(|| PredictionRequest {
        id: None,
        input: serde_json::json!({}),
        context: Default::default(),
        webhook: None,
        webhook_events_filter: default_webhook_events_filter(),
    });

    if let Some(ref req_id) = request.id
        && req_id != &training_id
    {
        return (
            StatusCode::UNPROCESSABLE_ENTITY,
            Json(serde_json::json!({
                "detail": [{
                    "loc": ["body", "id"],
                    "msg": "training ID must match the ID supplied in the URL",
                    "type": "value_error"
                }]
            })),
        );
    }

    // Idempotent: return existing state if already submitted
    if let Some(response) = service.get_prediction_response(&training_id) {
        return (StatusCode::ACCEPTED, Json(response));
    }

    let respond_async = should_respond_async(&headers);
    let trace_context = extract_trace_context(&headers);
    create_prediction_with_id(
        service,
        training_id,
        request.input,
        request.context,
        request.webhook,
        request.webhook_events_filter,
        respond_async,
        trace_context,
        true,
    )
    .await
}

async fn cancel_training(
    State(service): State<Arc<PredictionService>>,
    Path(training_id): Path<String>,
) -> impl IntoResponse {
    cancel_prediction(State(service), Path(training_id)).await
}

/// Maximum HTTP request body size (100 MiB).
///
/// Axum defaults to 2 MiB which is too small for models that accept large
/// inline inputs (e.g. base64-encoded images).  Inputs that exceed the IPC
/// frame limit are automatically spilled to disk by `build_slot_request`.
const MAX_HTTP_BODY_SIZE: usize = 100 * 1024 * 1024;

pub fn routes(service: Arc<PredictionService>) -> Router {
    Router::new()
        .route("/", get(root))
        .route("/health-check", get(health_check))
        .route("/openapi.json", get(openapi_schema))
        .route("/shutdown", post(shutdown))
        .route("/predictions", post(create_prediction))
        .route("/predictions/{id}", put(create_prediction_idempotent))
        .route("/predictions/{id}/cancel", post(cancel_prediction))
        .route("/trainings", post(create_training))
        .route("/trainings/{id}", put(create_training_idempotent))
        .route("/trainings/{id}/cancel", post(cancel_training))
        .layer(DefaultBodyLimit::max(MAX_HTTP_BODY_SIZE))
        .with_state(service)
}

#[cfg(test)]
mod tests {
    use super::*;
    use axum::body::Body;
    use axum::http::{Request, StatusCode};
    use http_body_util::BodyExt;
    use tower::ServiceExt;

    async fn response_json(response: axum::response::Response) -> serde_json::Value {
        let body = response.into_body();
        let bytes = body.collect().await.unwrap().to_bytes();
        serde_json::from_slice(&bytes).unwrap()
    }

    #[tokio::test]
    async fn health_check_returns_status_and_version() {
        let service = Arc::new(PredictionService::new_no_pool().with_health(Health::Starting));
        let app = routes(service);

        let response = app
            .oneshot(Request::get("/health-check").body(Body::empty()).unwrap())
            .await
            .unwrap();

        assert_eq!(response.status(), StatusCode::OK);

        let json = response_json(response).await;
        assert_eq!(json["status"], "STARTING");
        assert!(json["version"]["coglet"].is_string());
    }

    #[tokio::test]
    async fn health_check_unknown_when_no_predictor() {
        let service = Arc::new(PredictionService::new_no_pool());
        let app = routes(service);

        let response = app
            .oneshot(Request::get("/health-check").body(Body::empty()).unwrap())
            .await
            .unwrap();

        let json = response_json(response).await;
        assert_eq!(json["status"], "UNKNOWN");
    }

    #[tokio::test]
    async fn predictions_returns_503_when_not_ready() {
        let service = Arc::new(PredictionService::new_no_pool());
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
        assert!(
            json["error"]
                .as_str()
                .unwrap()
                .contains("Setup has not finished yet")
        );
    }

    #[tokio::test]
    async fn openapi_returns_503_when_schema_not_available() {
        let service = Arc::new(PredictionService::new_no_pool());
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
        let service = Arc::new(PredictionService::new_no_pool());
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

    // --- Tests with MockOrchestrator for full prediction flow ---

    use crate::PredictionOutput;
    use crate::bridge::protocol::SlotId;
    use crate::orchestrator::Orchestrator;
    use crate::permit::PermitPool;
    use std::sync::Mutex as StdMutex;
    use std::sync::atomic::{AtomicUsize, Ordering};

    /// Mock orchestrator that immediately completes predictions.
    struct MockOrchestrator {
        register_count: AtomicUsize,
        complete_immediately: bool,
    }

    impl MockOrchestrator {
        fn new() -> Self {
            Self {
                register_count: AtomicUsize::new(0),
                complete_immediately: true,
            }
        }

        /// Create a mock that never completes predictions (for capacity tests).
        fn never_complete() -> Self {
            Self {
                register_count: AtomicUsize::new(0),
                complete_immediately: false,
            }
        }
    }

    #[async_trait::async_trait]
    impl Orchestrator for MockOrchestrator {
        async fn register_prediction(
            &self,
            _slot_id: SlotId,
            prediction: Arc<StdMutex<crate::prediction::Prediction>>,
            _idle_sender: tokio::sync::oneshot::Sender<crate::permit::SlotIdleToken>,
        ) {
            self.register_count.fetch_add(1, Ordering::SeqCst);
            if self.complete_immediately {
                let mut pred = prediction.lock().unwrap();
                pred.set_succeeded(PredictionOutput::Single(serde_json::json!("mock output")));
            }
        }

        async fn cancel_by_prediction_id(
            &self,
            _prediction_id: &str,
        ) -> Result<(), crate::orchestrator::OrchestratorError> {
            Ok(())
        }

        async fn healthcheck(
            &self,
        ) -> Result<crate::orchestrator::HealthcheckResult, crate::orchestrator::OrchestratorError>
        {
            Ok(crate::orchestrator::HealthcheckResult::healthy())
        }

        async fn shutdown(&self) -> Result<(), crate::orchestrator::OrchestratorError> {
            Ok(())
        }
    }

    async fn create_test_pool(num_slots: usize) -> Arc<PermitPool> {
        use crate::bridge::codec::JsonCodec;
        use crate::bridge::protocol::SlotRequest;
        use futures::StreamExt;
        use tokio::net::UnixStream;

        let pool = Arc::new(PermitPool::new(num_slots));
        for _ in 0..num_slots {
            let (a, b) = UnixStream::pair().unwrap();
            let (_read_a, write_a) = a.into_split();
            let (read_b, _write_b) = b.into_split();

            // Spawn a task to consume messages from the socket (prevents broken pipe)
            let mut reader =
                tokio_util::codec::FramedRead::new(read_b, JsonCodec::<SlotRequest>::new());
            tokio::spawn(async move { while reader.next().await.is_some() {} });

            let writer =
                tokio_util::codec::FramedWrite::new(write_a, JsonCodec::<SlotRequest>::new());
            pool.add_permit(SlotId::new(), writer);
        }
        pool
    }

    async fn create_ready_service() -> Arc<PredictionService> {
        let service = Arc::new(PredictionService::new_no_pool());
        let pool = create_test_pool(2).await;
        let orchestrator = Arc::new(MockOrchestrator::new());
        service.set_orchestrator(pool, orchestrator).await;
        service.set_health(Health::Ready).await;
        service
    }

    #[tokio::test]
    async fn health_check_ready_with_orchestrator() {
        let service = create_ready_service().await;
        let app = routes(service);

        let response = app
            .oneshot(Request::get("/health-check").body(Body::empty()).unwrap())
            .await
            .unwrap();

        assert_eq!(response.status(), StatusCode::OK);
        let json = response_json(response).await;
        assert_eq!(json["status"], "READY");
    }

    #[tokio::test]
    async fn prediction_sync_success() {
        let service = create_ready_service().await;
        let app = routes(service);

        let response = app
            .oneshot(
                Request::post("/predictions")
                    .header("content-type", "application/json")
                    .body(Body::from(r#"{"input":{"prompt":"hello"}}"#))
                    .unwrap(),
            )
            .await
            .unwrap();

        assert_eq!(response.status(), StatusCode::OK);
        let json = response_json(response).await;
        assert_eq!(json["status"], "succeeded");
        assert_eq!(json["output"], "mock output");
        assert!(json["id"].is_string());
    }

    #[tokio::test]
    async fn prediction_async_returns_accepted() {
        let service = create_ready_service().await;
        let app = routes(service);

        let response = app
            .oneshot(
                Request::post("/predictions")
                    .header("content-type", "application/json")
                    .header("prefer", "respond-async")
                    .body(Body::from(r#"{"input":{}}"#))
                    .unwrap(),
            )
            .await
            .unwrap();

        assert_eq!(response.status(), StatusCode::ACCEPTED);
        let json = response_json(response).await;
        assert_eq!(json["status"], "starting");
    }

    #[tokio::test]
    async fn prediction_with_custom_id() {
        let service = create_ready_service().await;
        let app = routes(service);

        let response = app
            .oneshot(
                Request::post("/predictions")
                    .header("content-type", "application/json")
                    .body(Body::from(r#"{"id":"my-pred-123","input":{}}"#))
                    .unwrap(),
            )
            .await
            .unwrap();

        assert_eq!(response.status(), StatusCode::OK);
        let json = response_json(response).await;
        assert_eq!(json["id"], "my-pred-123");
        assert_eq!(json["status"], "succeeded");
    }

    #[tokio::test]
    async fn prediction_idempotent_put() {
        let service = create_ready_service().await;
        let app = routes(service);

        let response = app
            .oneshot(
                Request::put("/predictions/idempotent-123")
                    .header("content-type", "application/json")
                    .body(Body::from(r#"{"input":{}}"#))
                    .unwrap(),
            )
            .await
            .unwrap();

        assert_eq!(response.status(), StatusCode::OK);
        let json = response_json(response).await;
        assert_eq!(json["id"], "idempotent-123");
        assert_eq!(json["status"], "succeeded");
    }

    #[tokio::test]
    async fn prediction_idempotent_id_mismatch() {
        let service = create_ready_service().await;
        let app = routes(service);

        let response = app
            .oneshot(
                Request::put("/predictions/url-id")
                    .header("content-type", "application/json")
                    .body(Body::from(r#"{"id":"body-id","input":{}}"#))
                    .unwrap(),
            )
            .await
            .unwrap();

        assert_eq!(response.status(), StatusCode::UNPROCESSABLE_ENTITY);
        let json = response_json(response).await;
        assert!(
            json["detail"][0]["msg"]
                .as_str()
                .unwrap()
                .contains("must match")
        );
    }

    #[tokio::test]
    async fn prediction_at_capacity() {
        let service = Arc::new(PredictionService::new_no_pool());
        let pool = create_test_pool(1).await; // Only 1 slot
        // Use never_complete so the first prediction holds the slot
        let orchestrator = Arc::new(MockOrchestrator::never_complete());
        service.set_orchestrator(pool, orchestrator).await;
        service.set_health(Health::Ready).await;

        // Use async mode so first request doesn't block
        let app = routes(Arc::clone(&service));
        let _resp1 = app
            .oneshot(
                Request::post("/predictions")
                    .header("content-type", "application/json")
                    .header("prefer", "respond-async")
                    .body(Body::from(r#"{"input":{}}"#))
                    .unwrap(),
            )
            .await
            .unwrap();

        // Small delay to let async task acquire the slot
        tokio::time::sleep(tokio::time::Duration::from_millis(10)).await;

        // Second request should get 409 Conflict (at capacity)
        let app2 = routes(service);
        let response = app2
            .oneshot(
                Request::post("/predictions")
                    .header("content-type", "application/json")
                    .body(Body::from(r#"{"input":{}}"#))
                    .unwrap(),
            )
            .await
            .unwrap();

        assert_eq!(response.status(), StatusCode::CONFLICT);
        let json = response_json(response).await;
        assert!(json["error"].as_str().unwrap().contains("capacity"));
    }

    #[tokio::test]
    async fn health_check_busy_when_at_capacity() {
        let service = Arc::new(PredictionService::new_no_pool());
        let pool = create_test_pool(1).await;
        // Use never_complete so the prediction holds the slot
        let orchestrator = Arc::new(MockOrchestrator::never_complete());
        service.set_orchestrator(pool, orchestrator).await;
        service.set_health(Health::Ready).await;

        // Use async to hold the slot
        let app = routes(Arc::clone(&service));
        let _resp = app
            .oneshot(
                Request::post("/predictions")
                    .header("content-type", "application/json")
                    .header("prefer", "respond-async")
                    .body(Body::from(r#"{"input":{}}"#))
                    .unwrap(),
            )
            .await
            .unwrap();

        tokio::time::sleep(tokio::time::Duration::from_millis(10)).await;

        // Health should show BUSY
        let app2 = routes(service);
        let response = app2
            .oneshot(Request::get("/health-check").body(Body::empty()).unwrap())
            .await
            .unwrap();

        let json = response_json(response).await;
        assert_eq!(json["status"], "BUSY");
    }

    #[tokio::test]
    async fn training_routes_work() {
        let service = create_ready_service().await;
        let app = routes(service);

        let response = app
            .oneshot(
                Request::post("/trainings")
                    .header("content-type", "application/json")
                    .body(Body::from(r#"{"input":{}}"#))
                    .unwrap(),
            )
            .await
            .unwrap();

        assert_eq!(response.status(), StatusCode::OK);
        let json = response_json(response).await;
        assert_eq!(json["status"], "succeeded");
    }

    #[tokio::test]
    async fn training_idempotent_put() {
        let service = create_ready_service().await;
        let app = routes(service);

        let response = app
            .oneshot(
                Request::put("/trainings/train-123")
                    .header("content-type", "application/json")
                    .body(Body::from(r#"{"input":{}}"#))
                    .unwrap(),
            )
            .await
            .unwrap();

        assert_eq!(response.status(), StatusCode::OK);
        let json = response_json(response).await;
        assert_eq!(json["id"], "train-123");
        assert_eq!(json["status"], "succeeded");
    }

    #[tokio::test]
    async fn training_idempotent_id_mismatch() {
        let service = create_ready_service().await;
        let app = routes(service);

        let response = app
            .oneshot(
                Request::put("/trainings/url-id")
                    .header("content-type", "application/json")
                    .body(Body::from(r#"{"id":"body-id","input":{}}"#))
                    .unwrap(),
            )
            .await
            .unwrap();

        assert_eq!(response.status(), StatusCode::UNPROCESSABLE_ENTITY);
        let json = response_json(response).await;
        assert!(
            json["detail"][0]["msg"]
                .as_str()
                .unwrap()
                .contains("must match")
        );
    }

    #[tokio::test]
    async fn shutdown_triggers_service_shutdown() {
        let service = create_ready_service().await;
        let mut rx = service.shutdown_rx();
        let app = routes(service);

        assert!(!*rx.borrow());

        let response = app
            .oneshot(Request::post("/shutdown").body(Body::empty()).unwrap())
            .await
            .unwrap();

        assert_eq!(response.status(), StatusCode::OK);
        rx.changed().await.unwrap();
        assert!(*rx.borrow());
    }

    #[tokio::test]
    async fn root_returns_discovery_document() {
        let service = Arc::new(PredictionService::new_no_pool());
        let app = routes(service);

        let response = app
            .oneshot(Request::get("/").body(Body::empty()).unwrap())
            .await
            .unwrap();

        assert_eq!(response.status(), StatusCode::OK);
        assert_eq!(
            response.headers().get("content-type").unwrap(),
            "application/json"
        );

        let json = response_json(response).await;
        // Without a python_sdk version set, falls back to coglet version
        assert_eq!(json["cog_version"], crate::version::COGLET_VERSION);
        assert_eq!(json["docs_url"], "/docs");
        assert_eq!(json["openapi_url"], "/openapi.json");
        assert_eq!(json["shutdown_url"], "/shutdown");
        assert_eq!(json["healthcheck_url"], "/health-check");
        assert_eq!(json["predictions_url"], "/predictions");
        assert_eq!(
            json["predictions_idempotent_url"],
            "/predictions/{prediction_id}"
        );
        assert_eq!(
            json["predictions_cancel_url"],
            "/predictions/{prediction_id}/cancel"
        );
        // No training URLs without a TrainingInput schema
        assert!(json.get("trainings_url").is_none());
        assert!(json.get("trainings_idempotent_url").is_none());
        assert!(json.get("trainings_cancel_url").is_none());
    }

    #[tokio::test]
    async fn root_includes_training_urls_when_schema_has_training() {
        let service = Arc::new(PredictionService::new_no_pool());
        // Set a schema that includes a TrainingInput component
        service
            .set_schema(serde_json::json!({
                "openapi": "3.0.2",
                "info": {"title": "Cog", "version": "0.1.0"},
                "components": {
                    "schemas": {
                        "TrainingInput": {
                            "type": "object",
                            "properties": {
                                "data": {"type": "string"}
                            }
                        }
                    }
                }
            }))
            .await;
        let app = routes(service);

        let response = app
            .oneshot(Request::get("/").body(Body::empty()).unwrap())
            .await
            .unwrap();

        assert_eq!(response.status(), StatusCode::OK);

        let json = response_json(response).await;
        // Base fields still present
        assert_eq!(json["predictions_url"], "/predictions");
        // Training URLs included
        assert_eq!(json["trainings_url"], "/trainings");
        assert_eq!(json["trainings_idempotent_url"], "/trainings/{training_id}");
        assert_eq!(
            json["trainings_cancel_url"],
            "/trainings/{training_id}/cancel"
        );
    }

    #[tokio::test]
    async fn root_cog_version_prefers_python_sdk() {
        let version = VersionInfo::new().with_python_sdk("0.14.0".to_string());
        let service = Arc::new(PredictionService::new_no_pool().with_version(version));
        let app = routes(service);

        let response = app
            .oneshot(Request::get("/").body(Body::empty()).unwrap())
            .await
            .unwrap();

        let json = response_json(response).await;
        assert_eq!(json["cog_version"], "0.14.0");
    }
}
