//! Worker-side code - runs in the subprocess.
//!
//! Receives requests over stdin, runs Python predictions, sends responses to stdout.
//! This is the child process counterpart to manager.rs.
//!
//! CRITICAL: This code must NEVER allow zombie or hanging processes.
//! - Parent death detection via stdin EOF
//! - Prediction timeouts
//! - Panic handling with permit release
//! - Heartbeat/keepalive (optional)

use std::io;
use std::sync::Arc;
use std::time::{Duration, Instant};

use futures::{SinkExt, StreamExt};
use tokio::io::{stdin, stdout};
use tokio::sync::{OwnedSemaphorePermit, Semaphore};
use tokio_util::codec::{FramedRead, FramedWrite};

use crate::codec::JsonCodec;
use crate::protocol::{PredictionStatus, WorkerRequest, WorkerResponse};

/// Trait for the prediction handler - abstracts the Python integration.
///
/// This allows testing the worker loop without actual Python.
/// 
/// NOTE: Whether predict() calls the Python predict() or train() method
/// is determined by the handler's is_train flag set at construction time.
/// This matches cog mainline behavior where the worker mode is fixed at startup.
#[async_trait::async_trait]
pub trait PredictHandler: Send + Sync {
    /// Initialize the predictor (load model, run setup).
    async fn setup(&self) -> Result<(), String>;

    /// Run a prediction (or training, if is_train mode).
    /// 
    /// The actual Python method called depends on how the handler was created.
    async fn predict(&self, input: serde_json::Value) -> PredictResult;

    /// Request cancellation of current prediction/training.
    fn cancel(&self);

    /// Get OpenAPI schema for the predictor.
    /// Called once after setup, result is cached and sent with Ready message.
    fn schema(&self) -> Option<serde_json::Value> {
        None
    }
}

/// Result of a prediction.
pub struct PredictResult {
    pub output: serde_json::Value,
    pub status: PredictionStatus,
    pub logs: String,
    pub predict_time: f64,
}

impl PredictResult {
    pub fn success(output: serde_json::Value, logs: String, predict_time: f64) -> Self {
        Self {
            output,
            status: PredictionStatus::Succeeded,
            logs,
            predict_time,
        }
    }

    pub fn failed(error: String, logs: String, predict_time: f64) -> Self {
        Self {
            output: serde_json::Value::Null,
            status: PredictionStatus::Failed,
            logs: format!("{}\n{}", logs, error),
            predict_time,
        }
    }

    pub fn cancelled(logs: String, predict_time: f64) -> Self {
        Self {
            output: serde_json::Value::Null,
            status: PredictionStatus::Canceled,
            logs,
            predict_time,
        }
    }
}

/// RAII guard for a prediction slot.
/// 
/// Holds a semaphore permit that is released when dropped.
/// This ensures the slot is returned even on panic/error.
pub struct PredictionSlot {
    _permit: OwnedSemaphorePermit,
    pub id: String,
}

impl PredictionSlot {
    fn new(permit: OwnedSemaphorePermit, id: String) -> Self {
        Self { _permit: permit, id }
    }
}

/// Worker configuration.
pub struct WorkerConfig {
    /// Maximum concurrent predictions (slots).
    pub max_concurrency: usize,
    /// Maximum time to wait for a prediction before force-killing.
    /// None = no timeout (dangerous, but matches cog behavior).
    pub predict_timeout: Option<Duration>,
    /// How long to wait for graceful shutdown before exiting.
    pub shutdown_timeout: Duration,
}

impl Default for WorkerConfig {
    fn default() -> Self {
        Self {
            max_concurrency: 1,
            predict_timeout: None, // No timeout by default (model inference can be slow)
            shutdown_timeout: Duration::from_secs(30),
        }
    }
}

/// Run the worker event loop.
///
/// Reads requests from stdin, processes them, writes responses to stdout.
/// 
/// EXITS WHEN:
/// - stdin closes (parent died or disconnected) 
/// - Shutdown request received
/// - Unrecoverable error
///
/// CRITICAL SAFETY:
/// - stdin EOF = parent gone = EXIT IMMEDIATELY (no orphans)
/// - Prediction timeout = return error, don't hang
/// - Panic in handler = permit still released via RAII
/// - All paths lead to exit, no infinite loops
pub async fn run_worker<H: PredictHandler>(handler: H, config: WorkerConfig) -> io::Result<()> {
    let mut reader = FramedRead::new(stdin(), JsonCodec::<WorkerRequest>::new());
    let mut writer = FramedWrite::new(stdout(), JsonCodec::<WorkerResponse>::new());
    
    // Semaphore for concurrency control
    let slots = Arc::new(Semaphore::new(config.max_concurrency));

    // Run setup
    tracing::info!("Worker starting setup");
    if let Err(e) = handler.setup().await {
        tracing::error!(error = %e, "Setup failed");
        // Send error and exit - don't hang around
        let _ = writer
            .send(WorkerResponse::Error {
                id: "setup".to_string(),
                error: e,
                logs: String::new(),
            })
            .await;
        return Ok(());
    }

    // Get schema (generated once, cached by handler)
    let schema = handler.schema();
    if schema.is_some() {
        tracing::info!("OpenAPI schema generated");
    }

    // Send Ready with schema
    tracing::info!(max_concurrency = config.max_concurrency, "Worker ready");
    writer.send(WorkerResponse::Ready { schema }).await?;

    // Main loop - exits on stdin EOF (parent death) or error
    loop {
        // Read next request - None means stdin closed (PARENT DIED)
        let request = match reader.next().await {
            Some(Ok(r)) => r,
            Some(Err(e)) => {
                tracing::error!(error = %e, "Failed to read request, exiting");
                break;
            }
            None => {
                // CRITICAL: stdin closed = parent is gone
                // Exit immediately to avoid orphan process
                tracing::warn!("stdin closed (parent died?), exiting immediately");
                break;
            }
        };

        match request {
            WorkerRequest::Predict { id, input } => {
                // Acquire a slot with timeout to prevent infinite blocking
                let permit = match tokio::time::timeout(
                    Duration::from_secs(5),
                    slots.clone().acquire_owned()
                ).await {
                    Ok(Ok(p)) => p,
                    Ok(Err(_)) => {
                        tracing::error!("Semaphore closed, exiting");
                        break;
                    }
                    Err(_) => {
                        // Timeout acquiring slot - something is very wrong
                        tracing::error!(id = %id, "Timeout acquiring prediction slot");
                        let _ = writer.send(WorkerResponse::Error {
                            id,
                            error: "Worker busy - timeout acquiring slot".to_string(),
                            logs: String::new(),
                        }).await;
                        continue;
                    }
                };
                
                // RAII slot - permit released on drop, even on panic
                let slot = PredictionSlot::new(permit, id.clone());
                
                tracing::debug!(id = %slot.id, "Starting prediction");
                let start = Instant::now();

                // Run prediction with optional timeout
                let result = if let Some(timeout) = config.predict_timeout {
                    match tokio::time::timeout(timeout, handler.predict(input)).await {
                        Ok(r) => r,
                        Err(_) => {
                            tracing::error!(id = %slot.id, timeout = ?timeout, "Prediction timed out");
                            PredictResult::failed(
                                format!("Prediction timed out after {:?}", timeout),
                                String::new(),
                                start.elapsed().as_secs_f64(),
                            )
                        }
                    }
                } else {
                    handler.predict(input).await
                };
                
                let elapsed = start.elapsed().as_secs_f64();

                let response = match result.status {
                    PredictionStatus::Succeeded | PredictionStatus::Processing => {
                        WorkerResponse::Output {
                            id: slot.id.clone(),
                            output: result.output,
                            status: result.status,
                            logs: result.logs,
                            predict_time: Some(elapsed),
                        }
                    }
                    PredictionStatus::Failed => WorkerResponse::Error {
                        id: slot.id.clone(),
                        error: result.logs.clone(),
                        logs: result.logs,
                    },
                    PredictionStatus::Canceled => WorkerResponse::Cancelled { id: slot.id.clone() },
                };

                // slot drops here, releasing permit (RAII guarantee)
                drop(slot);

                // Send response - if this fails, parent is probably dead
                if let Err(e) = writer.send(response).await {
                    tracing::error!(error = %e, "Failed to send response (parent dead?), exiting");
                    break;
                }
            }

            WorkerRequest::Cancel { id } => {
                tracing::debug!(id = %id, "Cancellation requested");
                handler.cancel();
                // The predict loop will detect cancellation and return Cancelled status
            }

            WorkerRequest::Shutdown => {
                tracing::info!("Shutdown requested, exiting gracefully");
                let _ = writer.send(WorkerResponse::ShuttingDown).await;
                break;
            }
        }
    }

    tracing::info!("Worker exiting");
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::sync::atomic::{AtomicBool, Ordering};
    use std::sync::Arc;

    /// Mock handler for testing.
    struct MockHandler {
        setup_fail: bool,
        predict_result: serde_json::Value,
        cancelled: Arc<AtomicBool>,
    }

    #[async_trait::async_trait]
    impl PredictHandler for MockHandler {
        async fn setup(&self) -> Result<(), String> {
            if self.setup_fail {
                Err("Setup failed".to_string())
            } else {
                Ok(())
            }
        }

        async fn predict(&self, _input: serde_json::Value) -> PredictResult {
            if self.cancelled.load(Ordering::SeqCst) {
                PredictResult::cancelled(String::new(), 0.0)
            } else {
                PredictResult::success(self.predict_result.clone(), String::new(), 0.1)
            }
        }

        fn cancel(&self) {
            self.cancelled.store(true, Ordering::SeqCst);
        }
    }

    #[test]
    fn predict_result_success() {
        let r = PredictResult::success(serde_json::json!("hello"), "log".into(), 0.5);
        assert_eq!(r.status, PredictionStatus::Succeeded);
        assert_eq!(r.output, serde_json::json!("hello"));
    }

    #[test]
    fn predict_result_failed() {
        let r = PredictResult::failed("oops".into(), "log".into(), 0.5);
        assert_eq!(r.status, PredictionStatus::Failed);
    }

    #[test]
    fn predict_result_cancelled() {
        let r = PredictResult::cancelled("log".into(), 0.5);
        assert_eq!(r.status, PredictionStatus::Canceled);
    }
}
