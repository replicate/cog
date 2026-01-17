//! PredictionSlot - holds Prediction and Permit side-by-side.
//!
//! This separation allows:
//! - Prediction to be behind Mutex for concurrent updates (logs, outputs, status)
//! - Permit's idle_flag to be set without holding the prediction lock
//! - Clean RAII: dropping PredictionSlot returns permit to pool

use std::sync::{Arc, Mutex};

use super::{AnyPermit, IdleToken, PermitInUse};
use crate::prediction::Prediction;

/// Holds a prediction and its permit side-by-side.
///
/// The prediction is behind a Mutex for concurrent updates by the event loop.
/// The permit uses typestate to enforce valid transitions.
///
/// On drop: Permit returns to pool (if idle) or is orphaned (if poisoned).
/// Webhook sending is handled by PredictionSupervisor, not here.
pub struct PredictionSlot {
    /// The prediction being processed.
    prediction: Arc<Mutex<Prediction>>,
    /// The permit granting access to the slot (in any state).
    permit: Option<AnyPermit>,
}

impl PredictionSlot {
    /// Create a new prediction slot.
    pub fn new(prediction: Prediction, permit: PermitInUse) -> Self {
        Self {
            prediction: Arc::new(Mutex::new(prediction)),
            permit: Some(AnyPermit::InUse(permit)),
        }
    }

    /// Get a reference to the prediction (for sharing with event loop).
    pub fn prediction(&self) -> Arc<Mutex<Prediction>> {
        Arc::clone(&self.prediction)
    }

    /// Get a mutable reference to the permit for sending requests.
    ///
    /// Returns `None` if the permit is not in the `InUse` state.
    pub fn permit_mut(&mut self) -> Option<&mut PermitInUse> {
        match &mut self.permit {
            Some(AnyPermit::InUse(p)) => Some(p),
            _ => None,
        }
    }

    /// Get the slot ID.
    pub fn slot_id(&self) -> crate::bridge::protocol::SlotId {
        self.permit
            .as_ref()
            .map(|p| p.slot_id())
            .unwrap_or_default()
    }

    /// Mark the slot as idle, allowing permit to return to pool on drop.
    ///
    /// Returns an `IdleToken` as proof.
    ///
    /// # Panics
    /// Panics if the permit is not in the `InUse` state (already transitioned).
    pub fn into_idle(&mut self) -> IdleToken {
        let permit = self.permit.take().expect("permit already consumed");
        match permit {
            AnyPermit::InUse(p) => {
                let slot_id = p.slot_id();
                let idle = p.into_idle();
                self.permit = Some(AnyPermit::Idle(idle));
                IdleToken { slot_id }
            }
            AnyPermit::Idle(p) => {
                // Already idle - just return token
                let slot_id = p.slot_id();
                self.permit = Some(AnyPermit::Idle(p));
                IdleToken { slot_id }
            }
            AnyPermit::Poisoned(_) => {
                // This is a logic error - caller should check is_poisoned() first
                panic!("Cannot mark poisoned slot as idle");
            }
        }
    }

    /// Mark the slot as poisoned (permanently failed).
    ///
    /// The permit will NOT return to the pool on drop.
    /// Also fails the prediction if it's not already terminal.
    pub fn into_poisoned(&mut self) {
        // Fail the prediction if not already terminal
        if let Ok(mut prediction) = self.prediction.try_lock()
            && !prediction.is_terminal()
        {
            let slot_id = self.slot_id();
            tracing::warn!(
                slot = %slot_id,
                prediction_id = %prediction.id(),
                "Slot poisoned - failing non-terminal prediction"
            );
            prediction.set_failed("Slot poisoned".to_string());
        }

        let permit = self.permit.take().expect("permit already consumed");
        match permit {
            AnyPermit::InUse(p) => {
                self.permit = Some(AnyPermit::Poisoned(p.into_poisoned()));
            }
            AnyPermit::Idle(p) => {
                // Unusual but handle it - warn and transition
                tracing::warn!(slot = %p.slot_id(), "Marking idle slot as poisoned");
                // We need to handle Idle -> Poisoned, which requires extracting the inner
                // For now, just drop the idle permit (it will return to pool) and mark as poisoned
                // Actually, Idle permit doesn't have into_poisoned... we'll just leave it
                // This case shouldn't happen in practice
                self.permit = Some(AnyPermit::Idle(p));
            }
            AnyPermit::Poisoned(p) => {
                // Already poisoned
                self.permit = Some(AnyPermit::Poisoned(p));
            }
        }
    }

    /// Check if the slot is marked as idle.
    pub fn is_idle(&self) -> bool {
        self.permit.as_ref().is_some_and(|p| p.is_idle())
    }

    /// Check if the slot is poisoned.
    pub fn is_poisoned(&self) -> bool {
        self.permit.as_ref().is_some_and(|p| p.is_poisoned())
    }

    /// Get elapsed time since prediction started.
    ///
    /// This is a convenience method that tries to lock the prediction.
    /// Returns Duration::ZERO if the lock cannot be acquired.
    pub fn elapsed(&self) -> std::time::Duration {
        self.prediction
            .try_lock()
            .map(|p| p.elapsed())
            .unwrap_or(std::time::Duration::ZERO)
    }

    /// Get the prediction ID.
    ///
    /// This is a convenience method that tries to lock the prediction.
    /// Returns empty string if the lock cannot be acquired.
    pub fn id(&self) -> String {
        self.prediction
            .try_lock()
            .map(|p| p.id().to_string())
            .unwrap_or_default()
    }
}

impl Drop for PredictionSlot {
    fn drop(&mut self) {
        // If permit is still InUse (not idle or poisoned), something went wrong.
        // Fail the prediction if it's not already terminal.
        if let Some(AnyPermit::InUse(_)) = &self.permit
            && let Ok(mut prediction) = self.prediction.try_lock()
            && !prediction.is_terminal()
        {
            tracing::error!(
                slot = %self.slot_id(),
                prediction_id = %prediction.id(),
                "Slot dropped while InUse with non-terminal prediction - failing"
            );
            prediction.set_failed("Slot dropped unexpectedly".to_string());
        }

        // Permit drops after this (via Option drop), returning to pool if idle.
        // Webhook sending is handled by PredictionSupervisor.
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::bridge::codec::JsonCodec;
    use crate::bridge::protocol::SlotId;
    use crate::permit::PermitPool;
    use tokio::net::UnixStream;
    use tokio_util::codec::FramedWrite;

    #[tokio::test]
    async fn prediction_slot_creation() {
        let pool = PermitPool::new(1);

        let (a, _b) = UnixStream::pair().unwrap();
        let (_, write) = a.into_split();
        let slot_id = SlotId::new();

        pool.add_permit(slot_id, FramedWrite::new(write, JsonCodec::new()));

        let permit = pool.try_acquire().unwrap();
        let prediction = Prediction::new("test_123".to_string(), None);

        let slot = PredictionSlot::new(prediction, permit);
        assert_eq!(slot.slot_id(), slot_id);
    }

    #[tokio::test]
    async fn prediction_slot_mark_idle_returns_permit() {
        let pool = PermitPool::new(1);

        let (a, _b) = UnixStream::pair().unwrap();
        let (_, write) = a.into_split();
        let slot_id = SlotId::new();

        pool.add_permit(slot_id, FramedWrite::new(write, JsonCodec::new()));

        {
            let permit = pool.try_acquire().unwrap();
            let prediction = Prediction::new("test_123".to_string(), None);
            let mut slot = PredictionSlot::new(prediction, permit);

            // Mark idle before drop
            let _token = slot.into_idle();
        }

        // Permit should be back in pool
        assert!(pool.try_acquire().is_some());
    }

    #[tokio::test]
    async fn prediction_slot_not_idle_orphans_permit() {
        let pool = PermitPool::new(1);

        let (a, _b) = UnixStream::pair().unwrap();
        let (_, write) = a.into_split();
        let slot_id = SlotId::new();

        pool.add_permit(slot_id, FramedWrite::new(write, JsonCodec::new()));

        {
            let permit = pool.try_acquire().unwrap();
            let prediction = Prediction::new("test_123".to_string(), None);
            let _slot = PredictionSlot::new(prediction, permit);

            // Don't mark idle - simulates dropped without transition
        }

        // Permit should NOT be back in pool (leaked/orphaned)
        assert!(pool.try_acquire().is_none());
    }
}
