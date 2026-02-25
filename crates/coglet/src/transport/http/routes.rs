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

#[cfg(test)]
use crate::health::Health;
use crate::health::{HealthResponse, SetupResult};
use crate::prediction::PredictionStatus;
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

async fn health_check(State(service): State<Arc<PredictionService>>) -> Json<HealthCheckResponse> {
    let snapshot = service.health().await;

    // Run user healthcheck if ready (even when busy — healthcheck health
    // and slot availability are orthogonal concerns).
    let user_healthcheck_error = if snapshot.is_ready() {
        write_readiness_file();

        // Run user-defined healthcheck
        match service.healthcheck().await {
            Ok(result) if result.is_healthy() => None,
            Ok(result) => result.error,
            Err(e) => Some(format!("Healthcheck error: {}", e)),
        }
    } else {
        None
    };

    Json(HealthCheckResponse::from_snapshot(
        snapshot,
        user_healthcheck_error,
    ))
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
    input: serde_json::Value,
    webhook: Option<String>,
    webhook_events_filter: Vec<WebhookEventType>,
    respond_async: bool,
    trace_context: TraceContext,
    is_training: bool,
) -> (StatusCode, Json<serde_json::Value>) {
    // Validate input against the appropriate schema
    let validation_result = if is_training {
        service.validate_train_input(&input).await
    } else {
        service.validate_input(&input).await
    };
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

    // Submit to supervisor (tracks lifecycle, owns webhook)
    let supervisor = service.supervisor();
    let handle = supervisor.submit(prediction_id.clone(), input.clone(), webhook_sender);

    // Try to create prediction slot (acquires permit, checks health)
    let unregistered_slot = match service.create_prediction(prediction_id.clone(), None).await {
        Ok(p) => p,
        Err(CreatePredictionError::NotReady) => {
            let msg = PredictionError::NotReady.to_string();
            supervisor.update_status(
                &prediction_id,
                PredictionStatus::Failed,
                None,
                Some(msg.clone()),
                None,
            );
            return (
                StatusCode::SERVICE_UNAVAILABLE,
                Json(serde_json::json!({
                    "error": msg,
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
                None,
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

    let prediction = unregistered_slot.prediction();
    supervisor.update_status(
        &prediction_id,
        PredictionStatus::Processing,
        None,
        None,
        None,
    );

    // Async mode: spawn background task, return immediately
    if respond_async {
        let service_clone = Arc::clone(&service);
        let supervisor_clone = Arc::clone(supervisor);
        let id_for_cleanup = prediction_id.clone();
        tokio::spawn(async move {
            let result = service_clone.predict(unregistered_slot, input).await;

            match result {
                Ok(r) => {
                    supervisor_clone.update_status(
                        &id_for_cleanup,
                        PredictionStatus::Succeeded,
                        Some(serde_json::json!(r.output)),
                        None,
                        Some(r.metrics),
                    );
                }
                Err(PredictionError::Cancelled) => {
                    supervisor_clone.update_status(
                        &id_for_cleanup,
                        PredictionStatus::Canceled,
                        None,
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
                        None,
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

    // Sync mode: spawn prediction into a background task so the slot lifetime
    // is NOT tied to the HTTP connection. If the client disconnects, the
    // SyncPredictionGuard fires cancel, but the slot/permit stays alive in the
    // spawned task until the worker acknowledges the cancel.
    let mut sync_guard = handle.sync_guard();

    let service_bg = Arc::clone(&service);
    let supervisor_bg = Arc::clone(supervisor);
    let id_bg = prediction_id.clone();
    let result_rx = {
        let (tx, rx) = tokio::sync::oneshot::channel();
        tokio::spawn(async move {
            let result = service_bg.predict(unregistered_slot, input).await;

            match &result {
                Ok(r) => {
                    supervisor_bg.update_status(
                        &id_bg,
                        PredictionStatus::Succeeded,
                        Some(serde_json::json!(r.output)),
                        None,
                        Some(r.metrics.clone()),
                    );
                }
                Err(PredictionError::Cancelled) => {
                    supervisor_bg.update_status(
                        &id_bg,
                        PredictionStatus::Canceled,
                        None,
                        None,
                        None,
                    );
                }
                Err(e) => {
                    supervisor_bg.update_status(
                        &id_bg,
                        PredictionStatus::Failed,
                        None,
                        Some(e.to_string()),
                        None,
                    );
                }
            }

            service_bg.unregister_prediction(&id_bg);
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

    let predict_time = prediction
        .try_lock()
        .map(|p| p.elapsed())
        .unwrap_or(std::time::Duration::ZERO)
        .as_secs_f64();

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
        webhook: None,
        webhook_events_filter: default_webhook_events_filter(),
    });

    // Idempotent: return existing state if already submitted
    let supervisor = service.supervisor();
    if let Some(state) = supervisor.get_state(&training_id) {
        return (StatusCode::ACCEPTED, Json(state.to_response()));
    }

    let respond_async = should_respond_async(&headers);
    let trace_context = extract_trace_context(&headers);
    create_prediction_with_id(
        service,
        training_id,
        request.input,
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
}
