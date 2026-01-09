//! HTTP server implementation.

use std::net::SocketAddr;
use std::sync::Arc;

use tokio::net::TcpListener;
use tokio::sync::{RwLock, Semaphore};
use tracing::info;

use coglet_core::{AsyncPredictFn, Health, PredictFn, VersionInfo};

use crate::routes::routes;

/// Server configuration.
#[derive(Debug, Clone)]
pub struct ServerConfig {
    pub host: String,
    pub port: u16,
    /// Maximum concurrent predictions (slots).
    /// Default is 1 for sync predictors.
    pub max_concurrency: usize,
}

impl Default for ServerConfig {
    fn default() -> Self {
        Self {
            host: "0.0.0.0".to_string(),
            port: 5000,
            max_concurrency: 1,
        }
    }
}

/// Shared server state.
pub struct AppState {
    pub health: RwLock<Health>,
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
}

impl AppState {
    pub fn new(max_concurrency: usize) -> Self {
        Self {
            health: RwLock::new(Health::Unknown),
            predict_fn: None,
            async_predict_fn: None,
            slots: Semaphore::new(max_concurrency),
            version: VersionInfo::new(),
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
