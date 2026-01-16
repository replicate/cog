//! Permit pool implementation.
//!
//! Manages a fixed pool of prediction slot permits. Each permit owns:
//! - A slot ID (UUID)
//! - A socket writer for sending requests to that slot
//! - An idle flag (set by event loop when slot becomes available)
//!
//! Permits are returned to the pool on drop if idle, or orphaned if poisoned.

use std::sync::Arc;
use std::sync::atomic::{AtomicBool, AtomicUsize, Ordering};

use futures::SinkExt;
use tokio::net::unix::OwnedWriteHalf;
use tokio::sync::{Mutex, mpsc};
use tokio_util::codec::FramedWrite;

use crate::bridge::codec::JsonCodec;
use crate::bridge::protocol::{SlotId, SlotRequest};

/// Internal permit data that gets recycled through the pool.
pub(crate) struct PermitInner {
    pub slot_id: SlotId,
    pub writer: FramedWrite<OwnedWriteHalf, JsonCodec<SlotRequest>>,
    pub idle_flag: Arc<AtomicBool>,
}

/// Permit state - enforces that poisoned permits cannot become idle.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
enum PermitState {
    /// Permit is in use, running a prediction.
    InUse,
    /// Prediction complete, permit will return to pool on drop.
    Idle,
    /// Slot permanently failed, permit will NOT return to pool.
    Poisoned,
}

/// A permit granting exclusive access to a prediction slot.
///
/// Owns the socket writer for sending requests to the worker subprocess.
/// On drop, returns to pool if idle, or is orphaned if poisoned.
///
/// State transitions (enforced):
/// - InUse → Idle (via `into_idle()`)
/// - InUse → Poisoned (via `into_poisoned()`)
/// - Poisoned → Idle: NOT ALLOWED (panics)
pub struct Permit {
    /// Slot ID this permit is for.
    slot_id: SlotId,
    /// Socket writer (taken on drop to return to pool).
    writer: Option<FramedWrite<OwnedWriteHalf, JsonCodec<SlotRequest>>>,
    /// Current state - determines drop behavior.
    state: PermitState,
    /// Idle flag shared with pool (for recycling).
    idle_flag: Arc<AtomicBool>,
    /// Channel to return permit to pool.
    pool_tx: mpsc::Sender<PermitInner>,
    /// Pool's available count (incremented on return).
    pool_available: Arc<AtomicUsize>,
}

/// Token proving a permit has been marked idle.
///
/// This is returned by `Permit::into_idle()` and proves the permit
/// will return to the pool. The token itself does nothing - the actual
/// pool return happens when the Permit drops.
#[must_use = "IdleToken should be held until the permit is ready to return to pool"]
pub struct IdleToken {
    slot_id: SlotId,
}

impl IdleToken {
    /// Get the slot ID this token is for.
    pub fn slot_id(&self) -> SlotId {
        self.slot_id
    }
}

impl Permit {
    /// Create a new permit from inner data.
    pub(crate) fn new(
        inner: PermitInner,
        pool_tx: mpsc::Sender<PermitInner>,
        pool_available: Arc<AtomicUsize>,
    ) -> Self {
        // Clear idle flag - slot is now in use
        inner.idle_flag.store(false, Ordering::Release);

        Self {
            slot_id: inner.slot_id,
            writer: Some(inner.writer),
            state: PermitState::InUse,
            idle_flag: inner.idle_flag,
            pool_tx,
            pool_available,
        }
    }

    /// Get the slot ID.
    pub fn slot_id(&self) -> SlotId {
        self.slot_id
    }

    /// Check if this permit is marked as idle.
    pub fn is_idle(&self) -> bool {
        self.state == PermitState::Idle
    }

    /// Check if this permit is poisoned.
    pub fn is_poisoned(&self) -> bool {
        self.state == PermitState::Poisoned
    }

    /// Mark this permit as idle, allowing it to return to pool on drop.
    ///
    /// Returns an `IdleToken` as proof that the permit is idle.
    ///
    /// # Panics
    /// Panics if the permit is already poisoned. A poisoned permit
    /// can NEVER become idle - this is enforced at the type level.
    pub fn into_idle(&mut self) -> IdleToken {
        match self.state {
            PermitState::InUse => {
                self.state = PermitState::Idle;
                self.idle_flag.store(true, Ordering::Release);
                IdleToken {
                    slot_id: self.slot_id,
                }
            }
            PermitState::Idle => {
                // Already idle, return token
                IdleToken {
                    slot_id: self.slot_id,
                }
            }
            PermitState::Poisoned => {
                panic!(
                    "Cannot mark poisoned permit as idle - slot_id={}",
                    self.slot_id
                );
            }
        }
    }

    /// Mark this permit as poisoned (permanently failed).
    ///
    /// The permit will NOT return to the pool on drop.
    /// Called when orchestrator receives Failed control message.
    pub fn into_poisoned(&mut self) {
        if self.state == PermitState::Idle {
            // This shouldn't happen, but if it does, warn and proceed
            tracing::warn!(slot = %self.slot_id, "Marking idle permit as poisoned");
        }
        self.state = PermitState::Poisoned;
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
        match self.state {
            PermitState::Idle => {
                // Return to pool
                if let Some(writer) = self.writer.take() {
                    let inner = PermitInner {
                        slot_id: self.slot_id,
                        writer,
                        idle_flag: Arc::clone(&self.idle_flag),
                    };

                    // Try to return to pool (ignore error if pool is closed)
                    if self.pool_tx.try_send(inner).is_ok() {
                        self.pool_available.fetch_add(1, Ordering::Release);
                    }
                }
            }
            PermitState::Poisoned => {
                // Expected - slot is dead, capacity reduced
                tracing::warn!(slot = %self.slot_id, "Slot poisoned - capacity reduced");
            }
            PermitState::InUse => {
                // Bug - permit dropped without into_idle() or into_poisoned()
                tracing::error!(slot = %self.slot_id, "Permit leaked - dropped while still InUse");
            }
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
    /// Current count of available permits (tracked separately since mpsc doesn't expose len).
    available_count: Arc<AtomicUsize>,
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
            available_count: Arc::new(AtomicUsize::new(0)),
        }
    }

    /// Add a permit to the pool.
    ///
    /// Called during initialization to populate pool with slot sockets.
    pub fn add_permit(
        &self,
        slot_id: SlotId,
        writer: FramedWrite<OwnedWriteHalf, JsonCodec<SlotRequest>>,
    ) {
        let inner = PermitInner {
            slot_id,
            writer,
            idle_flag: Arc::new(AtomicBool::new(true)), // Start idle
        };

        // This should not fail during init (channel has capacity)
        if let Err(e) = self.available_tx.try_send(inner) {
            tracing::error!(slot = %slot_id, error = %e, "Failed to add permit to pool");
        } else {
            self.available_count.fetch_add(1, Ordering::Release);
        }
    }

    /// Try to acquire a permit without blocking.
    ///
    /// Returns `None` if no permits available.
    pub fn try_acquire(&self) -> Option<Permit> {
        let mut rx = self.available_rx.try_lock().ok()?;
        let inner = rx.try_recv().ok()?;
        self.available_count.fetch_sub(1, Ordering::Release);
        Some(Permit::new(
            inner,
            self.available_tx.clone(),
            Arc::clone(&self.available_count),
        ))
    }

    /// Acquire a permit, waiting if none available.
    pub async fn acquire(&self) -> Option<Permit> {
        let mut rx = self.available_rx.lock().await;
        let inner = rx.recv().await?;
        self.available_count.fetch_sub(1, Ordering::Release);
        Some(Permit::new(
            inner,
            self.available_tx.clone(),
            Arc::clone(&self.available_count),
        ))
    }

    /// Get the total number of slots.
    pub fn num_slots(&self) -> usize {
        self.num_slots
    }

    /// Get the number of currently available permits.
    ///
    /// Note: This is approximate due to concurrency.
    pub fn available(&self) -> usize {
        self.available_count.load(Ordering::Acquire)
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
            let mut permit = pool.try_acquire().unwrap();
            // Mark idle via into_idle()
            let _token = permit.into_idle();
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
