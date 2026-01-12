//! HTTP server implementation.

use std::collections::HashMap;
use std::net::SocketAddr;
use std::sync::Arc;

use tokio::net::TcpListener;
use tokio::sync::{Mutex, RwLock, Semaphore};
use tracing::info;

use coglet_core::{AsyncPredictFn, CancellationToken, Health, PredictFn, SetupResult, VersionInfo};

use crate::routes::routes;

/// Server configuration.
#[derive(Debug, Clone)]
pub struct ServerConfig {
    pub host: String,
    pub port: u16,
    /// Maximum concurrent predictions (slots).
    /// Default is 1 for sync predictors.
    pub max_concurrency: usize,
    /// If true, ignore SIGTERM and wait for explicit /shutdown or SIGINT.
    /// Used in Kubernetes to allow graceful draining.
    pub await_explicit_shutdown: bool,
}

impl Default for ServerConfig {
    fn default() -> Self {
        Self {
            host: "0.0.0.0".to_string(),
            port: 5000,
            max_concurrency: 1,
            await_explicit_shutdown: false,
        }
    }
}

/// Shared server state.
pub struct AppState {
    pub health: RwLock<Health>,
    /// Setup result (started_at, completed_at, status, logs).
    pub setup_result: RwLock<Option<SetupResult>>,
    /// Sync predict function (for sync predictors, runs in spawn_blocking).
    pub predict_fn: Option<Arc<PredictFn>>,
    /// Async predict function (for async predictors, runs in tokio).
    pub async_predict_fn: Option<Arc<AsyncPredictFn>>,
    /// Semaphore controlling concurrent prediction slots.
    /// 
    /// This enforces max_concurrency at the HTTP layer. Even with GIL Python
    /// (which serializes bytecode execution), this is useful because:
    /// 1. Prevents unbounded request queuing in spawn_blocking's thread pool
    /// 2. Allows early rejection (503) when at capacity
    /// 3. Works correctly for free-threaded Python where GIL doesn't serialize
    /// 4. Native extensions (torch) release GIL, so N slots can do parallel CUDA
    ///
    /// We use try_acquire() for immediate rejection rather than queueing.
    /// Sync predictors get 1 slot, async predictors can have N.
    pub slots: Semaphore,
    /// Version information for the runtime.
    pub version: VersionInfo,
    /// In-flight predictions mapped by ID to their cancellation token.
    /// Used by the cancel endpoint to trigger cancellation.
    pub predictions: Mutex<HashMap<String, CancellationToken>>,
}

impl AppState {
    pub fn new(max_concurrency: usize) -> Self {
        Self {
            health: RwLock::new(Health::Unknown),
            setup_result: RwLock::new(None),
            predict_fn: None,
            async_predict_fn: None,
            slots: Semaphore::new(max_concurrency),
            version: VersionInfo::new(),
            predictions: Mutex::new(HashMap::new()),
        }
    }
    
    /// Register a prediction with its cancellation token.
    pub async fn register_prediction(&self, id: String, token: CancellationToken) {
        let mut predictions = self.predictions.lock().await;
        predictions.insert(id, token);
    }
    
    /// Unregister a prediction (called when prediction completes).
    pub async fn unregister_prediction(&self, id: &str) {
        let mut predictions = self.predictions.lock().await;
        predictions.remove(id);
    }
    
    /// Cancel a prediction by ID. Returns true if found and cancelled.
    pub async fn cancel_prediction(&self, id: &str) -> bool {
        let predictions = self.predictions.lock().await;
        if let Some(token) = predictions.get(id) {
            token.cancel();
            true
        } else {
            false
        }
    }

    pub fn with_predict_fn(mut self, predict_fn: Arc<PredictFn>) -> Self {
        self.predict_fn = Some(predict_fn);
        self
    }

    pub fn with_async_predict_fn(mut self, predict_fn: Arc<AsyncPredictFn>) -> Self {
        self.async_predict_fn = Some(predict_fn);
        self
    }

    pub fn with_health(mut self, health: Health) -> Self {
        self.health = RwLock::new(health);
        self
    }

    pub fn with_version(mut self, version: VersionInfo) -> Self {
        self.version = version;
        self
    }
    
    /// Returns true if this predictor is async.
    pub fn is_async(&self) -> bool {
        self.async_predict_fn.is_some()
    }

    /// Set health state.
    pub async fn set_health(&self, health: Health) {
        let mut guard = self.health.write().await;
        *guard = health;
    }

    /// Set setup result.
    pub async fn set_setup_result(&self, result: SetupResult) {
        let mut guard = self.setup_result.write().await;
        *guard = Some(result);
    }

    /// Get setup result.
    pub async fn get_setup_result(&self) -> Option<SetupResult> {
        self.setup_result.read().await.clone()
    }
}

/// Start the HTTP server with provided state.
/// 
/// Handles SIGTERM and SIGINT for graceful shutdown.
/// If `await_explicit_shutdown` is true, SIGTERM is ignored.
pub async fn serve(config: ServerConfig, state: Arc<AppState>) -> anyhow::Result<()> {
    let app = routes(state);

    let addr: SocketAddr = format!("{}:{}", config.host, config.port).parse()?;

    info!("Starting coglet server on {}", addr);

    let listener = TcpListener::bind(addr).await?;
    
    // Set up graceful shutdown on SIGTERM/SIGINT
    axum::serve(listener, app)
        .with_graceful_shutdown(shutdown_signal(config.await_explicit_shutdown))
        .await?;

    info!("Server shutdown complete");
    Ok(())
}

/// Wait for shutdown signal (SIGTERM or SIGINT).
/// 
/// If `await_explicit_shutdown` is true, SIGTERM is ignored and only
/// SIGINT triggers shutdown. This allows Kubernetes to drain connections
/// before the pod is terminated.
async fn shutdown_signal(await_explicit_shutdown: bool) {
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

    tokio::select! {
        _ = ctrl_c => {
            info!("Received SIGINT, shutting down...");
        }
        _ = terminate => {
            info!("Received SIGTERM, shutting down...");
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
        assert_eq!(config.max_concurrency, 1);
    }

    #[test]
    fn app_state_new_defaults() {
        let state = AppState::new(1);
        assert!(state.predict_fn.is_none());
        assert!(state.async_predict_fn.is_none());
        assert!(!state.is_async());
    }

    #[test]
    fn app_state_builder_with_health() {
        let state = AppState::new(1).with_health(Health::Ready);
        // Can't easily test RwLock contents without async, but we can at least verify build works
        assert!(!state.is_async());
    }

    #[test]
    fn app_state_is_async_true_when_async_fn_set() {
        let state = AppState::new(10).with_async_predict_fn(Arc::new(|_| {
            Box::pin(async { Err(coglet_core::PredictionError::NotReady) })
        }));
        assert!(state.is_async());
    }

    #[test]
    fn app_state_is_async_false_when_sync_fn_set() {
        let state = AppState::new(1).with_predict_fn(Arc::new(|_| {
            Err(coglet_core::PredictionError::NotReady)
        }));
        assert!(!state.is_async());
    }
}
