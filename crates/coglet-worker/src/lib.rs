//! coglet-worker: Subprocess worker protocol and management.
//!
//! This crate defines the protocol between the parent coglet process and
//! worker subprocesses. Workers handle predictions in isolation, providing:
//! - Crash isolation (segfault in Python doesn't kill server)
//! - Clean cancellation (SIGKILL as last resort)
//! - Memory isolation (runaway prediction can't OOM server)
//!
//! Architecture:
//! - Control pipe (stdin/stdout): Control messages (Cancel, Shutdown, Ready, Idle)
//! - Slot sockets: One per slot, for prediction request/response + logs
//!   - Platform-specific transport (abstract sockets on Linux, named on macOS)
//!
//! Communication uses LengthDelimitedCodec with serde_json.

mod manager;
mod worker;

pub use manager::{SpawnConfig, Worker, WorkerError};
pub use worker::{
    PredictHandler, PredictResult, SetupLogHook, SlotSender, WorkerConfig, run_worker,
};

// Re-export bridge types for backward compatibility
pub use coglet_bridge::codec::JsonCodec;
pub use coglet_bridge::protocol::{
    ControlRequest, ControlResponse, LogSource, SlotId, SlotOutcome, SlotRequest, SlotResponse,
};
pub use coglet_bridge::transport::{
    ChildTransportInfo, NamedSocketTransport, SlotTransport, TRANSPORT_INFO_ENV, connect_transport,
    create_transport, get_transport_info_from_env,
};
