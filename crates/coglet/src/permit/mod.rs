//! Permit pool for concurrent slot management.
//!
//! The permit system uses typestate to enforce valid state transitions at compile time:
//! - `PermitInUse` → `PermitIdle` via `into_idle()` (returns to pool on drop)
//! - `PermitInUse` → `PermitPoisoned` via `into_poisoned()` (orphaned on drop)
//! - `PermitPoisoned` → `PermitIdle`: NOT POSSIBLE (no method exists)
//!
//! Slot poisoning is a pool-level property (`PermitPool::poison()`). A poisoned slot
//! is permanently removed from the pool regardless of whether a prediction was active.
//! `PermitIdle::drop` checks the pool-level poison flag and skips returning the permit.

mod pool;
mod slot;

pub use pool::{
    AnyPermit, InactiveSlotIdleToken, PermitError, PermitIdle, PermitInUse, PermitPoisoned,
    PermitPool, SlotIdleToken,
};
pub use slot::{PredictionSlot, UnregisteredPredictionSlot};
