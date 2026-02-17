//! Permit pool implementation with typestate for compile-time state transition safety.
//!
//! Slot poisoning is a pool-level property: a poisoned slot is permanently removed
//! from the pool regardless of whether a prediction was active on it.

use std::sync::Arc;
use std::sync::Mutex as StdMutex;
use std::sync::atomic::{AtomicBool, AtomicUsize, Ordering};

use futures::SinkExt;
use tokio::net::unix::OwnedWriteHalf;
use tokio::sync::{Mutex, mpsc};
use tokio_util::codec::FramedWrite;

use crate::bridge::codec::JsonCodec;
use crate::bridge::protocol::{SlotId, SlotRequest};

pub(crate) struct PermitInner {
    pub slot_id: SlotId,
    pub writer: FramedWrite<OwnedWriteHalf, JsonCodec<SlotRequest>>,
    pub idle_flag: Arc<AtomicBool>,
    pub poisoned: Arc<AtomicBool>,
}

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

/// A permit actively running a prediction.
pub struct PermitInUse {
    slot_id: SlotId,
    writer: Option<FramedWrite<OwnedWriteHalf, JsonCodec<SlotRequest>>>,
    idle_flag: Arc<AtomicBool>,
    poisoned: Arc<AtomicBool>,
    pool: PoolConnection,
}

impl PermitInUse {
    pub(crate) fn new(
        inner: PermitInner,
        pool_tx: mpsc::Sender<PermitInner>,
        pool_available: Arc<AtomicUsize>,
    ) -> Self {
        inner.idle_flag.store(false, Ordering::Release);

        Self {
            slot_id: inner.slot_id,
            writer: Some(inner.writer),
            idle_flag: inner.idle_flag,
            poisoned: inner.poisoned,
            pool: PoolConnection {
                pool_tx,
                pool_available,
            },
        }
    }

    pub fn slot_id(&self) -> SlotId {
        self.slot_id
    }

    /// Transition to idle state - permit will return to pool on drop
    /// (unless the slot has been poisoned at the pool level).
    pub fn into_idle(mut self) -> PermitIdle {
        self.idle_flag.store(true, Ordering::Release);
        PermitIdle {
            slot_id: self.slot_id,
            writer: self.writer.take(),
            idle_flag: Arc::clone(&self.idle_flag),
            poisoned: Arc::clone(&self.poisoned),
            pool: self.pool.clone(),
        }
    }

    /// Transition to poisoned state - permit will NOT return to pool.
    ///
    /// Also sets the pool-level poison flag so the slot is never reused.
    pub fn into_poisoned(mut self) -> PermitPoisoned {
        self.poisoned.store(true, Ordering::Release);
        PermitPoisoned {
            slot_id: self.slot_id,
            _writer: self.writer.take(),
        }
    }

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
        if self.writer.is_some() && !self.poisoned.load(Ordering::Acquire) {
            tracing::error!(slot = %self.slot_id, "PermitInUse dropped without state transition");
        }
    }
}

/// A permit that completed successfully - returns to pool on drop
/// (unless the slot has been poisoned at the pool level).
pub struct PermitIdle {
    slot_id: SlotId,
    writer: Option<FramedWrite<OwnedWriteHalf, JsonCodec<SlotRequest>>>,
    idle_flag: Arc<AtomicBool>,
    poisoned: Arc<AtomicBool>,
    pool: PoolConnection,
}

impl PermitIdle {
    pub fn slot_id(&self) -> SlotId {
        self.slot_id
    }
}

impl Drop for PermitIdle {
    fn drop(&mut self) {
        // If the slot was poisoned at the pool level, don't return it.
        if self.poisoned.load(Ordering::Acquire) {
            tracing::warn!(slot = %self.slot_id, "Slot poisoned - not returning to pool");
            return;
        }

        if let Some(writer) = self.writer.take() {
            let inner = PermitInner {
                slot_id: self.slot_id,
                writer,
                idle_flag: Arc::clone(&self.idle_flag),
                poisoned: Arc::clone(&self.poisoned),
            };

            if self.pool.pool_tx.try_send(inner).is_ok() {
                self.pool.pool_available.fetch_add(1, Ordering::Release);
            }
        }
    }
}

/// A poisoned permit - slot permanently failed, will NOT return to pool.
pub struct PermitPoisoned {
    slot_id: SlotId,
    _writer: Option<FramedWrite<OwnedWriteHalf, JsonCodec<SlotRequest>>>,
}

impl PermitPoisoned {
    pub fn slot_id(&self) -> SlotId {
        self.slot_id
    }
}

impl Drop for PermitPoisoned {
    fn drop(&mut self) {
        tracing::warn!(slot = %self.slot_id, "Slot poisoned - capacity reduced");
    }
}

/// A permit in any state (for containers needing dynamic state).
pub enum AnyPermit {
    InUse(PermitInUse),
    Idle(PermitIdle),
    Poisoned(PermitPoisoned),
}

impl AnyPermit {
    pub fn slot_id(&self) -> SlotId {
        match self {
            AnyPermit::InUse(p) => p.slot_id(),
            AnyPermit::Idle(p) => p.slot_id(),
            AnyPermit::Poisoned(p) => p.slot_id(),
        }
    }

    pub fn is_idle(&self) -> bool {
        matches!(self, AnyPermit::Idle(_))
    }

    pub fn is_poisoned(&self) -> bool {
        matches!(self, AnyPermit::Poisoned(_))
    }

    pub fn is_in_use(&self) -> bool {
        matches!(self, AnyPermit::InUse(_))
    }
}

#[must_use = "must be activated to enable slot idle transition"]
#[derive(Debug)]
pub struct InactiveSlotIdleToken {
    slot_id: SlotId,
}

impl InactiveSlotIdleToken {
    pub fn new(slot_id: SlotId) -> Self {
        Self { slot_id }
    }

    pub fn slot_id(&self) -> SlotId {
        self.slot_id
    }

    pub fn activate(self) -> SlotIdleToken {
        SlotIdleToken {
            slot_id: self.slot_id,
            create_time: std::time::Instant::now(),
            alarm_handle: tokio::spawn(async move {
                // This task exists solely to alert if the token isn't consumed within a reasonable time.
                // If we see this alert, it means the slot won't return to the pool until the process exits.
                tokio::time::sleep(SlotIdleToken::ALERT_THRESHOLD).await;
                tracing::error!(slot = %self.slot_id, "IdleToken not consumed after 5s - slot will not return to pool");
            }),
        }
    }
}

/// Token confirming the worker has marked the slot as idle, allowing the permit to return to the pool on drop.
#[must_use = "IdleToken confirms the worker has marked the slot as idle"]
#[derive(Debug)]
pub struct SlotIdleToken {
    pub(crate) slot_id: SlotId,
    pub(crate) create_time: std::time::Instant,
    pub(crate) alarm_handle: tokio::task::JoinHandle<()>,
}

impl SlotIdleToken {
    const ALERT_THRESHOLD: std::time::Duration = std::time::Duration::from_secs(5);

    pub fn slot_id(&self) -> SlotId {
        self.slot_id
    }

    pub fn consume(self) {
        let elapsed = self.create_time.elapsed();
        if elapsed > Self::ALERT_THRESHOLD {
            tracing::warn!(slot = %self.slot_id, latency = ?elapsed, "Delayed IdleToken Consumption");
        }
        tracing::debug!(slot = %self.slot_id, "IdleToken consumed");
    }
}

impl Drop for SlotIdleToken {
    fn drop(&mut self) {
        self.alarm_handle.abort();
    }
}

#[derive(Debug, Clone, thiserror::Error)]
pub enum PermitError {
    #[error("Permit already consumed")]
    Consumed,
    #[error("Failed to send on slot socket: {0}")]
    Send(String),
}

/// Pool of prediction slot permits.
///
/// Slot poisoning is tracked here. A poisoned slot is permanently removed
/// from the pool — its permit will not be returned or acquired again.
pub struct PermitPool {
    available_rx: Mutex<mpsc::Receiver<PermitInner>>,
    available_tx: mpsc::Sender<PermitInner>,
    num_slots: usize,
    available_count: Arc<AtomicUsize>,
    /// Per-slot poison flags, shared with permits for fast checking.
    poison_flags: StdMutex<Vec<(SlotId, Arc<AtomicBool>)>>,
}

impl PermitPool {
    pub fn new(num_slots: usize) -> Self {
        let (tx, rx) = mpsc::channel(num_slots);

        Self {
            available_rx: Mutex::new(rx),
            available_tx: tx,
            num_slots,
            available_count: Arc::new(AtomicUsize::new(0)),
            poison_flags: StdMutex::new(Vec::with_capacity(num_slots)),
        }
    }

    pub fn add_permit(
        &self,
        slot_id: SlotId,
        writer: FramedWrite<OwnedWriteHalf, JsonCodec<SlotRequest>>,
    ) {
        let poisoned = Arc::new(AtomicBool::new(false));

        // Store the flag for external poisoning.
        if let Ok(mut flags) = self.poison_flags.lock() {
            flags.push((slot_id, Arc::clone(&poisoned)));
        }

        let inner = PermitInner {
            slot_id,
            writer,
            idle_flag: Arc::new(AtomicBool::new(true)),
            poisoned,
        };

        if let Err(e) = self.available_tx.try_send(inner) {
            tracing::error!(slot = %slot_id, error = %e, "Failed to add permit to pool");
        } else {
            self.available_count.fetch_add(1, Ordering::Release);
        }
    }

    /// Poison a slot. The permit will not be returned to the pool.
    ///
    /// This works whether the slot is idle (in the pool) or in use (held by a prediction).
    /// - Idle: the permit will be discarded on next `acquire`/`try_acquire`.
    /// - In use: `PermitIdle::drop` will see the flag and not return it.
    pub fn poison(&self, slot_id: SlotId) {
        if let Ok(flags) = self.poison_flags.lock() {
            for (id, flag) in flags.iter() {
                if *id == slot_id {
                    if !flag.swap(true, Ordering::AcqRel) {
                        tracing::warn!(slot = %slot_id, "Slot poisoned - capacity permanently reduced");
                    }
                    return;
                }
            }
        }
        tracing::warn!(slot = %slot_id, "Attempted to poison unknown slot");
    }

    /// Check if a slot is poisoned.
    pub fn is_poisoned(&self, slot_id: SlotId) -> bool {
        if let Ok(flags) = self.poison_flags.lock() {
            for (id, flag) in flags.iter() {
                if *id == slot_id {
                    return flag.load(Ordering::Acquire);
                }
            }
        }
        false
    }

    pub fn try_acquire(&self) -> Option<PermitInUse> {
        let mut rx = self.available_rx.try_lock().ok()?;
        loop {
            let inner = rx.try_recv().ok()?;
            self.available_count.fetch_sub(1, Ordering::Release);

            // Skip poisoned permits — they're permanently dead.
            if inner.poisoned.load(Ordering::Acquire) {
                tracing::debug!(slot = %inner.slot_id, "Discarding poisoned permit from pool");
                continue;
            }

            return Some(PermitInUse::new(
                inner,
                self.available_tx.clone(),
                Arc::clone(&self.available_count),
            ));
        }
    }

    pub async fn acquire(&self) -> Option<PermitInUse> {
        let mut rx = self.available_rx.lock().await;
        loop {
            let inner = rx.recv().await?;
            self.available_count.fetch_sub(1, Ordering::Release);

            // Skip poisoned permits — they're permanently dead.
            if inner.poisoned.load(Ordering::Acquire) {
                tracing::debug!(slot = %inner.slot_id, "Discarding poisoned permit from pool");
                continue;
            }

            return Some(PermitInUse::new(
                inner,
                self.available_tx.clone(),
                Arc::clone(&self.available_count),
            ));
        }
    }

    pub fn num_slots(&self) -> usize {
        self.num_slots
    }

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
        let _ = b;
        (write, read)
    }

    #[tokio::test]
    async fn pool_add_and_acquire() {
        let pool = PermitPool::new(2);

        let (write1, _read1) = make_socket_pair().await;
        let (write2, _read2) = make_socket_pair().await;

        let slot1 = SlotId::new();
        let slot2 = SlotId::new();

        pool.add_permit(slot1, FramedWrite::new(write1, JsonCodec::new()));
        pool.add_permit(slot2, FramedWrite::new(write2, JsonCodec::new()));

        let p1 = pool.try_acquire();
        assert!(p1.is_some());

        let p2 = pool.try_acquire();
        assert!(p2.is_some());

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
            let _idle_permit = permit.into_idle();
        }

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
            let _poisoned_permit = permit.into_poisoned();
        }

        let permit = pool.try_acquire();
        assert!(permit.is_none());
    }

    #[tokio::test]
    async fn pool_poison_idle_slot() {
        // Poison a slot while it's idle in the pool — acquire should skip it.
        let pool = PermitPool::new(2);

        let (write1, _read1) = make_socket_pair().await;
        let (write2, _read2) = make_socket_pair().await;

        let slot1 = SlotId::new();
        let slot2 = SlotId::new();

        pool.add_permit(slot1, FramedWrite::new(write1, JsonCodec::new()));
        pool.add_permit(slot2, FramedWrite::new(write2, JsonCodec::new()));

        assert!(!pool.is_poisoned(slot1));
        pool.poison(slot1);
        assert!(pool.is_poisoned(slot1));
        assert!(!pool.is_poisoned(slot2));

        // First acquire should skip poisoned slot1, return slot2.
        let permit = pool.try_acquire().unwrap();
        assert_eq!(permit.slot_id(), slot2);

        // No more permits available.
        assert!(pool.try_acquire().is_none());
    }

    #[tokio::test]
    async fn pool_poison_in_use_slot_prevents_return() {
        // Poison a slot while a prediction holds it — into_idle + drop should NOT return it.
        let pool = PermitPool::new(1);

        let (write, _read) = make_socket_pair().await;
        let slot = SlotId::new();

        pool.add_permit(slot, FramedWrite::new(write, JsonCodec::new()));

        {
            let permit = pool.try_acquire().unwrap();
            // Poison while in use.
            pool.poison(slot);
            // Transition to idle — drop should see the poison flag.
            let _idle = permit.into_idle();
        }

        // Permit should NOT have returned to the pool.
        assert!(pool.try_acquire().is_none());
    }

    #[tokio::test]
    async fn pool_poison_is_idempotent() {
        let pool = PermitPool::new(1);

        let (write, _read) = make_socket_pair().await;
        let slot = SlotId::new();

        pool.add_permit(slot, FramedWrite::new(write, JsonCodec::new()));

        pool.poison(slot);
        pool.poison(slot); // Should not panic or double-count.
        assert!(pool.is_poisoned(slot));
    }
}
