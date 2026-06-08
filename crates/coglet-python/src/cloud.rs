//! Native cloud object-storage downloads for predictor inputs.
//!
//! Detects cloud-scheme URLs (`s3://`, `gs://`, `az://`) in input values and
//! downloads them to temp files using the `object_store` crate, so the rest of
//! the input pipeline can treat them as ordinary local paths.

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
}
