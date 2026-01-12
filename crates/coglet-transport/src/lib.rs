//! coglet-transport: HTTP transport for coglet using axum.

mod routes;
mod server;

pub use server::{serve, ServerConfig};

// Re-export core types for convenience
pub use coglet_core::{
    PredictionService, WebhookConfig, WebhookEventType, WebhookSender,
};
