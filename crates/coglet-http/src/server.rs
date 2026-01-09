//! HTTP server implementation.

use std::net::SocketAddr;
use std::sync::Arc;

use tokio::net::TcpListener;
use tokio::sync::RwLock;
use tracing::info;

use coglet_core::Health;

use crate::routes::routes;

/// Server configuration.
#[derive(Debug, Clone)]
pub struct ServerConfig {
    pub host: String,
    pub port: u16,
}

impl Default for ServerConfig {
    fn default() -> Self {
        Self {
            host: "0.0.0.0".to_string(),
            port: 5000,
        }
    }
}

/// Shared server state.
#[derive(Debug)]
pub struct AppState {
    pub health: RwLock<Health>,
}

impl Default for AppState {
    fn default() -> Self {
        Self {
            health: RwLock::new(Health::Unknown),
        }
    }
}

/// Start the HTTP server with provided state.
pub async fn serve(config: ServerConfig, state: Arc<AppState>) -> anyhow::Result<()> {
    let app = routes(state);

    let addr: SocketAddr = format!("{}:{}", config.host, config.port).parse()?;

    info!("Starting coglet server on {}", addr);

    let listener = TcpListener::bind(addr).await?;
    axum::serve(listener, app).await?;

    Ok(())
}
