//! HTTP route handlers.

use std::sync::Arc;

use axum::{extract::State, response::Json, routing::get, Router};
use serde::Serialize;

use coglet_core::Health;

use crate::server::AppState;

/// Health check response.
#[derive(Debug, Serialize)]
pub struct HealthCheckResponse {
    pub status: Health,
}

/// GET /health-check
async fn health_check(State(state): State<Arc<AppState>>) -> Json<HealthCheckResponse> {
    let health = *state.health.read().await;
    Json(HealthCheckResponse { status: health })
}

/// Build the router with all routes.
pub fn routes(state: Arc<AppState>) -> Router {
    Router::new()
        .route("/health-check", get(health_check))
        .with_state(state)
}
