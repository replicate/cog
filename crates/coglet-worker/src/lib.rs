//! coglet-worker: Subprocess worker protocol and management.
//!
//! This crate defines the protocol between the parent coglet process and
//! worker subprocesses. Workers handle predictions in isolation, providing:
//! - Crash isolation (segfault in Python doesn't kill server)
//! - Clean cancellation (SIGKILL as last resort)
//! - Memory isolation (runaway prediction can't OOM server)
//!
//! Communication uses LengthDelimitedCodec over pipes with serde_json.
//! The codec works over any AsyncRead/AsyncWrite (pipes, sockets, etc).

mod codec;
mod manager;
mod protocol;
mod worker;

pub use codec::JsonCodec;
pub use manager::{SpawnConfig, Worker, WorkerError};
pub use protocol::{PredictionStatus, WorkerRequest, WorkerResponse};
pub use worker::{run_worker, PredictHandler, PredictResult, PredictionSlot, WorkerConfig};
