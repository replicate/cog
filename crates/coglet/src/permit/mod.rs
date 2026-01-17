//! Permit pool for concurrent slot management.
//!
//! The permit system manages prediction slots with RAII semantics:
//! - Separate permit types enforce valid state transitions at compile time
//! - `PermitPool` manages available permits
//! - `PredictionSlot` holds Prediction + Permit side-by-side
//! - Permits return to pool on drop (if idle) or are orphaned (if poisoned)
//!
//! State transitions enforced at compile time:
//! - `PermitInUse` → `PermitIdle` via `into_idle()`
//! - `PermitInUse` → `PermitPoisoned` via `into_poisoned()`
//! - `PermitPoisoned` → `PermitIdle`: NOT POSSIBLE (no method exists)

mod pool;
mod slot;

pub use pool::{AnyPermit, IdleToken, PermitError, PermitInUse, PermitPool};
pub use slot::PredictionSlot;
