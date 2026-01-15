//! IPC bridge for coglet parent-worker communication.
//!
//! This crate provides the wire protocol, codec, and transport layer for
//! communication between the coglet orchestrator (parent) and worker subprocess.
//!
//! # Architecture
//!
//! - **protocol**: Message types (ControlRequest/Response, SlotRequest/Response)
//! - **codec**: JSON framing codec for AsyncRead/AsyncWrite
//! - **transport**: Platform-specific socket implementations (named/abstract)
//!
//! # Usage
//!
//! Parent side:
//! ```ignore
//! use coglet_bridge::{transport, protocol};
//!
//! let (transport, child_info) = transport::create_transport(num_slots).await?;
//! // Pass child_info to subprocess
//! // Communicate using transport.slot_socket(n)
//! ```
//!
//! Child side:
//! ```ignore
//! use coglet_bridge::transport;
//!
//! let info = transport::get_transport_info_from_env()?;
//! let transport = transport::connect_transport(info).await?;
//! ```

pub mod codec;
pub mod protocol;
pub mod transport;
