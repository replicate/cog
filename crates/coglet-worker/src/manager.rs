//! Worker manager - spawns and manages worker subprocess.
//!
//! Architecture:
//! - Control channel (stdin/stdout): Cancel, Shutdown, Ready, Idle
//! - Slot sockets: Prediction request/response + streaming logs
//!
//! The manager spawns a single worker subprocess with N slots for concurrent
//! predictions. Each slot has a dedicated socket for data (avoids HOL blocking).

use std::process::Stdio;
use std::time::Duration;

use futures::{SinkExt, StreamExt};
use tokio::process::{Child, ChildStdin, ChildStdout, Command};
use tokio_util::codec::{FramedRead, FramedWrite};

use crate::codec::JsonCodec;
use crate::protocol::{ControlRequest, ControlResponse, SlotRequest, SlotResponse};
use crate::transport::{create_transport, SlotTransport, TRANSPORT_INFO_ENV};

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

    #[error("Slot {0} is poisoned")]
    SlotPoisoned(usize),

    #[error("All slots poisoned")]
    AllSlotsPoisoned,
}

/// A handle to a running worker process.
pub struct Worker {
    /// The child process.
    child: Child,

    /// Control channel writer (stdin).
    ctrl_writer: FramedWrite<ChildStdin, JsonCodec<ControlRequest>>,

    /// Control channel reader (stdout).
    ctrl_reader: FramedRead<ChildStdout, JsonCodec<ControlResponse>>,

    /// Slot transport (platform-specific sockets).
    transport: SlotTransport,

    /// Number of slots.
    num_slots: usize,

    /// Which slots are poisoned.
    poisoned: Vec<bool>,

    /// OpenAPI schema from the predictor.
    schema: Option<serde_json::Value>,
}

/// Configuration for spawning workers.
#[derive(Clone)]
pub struct SpawnConfig {
    /// Python executable to use.
    pub python_exe: String,
    /// Number of concurrent prediction slots.
    pub max_concurrency: usize,
    /// Extra environment variables.
    pub env: Vec<(String, String)>,
}

impl Default for SpawnConfig {
    fn default() -> Self {
        Self {
            python_exe: "python3".to_string(),
            max_concurrency: 1,
            env: vec![],
        }
    }
}

impl Worker {
    /// Spawn a new worker process.
    ///
    /// Creates slot sockets, spawns Python worker, waits for Ready.
    pub async fn spawn(
        predictor_ref: &str,
        ready_timeout: Duration,
        config: &SpawnConfig,
    ) -> Result<Self, WorkerError> {
        let num_slots = config.max_concurrency;

        // Create transport (platform-specific sockets)
        let (mut transport, child_info) = create_transport(num_slots).await?;
        let child_info_json = serde_json::to_string(&child_info)
            .map_err(|e| WorkerError::Protocol(format!("Failed to serialize transport info: {}", e)))?;

        // Spawn Python worker
        let code = format!(
            "import coglet; coglet._run_worker('{}', {})",
            predictor_ref.replace('\'', "\\'"),
            num_slots
        );

        tracing::debug!(
            python = %config.python_exe,
            predictor_ref,
            num_slots,
            "Spawning worker"
        );

        let mut cmd = Command::new(&config.python_exe);
        cmd.arg("-c")
            .arg(&code)
            .env(TRANSPORT_INFO_ENV, &child_info_json)
            .stdin(Stdio::piped())
            .stdout(Stdio::piped())
            .stderr(Stdio::inherit())
            .kill_on_drop(true);

        for (key, value) in &config.env {
            cmd.env(key, value);
        }

        let mut child = cmd.spawn()?;

        let stdin = child.stdin.take().expect("stdin was piped");
        let stdout = child.stdout.take().expect("stdout was piped");

        let ctrl_writer = FramedWrite::new(stdin, JsonCodec::new());
        let mut ctrl_reader = FramedRead::new(stdout, JsonCodec::new());

        // Wait for child to connect to slot sockets
        transport.accept_connections(num_slots).await?;

        // Wait for Ready message
        let ready = tokio::time::timeout(ready_timeout, ctrl_reader.next()).await;

        match ready {
            Ok(Some(Ok(ControlResponse::Ready { schema }))) => {
                tracing::info!(num_slots, "Worker ready");
                Ok(Self {
                    child,
                    ctrl_writer,
                    ctrl_reader,
                    transport,
                    num_slots,
                    poisoned: vec![false; num_slots],
                    schema,
                })
            }
            Ok(Some(Ok(other))) => {
                Err(WorkerError::Protocol(format!("Expected Ready, got {:?}", other)))
            }
            Ok(Some(Err(e))) => {
                Err(WorkerError::Protocol(format!("Failed to read Ready: {}", e)))
            }
            Ok(None) => {
                Err(WorkerError::Died("Worker closed before Ready".to_string()))
            }
            Err(_) => {
                let _ = child.kill().await;
                Err(WorkerError::ReadyTimeout)
            }
        }
    }

    /// Get the OpenAPI schema.
    pub fn schema(&self) -> Option<&serde_json::Value> {
        self.schema.as_ref()
    }

    /// Get process ID.
    pub fn pid(&self) -> Option<u32> {
        self.child.id()
    }

    /// Number of slots.
    pub fn num_slots(&self) -> usize {
        self.num_slots
    }

    /// Check if a slot is poisoned.
    pub fn is_poisoned(&self, slot: usize) -> bool {
        self.poisoned.get(slot).copied().unwrap_or(true)
    }

    /// Mark a slot as poisoned.
    pub fn poison_slot(&mut self, slot: usize) {
        if slot < self.poisoned.len() {
            self.poisoned[slot] = true;
        }
    }

    /// Check if all slots are poisoned.
    pub fn all_poisoned(&self) -> bool {
        self.poisoned.iter().all(|&p| p)
    }

    /// Send a prediction request on a slot.
    pub async fn send_predict(
        &mut self,
        slot: usize,
        id: String,
        input: serde_json::Value,
    ) -> Result<(), WorkerError> {
        if self.is_poisoned(slot) {
            return Err(WorkerError::SlotPoisoned(slot));
        }

        let socket = self.transport.slot_socket(slot)
            .ok_or_else(|| WorkerError::Protocol(format!("No socket for slot {}", slot)))?;

        let request = SlotRequest::Predict { id, input };
        let mut writer = FramedWrite::new(socket, JsonCodec::<SlotRequest>::new());
        writer
            .send(request)
            .await
            .map_err(|e| WorkerError::Protocol(format!("Failed to send predict: {}", e)))
    }

    /// Read next response from a slot.
    pub async fn recv_slot(&mut self, slot: usize) -> Result<SlotResponse, WorkerError> {
        let socket = self.transport.slot_socket(slot)
            .ok_or_else(|| WorkerError::Protocol(format!("No socket for slot {}", slot)))?;

        let mut reader = FramedRead::new(socket, JsonCodec::<SlotResponse>::new());
        match reader.next().await {
            Some(Ok(resp)) => Ok(resp),
            Some(Err(e)) => Err(WorkerError::Protocol(format!("Slot {} read error: {}", slot, e))),
            None => Err(WorkerError::ConnectionLost),
        }
    }

    /// Send cancel request for a slot.
    pub async fn cancel(&mut self, slot: usize) -> Result<(), WorkerError> {
        let request = ControlRequest::Cancel { slot };
        self.ctrl_writer
            .send(request)
            .await
            .map_err(|e| WorkerError::Protocol(format!("Failed to send cancel: {}", e)))
    }

    /// Request graceful shutdown.
    pub async fn shutdown(&mut self) -> Result<(), WorkerError> {
        self.ctrl_writer
            .send(ControlRequest::Shutdown)
            .await
            .map_err(|e| WorkerError::Protocol(format!("Failed to send shutdown: {}", e)))
    }

    /// Read next control response.
    pub async fn recv_control(&mut self) -> Result<ControlResponse, WorkerError> {
        match self.ctrl_reader.next().await {
            Some(Ok(resp)) => Ok(resp),
            Some(Err(e)) => Err(WorkerError::Protocol(format!("Control read error: {}", e))),
            None => Err(WorkerError::ConnectionLost),
        }
    }

    /// Kill the worker process.
    #[cfg(unix)]
    pub async fn kill(&mut self, grace_period: Duration) {
        use nix::sys::signal::{kill, Signal};
        use nix::unistd::Pid;

        if let Some(pid) = self.child.id() {
            let pid = Pid::from_raw(pid as i32);

            tracing::debug!(?pid, "Sending SIGTERM to worker");
            let _ = kill(pid, Signal::SIGTERM);

            let exited = tokio::time::timeout(grace_period, self.child.wait()).await;

            if exited.is_err() {
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
    use super::*;

    #[test]
    fn spawn_config_default() {
        let config = SpawnConfig::default();
        assert_eq!(config.max_concurrency, 1);
    }
}
