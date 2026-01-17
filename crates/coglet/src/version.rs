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

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn version_info_has_coglet_version() {
        let info = VersionInfo::new();
        assert_eq!(info.coglet, COGLET_VERSION);
        assert!(info.cog.is_none());
        assert!(info.python.is_none());
    }

    #[test]
    fn version_info_builder_pattern() {
        let info = VersionInfo::new()
            .with_cog("0.9.0".to_string())
            .with_python("3.11.0".to_string());

        assert_eq!(info.cog, Some("0.9.0".to_string()));
        assert_eq!(info.python, Some("3.11.0".to_string()));
    }

    #[test]
    fn version_info_serializes_minimal() {
        // Only coglet when no optional fields set
        let info = VersionInfo {
            coglet: "0.1.0",
            cog: None,
            python: None,
        };
        insta::assert_json_snapshot!("version_minimal", info);
    }

    #[test]
    fn version_info_serializes_full() {
        let info = VersionInfo {
            coglet: "0.1.0",
            cog: Some("0.9.0".to_string()),
            python: Some("3.11.0".to_string()),
        };
        insta::assert_json_snapshot!("version_full", info);
    }
}
