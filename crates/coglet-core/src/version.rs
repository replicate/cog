//! Version information for coglet.

/// Coglet version from Cargo.toml
pub const COGLET_VERSION: &str = env!("CARGO_PKG_VERSION");

/// Version information for the runtime.
#[derive(Debug, Clone, serde::Serialize)]
pub struct VersionInfo {
    /// Coglet runtime version.
    pub coglet: &'static str,
    /// Cog Python SDK version (if available).
    #[serde(skip_serializing_if = "Option::is_none")]
    pub cog: Option<String>,
    /// Python version.
    #[serde(skip_serializing_if = "Option::is_none")]
    pub python: Option<String>,
}

impl Default for VersionInfo {
    fn default() -> Self {
        Self {
            coglet: COGLET_VERSION,
            cog: None,
            python: None,
        }
    }
}

impl VersionInfo {
    /// Create version info with coglet version only.
    pub fn new() -> Self {
        Self::default()
    }

    /// Set cog SDK version.
    pub fn with_cog(mut self, version: String) -> Self {
        self.cog = Some(version);
        self
    }

    /// Set Python version.
    pub fn with_python(mut self, version: String) -> Self {
        self.python = Some(version);
        self
    }
}
