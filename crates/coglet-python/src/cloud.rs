//! Native cloud object-storage downloads for predictor inputs.
//!
//! Detects cloud-scheme URLs (`s3://`, `gs://`, `az://`) in input values and
//! downloads them to temp files using the `object_store` crate, so the rest of
//! the input pipeline can treat them as ordinary local paths.

use std::io::Write as _;
use std::sync::Arc;

use object_store::path::Path as ObjectPath;
use object_store::{ObjectStore, ObjectStoreExt};

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
    let (store, path) =
        object_store::parse_url_opts(&parsed, opts).map_err(|source| CloudError::Store {
            url: url.to_string(),
            source,
        })?;
    Ok((Arc::from(store), path))
}

/// Async: fetch the full object body for an already-built store + path.
/// Composable so callers can run many fetches concurrently under one runtime.
async fn fetch_bytes(
    store: &dyn ObjectStore,
    path: &ObjectPath,
    url: &str,
) -> Result<bytes::Bytes, CloudError> {
    let get_result = store
        .get(path)
        .await
        .map_err(|source| CloudError::Download {
            url: url.to_string(),
            source,
        })?;
    get_result
        .bytes()
        .await
        .map_err(|source| CloudError::Download {
            url: url.to_string(),
            source,
        })
}

/// Write already-fetched bytes to a uniquely-named temp file, preserving the
/// suggested filename as the suffix (so the extension survives). Returns the
/// temp file path; cleanup happens later via PreparedInput's drop calling
/// unlink() on the path we hand to Python.
fn write_temp(
    url: &str,
    suggested_filename: &str,
    data: &[u8],
) -> Result<std::path::PathBuf, CloudError> {
    let suffix = sanitize_suffix(suggested_filename);
    let mut temp = tempfile::Builder::new()
        .suffix(&suffix)
        .tempfile()
        .map_err(|source| CloudError::Io {
            url: url.to_string(),
            source,
        })?;
    temp.write_all(data).map_err(|source| CloudError::Io {
        url: url.to_string(),
        source,
    })?;
    let (_file, pathbuf) = temp.keep().map_err(|e| CloudError::Io {
        url: url.to_string(),
        source: e.error,
    })?;
    Ok(pathbuf)
}

/// Download MANY cloud URLs to temp files concurrently, returning the local
/// temp paths in the SAME ORDER as the input `urls`.
///
/// This is synchronous (blocks on a single tokio runtime) but performs all
/// network transfers concurrently via `try_join_all`. If any download fails,
/// the whole call fails (first error wins) and already-written temp files are
/// best-effort removed. Callers holding the GIL should wrap this in
/// `py.allow_threads(...)`.
pub fn download_many_to_temp(urls: &[String]) -> Result<Vec<std::path::PathBuf>, CloudError> {
    if urls.is_empty() {
        return Ok(Vec::new());
    }
    // Build a store + object path for each URL up front (offline, no network).
    let mut stores: Vec<(Arc<dyn ObjectStore>, ObjectPath, String, String)> =
        Vec::with_capacity(urls.len());
    for url in urls {
        let (store, path) = build_store_for_url(url)?;
        let filename = url.rsplit('/').next().unwrap_or("file").to_string();
        stores.push((store, path, url.clone(), filename));
    }

    let runtime = pyo3_async_runtimes::tokio::get_runtime();
    let bodies: Vec<bytes::Bytes> = runtime.block_on(async {
        let fetches = stores
            .iter()
            .map(|(store, path, url, _)| fetch_bytes(store.as_ref(), path, url));
        futures::future::try_join_all(fetches).await
    })?;

    // Write each body to a temp file, preserving order. Roll back on error.
    let mut written: Vec<std::path::PathBuf> = Vec::with_capacity(bodies.len());
    for (body, (_, _, url, filename)) in bodies.iter().zip(stores.iter()) {
        match write_temp(url, filename, body) {
            Ok(p) => written.push(p),
            Err(e) => {
                for p in &written {
                    std::fs::remove_file(p).ok();
                }
                return Err(e);
            }
        }
    }
    Ok(written)
}

/// Build a filesystem-safe temp-file suffix from a suggested filename.
fn sanitize_suffix(name: &str) -> String {
    let base = name.rsplit('/').next().unwrap_or(name);
    if base.is_empty() {
        return String::new();
    }
    format!("-{}", base.replace(['\0', '/'], "_"))
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

    #[test]
    fn fetch_and_write_temp_roundtrips() {
        use object_store::ObjectStoreExt as _;
        use object_store::memory::InMemory;

        let runtime = pyo3_async_runtimes::tokio::get_runtime();
        let store = InMemory::new();
        let obj_path = ObjectPath::from("inputs/hello.txt");
        runtime
            .block_on(store.put(&obj_path, b"hello cloud".to_vec().into()))
            .expect("seed object");

        let body = runtime
            .block_on(fetch_bytes(
                &store,
                &obj_path,
                "s3://bucket/inputs/hello.txt",
            ))
            .expect("fetch should succeed");
        let temp = write_temp("s3://bucket/inputs/hello.txt", "hello.txt", &body)
            .expect("write should succeed");

        let contents = std::fs::read(&temp).expect("temp file should exist");
        assert_eq!(contents, b"hello cloud");
        assert!(
            temp.to_string_lossy().ends_with("hello.txt"),
            "temp file should preserve filename suffix, got {temp:?}"
        );
        std::fs::remove_file(&temp).ok();
    }

    #[test]
    fn parallel_download_preserves_order() {
        // Seed three objects in a single InMemory store, then fetch them
        // concurrently via try_join_all and assert ordering. This is the
        // ordering contract for download_many_to_temp (which builds a fresh
        // store per URL from env, so it cannot reuse this single InMemory).
        use object_store::ObjectStoreExt as _;
        use object_store::memory::InMemory;

        let runtime = pyo3_async_runtimes::tokio::get_runtime();
        let store = InMemory::new();
        for (i, name) in ["a.txt", "b.txt", "c.txt"].iter().enumerate() {
            let p = ObjectPath::from(format!("in/{name}"));
            runtime
                .block_on(store.put(&p, format!("body-{i}").into_bytes().into()))
                .expect("seed");
        }
        let paths = [
            ObjectPath::from("in/a.txt"),
            ObjectPath::from("in/b.txt"),
            ObjectPath::from("in/c.txt"),
        ];
        let bodies = runtime
            .block_on(async {
                let fs = paths
                    .iter()
                    .map(|p| fetch_bytes(&store, p, "s3://bucket/x"));
                futures::future::try_join_all(fs).await
            })
            .expect("all fetches succeed");
        assert_eq!(bodies[0].as_ref(), b"body-0");
        assert_eq!(bodies[1].as_ref(), b"body-1");
        assert_eq!(bodies[2].as_ref(), b"body-2");
    }

    #[test]
    fn download_many_empty_is_ok() {
        assert!(download_many_to_temp(&[]).expect("empty ok").is_empty());
    }

    #[test]
    fn sanitize_suffix_strips_path_and_nulls() {
        assert_eq!(sanitize_suffix("a/b/c.png"), "-c.png");
        assert_eq!(sanitize_suffix(""), "");
    }
}
