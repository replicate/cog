//! Build script for coglet-python.
//!
//! Captures build metadata and converts semver to PEP 440 for Python compatibility.

use std::process::Command;

fn main() {
    // Convert CARGO_PKG_VERSION (semver) to PEP 440
    let version = env!("CARGO_PKG_VERSION");
    let pep440 = semver_to_pep440(version);
    println!("cargo:rustc-env=COGLET_PEP440_VERSION={pep440}");

    // Git SHA (short)
    let git_sha = Command::new("git")
        .args(["rev-parse", "--short", "HEAD"])
        .output()
        .ok()
        .filter(|o| o.status.success())
        .map(|o| String::from_utf8_lossy(&o.stdout).trim().to_string())
        .unwrap_or_else(|| "unknown".to_string());
    println!("cargo:rustc-env=COGLET_GIT_SHA={git_sha}");

    // Build timestamp (UTC, ISO 8601)
    let build_time = Command::new("date")
        .args(["-u", "+%Y-%m-%dT%H:%M:%SZ"])
        .output()
        .ok()
        .filter(|o| o.status.success())
        .map(|o| String::from_utf8_lossy(&o.stdout).trim().to_string())
        .unwrap_or_else(|| "unknown".to_string());
    println!("cargo:rustc-env=COGLET_BUILD_TIME={build_time}");

    // Rustc version
    let rustc_version = Command::new("rustc")
        .args(["--version"])
        .output()
        .ok()
        .filter(|o| o.status.success())
        .map(|o| String::from_utf8_lossy(&o.stdout).trim().to_string())
        .unwrap_or_else(|| "unknown".to_string());
    println!("cargo:rustc-env=COGLET_RUSTC_VERSION={rustc_version}");

    // Rebuild if git HEAD changes
    println!("cargo:rerun-if-changed=../../.git/HEAD");
    println!("cargo:rerun-if-changed=../../.git/refs");
}

/// Convert a semver version string to PEP 440 format.
///
/// Mapping:
///   0.17.0              → 0.17.0
///   0.17.0-alpha.2      → 0.17.0a2
///   0.17.0-beta.1       → 0.17.0b1
///   0.17.0-rc.3         → 0.17.0rc3
///   0.17.0-dev.4        → 0.17.0.dev4
fn semver_to_pep440(version: &str) -> String {
    let Some((base, pre)) = version.split_once('-') else {
        return version.to_string();
    };

    if let Some(n) = pre.strip_prefix("alpha.") {
        format!("{base}a{n}")
    } else if let Some(n) = pre.strip_prefix("alpha") {
        if n.is_empty() {
            format!("{base}a0")
        } else {
            format!("{base}a{n}")
        }
    } else if let Some(n) = pre.strip_prefix("beta.") {
        format!("{base}b{n}")
    } else if let Some(n) = pre.strip_prefix("beta") {
        if n.is_empty() {
            format!("{base}b0")
        } else {
            format!("{base}b{n}")
        }
    } else if let Some(n) = pre.strip_prefix("rc.") {
        format!("{base}rc{n}")
    } else if let Some(n) = pre.strip_prefix("rc") {
        if n.is_empty() {
            format!("{base}rc0")
        } else {
            format!("{base}rc{n}")
        }
    } else if let Some(n) = pre.strip_prefix("dev.") {
        format!("{base}.dev{n}")
    } else if let Some(n) = pre.strip_prefix("dev") {
        if n.is_empty() {
            format!("{base}.dev0")
        } else {
            format!("{base}.dev{n}")
        }
    } else {
        // Unknown pre-release format, pass through
        version.to_string()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_stable_version() {
        assert_eq!(semver_to_pep440("0.17.0"), "0.17.0");
        assert_eq!(semver_to_pep440("1.0.0"), "1.0.0");
    }

    #[test]
    fn test_alpha() {
        assert_eq!(semver_to_pep440("0.17.0-alpha.2"), "0.17.0a2");
        assert_eq!(semver_to_pep440("0.17.0-alpha.0"), "0.17.0a0");
        assert_eq!(semver_to_pep440("0.17.0-alpha"), "0.17.0a0");
    }

    #[test]
    fn test_beta() {
        assert_eq!(semver_to_pep440("0.17.0-beta.1"), "0.17.0b1");
        assert_eq!(semver_to_pep440("0.17.0-beta"), "0.17.0b0");
    }

    #[test]
    fn test_rc() {
        assert_eq!(semver_to_pep440("0.17.0-rc.3"), "0.17.0rc3");
        assert_eq!(semver_to_pep440("0.17.0-rc"), "0.17.0rc0");
    }

    #[test]
    fn test_dev() {
        assert_eq!(semver_to_pep440("0.17.0-dev.4"), "0.17.0.dev4");
        assert_eq!(semver_to_pep440("0.17.0-dev"), "0.17.0.dev0");
    }
}
