//! Permit pool for concurrent slot management.
//!
//! The permit system uses typestate to enforce valid state transitions at compile time:
//! - `PermitInUse` → `PermitIdle` via `into_idle()` (returns to pool on drop)
//! - `PermitInUse` → `PermitPoisoned` via `into_poisoned()` (orphaned on drop)
//! - `PermitPoisoned` → `PermitIdle`: NOT POSSIBLE (no method exists)

mod pool;
mod slot;

pub use pool::{
    AnyPermit, IdleToken, PermitError, PermitIdle, PermitInUse, PermitPoisoned, PermitPool,
};
pub use slot::PredictionSlot;
