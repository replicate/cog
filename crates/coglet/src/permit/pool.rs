//! Permit pool implementation with typestate for compile-time state transition safety.

use std::sync::atomic::{AtomicBool, AtomicUsize, Ordering};
use std::sync::Arc;

use futures::SinkExt;
use tokio::net::unix::OwnedWriteHalf;
use tokio::sync::{mpsc, Mutex};
use tokio_util::codec::FramedWrite;

use crate::bridge::codec::JsonCodec;
use crate::bridge::protocol::{SlotId, SlotRequest};

pub(crate) struct PermitInner {
    pub slot_id: SlotId,
    pub writer: FramedWrite<OwnedWriteHalf, JsonCodec<SlotRequest>>,
    pub idle_flag: Arc<AtomicBool>,
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
            pool: PoolConnection {
                pool_tx,
                pool_available,
            },
        }
    }

    pub fn slot_id(&self) -> SlotId {
        self.slot_id
    }

    /// Transition to idle state - permit will return to pool on drop.
    pub fn into_idle(mut self) -> PermitIdle {
        self.idle_flag.store(true, Ordering::Release);
        PermitIdle {
            slot_id: self.slot_id,
            writer: self.writer.take(),
            idle_flag: Arc::clone(&self.idle_flag),
            pool: self.pool.clone(),
        }
    }

    /// Transition to poisoned state - permit will NOT return to pool.
    pub fn into_poisoned(mut self) -> PermitPoisoned {
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
        if self.writer.is_some() {
            tracing::error!(slot = %self.slot_id, "PermitInUse dropped without state transition");
        }
    }
}

/// A permit that completed successfully - returns to pool on drop.
pub struct PermitIdle {
    slot_id: SlotId,
    writer: Option<FramedWrite<OwnedWriteHalf, JsonCodec<SlotRequest>>>,
    idle_flag: Arc<AtomicBool>,
    pool: PoolConnection,
}

impl PermitIdle {
    pub fn slot_id(&self) -> SlotId {
        self.slot_id
    }
}

impl Drop for PermitIdle {
    fn drop(&mut self) {
        if let Some(writer) = self.writer.take() {
            let inner = PermitInner {
                slot_id: self.slot_id,
                writer,
                idle_flag: Arc::clone(&self.idle_flag),
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

/// Proof that a permit has been marked idle.
#[must_use = "IdleToken proves permit will return to pool"]
pub struct IdleToken {
    pub(crate) slot_id: SlotId,
}

impl IdleToken {
    pub fn slot_id(&self) -> SlotId {
        self.slot_id
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
pub struct PermitPool {
    available_rx: Mutex<mpsc::Receiver<PermitInner>>,
    available_tx: mpsc::Sender<PermitInner>,
    num_slots: usize,
    available_count: Arc<AtomicUsize>,
}

impl PermitPool {
    pub fn new(num_slots: usize) -> Self {
        let (tx, rx) = mpsc::channel(num_slots);

        Self {
            available_rx: Mutex::new(rx),
            available_tx: tx,
            num_slots,
            available_count: Arc::new(AtomicUsize::new(0)),
        }
    }

    pub fn add_permit(
        &self,
        slot_id: SlotId,
        writer: FramedWrite<OwnedWriteHalf, JsonCodec<SlotRequest>>,
    ) {
        let inner = PermitInner {
            slot_id,
            writer,
            idle_flag: Arc::new(AtomicBool::new(true)),
        };

        if let Err(e) = self.available_tx.try_send(inner) {
            tracing::error!(slot = %slot_id, error = %e, "Failed to add permit to pool");
        } else {
            self.available_count.fetch_add(1, Ordering::Release);
        }
    }

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
}
