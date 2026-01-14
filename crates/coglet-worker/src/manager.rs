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

use std::collections::HashMap;

use crate::codec::JsonCodec;
use crate::protocol::{ControlRequest, ControlResponse, SlotId, SlotRequest, SlotResponse};
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
    SlotPoisoned(SlotId),

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

    /// Slot IDs (ordered by socket index).
    slot_ids: Vec<SlotId>,

    /// Which slots are poisoned (keyed by SlotId).
    poisoned: HashMap<SlotId, bool>,

    /// OpenAPI schema from the predictor.
    schema: Option<serde_json::Value>,

    /// Logs captured during setup.
    setup_logs: String,
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

        // Collect setup logs and wait for Ready message
        let mut setup_logs = String::new();
        let deadline = tokio::time::Instant::now() + ready_timeout;
        
        loop {
            let remaining = deadline.saturating_duration_since(tokio::time::Instant::now());
            if remaining.is_zero() {
                let _ = child.kill().await;
                return Err(WorkerError::ReadyTimeout);
            }
            
            let msg = tokio::time::timeout(remaining, ctrl_reader.next()).await;
            
            match msg {
                Ok(Some(Ok(ControlResponse::Log { source: _, data }))) => {
                    // Emit setup logs via tracing
                    for line in data.lines() {
                        tracing::info!(target: "coglet::setup", "{}", line);
                    }
                    // Accumulate for health-check
                    setup_logs.push_str(&data);
                }
                Ok(Some(Ok(ControlResponse::Ready { slots, schema }))) => {
                    tracing::info!(num_slots, ?slots, "Worker ready");
                    
                    // Log schema info
                    if schema.is_some() {
                        tracing::info!(target: "coglet::schema", "Setting OpenAPI schema from worker");
                        // Full schema dump - opt-in only (requires explicit RUST_LOG=coglet_worker::schema=trace)
                        if let Some(ref s) = schema {
                            if let Ok(json) = serde_json::to_string_pretty(s) {
                                tracing::trace!(target: "coglet_worker::schema", schema = %json, "OpenAPI schema content");
                            }
                        }
                    }
                    
                    let poisoned: HashMap<SlotId, bool> = slots.iter().map(|id| (*id, false)).collect();
                    return Ok(Self {
                        child,
                        ctrl_writer,
                        ctrl_reader,
                        transport,
                        slot_ids: slots,
                        poisoned,
                        schema,
                        setup_logs,
                    });
                }
                Ok(Some(Ok(other))) => {
                    return Err(WorkerError::Protocol(format!("Expected Ready or Log, got {:?}", other)));
                }
                Ok(Some(Err(e))) => {
                    return Err(WorkerError::Protocol(format!("Failed to read control message: {}", e)));
                }
                Ok(None) => {
                    return Err(WorkerError::Died("Worker closed before Ready".to_string()));
                }
                Err(_) => {
                    let _ = child.kill().await;
                    return Err(WorkerError::ReadyTimeout);
                }
            }
        }
    }

    /// Get the OpenAPI schema.
    pub fn schema(&self) -> Option<&serde_json::Value> {
        self.schema.as_ref()
    }

    /// Get setup logs (accumulated during setup phase).
    pub fn setup_logs(&self) -> &str {
        &self.setup_logs
    }

    /// Get process ID.
    pub fn pid(&self) -> Option<u32> {
        self.child.id()
    }

    /// Number of slots.
    pub fn num_slots(&self) -> usize {
        self.slot_ids.len()
    }

    /// Get all slot IDs.
    pub fn slot_ids(&self) -> &[SlotId] {
        &self.slot_ids
    }

    /// Get slot ID by index.
    pub fn slot_id(&self, index: usize) -> Option<SlotId> {
        self.slot_ids.get(index).copied()
    }

    /// Check if a slot is poisoned.
    pub fn is_poisoned(&self, slot: SlotId) -> bool {
        self.poisoned.get(&slot).copied().unwrap_or(true)
    }

    /// Mark a slot as poisoned.
    pub fn poison_slot(&mut self, slot: SlotId) {
        self.poisoned.insert(slot, true);
    }

    /// Check if all slots are poisoned.
    pub fn all_poisoned(&self) -> bool {
        self.poisoned.values().all(|&p| p)
    }

    /// Send a prediction request on a slot (by index).
    pub async fn send_predict(
        &mut self,
        index: usize,
        id: String,
        input: serde_json::Value,
    ) -> Result<(), WorkerError> {
        let slot = self.slot_ids.get(index)
            .copied()
            .ok_or_else(|| WorkerError::Protocol(format!("Invalid slot index {}", index)))?;
        
        if self.is_poisoned(slot) {
            return Err(WorkerError::SlotPoisoned(slot));
        }

        let socket = self.transport.slot_socket(index)
            .ok_or_else(|| WorkerError::Protocol(format!("No socket for slot {}", slot)))?;

        tracing::debug!(%slot, %id, "Dispatching prediction to slot");
        let request = SlotRequest::Predict { id, input };
        let mut writer = FramedWrite::new(socket, JsonCodec::<SlotRequest>::new());
        writer
            .send(request)
            .await
            .map_err(|e| WorkerError::Protocol(format!("Failed to send predict: {}", e)))
    }

    /// Read next response from a slot (by index).
    pub async fn recv_slot(&mut self, index: usize) -> Result<SlotResponse, WorkerError> {
        let slot = self.slot_ids.get(index)
            .ok_or_else(|| WorkerError::Protocol(format!("Invalid slot index {}", index)))?;
        
        let socket = self.transport.slot_socket(index)
            .ok_or_else(|| WorkerError::Protocol(format!("No socket for slot {}", slot)))?;

        let mut reader = FramedRead::new(socket, JsonCodec::<SlotResponse>::new());
        match reader.next().await {
            Some(Ok(resp)) => Ok(resp),
            Some(Err(e)) => Err(WorkerError::Protocol(format!("Slot {} read error: {}", slot, e))),
            None => Err(WorkerError::ConnectionLost),
        }
    }

    /// Send cancel request for a slot.
    pub async fn cancel(&mut self, slot: SlotId) -> Result<(), WorkerError> {
        tracing::debug!(%slot, "Sending cancel to worker");
        let request = ControlRequest::Cancel { slot };
        self.ctrl_writer
            .send(request)
            .await
            .map_err(|e| WorkerError::Protocol(format!("Failed to send cancel: {}", e)))
    }

    /// Request graceful shutdown.
    pub async fn shutdown(&mut self) -> Result<(), WorkerError> {
        tracing::info!("Sending shutdown to worker");
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
