//! Permit pool for concurrent slot management.
//!
//! The permit system manages prediction slots with RAII semantics:
//! - `Permit` owns a slot's socket writer and tracks idle state
//! - `PermitPool` manages available permits
//! - `PredictionSlot` holds Prediction + Permit side-by-side
//! - Permits return to pool on drop (if idle) or are orphaned (if poisoned)

mod pool;
mod slot;

pub use pool::{Permit, PermitError, PermitPool};
pub use slot::PredictionSlot;
