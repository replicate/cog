//! coglet: Rust execution engine for cog models.

mod health;
mod version;

pub mod bridge;

pub use health::{Health, SetupResult, SetupStatus};
pub use version::{VersionInfo, COGLET_VERSION};
