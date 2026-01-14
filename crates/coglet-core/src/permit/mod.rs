//! Permit pool for concurrent slot management.
//!
//! The permit system manages prediction slots with RAII semantics:
//! - `Permit` owns a slot's socket writer and tracks idle state
//! - `PermitPool` manages available permits
//! - Permits return to pool on drop (if idle) or are orphaned (if poisoned)
//!
//! Note: `PredictionSlot` (holds Prediction + Permit) will be added when
//! we wire up PredictionService to use PermitPool.

mod pool;
mod slot;

pub use pool::{Permit, PermitError, PermitPool};
