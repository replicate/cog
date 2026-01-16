#![allow(clippy::module_inception)]
//! Worker subprocess protocol and management.
//!
//! This module defines the protocol between the parent coglet process and
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
