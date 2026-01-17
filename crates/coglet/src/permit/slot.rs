//! PredictionSlot - holds Prediction and Permit side-by-side.
//!
//! This separation allows the prediction to be behind Mutex for concurrent
//! updates while the permit's idle_flag can be set without holding the lock.

use std::sync::{Arc, Mutex};

use super::{AnyPermit, IdleToken, PermitInUse};
use crate::bridge::protocol::SlotId;
use crate::prediction::Prediction;

/// Holds a prediction and its permit side-by-side.
///
/// On drop: Permit returns to pool (if idle) or is orphaned (if poisoned).
pub struct PredictionSlot {
    prediction: Arc<Mutex<Prediction>>,
    slot_id: SlotId,
    permit: Option<AnyPermit>,
}

impl PredictionSlot {
    pub fn new(prediction: Prediction, permit: PermitInUse) -> Self {
        let slot_id = permit.slot_id();
        Self {
            prediction: Arc::new(Mutex::new(prediction)),
            slot_id,
            permit: Some(AnyPermit::InUse(permit)),
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

    /// Mark the slot as idle - permit will return to pool on drop.
    ///
    /// # Panics
    /// Panics if permit is poisoned.
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
                let slot_id = p.slot_id();
                self.permit = Some(AnyPermit::Idle(p));
                IdleToken { slot_id }
            }
            AnyPermit::Poisoned(_) => {
                panic!("Cannot mark poisoned slot as idle");
            }
        }
    }

    /// Mark the slot as poisoned - permit will NOT return to pool.
    ///
    /// Also fails the prediction if not already terminal.
    ///
    /// # Panics
    /// Panics if permit is idle (completed successfully, should not be poisoned).
    pub fn into_poisoned(&mut self) {
        if let Ok(mut prediction) = self.prediction.try_lock()
            && !prediction.is_terminal()
        {
            tracing::warn!(
                slot = %self.slot_id,
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
            AnyPermit::Idle(_) => {
                panic!(
                    "Cannot poison idle slot {} - prediction already completed",
                    self.slot_id
                );
            }
            AnyPermit::Poisoned(p) => {
                self.permit = Some(AnyPermit::Poisoned(p));
            }
        }
    }

    pub fn is_idle(&self) -> bool {
        self.permit.as_ref().is_some_and(|p| p.is_idle())
    }

    pub fn is_poisoned(&self) -> bool {
        self.permit.as_ref().is_some_and(|p| p.is_poisoned())
    }

    pub fn elapsed(&self) -> std::time::Duration {
        self.prediction
            .try_lock()
            .map(|p| p.elapsed())
            .unwrap_or(std::time::Duration::ZERO)
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
    use crate::permit::PermitPool;
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

        let slot = PredictionSlot::new(prediction, permit);
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
            let mut slot = PredictionSlot::new(prediction, permit);

            let _token = slot.into_idle();
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
            let _slot = PredictionSlot::new(prediction, permit);
        }

        assert!(pool.try_acquire().is_none());
    }
}
