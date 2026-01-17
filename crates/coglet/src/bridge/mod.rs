//! IPC bridge for coglet parent-worker communication.
//!
//! This module provides the wire protocol and codec for communication between
//! the coglet orchestrator (parent) and worker subprocess.
//!
//! # Architecture
//!
//! - **protocol**: Message types (ControlRequest/Response, SlotRequest/Response)
//! - **codec**: JSON framing codec for AsyncRead/AsyncWrite

pub mod codec;
pub mod protocol;
