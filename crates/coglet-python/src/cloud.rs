//! Native cloud object-storage downloads for predictor inputs.
//!
//! Detects cloud-scheme URLs (`s3://`, `gs://`, `az://`) in input values and
//! downloads them to temp files using the `object_store` crate, so the rest of
//! the input pipeline can treat them as ordinary local paths.

use std::sync::Arc;

use object_store::path::Path as ObjectPath;
use object_store::ObjectStore;

/// Errors that can occur while resolving and downloading a cloud object.
#[derive(Debug, thiserror::Error)]
pub enum CloudError {
    #[error("invalid cloud url '{0}'")]
    InvalidUrl(String),
    #[error("failed to build object store for '{url}': {source}")]
    Store {
        url: String,
        #[source]
        source: object_store::Error,
    },
    #[error("failed to download '{url}': {source}")]
    Download {
        url: String,
        #[source]
        source: object_store::Error,
    },
    #[error("failed to write temp file for '{url}': {source}")]
    Io {
        url: String,
        #[source]
        source: std::io::Error,
    },
}

/// Build an object store for the given cloud URL, reading credentials and
/// endpoint configuration from the process environment, and return the store
/// together with the in-bucket object path to fetch.
///
/// Credentials use each provider's standard environment variables, resolved by
/// `object_store`'s builders via `parse_url_opts`:
///   - S3 / R2 / MinIO: `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`,
///     `AWS_SESSION_TOKEN`, `AWS_REGION`, `AWS_ENDPOINT_URL` (set endpoint for R2/MinIO).
///   - GCS: `GOOGLE_SERVICE_ACCOUNT` / `GOOGLE_APPLICATION_CREDENTIALS`.
///   - Azure: `AZURE_STORAGE_ACCOUNT_NAME`, `AZURE_STORAGE_ACCOUNT_KEY`, etc.
pub fn build_store_for_url(url: &str) -> Result<(Arc<dyn ObjectStore>, ObjectPath), CloudError> {
    let parsed = url::Url::parse(url).map_err(|_| CloudError::InvalidUrl(url.to_string()))?;
    // `object_store::parse_url_opts` builds the right store from the URL scheme
    // and an iterator of (config-key, value) options, ignoring keys a given
    // provider does not recognize. Passing the whole process environment lets
    // each provider pick up its standard credentials (verified against
    // object_store 0.13.2: signature is `I: IntoIterator<Item = (K, V)>,
    // K: AsRef<str>, V: Into<String>`, so `Vec<(String, String)>` fits).
    //
    // Offline construction is fine: S3 defaults its region to `us-east-1` and
    // GCS falls back to a lazy instance-credential provider, so neither builder
    // performs network I/O at `build()` time.
    let opts: Vec<(String, String)> = std::env::vars().collect();
    let (store, path) = object_store::parse_url_opts(&parsed, opts).map_err(|source| {
        CloudError::Store {
            url: url.to_string(),
            source,
        }
    })?;
    Ok((Arc::from(store), path))
}

/// Returns true if `s` is a cloud object-storage URL that this module can
/// download (`s3://`, `gs://`, `az://`/`azure://`).
///
/// Note: Cloudflare R2 and MinIO are S3-compatible and have NO scheme of their
/// own. They are addressed with the `s3://` scheme and reached by setting
/// `AWS_ENDPOINT_URL` (and `AWS_REGION=auto` for R2) in the environment, which
/// `build_store_for_url` picks up. So there is intentionally no `r2://` here.
pub fn is_cloud_url(s: &str) -> bool {
    s.starts_with("s3://")
        || s.starts_with("gs://")
        || s.starts_with("az://")
        || s.starts_with("azure://")
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn detects_supported_schemes() {
        assert!(is_cloud_url("s3://bucket/key.png"));
        assert!(is_cloud_url("gs://bucket/key.png"));
        assert!(is_cloud_url("az://container/key.png"));
        assert!(is_cloud_url("azure://container/key.png"));
    }

    #[test]
    fn r2_and_minio_use_s3_scheme() {
        // R2/MinIO have no dedicated scheme; they are addressed via s3://.
        assert!(is_cloud_url("s3://my-r2-bucket/inputs/img.png"));
    }

    #[test]
    fn rejects_non_cloud_schemes() {
        assert!(!is_cloud_url("https://example.com/x.png"));
        assert!(!is_cloud_url("http://example.com/x.png"));
        assert!(!is_cloud_url("data:image/png;base64,AAAA"));
        assert!(!is_cloud_url("/local/path.png"));
        assert!(!is_cloud_url("file.png"));
    }

    #[test]
    fn builds_s3_store_and_extracts_path() {
        // No real network call — building the store and parsing the path is offline.
        let (_store, path) =
            build_store_for_url("s3://my-bucket/inputs/img.png").expect("should build s3 store");
        assert_eq!(path.as_ref(), "inputs/img.png");
    }

    #[test]
    fn builds_gcs_store_and_extracts_path() {
        let (_store, path) =
            build_store_for_url("gs://my-bucket/a/b/c.jpg").expect("should build gcs store");
        assert_eq!(path.as_ref(), "a/b/c.jpg");
    }

    #[test]
    fn rejects_unparseable_url() {
        let err = build_store_for_url("s3://").err();
        assert!(err.is_some(), "empty bucket url should error");
    }
}
