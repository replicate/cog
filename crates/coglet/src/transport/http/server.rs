//! HTTP server implementation.

use std::net::SocketAddr;
use std::sync::Arc;

use tokio::net::TcpListener;
use tokio::sync::watch;
use tracing::info;

use crate::service::PredictionService;

use super::routes::routes;

#[derive(Debug, Clone)]
pub struct ServerConfig {
    pub host: String,
    pub port: u16,
    /// If true, ignore SIGTERM and wait for explicit /shutdown or SIGINT.
    /// Used in Kubernetes to allow graceful draining.
    pub await_explicit_shutdown: bool,
}

impl Default for ServerConfig {
    fn default() -> Self {
        Self {
            host: "0.0.0.0".to_string(),
            port: 5000,
            await_explicit_shutdown: false,
        }
    }
}

/// Start the HTTP server with provided service.
pub async fn serve(config: ServerConfig, service: Arc<PredictionService>) -> anyhow::Result<()> {
    let shutdown_rx = service.shutdown_rx();
    let app = routes(service.clone());

    let addr: SocketAddr = format!("{}:{}", config.host, config.port).parse()?;

    let listener = TcpListener::bind(addr).await?;
    let actual_addr = listener.local_addr()?;

    info!("Starting coglet server on {}", actual_addr);

    axum::serve(listener, app)
        .with_graceful_shutdown(shutdown_signal(config.await_explicit_shutdown, shutdown_rx))
        .await?;

    info!("Server shutdown complete");

    // Gracefully shutdown the orchestrator worker
    service.shutdown().await;

    Ok(())
}

/// Wait for shutdown signal (SIGTERM, SIGINT, or /shutdown endpoint).
///
/// # Panics
///
/// Panics if signal handlers cannot be installed. This can only happen if:
/// - Called from a non-main thread without the runtime being properly configured
/// - The tokio runtime is not properly initialized
///
/// These are unrecoverable configuration errors that should fail fast at startup.
async fn shutdown_signal(await_explicit_shutdown: bool, mut shutdown_rx: watch::Receiver<bool>) {
    let ctrl_c = async {
        tokio::signal::ctrl_c()
            .await
            .expect("failed to install Ctrl+C handler - is tokio runtime configured correctly?");
    };

    #[cfg(unix)]
    let terminate = async {
        if await_explicit_shutdown {
            // Ignore SIGTERM - wait forever (until SIGINT or explicit shutdown)
            tracing::info!("await_explicit_shutdown enabled, ignoring SIGTERM");
            std::future::pending::<()>().await
        } else {
            tokio::signal::unix::signal(tokio::signal::unix::SignalKind::terminate())
                .expect(
                    "failed to install SIGTERM handler - is tokio runtime configured correctly?",
                )
                .recv()
                .await;
        }
    };

    #[cfg(not(unix))]
    let terminate = std::future::pending::<()>();

    let explicit_shutdown = async {
        while !*shutdown_rx.borrow() {
            if shutdown_rx.changed().await.is_err() {
                std::future::pending::<()>().await;
            }
        }
    };

    tokio::select! {
        _ = ctrl_c => {
            info!("Received SIGINT, shutting down...");
        }
        _ = terminate => {
            info!("Received SIGTERM, shutting down...");
        }
        _ = explicit_shutdown => {
            info!("Shutdown requested via /shutdown endpoint...");
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn server_config_default() {
        let config = ServerConfig::default();
        assert_eq!(config.host, "0.0.0.0");
        assert_eq!(config.port, 5000);
        assert!(!config.await_explicit_shutdown);
    }
}
