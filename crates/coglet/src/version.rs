//! Version information for coglet.

/// Coglet version from Cargo.toml
pub const COGLET_VERSION: &str = env!("CARGO_PKG_VERSION");

/// Version information for the runtime.
#[derive(Debug, Clone, serde::Serialize)]
pub struct VersionInfo {
    /// Coglet runtime version.
    pub coglet: &'static str,
    /// Git SHA (with optional `-dirty` suffix).
    #[serde(skip_serializing_if = "Option::is_none")]
    pub git_sha: Option<String>,
    /// Build timestamp (UTC, ISO 8601).
    #[serde(skip_serializing_if = "Option::is_none")]
    pub build_time: Option<String>,
    /// Python SDK version (if available).
    #[serde(skip_serializing_if = "Option::is_none")]
    pub python_sdk: Option<String>,
    /// Python version.
    #[serde(skip_serializing_if = "Option::is_none")]
    pub python: Option<String>,
}

impl Default for VersionInfo {
    fn default() -> Self {
        Self {
            coglet: COGLET_VERSION,
            git_sha: None,
            build_time: None,
            python_sdk: None,
            python: None,
        }
    }
}

impl VersionInfo {
    /// Create version info with coglet version only.
    pub fn new() -> Self {
        Self::default()
    }

    /// Set git SHA (with optional `-dirty` suffix).
    pub fn with_git_sha(mut self, sha: String) -> Self {
        self.git_sha = Some(sha);
        self
    }

    /// Set build timestamp.
    pub fn with_build_time(mut self, time: String) -> Self {
        self.build_time = Some(time);
        self
    }

    /// Set Python SDK version.
    pub fn with_python_sdk(mut self, version: String) -> Self {
        self.python_sdk = Some(version);
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
        assert!(info.python_sdk.is_none());
        assert!(info.python.is_none());
    }

    #[test]
    fn version_info_builder_pattern() {
        let info = VersionInfo::new()
            .with_git_sha("abc1234".to_string())
            .with_build_time("2026-03-12T18:00:00Z".to_string())
            .with_python_sdk("0.9.0".to_string())
            .with_python("3.11.0".to_string());

        assert_eq!(info.git_sha, Some("abc1234".to_string()));
        assert_eq!(info.build_time, Some("2026-03-12T18:00:00Z".to_string()));
        assert_eq!(info.python_sdk, Some("0.9.0".to_string()));
        assert_eq!(info.python, Some("3.11.0".to_string()));
    }

    #[test]
    fn version_info_serializes_minimal() {
        // Only coglet when no optional fields set
        let info = VersionInfo {
            coglet: "0.1.0",
            git_sha: None,
            build_time: None,
            python_sdk: None,
            python: None,
        };
        insta::assert_json_snapshot!("version_minimal", info);
    }

    #[test]
    fn version_info_serializes_full() {
        let info = VersionInfo {
            coglet: "0.1.0",
            git_sha: Some("abc1234-dirty".to_string()),
            build_time: Some("2026-03-12T18:00:00Z".to_string()),
            python_sdk: Some("0.9.0".to_string()),
            python: Some("3.11.0".to_string()),
        };
        insta::assert_json_snapshot!("version_full", info);
    }
}
