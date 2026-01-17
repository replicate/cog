//! Permit pool implementation.
//!
//! Manages a fixed pool of prediction slot permits. Each permit owns:
//! - A slot ID (UUID)
//! - A socket writer for sending requests to that slot
//! - An idle flag (set by event loop when slot becomes available)
//!
//! Uses separate types to enforce valid state transitions at compile time:
//! - `PermitInUse` → `PermitIdle` (via `into_idle()`)
//! - `PermitInUse` → `PermitPoisoned` (via `into_poisoned()`)
//! - `PermitPoisoned` → `PermitIdle`: NOT POSSIBLE (no method exists)

use std::sync::Arc;
use std::sync::atomic::{AtomicBool, AtomicUsize, Ordering};

use futures::SinkExt;
use tokio::net::unix::OwnedWriteHalf;
use tokio::sync::{Mutex, mpsc};
use tokio_util::codec::FramedWrite;

use crate::bridge::codec::JsonCodec;
use crate::bridge::protocol::{SlotId, SlotRequest};

// =============================================================================
// Permit internals (shared between states)
// =============================================================================

/// Internal permit data that gets recycled through the pool.
pub(crate) struct PermitInner {
    pub slot_id: SlotId,
    pub writer: FramedWrite<OwnedWriteHalf, JsonCodec<SlotRequest>>,
    pub idle_flag: Arc<AtomicBool>,
}

/// Shared pool connection data.
struct PoolConnection {
    pool_tx: mpsc::Sender<PermitInner>,
    pool_available: Arc<AtomicUsize>,
}

impl Clone for PoolConnection {
    fn clone(&self) -> Self {
        Self {
            pool_tx: self.pool_tx.clone(),
            pool_available: Arc::clone(&self.pool_available),
        }
    }
}

// =============================================================================
// PermitInUse - active permit running a prediction
// =============================================================================

/// A permit in the InUse state - running a prediction.
///
/// Can transition to:
/// - `PermitIdle` via `into_idle()` (returns to pool on drop)
/// - `PermitPoisoned` via `into_poisoned()` (orphaned on drop)
pub struct PermitInUse {
    slot_id: SlotId,
    writer: Option<FramedWrite<OwnedWriteHalf, JsonCodec<SlotRequest>>>,
    idle_flag: Arc<AtomicBool>,
    pool: PoolConnection,
}

impl PermitInUse {
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
            idle_flag: inner.idle_flag,
            pool: PoolConnection {
                pool_tx,
                pool_available,
            },
        }
    }

    /// Get the slot ID.
    pub fn slot_id(&self) -> SlotId {
        self.slot_id
    }

    /// Mark this permit as idle, allowing it to return to pool on drop.
    ///
    /// Consumes self and returns a `PermitIdle`.
    pub fn into_idle(mut self) -> PermitIdle {
        self.idle_flag.store(true, Ordering::Release);
        PermitIdle {
            slot_id: self.slot_id,
            writer: self.writer.take(),
            idle_flag: Arc::clone(&self.idle_flag),
            pool: self.pool.clone(),
        }
    }

    /// Mark this permit as poisoned (permanently failed).
    ///
    /// Consumes self and returns a `PermitPoisoned`.
    /// The permit will NOT return to the pool on drop.
    pub fn into_poisoned(mut self) -> PermitPoisoned {
        PermitPoisoned {
            slot_id: self.slot_id,
            _writer: self.writer.take(),
        }
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

impl Drop for PermitInUse {
    fn drop(&mut self) {
        // Bug - permit dropped without into_idle() or into_poisoned()
        if self.writer.is_some() {
            tracing::error!(slot = %self.slot_id, "PermitInUse leaked - dropped without state transition");
        }
        // Don't return to pool - treat as poisoned
    }
}

// =============================================================================
// PermitIdle - completed permit, returns to pool on drop
// =============================================================================

/// A permit in the Idle state - will return to pool on drop.
pub struct PermitIdle {
    slot_id: SlotId,
    writer: Option<FramedWrite<OwnedWriteHalf, JsonCodec<SlotRequest>>>,
    idle_flag: Arc<AtomicBool>,
    pool: PoolConnection,
}

impl PermitIdle {
    /// Get the slot ID.
    pub fn slot_id(&self) -> SlotId {
        self.slot_id
    }
}

impl Drop for PermitIdle {
    fn drop(&mut self) {
        // Return to pool
        if let Some(writer) = self.writer.take() {
            let inner = PermitInner {
                slot_id: self.slot_id,
                writer,
                idle_flag: Arc::clone(&self.idle_flag),
            };

            // Try to return to pool (ignore error if pool is closed)
            if self.pool.pool_tx.try_send(inner).is_ok() {
                self.pool.pool_available.fetch_add(1, Ordering::Release);
            }
        }
    }
}

// =============================================================================
// PermitPoisoned - failed permit, orphaned on drop
// =============================================================================

/// A permit in the Poisoned state - will be orphaned on drop.
///
/// Notably: there is NO `into_idle()` method - a poisoned permit can NEVER
/// become idle. This is enforced at compile time.
pub struct PermitPoisoned {
    slot_id: SlotId,
    _writer: Option<FramedWrite<OwnedWriteHalf, JsonCodec<SlotRequest>>>,
}

impl PermitPoisoned {
    /// Get the slot ID.
    pub fn slot_id(&self) -> SlotId {
        self.slot_id
    }
}

impl Drop for PermitPoisoned {
    fn drop(&mut self) {
        // Expected - slot is dead, capacity reduced
        tracing::warn!(slot = %self.slot_id, "Slot poisoned - capacity reduced");
    }
}

// =============================================================================
// AnyPermit - for holding permits of unknown state
// =============================================================================

/// A permit in any state (for containers that need to hold permits dynamically).
///
/// Used by `PredictionSlot` which needs to transition the permit through states.
pub enum AnyPermit {
    InUse(PermitInUse),
    Idle(PermitIdle),
    Poisoned(PermitPoisoned),
}

impl AnyPermit {
    /// Get the slot ID.
    pub fn slot_id(&self) -> SlotId {
        match self {
            AnyPermit::InUse(p) => p.slot_id(),
            AnyPermit::Idle(p) => p.slot_id(),
            AnyPermit::Poisoned(p) => p.slot_id(),
        }
    }

    /// Check if this permit is idle.
    pub fn is_idle(&self) -> bool {
        matches!(self, AnyPermit::Idle(_))
    }

    /// Check if this permit is poisoned.
    pub fn is_poisoned(&self) -> bool {
        matches!(self, AnyPermit::Poisoned(_))
    }

    /// Check if this permit is in use.
    pub fn is_in_use(&self) -> bool {
        matches!(self, AnyPermit::InUse(_))
    }
}

// =============================================================================
// IdleToken
// =============================================================================

/// Token proving a permit has been marked idle.
///
/// This is returned when transitioning to idle and proves the permit
/// will return to the pool on drop.
#[must_use = "IdleToken should be held until the permit is ready to return to pool"]
pub struct IdleToken {
    pub(crate) slot_id: SlotId,
}

impl IdleToken {
    /// Get the slot ID this token is for.
    pub fn slot_id(&self) -> SlotId {
        self.slot_id
    }
}

// =============================================================================
// PermitError
// =============================================================================

/// Errors from permit operations.
#[derive(Debug, Clone, thiserror::Error)]
pub enum PermitError {
    #[error("Permit already consumed")]
    Consumed,
    #[error("Failed to send on slot socket: {0}")]
    Send(String),
}

// =============================================================================
// PermitPool
// =============================================================================

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
    pub fn try_acquire(&self) -> Option<PermitInUse> {
        let mut rx = self.available_rx.try_lock().ok()?;
        let inner = rx.try_recv().ok()?;
        self.available_count.fetch_sub(1, Ordering::Release);
        Some(PermitInUse::new(
            inner,
            self.available_tx.clone(),
            Arc::clone(&self.available_count),
        ))
    }

    /// Acquire a permit, waiting if none available.
    pub async fn acquire(&self) -> Option<PermitInUse> {
        let mut rx = self.available_rx.lock().await;
        let inner = rx.recv().await?;
        self.available_count.fetch_sub(1, Ordering::Release);
        Some(PermitInUse::new(
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
            let permit = pool.try_acquire().unwrap();
            // Transition to idle (consumes permit, returns PermitIdle)
            let _idle_permit = permit.into_idle();
            // Drop idle permit - returns to pool
        }

        // Should be available again
        let permit = pool.try_acquire();
        assert!(permit.is_some());
    }

    #[tokio::test]
    async fn permit_orphaned_when_poisoned() {
        let pool = PermitPool::new(1);

        let (write, _read) = make_socket_pair().await;
        let slot = SlotId::new();

        pool.add_permit(slot, FramedWrite::new(write, JsonCodec::new()));

        {
            let permit = pool.try_acquire().unwrap();
            // Transition to poisoned (consumes permit, returns PermitPoisoned)
            let _poisoned_permit = permit.into_poisoned();
            // Drop poisoned permit - orphaned, NOT returned to pool
        }

        // Should NOT be available (orphaned)
        let permit = pool.try_acquire();
        assert!(permit.is_none());
    }
}
