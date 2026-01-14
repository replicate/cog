//! Slot socket transport abstraction.
//!
//! Provides platform-specific implementations for slot socket communication
//! between coglet parent and worker subprocess.
//!
//! # Implementations
//!
//! - **NamedSocketTransport**: Uses named Unix sockets in temp directory.
//!   Works on all platforms (macOS, Linux, BSD).
//!
//! - **AbstractSocketTransport**: Uses Linux abstract namespace sockets.
//!   No filesystem, auto-cleanup. Linux only.
//!
//! # Usage
//!
//! Parent side:
//! ```ignore
//! let (transport, child_info) = create_default_transport(num_slots).await?;
//! // Pass child_info to subprocess via env var
//! // Use transport.slot_socket(n) to communicate
//! ```
//!
//! Child side:
//! ```ignore
//! let child_info = get_child_info_from_env()?;
//! let transport = connect_transport(child_info).await?;
//! // Use transport.slot_socket(n) to communicate
//! ```

use std::io;
use std::path::PathBuf;

use serde::{Deserialize, Serialize};
use tokio::net::UnixStream;

/// Information passed to child process for connecting to slot sockets.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub enum ChildTransportInfo {
    /// Named sockets in a directory.
    NamedSockets {
        /// Directory containing socket files.
        dir: PathBuf,
        /// Number of slots.
        num_slots: usize,
    },
    /// Abstract namespace sockets (Linux only).
    #[cfg(target_os = "linux")]
    AbstractSockets {
        /// Socket name prefix (slots are prefix-0, prefix-1, etc.)
        prefix: String,
        /// Number of slots.
        num_slots: usize,
    },
}

/// Environment variable name for passing transport info to child.
pub const TRANSPORT_INFO_ENV: &str = "COGLET_TRANSPORT_INFO";

/// Named socket transport using filesystem sockets.
///
/// Creates sockets in a temporary directory:
/// - `{temp_dir}/coglet-{pid}/slot-0.sock`
/// - `{temp_dir}/coglet-{pid}/slot-1.sock`
/// - etc.
pub struct NamedSocketTransport {
    /// Directory containing socket files.
    dir: PathBuf,
    /// Connected sockets for each slot.
    sockets: Vec<UnixStream>,
    /// Listeners for each slot (created at bind time, used at accept time).
    listeners: Vec<tokio::net::UnixListener>,
    /// Whether this is the parent (owns cleanup) or child.
    is_parent: bool,
}

impl NamedSocketTransport {
    /// Create transport on parent side.
    ///
    /// Creates socket directory and binds listeners for each slot.
    /// Returns transport and info for child to connect.
    pub async fn create(num_slots: usize) -> io::Result<(Self, ChildTransportInfo)> {
        use std::os::unix::net::UnixListener as StdUnixListener;
        use tokio::net::UnixListener;

        // Create directory in platform temp location
        let dir = std::env::temp_dir().join(format!("coglet-{}", std::process::id()));
        std::fs::create_dir_all(&dir)?;

        tracing::info!(transport_type = "named", dir = %dir.display(), num_slots, "Creating slot transport");

        // Bind all listeners now so child can connect to any slot
        let mut listeners = Vec::with_capacity(num_slots);
        for i in 0..num_slots {
            let path = dir.join(format!("slot-{}.sock", i));

            // Remove stale socket if exists
            if path.exists() {
                std::fs::remove_file(&path)?;
            }

            let std_listener = StdUnixListener::bind(&path)?;
            std_listener.set_nonblocking(true)?;
            let listener = UnixListener::from_std(std_listener)?;

            tracing::trace!(slot = i, path = %path.display(), "Bound socket");
            listeners.push(listener);
        }

        let transport = Self {
            dir: dir.clone(),
            sockets: Vec::with_capacity(num_slots),
            listeners,
            is_parent: true,
        };

        let child_info = ChildTransportInfo::NamedSockets {
            dir: dir.clone(),
            num_slots,
        };

        Ok((transport, child_info))
    }

    /// Accept connections from child on all slots.
    ///
    /// Listeners were already bound in `create()`, so child can connect at any time.
    pub async fn accept_connections(&mut self, num_slots: usize) -> io::Result<()> {
        // Accept on each pre-bound listener
        for i in 0..num_slots {
            let listener = &self.listeners[i];
            tracing::trace!(slot = i, "Waiting for child connection");
            let (stream, _) = listener.accept().await?;
            self.sockets.push(stream);
            tracing::trace!(slot = i, "Child connected");
        }

        // Clear listeners (no longer needed after accept)
        self.listeners.clear();

        Ok(())
    }

    /// Connect from child side.
    pub async fn connect(dir: PathBuf, num_slots: usize) -> io::Result<Self> {
        let mut sockets = Vec::with_capacity(num_slots);

        for i in 0..num_slots {
            let path = dir.join(format!("slot-{}.sock", i));
            tracing::trace!(slot = i, path = %path.display(), "Connecting to socket");

            let stream = UnixStream::connect(&path).await?;
            sockets.push(stream);

            tracing::trace!(slot = i, "Connected");
        }

        Ok(Self {
            dir,
            sockets,
            listeners: Vec::new(), // Child doesn't use listeners
            is_parent: false,
        })
    }

    /// Get mutable reference to slot socket.
    pub fn slot_socket(&mut self, slot: usize) -> Option<&mut UnixStream> {
        self.sockets.get_mut(slot)
    }

    /// Drain all sockets from the transport.
    ///
    /// Returns owned sockets for splitting into read/write halves.
    /// After this call, the transport has no sockets.
    pub fn drain_sockets(&mut self) -> Vec<UnixStream> {
        std::mem::take(&mut self.sockets)
    }

    /// Get the socket directory path.
    pub fn dir(&self) -> &PathBuf {
        &self.dir
    }

    /// Number of slots.
    pub fn num_slots(&self) -> usize {
        self.sockets.len()
    }

    /// Cleanup socket directory (parent only).
    pub fn cleanup(&mut self) -> io::Result<()> {
        if self.is_parent && self.dir.exists() {
            tracing::debug!(dir = %self.dir.display(), "Cleaning up socket directory");
            std::fs::remove_dir_all(&self.dir)?;
        }
        Ok(())
    }
}

impl Drop for NamedSocketTransport {
    fn drop(&mut self) {
        if let Err(e) = self.cleanup() {
            tracing::warn!(error = %e, "Failed to cleanup socket directory");
        }
    }
}

/// Abstract namespace socket transport (Linux only).
///
/// Uses abstract namespace sockets which don't create filesystem entries
/// and are automatically cleaned up when all references are closed.
#[cfg(target_os = "linux")]
pub struct AbstractSocketTransport {
    /// Socket name prefix.
    prefix: String,
    /// Connected sockets for each slot.
    sockets: Vec<UnixStream>,
    /// Listeners for each slot (created at bind time, used at accept time).
    listeners: Vec<tokio::net::UnixListener>,
}

#[cfg(target_os = "linux")]
impl AbstractSocketTransport {
    /// Create transport on parent side.
    ///
    /// Binds all listeners immediately so child can connect to any slot.
    pub async fn create(num_slots: usize) -> io::Result<(Self, ChildTransportInfo)> {
        use std::os::linux::net::SocketAddrExt;
        use std::os::unix::net::{SocketAddr, UnixListener as StdUnixListener};
        use tokio::net::UnixListener;

        let prefix = format!("coglet-{}", std::process::id());

        tracing::info!(transport_type = "abstract", prefix = %prefix, num_slots, "Creating slot transport");

        // Bind all listeners now so child can connect to any slot
        let mut listeners = Vec::with_capacity(num_slots);
        for i in 0..num_slots {
            let name = format!("{}-{}", prefix, i);
            let addr = SocketAddr::from_abstract_name(name.as_bytes())?;

            let std_listener = StdUnixListener::bind_addr(&addr)?;
            std_listener.set_nonblocking(true)?;
            let listener = UnixListener::from_std(std_listener)?;

            tracing::trace!(slot = i, name = %name, "Bound abstract socket");
            listeners.push(listener);
        }

        let transport = Self {
            prefix: prefix.clone(),
            sockets: Vec::with_capacity(num_slots),
            listeners,
        };

        let child_info = ChildTransportInfo::AbstractSockets { prefix, num_slots };

        Ok((transport, child_info))
    }

    /// Accept connections from child on all slots.
    ///
    /// Listeners were already bound in `create()`, so child can connect at any time.
    pub async fn accept_connections(&mut self, num_slots: usize) -> io::Result<()> {
        // Accept on each pre-bound listener
        for i in 0..num_slots {
            let listener = &self.listeners[i];
            tracing::trace!(slot = i, "Waiting for child connection");
            let (stream, _) = listener.accept().await?;
            self.sockets.push(stream);
            tracing::trace!(slot = i, "Child connected");
        }

        // Clear listeners (no longer needed after accept)
        self.listeners.clear();

        Ok(())
    }

    /// Connect from child side.
    pub async fn connect(prefix: String, num_slots: usize) -> io::Result<Self> {
        use std::os::linux::net::SocketAddrExt;
        use std::os::unix::net::SocketAddr;

        let mut sockets = Vec::with_capacity(num_slots);

        for i in 0..num_slots {
            let name = format!("{}-{}", prefix, i);
            let addr = SocketAddr::from_abstract_name(name.as_bytes())?;

            tracing::trace!(slot = i, name = %name, "Connecting to abstract socket");

            // tokio doesn't directly support abstract sockets, use std then convert
            let std_stream = std::os::unix::net::UnixStream::connect_addr(&addr)?;
            std_stream.set_nonblocking(true)?;
            let stream = UnixStream::from_std(std_stream)?;

            sockets.push(stream);
            tracing::trace!(slot = i, "Connected");
        }

        Ok(Self {
            prefix,
            sockets,
            listeners: Vec::new(), // Child doesn't use listeners
        })
    }

    /// Get mutable reference to slot socket.
    pub fn slot_socket(&mut self, slot: usize) -> Option<&mut UnixStream> {
        self.sockets.get_mut(slot)
    }

    /// Drain all sockets from the transport.
    pub fn drain_sockets(&mut self) -> Vec<UnixStream> {
        std::mem::take(&mut self.sockets)
    }

    /// Number of slots.
    pub fn num_slots(&self) -> usize {
        self.sockets.len()
    }
}

/// Transport type enum for runtime dispatch.
pub enum SlotTransport {
    Named(NamedSocketTransport),
    #[cfg(target_os = "linux")]
    Abstract(AbstractSocketTransport),
}

impl SlotTransport {
    /// Get mutable reference to slot socket.
    pub fn slot_socket(&mut self, slot: usize) -> Option<&mut UnixStream> {
        match self {
            Self::Named(t) => t.slot_socket(slot),
            #[cfg(target_os = "linux")]
            Self::Abstract(t) => t.slot_socket(slot),
        }
    }

    /// Drain all sockets from the transport.
    pub fn drain_sockets(&mut self) -> Vec<UnixStream> {
        match self {
            Self::Named(t) => t.drain_sockets(),
            #[cfg(target_os = "linux")]
            Self::Abstract(t) => t.drain_sockets(),
        }
    }

    /// Number of slots.
    pub fn num_slots(&self) -> usize {
        match self {
            Self::Named(t) => t.num_slots(),
            #[cfg(target_os = "linux")]
            Self::Abstract(t) => t.num_slots(),
        }
    }

    /// Accept connections (parent side, after spawning child).
    pub async fn accept_connections(&mut self, num_slots: usize) -> io::Result<()> {
        match self {
            Self::Named(t) => t.accept_connections(num_slots).await,
            #[cfg(target_os = "linux")]
            Self::Abstract(t) => t.accept_connections(num_slots).await,
        }
    }
}

/// Create transport using platform default.
///
/// - Linux: Uses abstract sockets (no filesystem)
/// - macOS/BSD: Uses named sockets in temp directory
pub async fn create_transport(num_slots: usize) -> io::Result<(SlotTransport, ChildTransportInfo)> {
    #[cfg(target_os = "linux")]
    {
        let (transport, info) = AbstractSocketTransport::create(num_slots).await?;
        Ok((SlotTransport::Abstract(transport), info))
    }

    #[cfg(not(target_os = "linux"))]
    {
        let (transport, info) = NamedSocketTransport::create(num_slots).await?;
        Ok((SlotTransport::Named(transport), info))
    }
}

/// Connect to transport from child side.
pub async fn connect_transport(info: ChildTransportInfo) -> io::Result<SlotTransport> {
    match info {
        ChildTransportInfo::NamedSockets { dir, num_slots } => {
            let transport = NamedSocketTransport::connect(dir, num_slots).await?;
            Ok(SlotTransport::Named(transport))
        }
        #[cfg(target_os = "linux")]
        ChildTransportInfo::AbstractSockets { prefix, num_slots } => {
            let transport = AbstractSocketTransport::connect(prefix, num_slots).await?;
            Ok(SlotTransport::Abstract(transport))
        }
    }
}

/// Read transport info from environment variable.
pub fn get_transport_info_from_env() -> io::Result<ChildTransportInfo> {
    let json = std::env::var(TRANSPORT_INFO_ENV).map_err(|_| {
        io::Error::new(
            io::ErrorKind::NotFound,
            format!("{} environment variable not set", TRANSPORT_INFO_ENV),
        )
    })?;

    serde_json::from_str(&json).map_err(|e| {
        io::Error::new(
            io::ErrorKind::InvalidData,
            format!("Failed to parse transport info: {}", e),
        )
    })
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn child_transport_info_serializes() {
        let info = ChildTransportInfo::NamedSockets {
            dir: PathBuf::from("/tmp/coglet-123"),
            num_slots: 3,
        };
        let json = serde_json::to_string(&info).unwrap();
        let parsed: ChildTransportInfo = serde_json::from_str(&json).unwrap();

        match parsed {
            ChildTransportInfo::NamedSockets { dir, num_slots } => {
                assert_eq!(dir, PathBuf::from("/tmp/coglet-123"));
                assert_eq!(num_slots, 3);
            }
            #[cfg(target_os = "linux")]
            _ => panic!("Wrong variant"),
        }
    }

    #[cfg(target_os = "linux")]
    #[test]
    fn abstract_socket_info_serializes() {
        let info = ChildTransportInfo::AbstractSockets {
            prefix: "coglet-456".to_string(),
            num_slots: 2,
        };
        let json = serde_json::to_string(&info).unwrap();
        let parsed: ChildTransportInfo = serde_json::from_str(&json).unwrap();

        match parsed {
            ChildTransportInfo::AbstractSockets { prefix, num_slots } => {
                assert_eq!(prefix, "coglet-456");
                assert_eq!(num_slots, 2);
            }
            _ => panic!("Wrong variant"),
        }
    }
}
