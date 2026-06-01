//! Slot socket transport for parent-worker IPC.
//!
//! Platform-specific implementations:
//! - **NamedSocketTransport**: Filesystem sockets (macOS, Linux, BSD)
//! - **AbstractSocketTransport**: Linux abstract namespace (no filesystem, auto-cleanup)

use std::io;
use std::path::PathBuf;

use serde::{Deserialize, Serialize};
use tokio::net::UnixStream;

/// Information passed to child process for connecting to slot sockets.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub enum ChildTransportInfo {
    NamedSockets {
        dir: PathBuf,
        num_slots: usize,
    },
    #[cfg(target_os = "linux")]
    AbstractSockets {
        prefix: String,
        num_slots: usize,
    },
}

/// Named socket transport using filesystem sockets.
///
/// Socket path format: `{temp_dir}/coglet-{pid}/slot-{n}.sock`
pub struct NamedSocketTransport {
    dir: PathBuf,
    sockets: Vec<UnixStream>,
    listeners: Vec<tokio::net::UnixListener>,
    is_parent: bool,
}

impl NamedSocketTransport {
    /// Create transport on parent side, binding listeners for child to connect.
    pub async fn create(num_slots: usize) -> io::Result<(Self, ChildTransportInfo)> {
        use std::os::unix::net::UnixListener as StdUnixListener;
        use tokio::net::UnixListener;

        let dir = std::env::temp_dir().join(format!("coglet-{}", std::process::id()));
        std::fs::create_dir_all(&dir)?;

        tracing::debug!(transport_type = "named", dir = %dir.display(), num_slots, "Creating slot transport");

        let mut listeners = Vec::with_capacity(num_slots);
        for i in 0..num_slots {
            let path = dir.join(format!("slot-{}.sock", i));

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
    pub async fn accept_connections(&mut self, num_slots: usize) -> io::Result<()> {
        for i in 0..num_slots {
            let listener = &self.listeners[i];
            tracing::trace!(slot = i, "Waiting for child connection");
            let (stream, _) = listener.accept().await?;
            self.sockets.push(stream);
            tracing::trace!(slot = i, "Child connected");
        }

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
            listeners: Vec::new(),
            is_parent: false,
        })
    }

    pub fn slot_socket(&mut self, slot: usize) -> Option<&mut UnixStream> {
        self.sockets.get_mut(slot)
    }

    /// Returns owned sockets for splitting into read/write halves.
    pub fn drain_sockets(&mut self) -> Vec<UnixStream> {
        std::mem::take(&mut self.sockets)
    }

    pub fn dir(&self) -> &PathBuf {
        &self.dir
    }

    pub fn num_slots(&self) -> usize {
        self.sockets.len()
    }

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
/// No filesystem entries, auto-cleanup when all references close.
#[cfg(target_os = "linux")]
pub struct AbstractSocketTransport {
    #[allow(dead_code)] // Kept for debugging/identification
    prefix: String,
    sockets: Vec<UnixStream>,
    listeners: Vec<tokio::net::UnixListener>,
}

#[cfg(target_os = "linux")]
impl AbstractSocketTransport {
    /// Create transport on parent side, binding listeners for child to connect.
    pub async fn create(num_slots: usize) -> io::Result<(Self, ChildTransportInfo)> {
        use std::os::linux::net::SocketAddrExt;
        use std::os::unix::net::{SocketAddr, UnixListener as StdUnixListener};
        use tokio::net::UnixListener;

        let prefix = format!("coglet-{}", std::process::id());

        tracing::debug!(transport_type = "abstract", prefix = %prefix, num_slots, "Creating slot transport");

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
    pub async fn accept_connections(&mut self, num_slots: usize) -> io::Result<()> {
        for i in 0..num_slots {
            let listener = &self.listeners[i];
            tracing::trace!(slot = i, "Waiting for child connection");
            let (stream, _) = listener.accept().await?;
            self.sockets.push(stream);
            tracing::trace!(slot = i, "Child connected");
        }

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

            // tokio doesn't support abstract sockets directly
            let std_stream = std::os::unix::net::UnixStream::connect_addr(&addr)?;
            std_stream.set_nonblocking(true)?;
            let stream = UnixStream::from_std(std_stream)?;

            sockets.push(stream);
            tracing::trace!(slot = i, "Connected");
        }

        Ok(Self {
            prefix,
            sockets,
            listeners: Vec::new(),
        })
    }

    pub fn slot_socket(&mut self, slot: usize) -> Option<&mut UnixStream> {
        self.sockets.get_mut(slot)
    }

    pub fn drain_sockets(&mut self) -> Vec<UnixStream> {
        std::mem::take(&mut self.sockets)
    }

    pub fn num_slots(&self) -> usize {
        self.sockets.len()
    }
}

pub enum SlotTransport {
    Named(NamedSocketTransport),
    #[cfg(target_os = "linux")]
    Abstract(AbstractSocketTransport),
}

impl SlotTransport {
    pub fn slot_socket(&mut self, slot: usize) -> Option<&mut UnixStream> {
        match self {
            Self::Named(t) => t.slot_socket(slot),
            #[cfg(target_os = "linux")]
            Self::Abstract(t) => t.slot_socket(slot),
        }
    }

    pub fn drain_sockets(&mut self) -> Vec<UnixStream> {
        match self {
            Self::Named(t) => t.drain_sockets(),
            #[cfg(target_os = "linux")]
            Self::Abstract(t) => t.drain_sockets(),
        }
    }

    pub fn num_slots(&self) -> usize {
        match self {
            Self::Named(t) => t.num_slots(),
            #[cfg(target_os = "linux")]
            Self::Abstract(t) => t.num_slots(),
        }
    }

    pub async fn accept_connections(&mut self, num_slots: usize) -> io::Result<()> {
        match self {
            Self::Named(t) => t.accept_connections(num_slots).await,
            #[cfg(target_os = "linux")]
            Self::Abstract(t) => t.accept_connections(num_slots).await,
        }
    }
}

/// Create transport using platform default (abstract on Linux, named elsewhere).
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

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn child_transport_info_roundtrips() {
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
    fn abstract_socket_info_roundtrips() {
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
