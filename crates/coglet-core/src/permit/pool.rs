//! Permit pool implementation.
//!
//! Manages a fixed pool of prediction slot permits. Each permit owns:
//! - A slot ID (UUID)
//! - A socket writer for sending requests to that slot
//! - An idle flag (set by event loop when slot becomes available)
//!
//! Permits are returned to the pool on drop if idle, or orphaned if poisoned.

use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::Arc;

use futures::SinkExt;
use tokio::net::unix::OwnedWriteHalf;
use tokio::sync::{mpsc, Mutex};
use tokio_util::codec::FramedWrite;

use coglet_bridge::codec::JsonCodec;
use coglet_bridge::protocol::{SlotId, SlotRequest};

/// Internal permit data that gets recycled through the pool.
pub(crate) struct PermitInner {
    pub slot_id: SlotId,
    pub writer: FramedWrite<OwnedWriteHalf, JsonCodec<SlotRequest>>,
    pub idle_flag: Arc<AtomicBool>,
}

/// A permit granting exclusive access to a prediction slot.
///
/// Owns the socket writer for sending requests to the worker subprocess.
/// On drop, returns to pool if idle, or is orphaned if poisoned.
pub struct Permit {
    /// Slot ID this permit is for.
    slot_id: SlotId,
    /// Socket writer (taken on drop to return to pool).
    writer: Option<FramedWrite<OwnedWriteHalf, JsonCodec<SlotRequest>>>,
    /// Idle flag - set by event loop when slot completes work.
    /// If false on drop, slot is poisoned and permit is orphaned.
    idle_flag: Arc<AtomicBool>,
    /// Channel to return permit to pool.
    pool_tx: mpsc::Sender<PermitInner>,
}

impl Permit {
    /// Create a new permit from inner data.
    pub(crate) fn new(inner: PermitInner, pool_tx: mpsc::Sender<PermitInner>) -> Self {
        // Clear idle flag - slot is now in use
        inner.idle_flag.store(false, Ordering::Release);

        Self {
            slot_id: inner.slot_id,
            writer: Some(inner.writer),
            idle_flag: inner.idle_flag,
            pool_tx,
        }
    }

    /// Get the slot ID.
    pub fn slot_id(&self) -> SlotId {
        self.slot_id
    }

    /// Check if this slot is marked as idle.
    pub fn is_idle(&self) -> bool {
        self.idle_flag.load(Ordering::Acquire)
    }

    /// Get the idle flag (for event loop to set).
    pub fn idle_flag(&self) -> Arc<AtomicBool> {
        Arc::clone(&self.idle_flag)
    }

    /// Send a request on this slot's socket.
    pub async fn send(&mut self, request: SlotRequest) -> Result<(), PermitError> {
        let writer = self.writer.as_mut().ok_or(PermitError::Consumed)?;
        writer
            .send(request)
            .await
            .map_err(|e| PermitError::Send(e.to_string()))
    }
}

impl Drop for Permit {
    fn drop(&mut self) {
        // Only return to pool if idle (not poisoned)
        if self.idle_flag.load(Ordering::Acquire) {
            if let Some(writer) = self.writer.take() {
                let inner = PermitInner {
                    slot_id: self.slot_id,
                    writer,
                    idle_flag: Arc::clone(&self.idle_flag),
                };

                // Try to return to pool (ignore error if pool is closed)
                let _ = self.pool_tx.try_send(inner);
            }
        } else {
            // Slot is poisoned - log and orphan
            tracing::warn!(slot = %self.slot_id, "Permit dropped without idle flag - slot orphaned");
        }
    }
}

/// Errors from permit operations.
#[derive(Debug, Clone, thiserror::Error)]
pub enum PermitError {
    #[error("Permit already consumed")]
    Consumed,
    #[error("Failed to send on slot socket: {0}")]
    Send(String),
}

/// Pool of prediction slot permits.
///
/// Manages a fixed number of permits, one per slot. Permits are acquired
/// to run predictions and returned when done (if not poisoned).
pub struct PermitPool {
    /// Channel to receive available permits.
    available_rx: Mutex<mpsc::Receiver<PermitInner>>,
    /// Channel to return permits to pool (cloned into each Permit).
    available_tx: mpsc::Sender<PermitInner>,
    /// Total number of slots (for health reporting).
    num_slots: usize,
}

impl PermitPool {
    /// Create a new permit pool.
    ///
    /// Initially empty - call `add_permit` to populate with slot sockets.
    pub fn new(num_slots: usize) -> Self {
        // Buffer size = num_slots so all permits can be returned without blocking
        let (tx, rx) = mpsc::channel(num_slots);

        Self {
            available_rx: Mutex::new(rx),
            available_tx: tx,
            num_slots,
        }
    }

    /// Add a permit to the pool.
    ///
    /// Called during initialization to populate pool with slot sockets.
    pub fn add_permit(&self, slot_id: SlotId, writer: FramedWrite<OwnedWriteHalf, JsonCodec<SlotRequest>>) {
        let inner = PermitInner {
            slot_id,
            writer,
            idle_flag: Arc::new(AtomicBool::new(true)), // Start idle
        };

        // This should not fail during init (channel has capacity)
        if let Err(e) = self.available_tx.try_send(inner) {
            tracing::error!(slot = %slot_id, error = %e, "Failed to add permit to pool");
        }
    }

    /// Try to acquire a permit without blocking.
    ///
    /// Returns `None` if no permits available.
    pub fn try_acquire(&self) -> Option<Permit> {
        let mut rx = self.available_rx.try_lock().ok()?;
        let inner = rx.try_recv().ok()?;
        Some(Permit::new(inner, self.available_tx.clone()))
    }

    /// Acquire a permit, waiting if none available.
    pub async fn acquire(&self) -> Option<Permit> {
        let mut rx = self.available_rx.lock().await;
        let inner = rx.recv().await?;
        Some(Permit::new(inner, self.available_tx.clone()))
    }

    /// Get the total number of slots.
    pub fn num_slots(&self) -> usize {
        self.num_slots
    }

    /// Get the number of currently available permits.
    ///
    /// Note: This is approximate due to concurrency.
    pub fn available(&self) -> usize {
        // Channel length gives us available count
        // This is a bit hacky but mpsc doesn't expose len() directly
        // We'd need to track this separately for accurate count
        // For now, return 0 as placeholder
        // TODO: Track available count properly
        0
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use tokio::net::UnixStream;

    async fn make_socket_pair() -> (OwnedWriteHalf, tokio::net::unix::OwnedReadHalf) {
        let (a, b) = UnixStream::pair().unwrap();
        let (read, write) = a.into_split();
        let _ = b; // Drop the other end
        (write, read)
    }

    #[tokio::test]
    async fn permit_pool_add_and_acquire() {
        let pool = PermitPool::new(2);
        
        let (write1, _read1) = make_socket_pair().await;
        let (write2, _read2) = make_socket_pair().await;
        
        let slot1 = SlotId::new();
        let slot2 = SlotId::new();
        
        pool.add_permit(slot1, FramedWrite::new(write1, JsonCodec::new()));
        pool.add_permit(slot2, FramedWrite::new(write2, JsonCodec::new()));
        
        // Should be able to acquire both
        let p1 = pool.try_acquire();
        assert!(p1.is_some());
        
        let p2 = pool.try_acquire();
        assert!(p2.is_some());
        
        // No more available
        let p3 = pool.try_acquire();
        assert!(p3.is_none());
    }

    #[tokio::test]
    async fn permit_returns_to_pool_when_idle() {
        let pool = PermitPool::new(1);
        
        let (write, _read) = make_socket_pair().await;
        let slot = SlotId::new();
        
        pool.add_permit(slot, FramedWrite::new(write, JsonCodec::new()));
        
        {
            let permit = pool.try_acquire().unwrap();
            // Simulate event loop marking idle
            permit.idle_flag().store(true, Ordering::Release);
            // Drop permit
        }
        
        // Should be available again
        let permit = pool.try_acquire();
        assert!(permit.is_some());
    }

    #[tokio::test]
    async fn permit_orphaned_when_not_idle() {
        let pool = PermitPool::new(1);
        
        let (write, _read) = make_socket_pair().await;
        let slot = SlotId::new();
        
        pool.add_permit(slot, FramedWrite::new(write, JsonCodec::new()));
        
        {
            let permit = pool.try_acquire().unwrap();
            // Don't mark idle - simulates poisoned slot
            assert!(!permit.is_idle());
            // Drop permit
        }
        
        // Should NOT be available (orphaned)
        let permit = pool.try_acquire();
        assert!(permit.is_none());
    }
}
