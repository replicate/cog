//! Worker manager - spawns and manages worker subprocesses.
//!
//! The manager maintains a pool of workers, each handling predictions in isolation.
//! Workers communicate via stdin/stdout pipes using LengthDelimitedCodec + JSON.

use std::process::Stdio;
use std::time::Duration;

use futures::{SinkExt, StreamExt};
use tokio::process::{Child, ChildStdin, ChildStdout, Command};
use tokio_util::codec::{FramedRead, FramedWrite};

use crate::codec::JsonCodec;
use crate::protocol::{WorkerRequest, WorkerResponse};

/// Error from worker operations.
#[derive(Debug, thiserror::Error)]
pub enum WorkerError {
    #[error("Failed to spawn worker: {0}")]
    SpawnFailed(#[from] std::io::Error),

    #[error("Worker not ready within timeout")]
    ReadyTimeout,

    #[error("Worker connection lost")]
    ConnectionLost,

    #[error("Worker died unexpectedly: {0}")]
    Died(String),

    #[error("Protocol error: {0}")]
    Protocol(String),

    #[error("Prediction failed: {0}")]
    PredictionFailed(String),

    #[error("Prediction cancelled")]
    Cancelled,
}

/// A handle to a running worker process.
pub struct Worker {
    /// The child process.
    child: Child,

    /// Framed writer to send requests.
    writer: FramedWrite<ChildStdin, JsonCodec<WorkerRequest>>,

    /// Framed reader to receive responses.
    reader: FramedRead<ChildStdout, JsonCodec<WorkerResponse>>,

    /// OpenAPI schema from the predictor (captured at spawn time).
    schema: Option<serde_json::Value>,
}

/// Configuration for spawning workers.
#[derive(Clone)]
pub struct SpawnConfig {
    /// Python executable to use (defaults to "python3")
    pub python_exe: String,
    /// Extra environment variables to set
    pub env: Vec<(String, String)>,
}

impl Default for SpawnConfig {
    fn default() -> Self {
        Self {
            python_exe: "python3".to_string(),
            env: vec![],
        }
    }
}

impl Worker {
    /// Spawn a new worker process.
    ///
    /// The worker is spawned as a Python process running `coglet._run_worker()`.
    /// It will load the predictor and send `Ready` when initialized.
    pub async fn spawn(
        predictor_ref: &str,
        ready_timeout: Duration,
        config: &SpawnConfig,
    ) -> Result<Self, WorkerError> {
        // Spawn Python running coglet._run_worker(predictor_ref)
        let code = format!(
            "import coglet; coglet._run_worker('{}')",
            predictor_ref.replace('\'', "\\'")
        );

        tracing::debug!(python = %config.python_exe, predictor_ref, "Spawning worker");

        let mut cmd = Command::new(&config.python_exe);
        cmd.arg("-c")
            .arg(&code)
            .stdin(Stdio::piped())
            .stdout(Stdio::piped())
            .stderr(Stdio::inherit()) // Let stderr pass through for debugging
            .kill_on_drop(true);

        // Add extra environment variables
        for (key, value) in &config.env {
            cmd.env(key, value);
        }

        let mut child = cmd.spawn()?;

        let stdin = child.stdin.take().expect("stdin was piped");
        let stdout = child.stdout.take().expect("stdout was piped");

        let writer = FramedWrite::new(stdin, JsonCodec::new());
        let mut reader = FramedRead::new(stdout, JsonCodec::new());

        // Wait for Ready message
        let ready = tokio::time::timeout(ready_timeout, reader.next()).await;

        match ready {
            Ok(Some(Ok(WorkerResponse::Ready { schema }))) => {
                if schema.is_some() {
                    tracing::info!("Worker ready with OpenAPI schema");
                } else {
                    tracing::info!("Worker ready");
                }
                Ok(Self { child, writer, reader, schema })
            }
            Ok(Some(Ok(other))) => {
                Err(WorkerError::Protocol(format!("Expected Ready, got {:?}", other)))
            }
            Ok(Some(Err(e))) => {
                Err(WorkerError::Protocol(format!("Failed to read Ready: {}", e)))
            }
            Ok(None) => {
                Err(WorkerError::Died("Worker closed stdout before Ready".to_string()))
            }
            Err(_) => {
                // Timeout - kill the worker
                let _ = child.kill().await;
                Err(WorkerError::ReadyTimeout)
            }
        }
    }

    /// Send a prediction request and wait for the response.
    /// 
    /// WARNING: This can block forever if the worker hangs.
    /// Use `predict_with_timeout` for safety.
    pub async fn predict(
        &mut self,
        id: String,
        input: serde_json::Value,
    ) -> Result<WorkerResponse, WorkerError> {
        let request = WorkerRequest::Predict { id: id.clone(), input };

        self.writer
            .send(request)
            .await
            .map_err(|e| WorkerError::Protocol(format!("Failed to send request: {}", e)))?;

        // Read responses until we get a terminal one
        self.read_until_terminal().await
    }

    /// Send a prediction request with timeout.
    /// 
    /// If timeout expires, attempts to cancel then kill the worker.
    /// This is the SAFE way to run predictions.
    pub async fn predict_with_timeout(
        &mut self,
        id: String,
        input: serde_json::Value,
        timeout: Duration,
        kill_grace: Duration,
    ) -> Result<WorkerResponse, WorkerError> {
        match tokio::time::timeout(timeout, self.predict(id.clone(), input)).await {
            Ok(result) => result,
            Err(_) => {
                // Timeout! Try to cancel first
                tracing::warn!(id = %id, "Prediction timed out, attempting cancel");
                let _ = self.cancel(id.clone()).await;
                
                // Give it a moment to respond to cancel
                match tokio::time::timeout(Duration::from_secs(1), self.read_until_terminal()).await {
                    Ok(Ok(resp)) => return Ok(resp),
                    _ => {
                        // Cancel didn't work, kill escalation
                        tracing::error!(id = %id, "Cancel failed, killing worker");
                        self.kill(kill_grace).await;
                        return Err(WorkerError::Died("Killed after timeout".to_string()));
                    }
                }
            }
        }
    }

    /// Read responses until we get a terminal one.
    async fn read_until_terminal(&mut self) -> Result<WorkerResponse, WorkerError> {
        loop {
            match self.reader.next().await {
                Some(Ok(resp)) => {
                    match &resp {
                        WorkerResponse::Output { status, .. } if status.is_terminal() => {
                            return Ok(resp);
                        }
                        WorkerResponse::Output { .. } => {
                            // Intermediate output for streaming - for now just continue
                            // TODO: stream these to caller
                            continue;
                        }
                        WorkerResponse::Cancelled { .. } => {
                            return Err(WorkerError::Cancelled);
                        }
                        WorkerResponse::Error { error, .. } => {
                            return Err(WorkerError::PredictionFailed(error.clone()));
                        }
                        other => {
                            return Err(WorkerError::Protocol(format!(
                                "Unexpected response: {:?}",
                                other
                            )));
                        }
                    }
                }
                Some(Err(e)) => {
                    return Err(WorkerError::Protocol(format!("Read error: {}", e)));
                }
                None => {
                    // Worker died or closed pipe
                    return Err(WorkerError::ConnectionLost);
                }
            }
        }
    }

    /// Request cancellation of a prediction.
    pub async fn cancel(&mut self, id: String) -> Result<(), WorkerError> {
        let request = WorkerRequest::Cancel { id };
        self.writer
            .send(request)
            .await
            .map_err(|e| WorkerError::Protocol(format!("Failed to send cancel: {}", e)))
    }

    /// Request graceful shutdown.
    pub async fn shutdown(&mut self) -> Result<(), WorkerError> {
        let request = WorkerRequest::Shutdown;
        self.writer
            .send(request)
            .await
            .map_err(|e| WorkerError::Protocol(format!("Failed to send shutdown: {}", e)))
    }

    /// Get the process ID of the worker.
    pub fn pid(&self) -> Option<u32> {
        self.child.id()
    }

    /// Get the OpenAPI schema (if available).
    /// Schema is captured once when the worker starts.
    pub fn schema(&self) -> Option<&serde_json::Value> {
        self.schema.as_ref()
    }

    /// Kill the worker process with escalation.
    ///
    /// Tries SIGTERM first, then SIGKILL after timeout.
    #[cfg(unix)]
    pub async fn kill(&mut self, grace_period: Duration) {
        use nix::sys::signal::{kill, Signal};
        use nix::unistd::Pid;

        if let Some(pid) = self.child.id() {
            let pid = Pid::from_raw(pid as i32);

            // Try SIGTERM first
            tracing::debug!(?pid, "Sending SIGTERM to worker");
            let _ = kill(pid, Signal::SIGTERM);

            // Wait for graceful exit
            let exited = tokio::time::timeout(grace_period, self.child.wait()).await;

            if exited.is_err() {
                // Still alive, SIGKILL
                tracing::warn!(?pid, "Worker didn't exit, sending SIGKILL");
                let _ = kill(pid, Signal::SIGKILL);
                let _ = self.child.wait().await;
            }
        }
    }

    #[cfg(not(unix))]
    pub async fn kill(&mut self, _grace_period: Duration) {
        let _ = self.child.kill().await;
        let _ = self.child.wait().await;
    }
}

#[cfg(test)]
mod tests {
    // Integration tests would go here - need actual worker binary
}
