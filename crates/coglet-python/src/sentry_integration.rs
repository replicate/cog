//! Sentry error reporting integration.
//!
//! Initializes Sentry when `SENTRY_DSN` is present in the environment.
//! When no DSN is configured, the SDK creates a disabled (no-op) client
//! with zero runtime overhead.

use tracing::info;

/// Guard returned by [`init_sentry`]. Must be held for the lifetime of the
/// process — dropping it flushes pending events and shuts down the SDK.
pub type SentryGuard = Option<sentry::ClientInitGuard>;

/// Initialize the Sentry SDK if `SENTRY_DSN` is set.
///
/// Returns `Some(guard)` when Sentry is active, `None` otherwise.
/// The guard **must** be held until process exit to ensure events are flushed.
///
/// This must be called **before** `init_tracing()` so the sentry tracing layer
/// can be registered with the subscriber (see [`sentry_tracing_layer`]).
pub fn init_sentry() -> SentryGuard {
    // sentry::init with an empty DSN string reads SENTRY_DSN from env.
    // If neither is set, it returns a guard with a disabled client.
    let guard = sentry::init(sentry::ClientOptions {
        release: Some(env!("CARGO_PKG_VERSION").into()),
        // Only send events, not performance traces
        traces_sample_rate: 0.0,
        // Attach stack traces to events
        attach_stacktrace: true,
        ..Default::default()
    });

    if guard.is_enabled() {
        info!("Sentry error reporting enabled");
        Some(guard)
    } else {
        None
    }
}

/// Configure Sentry scope with runtime context.
///
/// Call this after the predictor configuration is known to enrich all
/// subsequent events with model/server metadata.
pub fn configure_sentry_scope(
    predictor_ref: &str,
    max_concurrency: usize,
    coglet_version: &str,
    python_version: Option<&str>,
    sdk_version: Option<&str>,
) {
    sentry::configure_scope(|scope| {
        scope.set_tag("coglet.version", coglet_version);
        scope.set_tag("coglet.predictor_ref", predictor_ref);
        scope.set_tag("coglet.max_concurrency", max_concurrency);

        if let Some(pv) = python_version {
            scope.set_tag("python.version", pv);
        }
        if let Some(sv) = sdk_version {
            scope.set_tag("cog.sdk_version", sv);
        }
    });
}

/// Create the Sentry tracing layer for integration with `tracing-subscriber`.
///
/// This layer captures `ERROR`-level tracing events as Sentry events and
/// lower-level events as breadcrumbs for context.
///
/// Returns `None` if Sentry is not initialized (no DSN), in which case
/// the `Option<Layer>` acts as a no-op in the subscriber stack.
pub fn sentry_tracing_layer<S>() -> Option<sentry::integrations::tracing::SentryLayer<S>>
where
    S: tracing::Subscriber + for<'a> tracing_subscriber::registry::LookupSpan<'a>,
{
    use sentry::integrations::tracing::EventFilter;

    // Only create the layer if Sentry is actually enabled
    let client = sentry::Hub::current().client()?;
    if !client.is_enabled() {
        return None;
    }

    let layer = sentry::integrations::tracing::layer().event_filter(|md| {
        match *md.level() {
            // Capture error-level events as Sentry issues
            tracing::Level::ERROR => EventFilter::Event,
            // Capture warn as breadcrumbs for context on future errors
            tracing::Level::WARN => EventFilter::Breadcrumb,
            // Ignore info/debug/trace to avoid noise
            _ => EventFilter::Ignore,
        }
    });

    Some(layer)
}
