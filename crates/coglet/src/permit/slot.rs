//! PredictionSlot - holds Prediction and Permit side-by-side.
//!
//! This separation allows:
//! - Prediction to be behind Mutex for concurrent updates (logs, outputs, status)
//! - Permit's idle_flag to be set without holding the prediction lock
//! - Clean RAII: dropping PredictionSlot returns permit to pool

use std::sync::{Arc, Mutex};

use super::Permit;
use crate::prediction::Prediction;

/// Holds a prediction and its permit side-by-side.
///
/// The prediction is behind a Mutex for concurrent updates by the event loop.
/// The permit is separate so its idle_flag can be set without locking.
///
/// On drop: Permit returns to pool (if idle) or is orphaned (if poisoned).
/// Webhook sending is handled by PredictionSupervisor, not here.
pub struct PredictionSlot {
    /// The prediction being processed.
    prediction: Arc<Mutex<Prediction>>,
    /// The permit granting access to the slot.
    permit: Permit,
}

impl PredictionSlot {
    /// Create a new prediction slot.
    pub fn new(prediction: Prediction, permit: Permit) -> Self {
        Self {
            prediction: Arc::new(Mutex::new(prediction)),
            permit,
        }
    }

    /// Get a reference to the prediction (for sharing with event loop).
    pub fn prediction(&self) -> Arc<Mutex<Prediction>> {
        Arc::clone(&self.prediction)
    }

    /// Get a mutable reference to the permit (for sending requests).
    pub fn permit_mut(&mut self) -> &mut Permit {
        &mut self.permit
    }

    /// Get the permit (immutable, for reading slot_id, idle_flag, etc).
    pub fn permit(&self) -> &Permit {
        &self.permit
    }

    /// Mark the slot as idle, allowing permit to return to pool on drop.
    ///
    /// Returns an `IdleToken` as proof. Panics if slot is poisoned.
    pub fn into_idle(&mut self) -> super::IdleToken {
        self.permit.into_idle()
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
            tracing::warn!(
                slot = %self.permit.slot_id(),
                prediction_id = %prediction.id(),
                "Slot poisoned - failing non-terminal prediction"
            );
            prediction.set_failed("Slot poisoned".to_string());
        }
        self.permit.into_poisoned()
    }

    /// Check if the slot is marked as idle.
    pub fn is_idle(&self) -> bool {
        self.permit.is_idle()
    }

    /// Check if the slot is poisoned.
    pub fn is_poisoned(&self) -> bool {
        self.permit.is_poisoned()
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
        if !self.permit.is_idle()
            && !self.permit.is_poisoned()
            && let Ok(mut prediction) = self.prediction.try_lock()
            && !prediction.is_terminal()
        {
            tracing::error!(
                slot = %self.permit.slot_id(),
                prediction_id = %prediction.id(),
                "Slot dropped while InUse with non-terminal prediction - failing"
            );
            prediction.set_failed("Slot dropped unexpectedly".to_string());
        }

        // Permit drops after this, returning to pool if idle.
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
        assert_eq!(slot.permit().slot_id(), slot_id);
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

            // Don't mark idle - simulates poisoned slot
        }

        // Permit should NOT be back in pool
        assert!(pool.try_acquire().is_none());
    }
}
