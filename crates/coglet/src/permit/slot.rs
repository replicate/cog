//! PredictionSlot - holds Prediction and Permit side-by-side.
//!
//! This separation allows the prediction to be behind Mutex for concurrent
//! updates while the permit's idle_flag can be set without holding the lock.
//!
//! Slot poisoning is NOT managed here — it's a pool-level property.
//! The slot always transitions to idle when done; `PermitIdle::drop` checks
//! the pool-level poison flag to decide whether to return the permit.

use std::sync::{Arc, Mutex};

use super::{AnyPermit, PermitInUse, SlotIdleToken};
use crate::bridge::protocol::SlotId;
use crate::prediction::Prediction;

#[derive(Debug, Clone, thiserror::Error)]
pub enum SlotError {
    #[error("receive error while waiting for idle token")]
    IdleTokenReceiveError(#[from] tokio::sync::oneshot::error::RecvError),
    #[error("permit already consumed")]
    PermitAlreadyConsumed,
}

/// Pre-registration slot state - holds prediction while permit is being
/// acquired and slot is being registered with orchestrator.
pub struct UnregisteredPredictionSlot {
    prediction_slot: PredictionSlot,
    idle_tx: tokio::sync::oneshot::Sender<SlotIdleToken>,
}

impl UnregisteredPredictionSlot {
    pub fn new(
        prediction_slot: PredictionSlot,
        idle_tx: tokio::sync::oneshot::Sender<SlotIdleToken>,
    ) -> Self {
        Self {
            prediction_slot,
            idle_tx,
        }
    }

    /// Consumes the unregistered slot and returns its components for registration.
    pub fn into_parts(self) -> (tokio::sync::oneshot::Sender<SlotIdleToken>, PredictionSlot) {
        (self.idle_tx, self.prediction_slot)
    }

    pub fn prediction(&self) -> Arc<Mutex<Prediction>> {
        self.prediction_slot.prediction()
    }
}

/// Holds a prediction and its permit side-by-side.
///
/// On drop: Permit returns to pool (if idle and not poisoned at pool level).
pub struct PredictionSlot {
    prediction: Arc<Mutex<Prediction>>,
    slot_id: SlotId,
    permit: Option<AnyPermit>,
    idle_rx: Option<tokio::sync::oneshot::Receiver<SlotIdleToken>>,
}

impl PredictionSlot {
    pub fn new(
        prediction: Prediction,
        permit: PermitInUse,
        idle_rx: tokio::sync::oneshot::Receiver<SlotIdleToken>,
    ) -> Self {
        let slot_id = permit.slot_id();
        Self {
            prediction: Arc::new(Mutex::new(prediction)),
            slot_id,
            permit: Some(AnyPermit::InUse(permit)),
            idle_rx: Some(idle_rx),
        }
    }

    pub fn prediction(&self) -> Arc<Mutex<Prediction>> {
        Arc::clone(&self.prediction)
    }

    pub fn permit_mut(&mut self) -> Option<&mut PermitInUse> {
        match &mut self.permit {
            Some(AnyPermit::InUse(p)) => Some(p),
            _ => None,
        }
    }

    pub fn slot_id(&self) -> SlotId {
        self.slot_id
    }

    /// Mark the slot as idle - permit will return to pool on drop (unless the slot has
    /// been poisoned at the pool level). Awaits until the idle token is received, which
    /// ensures the slot has been confirmed idle by the worker. If the idle token is not
    /// received, the permit is not returned to the pool,
    #[must_use = "into_idle confirms the slot is idle and allows the permit to return to the pool on drop"]
    pub async fn into_idle(mut self) -> Result<(), SlotError> {
        if let Some(receiver) = self.idle_rx.take() {
            let idle_token = receiver.await?;
            debug_assert_eq!(
                idle_token.slot_id(),
                self.slot_id,
                "IdleToken slot_id mismatch"
            );
            idle_token.consume();
        }

        let permit = self.permit.take();
        debug_assert!(
            permit.is_some(),
            "Attempted to mark slot as idle but permit was already consumed"
        );
        match permit {
            Some(AnyPermit::InUse(p)) => {
                let idle = p.into_idle();
                self.permit = Some(AnyPermit::Idle(idle));
                Ok(())
            }
            Some(AnyPermit::Idle(p)) => {
                self.permit = Some(AnyPermit::Idle(p));
                Ok(())
            }
            Some(AnyPermit::Poisoned(p)) => {
                // Permit was explicitly poisoned (legacy path) — keep it.
                debug_assert!(false, "Cannot mark poisoned slot as idle");
                tracing::error!(slot = %p.slot_id(), "Bug: attempted to mark poisoned slot as idle");
                self.permit = Some(AnyPermit::Poisoned(p));
                Ok(())
            }
            None => {
                // Permit was already consumed (bug) — log and do nothing.
                tracing::error!(slot = %self.slot_id(), "Bug: attempted to mark slot as idle but permit was already consumed");
                Err(SlotError::PermitAlreadyConsumed)
            }
        }
    }

    pub fn is_idle(&self) -> bool {
        self.permit.as_ref().is_some_and(|p| p.is_idle())
    }

    pub fn id(&self) -> String {
        self.prediction
            .try_lock()
            .map(|p| p.id().to_string())
            .unwrap_or_default()
    }
}

impl Drop for PredictionSlot {
    fn drop(&mut self) {
        if let Some(AnyPermit::InUse(_)) = &self.permit
            && let Ok(mut prediction) = self.prediction.try_lock()
            && !prediction.is_terminal()
        {
            tracing::error!(
                slot = %self.slot_id(),
                prediction_id = %prediction.id(),
                "Slot dropped while InUse with non-terminal prediction"
            );
            prediction.set_failed("Slot dropped unexpectedly".to_string());
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::bridge::codec::JsonCodec;
    use crate::permit::{InactiveSlotIdleToken, PermitPool};
    use tokio::net::UnixStream;
    use tokio_util::codec::FramedWrite;

    #[tokio::test]
    async fn slot_creation() {
        let pool = PermitPool::new(1);

        let (a, _b) = UnixStream::pair().unwrap();
        let (_, write) = a.into_split();
        let slot_id = SlotId::new();

        pool.add_permit(slot_id, FramedWrite::new(write, JsonCodec::new()));

        let permit = pool.try_acquire().unwrap();
        let prediction = Prediction::new("test_123".to_string(), None);

        let (_idle_tx, idle_rx) = tokio::sync::oneshot::channel();
        let slot = PredictionSlot::new(prediction, permit, idle_rx);
        assert_eq!(slot.slot_id(), slot_id);
    }

    #[tokio::test]
    async fn slot_mark_idle_returns_permit() {
        let pool = PermitPool::new(1);

        let (a, _b) = UnixStream::pair().unwrap();
        let (_, write) = a.into_split();
        let slot_id = SlotId::new();

        pool.add_permit(slot_id, FramedWrite::new(write, JsonCodec::new()));

        {
            let permit = pool.try_acquire().unwrap();
            let prediction = Prediction::new("test_123".to_string(), None);
            let (idle_tx, idle_rx) = tokio::sync::oneshot::channel();
            let slot = PredictionSlot::new(prediction, permit, idle_rx);
            idle_tx
                .send(InactiveSlotIdleToken::new(slot_id).activate())
                .unwrap();
            slot.into_idle().await.unwrap();
        }

        assert!(pool.try_acquire().is_some());
    }

    #[tokio::test]
    async fn slot_not_idle_orphans_permit() {
        let pool = PermitPool::new(1);

        let (a, _b) = UnixStream::pair().unwrap();
        let (_, write) = a.into_split();
        let slot_id = SlotId::new();

        pool.add_permit(slot_id, FramedWrite::new(write, JsonCodec::new()));

        {
            let permit = pool.try_acquire().unwrap();
            let prediction = Prediction::new("test_123".to_string(), None);
            let (_idle_tx, idle_rx) = tokio::sync::oneshot::channel();
            let _slot = PredictionSlot::new(prediction, permit, idle_rx);
        }

        assert!(pool.try_acquire().is_none());
    }

    #[tokio::test]
    async fn slot_idle_channel_closed_does_not_return_permit() {
        let pool = PermitPool::new(1);

        let (a, _b) = UnixStream::pair().unwrap();
        let (_, write) = a.into_split();
        let slot_id = SlotId::new();

        pool.add_permit(slot_id, FramedWrite::new(write, JsonCodec::new()));

        let permit = pool.try_acquire().unwrap();
        let prediction = Prediction::new("test_123".to_string(), None);
        let (idle_tx, idle_rx) = tokio::sync::oneshot::channel::<SlotIdleToken>();
        let slot = PredictionSlot::new(prediction, permit, idle_rx);
        drop(idle_tx);

        let result = slot.into_idle().await;
        assert!(matches!(result, Err(SlotError::IdleTokenReceiveError(_))));
        assert!(pool.try_acquire().is_none());
    }
}
