//! coglet-transport: Transport layer for coglet.
//!
//! Currently provides HTTP transport via axum. Future transports
//! (gRPC, bnet) will be added as separate submodules.

pub mod http;

pub use http::{ServerConfig, serve};

// Re-export core types for convenience
pub use coglet_core::{PredictionService, WebhookConfig, WebhookEventType, WebhookSender};
