//! coglet-transport: HTTP transport for coglet using axum.

mod routes;
mod server;

pub use server::{serve, AppState, ServerConfig};

// Re-export webhook types from coglet-core for convenience
pub use coglet_core::{WebhookConfig, WebhookEventType, WebhookSender};
