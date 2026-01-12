//! HTTP server implementation.

use std::net::SocketAddr;
use std::sync::Arc;

use tokio::net::TcpListener;
use tokio::sync::watch;
use tracing::info;

use coglet_core::PredictionService;

use crate::routes::routes;

/// Server configuration.
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
/// 
/// Handles SIGTERM and SIGINT for graceful shutdown.
/// If `await_explicit_shutdown` is true, SIGTERM is ignored.
pub async fn serve(config: ServerConfig, service: Arc<PredictionService>) -> anyhow::Result<()> {
    let shutdown_rx = service.shutdown_rx();
    let app = routes(service);

    let addr: SocketAddr = format!("{}:{}", config.host, config.port).parse()?;

    info!("Starting coglet server on {}", addr);

    let listener = TcpListener::bind(addr).await?;
    
    // Set up graceful shutdown on SIGTERM/SIGINT or /shutdown endpoint
    axum::serve(listener, app)
        .with_graceful_shutdown(shutdown_signal(config.await_explicit_shutdown, shutdown_rx))
        .await?;

    info!("Server shutdown complete");
    Ok(())
}

/// Wait for shutdown signal (SIGTERM, SIGINT, or /shutdown endpoint).
/// 
/// If `await_explicit_shutdown` is true, SIGTERM is ignored and only
/// SIGINT or /shutdown triggers shutdown. This allows Kubernetes to drain 
/// connections before the pod is terminated.
async fn shutdown_signal(await_explicit_shutdown: bool, mut shutdown_rx: watch::Receiver<bool>) {
    let ctrl_c = async {
        tokio::signal::ctrl_c()
            .await
            .expect("failed to install Ctrl+C handler");
    };

    #[cfg(unix)]
    let terminate = async {
        if await_explicit_shutdown {
            // Ignore SIGTERM - wait forever (until SIGINT or explicit shutdown)
            tracing::info!("await_explicit_shutdown enabled, ignoring SIGTERM");
            std::future::pending::<()>().await
        } else {
            tokio::signal::unix::signal(tokio::signal::unix::SignalKind::terminate())
                .expect("failed to install SIGTERM handler")
                .recv()
                .await;
        }
    };

    #[cfg(not(unix))]
    let terminate = std::future::pending::<()>();

    let explicit_shutdown = async {
        // Wait for /shutdown endpoint to be called
        while !*shutdown_rx.borrow() {
            if shutdown_rx.changed().await.is_err() {
                // Channel closed, wait forever
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
