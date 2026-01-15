//! PredictionSlot - holds Prediction and Permit side-by-side.
//!
//! This separation allows:
//! - Prediction to be behind Mutex for concurrent updates (logs, outputs, status)
//! - Permit's idle_flag to be set without holding the prediction lock
//! - Clean RAII: dropping PredictionSlot sends webhook, then returns permit to pool

use std::sync::Arc;
use std::sync::atomic::Ordering;

use tokio::sync::Mutex;

use super::Permit;
use crate::prediction::Prediction;

/// Holds a prediction and its permit side-by-side.
///
/// The prediction is behind a Mutex for concurrent updates by the event loop.
/// The permit is separate so its idle_flag can be set without locking.
///
/// On drop:
/// 1. Sends terminal webhook (if configured)
/// 2. Permit drops → returns to pool (if idle) or orphaned (if poisoned)
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

    /// Mark the slot as idle (called by event loop when prediction completes).
    ///
    /// This allows the permit to be returned to the pool on drop.
    pub fn mark_idle(&self) {
        self.permit.idle_flag().store(true, Ordering::Release);
    }

    /// Check if the slot is marked as idle.
    pub fn is_idle(&self) -> bool {
        self.permit.is_idle()
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
        // Try to send terminal webhook synchronously
        // We need to block to get the lock, but this is in Drop so we can't be async
        if let Ok(mut prediction) = self.prediction.try_lock() {
            if let Some(webhook) = prediction.take_webhook() {
                let response = prediction.build_terminal_response();

                // Spawn thread to send webhook - don't block Drop on network
                std::thread::spawn(move || {
                    webhook.send_terminal_sync(&response);
                });
            }
        } else {
            // Lock held elsewhere - this shouldn't happen in normal flow
            tracing::warn!("PredictionSlot dropped while prediction lock held");
        }

        // Permit drops after this, returning to pool if idle
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::permit::PermitPool;
    use crate::bridge::codec::JsonCodec;
    use crate::bridge::protocol::SlotId;
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
            let slot = PredictionSlot::new(prediction, permit);

            // Mark idle before drop
            slot.mark_idle();
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
