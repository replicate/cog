//! Webhook sender for async predictions.
//!
//! Implements the cog webhook protocol:
//! - Throttling (default 500ms between non-terminal updates)
//! - Webhooks for a prediction delivered in order, terminal last
//! - Terminal webhooks retried with exponential backoff
//! - WEBHOOK_AUTH_TOKEN bearer authentication
//! - Events filtering (start, output, logs, completed)
//!
//! # Panic Safety
//!
//! This module avoids panics:
//! - `WebhookSender::new()` returns `Result` for HTTP client creation
//! - Mutex locks use `lock().unwrap_or_else(|e| e.into_inner())` to recover from
//!   poison - worst case is we lose throttle tracking, which is acceptable.

use std::collections::HashSet;
use std::sync::{Arc, Mutex};
use std::time::{Duration, Instant};

use thiserror::Error;
use tokio::sync::Notify;
use tokio::task::JoinHandle;

use serde::{Deserialize, Serialize};

use crate::version::COGLET_VERSION;

#[derive(Debug, Clone, Copy, Default, PartialEq, Eq, Hash, Deserialize, Serialize)]
#[serde(rename_all = "lowercase")]
pub enum WebhookEventType {
    Start,
    Output,
    Logs,
    #[default]
    Completed,
}

impl WebhookEventType {
    pub fn is_terminal(&self) -> bool {
        matches!(self, Self::Completed)
    }

    pub fn all() -> HashSet<WebhookEventType> {
        [Self::Start, Self::Output, Self::Logs, Self::Completed]
            .into_iter()
            .collect()
    }
}

/// Error creating a WebhookSender.
#[derive(Debug, Error)]
pub enum WebhookSenderError {
    #[error("failed to create HTTP client: {0}")]
    HttpClient(#[from] reqwest::Error),
}

#[derive(Debug, Clone)]
pub struct WebhookConfig {
    pub response_interval: Duration,
    pub events_filter: HashSet<WebhookEventType>,
    pub max_retries: u32,
    pub backoff_base: Duration,
    pub retry_status_codes: Vec<u16>,
}

impl Default for WebhookConfig {
    fn default() -> Self {
        Self {
            response_interval: Duration::from_millis(
                std::env::var("COG_THROTTLE_RESPONSE_INTERVAL")
                    .ok()
                    .and_then(|s| s.parse::<f64>().ok())
                    .map(|s| (s * 1000.0) as u64)
                    .unwrap_or(500),
            ),
            events_filter: WebhookEventType::all(),
            max_retries: 12,
            backoff_base: Duration::from_millis(100),
            retry_status_codes: vec![429, 500, 502, 503, 504],
        }
    }
}

/// W3C Trace Context for distributed tracing.
#[derive(Debug, Clone, Default)]
pub struct TraceContext {
    pub traceparent: Option<String>,
    pub tracestate: Option<String>,
    pub custom_header: Option<(String, String)>,
}

pub struct WebhookSender {
    url: String,
    config: WebhookConfig,
    client: reqwest::Client,
    last_sent: Mutex<Instant>,
    trace_context: TraceContext,
    queue: Arc<WebhookQueue>,
}

/// Sends non-terminal webhooks one at a time from a single task, so they
/// arrive in order and before the terminal webhook. Spawning a task per
/// request would let a later request, including the terminal one, reach the
/// receiver first.
///
/// Each payload is a full snapshot of the prediction, so only the newest
/// request not yet sent matters. A new request replaces the one waiting, and
/// `send_terminal` drops whatever is still waiting.
#[derive(Default)]
struct WebhookQueue {
    state: Mutex<QueueState>,
    ready: Notify,
}

#[derive(Default)]
struct QueueState {
    /// The newest request not yet sent.
    pending: Option<reqwest::RequestBuilder>,
    /// Runs `send_queued`. Started by the first non-terminal webhook.
    task: Option<JoinHandle<()>>,
    /// Set when `send_terminal` starts or the sender is dropped. Later
    /// non-terminal webhooks are dropped.
    closed: bool,
}

impl WebhookQueue {
    fn lock(&self) -> std::sync::MutexGuard<'_, QueueState> {
        // Recover from poison - fields are only ever replaced whole
        self.state.lock().unwrap_or_else(|e| e.into_inner())
    }

    /// Stop sending, drop the waiting request and return the task so the
    /// caller can wait for the request in flight.
    fn close(&self) -> Option<JoinHandle<()>> {
        let task = {
            let mut state = self.lock();
            state.closed = true;
            state.pending = None;
            state.task.take()
        };
        self.ready.notify_one();
        task
    }
}

async fn send_queued(queue: Arc<WebhookQueue>) {
    loop {
        let request = {
            let mut state = queue.lock();
            if state.closed {
                return;
            }
            state.pending.take()
        };
        match request {
            Some(request) => {
                if let Err(e) = request.send().await {
                    tracing::warn!(error = %e, "Failed to send webhook (non-terminal)");
                }
            }
            // notify_one stores a permit, so a request queued since the
            // check above isn't missed.
            None => queue.ready.notified().await,
        }
    }
}

impl Drop for WebhookSender {
    fn drop(&mut self) {
        // Lets the task exit; a request in flight still finishes.
        self.queue.close();
    }
}

impl WebhookSender {
    pub fn new(url: String, config: WebhookConfig) -> Result<Self, WebhookSenderError> {
        Self::with_trace_context(url, config, TraceContext::default())
    }

    pub fn with_trace_context(
        url: String,
        config: WebhookConfig,
        trace_context: TraceContext,
    ) -> Result<Self, WebhookSenderError> {
        let mut headers = reqwest::header::HeaderMap::new();

        if let Ok(token) = std::env::var("WEBHOOK_AUTH_TOKEN")
            && let Ok(value) = reqwest::header::HeaderValue::from_str(&format!("Bearer {}", token))
        {
            headers.insert(reqwest::header::AUTHORIZATION, value);
        }

        let user_agent = format!("coglet/{}", COGLET_VERSION);
        if let Ok(value) = reqwest::header::HeaderValue::from_str(&user_agent) {
            headers.insert(reqwest::header::USER_AGENT, value);
        }

        let client = reqwest::Client::builder()
            .default_headers(headers)
            .timeout(Duration::from_secs(30))
            .build()?;

        Ok(Self {
            url,
            config,
            client,
            // Allow immediate first send
            last_sent: Mutex::new(Instant::now() - Duration::from_secs(10)),
            trace_context,
            queue: Arc::default(),
        })
    }

    pub fn url(&self) -> &str {
        &self.url
    }

    fn should_send(&self, event: WebhookEventType) -> bool {
        if !self.config.events_filter.contains(&event) {
            return false;
        }

        if event.is_terminal() {
            return true;
        }

        // Output events are never throttled: they are high-value (contain actual
        // prediction results), relatively infrequent (one per output chunk/file),
        // and in the old Python runtime were effectively unthrottled because file
        // uploads were synchronous.  Throttling them causes the director to miss
        // intermediate output data.
        if matches!(event, WebhookEventType::Output) {
            return true;
        }

        // Recover from poison - losing throttle state is acceptable
        let last = self.last_sent.lock().unwrap_or_else(|e| e.into_inner());
        last.elapsed() >= self.config.response_interval
    }

    fn update_last_sent(&self) {
        // Recover from poison - losing throttle state is acceptable
        let mut last = self.last_sent.lock().unwrap_or_else(|e| e.into_inner());
        *last = Instant::now();
    }

    fn build_request(&self, payload: &serde_json::Value) -> reqwest::RequestBuilder {
        let mut request = self.client.post(&self.url).json(payload);

        if let Some(ref traceparent) = self.trace_context.traceparent {
            request = request.header("traceparent", traceparent);
        }
        if let Some(ref tracestate) = self.trace_context.tracestate {
            request = request.header("tracestate", tracestate);
        }
        if let Some((ref name, ref value)) = self.trace_context.custom_header {
            request = request.header(name, value);
        }

        request
    }

    /// Queue a non-terminal webhook (fire and forget, no retry).
    ///
    /// Webhooks are sent one at a time, in order. If an earlier one is still
    /// waiting to be sent, this one replaces it.
    pub fn send(&self, event: WebhookEventType, payload: &serde_json::Value) {
        if !self.should_send(event) {
            return;
        }

        let request = self.build_request(payload);
        self.update_last_sent();

        let mut state = self.queue.lock();
        if state.closed {
            tracing::debug!("Dropping non-terminal webhook sent after the terminal webhook");
            return;
        }
        state.pending = Some(request);
        if state.task.is_none() {
            state.task = Some(tokio::spawn(send_queued(Arc::clone(&self.queue))));
        }
        drop(state);
        self.queue.ready.notify_one();
    }

    /// Send a terminal webhook with exponential backoff retries.
    ///
    /// Waits for a non-terminal webhook already in flight so the terminal one
    /// is always delivered last. A non-terminal webhook still waiting is
    /// dropped; the terminal payload has the newer state.
    pub async fn send_terminal(&self, event: WebhookEventType, payload: &serde_json::Value) {
        if let Some(task) = self.queue.close()
            && let Err(e) = task.await
        {
            tracing::warn!(error = %e, "Webhook queue task failed");
        }

        if !self.config.events_filter.contains(&event) {
            return;
        }

        let mut attempt = 0;
        loop {
            match self.build_request(payload).send().await {
                Ok(response) => {
                    let status = response.status().as_u16();
                    if response.status().is_success() {
                        tracing::debug!(status = %status, "Terminal webhook sent successfully");
                        return;
                    }

                    if self.config.retry_status_codes.contains(&status) {
                        attempt += 1;
                        if attempt > self.config.max_retries {
                            tracing::error!(
                                status = %status,
                                attempts = attempt,
                                "Terminal webhook failed after max retries"
                            );
                            return;
                        }

                        let backoff = self.config.backoff_base * (1 << attempt.min(10));
                        tracing::warn!(
                            status = %status,
                            attempt = attempt,
                            backoff_ms = backoff.as_millis(),
                            "Terminal webhook failed, retrying"
                        );
                        tokio::time::sleep(backoff).await;
                        continue;
                    }

                    tracing::error!(
                        status = %status,
                        "Terminal webhook failed with non-retryable status"
                    );
                    return;
                }
                Err(e) => {
                    attempt += 1;
                    if attempt > self.config.max_retries {
                        tracing::error!(
                            error = %e,
                            attempts = attempt,
                            "Terminal webhook failed after max retries"
                        );
                        return;
                    }

                    let backoff = self.config.backoff_base * (1 << attempt.min(10));
                    tracing::warn!(
                        error = %e,
                        attempt = attempt,
                        backoff_ms = backoff.as_millis(),
                        "Terminal webhook request error, retrying"
                    );
                    tokio::time::sleep(backoff).await;
                }
            }
        }
    }

    /// Send a terminal webhook synchronously (for Drop contexts).
    ///
    /// Uses ureq (blocking HTTP) instead of reqwest for non-async contexts.
    pub fn send_terminal_sync(&self, payload: &serde_json::Value) {
        if !self
            .config
            .events_filter
            .contains(&WebhookEventType::Completed)
        {
            return;
        }

        let agent = ureq::Agent::config_builder()
            .timeout_global(Some(Duration::from_secs(30)))
            .tls_config(
                ureq::tls::TlsConfig::builder()
                    .root_certs(ureq::tls::RootCerts::PlatformVerifier)
                    .build(),
            )
            .build()
            .new_agent();

        let auth_header = std::env::var("WEBHOOK_AUTH_TOKEN")
            .ok()
            .map(|token| format!("Bearer {}", token));

        let user_agent = format!("coglet/{}", COGLET_VERSION);

        let mut attempt = 0;
        loop {
            let mut request = agent
                .post(&self.url)
                .header("Content-Type", "application/json")
                .header("User-Agent", &user_agent);

            if let Some(ref auth) = auth_header {
                request = request.header("Authorization", auth);
            }

            if let Some(ref traceparent) = self.trace_context.traceparent {
                request = request.header("traceparent", traceparent);
            }
            if let Some(ref tracestate) = self.trace_context.tracestate {
                request = request.header("tracestate", tracestate);
            }
            if let Some((ref name, ref value)) = self.trace_context.custom_header {
                request = request.header(name, value);
            }

            let result = request.send_json(payload);

            match result {
                Ok(response) => {
                    let status = response.status().as_u16();
                    if (200..300).contains(&status) {
                        tracing::debug!(status = %status, "Terminal webhook (sync) sent successfully");
                        return;
                    }

                    if self.config.retry_status_codes.contains(&status) {
                        attempt += 1;
                        if attempt > self.config.max_retries {
                            tracing::error!(
                                status = %status,
                                attempts = attempt,
                                "Terminal webhook (sync) failed after max retries"
                            );
                            return;
                        }

                        let backoff = self.config.backoff_base * (1 << attempt.min(10));
                        tracing::warn!(
                            status = %status,
                            attempt = attempt,
                            backoff_ms = backoff.as_millis(),
                            "Terminal webhook (sync) failed, retrying"
                        );
                        std::thread::sleep(backoff);
                        continue;
                    }

                    tracing::error!(
                        status = %status,
                        "Terminal webhook (sync) failed with non-retryable status"
                    );
                    return;
                }
                Err(e) => {
                    attempt += 1;
                    if attempt > self.config.max_retries {
                        tracing::error!(
                            error = %e,
                            attempts = attempt,
                            "Terminal webhook (sync) failed after max retries"
                        );
                        return;
                    }

                    let backoff = self.config.backoff_base * (1 << attempt.min(10));
                    tracing::warn!(
                        error = %e,
                        attempt = attempt,
                        backoff_ms = backoff.as_millis(),
                        "Terminal webhook (sync) request error, retrying"
                    );
                    std::thread::sleep(backoff);
                }
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use wiremock::matchers::{body_partial_json, method, path};
    use wiremock::{Mock, MockServer, ResponseTemplate};

    /// Install the ring crypto provider once for all webhook tests.
    fn init() {
        crate::install_crypto_provider();
    }

    #[test]
    fn config_defaults() {
        let config = WebhookConfig::default();
        assert_eq!(config.response_interval, Duration::from_millis(500));
        assert_eq!(config.max_retries, 12);
        assert!(config.events_filter.contains(&WebhookEventType::Start));
        assert!(config.events_filter.contains(&WebhookEventType::Completed));
    }

    #[test]
    fn event_is_terminal() {
        assert!(!WebhookEventType::Start.is_terminal());
        assert!(!WebhookEventType::Output.is_terminal());
        assert!(!WebhookEventType::Logs.is_terminal());
        assert!(WebhookEventType::Completed.is_terminal());
    }

    fn test_config() -> WebhookConfig {
        init();
        WebhookConfig {
            response_interval: Duration::ZERO,
            max_retries: 2,
            backoff_base: Duration::from_millis(1),
            ..Default::default()
        }
    }

    #[tokio::test]
    async fn send_terminal_posts_json() {
        let server = MockServer::start().await;

        Mock::given(method("POST"))
            .and(path("/webhook"))
            .respond_with(ResponseTemplate::new(200))
            .expect(1)
            .mount(&server)
            .await;

        let url = format!("{}/webhook", server.uri());
        let sender = WebhookSender::new(url, test_config()).unwrap();

        sender
            .send_terminal(
                WebhookEventType::Completed,
                &serde_json::json!({"id": "pred_123", "status": "succeeded"}),
            )
            .await;
    }

    #[tokio::test]
    async fn send_terminal_retries_on_500() {
        let server = MockServer::start().await;

        Mock::given(method("POST"))
            .and(path("/webhook"))
            .respond_with(ResponseTemplate::new(500))
            .up_to_n_times(1)
            .mount(&server)
            .await;

        Mock::given(method("POST"))
            .and(path("/webhook"))
            .respond_with(ResponseTemplate::new(200))
            .expect(1)
            .mount(&server)
            .await;

        let url = format!("{}/webhook", server.uri());
        let sender = WebhookSender::new(url, test_config()).unwrap();

        sender
            .send_terminal(
                WebhookEventType::Completed,
                &serde_json::json!({"status": "succeeded"}),
            )
            .await;
    }

    #[tokio::test]
    async fn send_terminal_no_retry_on_400() {
        let server = MockServer::start().await;

        Mock::given(method("POST"))
            .and(path("/webhook"))
            .respond_with(ResponseTemplate::new(400))
            .expect(1)
            .mount(&server)
            .await;

        let url = format!("{}/webhook", server.uri());
        let sender = WebhookSender::new(url, test_config()).unwrap();

        sender
            .send_terminal(
                WebhookEventType::Completed,
                &serde_json::json!({"status": "succeeded"}),
            )
            .await;
    }

    #[tokio::test]
    async fn send_terminal_respects_filter() {
        let server = MockServer::start().await;

        Mock::given(method("POST"))
            .and(path("/webhook"))
            .respond_with(ResponseTemplate::new(200))
            .expect(0)
            .mount(&server)
            .await;

        let url = format!("{}/webhook", server.uri());
        let config = WebhookConfig {
            events_filter: [WebhookEventType::Start].into_iter().collect(),
            ..test_config()
        };
        let sender = WebhookSender::new(url, config).unwrap();

        sender
            .send_terminal(
                WebhookEventType::Completed,
                &serde_json::json!({"status": "succeeded"}),
            )
            .await;
    }

    #[tokio::test]
    async fn send_non_terminal_fires_and_forgets() {
        let server = MockServer::start().await;

        Mock::given(method("POST"))
            .and(path("/webhook"))
            .respond_with(ResponseTemplate::new(200))
            .expect(1)
            .mount(&server)
            .await;

        let url = format!("{}/webhook", server.uri());
        let sender = WebhookSender::new(url, test_config()).unwrap();

        sender.send(
            WebhookEventType::Start,
            &serde_json::json!({"status": "starting"}),
        );

        tokio::time::sleep(Duration::from_millis(50)).await;
    }

    #[tokio::test]
    async fn send_non_terminal_logs_throttled() {
        let server = MockServer::start().await;

        Mock::given(method("POST"))
            .and(path("/webhook"))
            .respond_with(ResponseTemplate::new(200))
            .expect(1)
            .mount(&server)
            .await;

        let url = format!("{}/webhook", server.uri());
        let config = WebhookConfig {
            response_interval: Duration::from_secs(10),
            ..test_config()
        };
        let sender = WebhookSender::new(url, config).unwrap();

        sender.send(
            WebhookEventType::Logs,
            &serde_json::json!({"logs": "line 1"}),
        );
        // Second send should be throttled
        sender.send(
            WebhookEventType::Logs,
            &serde_json::json!({"logs": "line 2"}),
        );

        tokio::time::sleep(Duration::from_millis(50)).await;
    }

    #[tokio::test]
    async fn send_output_not_throttled() {
        let server = MockServer::start().await;

        Mock::given(method("POST"))
            .and(path("/webhook"))
            .respond_with(ResponseTemplate::new(200))
            .expect(2)
            .mount(&server)
            .await;

        let url = format!("{}/webhook", server.uri());
        let config = WebhookConfig {
            response_interval: Duration::from_secs(10),
            ..test_config()
        };
        let sender = WebhookSender::new(url, config).unwrap();

        // Output events bypass throttling — both should be sent
        sender.send(
            WebhookEventType::Output,
            &serde_json::json!({"output": "1"}),
        );
        wait_for_requests(&server, 1).await;
        sender.send(
            WebhookEventType::Output,
            &serde_json::json!({"output": "2"}),
        );

        wait_for_requests(&server, 2).await;
    }

    async fn received_payloads(server: &MockServer) -> Vec<serde_json::Value> {
        server
            .received_requests()
            .await
            .unwrap()
            .iter()
            .map(|r| serde_json::from_slice(&r.body).unwrap())
            .collect()
    }

    async fn wait_for_requests(server: &MockServer, count: usize) {
        tokio::time::timeout(Duration::from_secs(5), async {
            while server.received_requests().await.unwrap().len() < count {
                tokio::time::sleep(Duration::from_millis(1)).await;
            }
        })
        .await
        .expect("webhook was not received");
    }

    async fn mount_slow_non_terminal_webhooks(server: &MockServer, delay: Duration) {
        Mock::given(method("POST"))
            .and(body_partial_json(
                serde_json::json!({"status": "processing"}),
            ))
            .respond_with(ResponseTemplate::new(200).set_delay(delay))
            .mount(server)
            .await;
        Mock::given(method("POST"))
            .and(body_partial_json(
                serde_json::json!({"status": "succeeded"}),
            ))
            .respond_with(ResponseTemplate::new(200))
            .expect(1)
            .mount(server)
            .await;
    }

    #[tokio::test]
    async fn send_terminal_waits_for_in_flight_webhook() {
        let server = MockServer::start().await;
        let delay = Duration::from_millis(300);
        mount_slow_non_terminal_webhooks(&server, delay).await;
        let sender =
            WebhookSender::new(format!("{}/webhook", server.uri()), test_config()).unwrap();

        let started = Instant::now();
        sender.send(
            WebhookEventType::Output,
            &serde_json::json!({"status": "processing"}),
        );
        wait_for_requests(&server, 1).await;
        sender
            .send_terminal(
                WebhookEventType::Completed,
                &serde_json::json!({"status": "succeeded"}),
            )
            .await;

        assert!(
            started.elapsed() >= delay,
            "terminal webhook was sent before the in-flight webhook finished"
        );
        let statuses: Vec<_> = received_payloads(&server)
            .await
            .into_iter()
            .map(|p| p["status"].clone())
            .collect();
        assert_eq!(statuses, ["processing", "succeeded"]);
    }

    #[tokio::test]
    async fn send_replaces_webhook_waiting_behind_in_flight_one() {
        let server = MockServer::start().await;
        mount_slow_non_terminal_webhooks(&server, Duration::from_millis(100)).await;
        let sender =
            WebhookSender::new(format!("{}/webhook", server.uri()), test_config()).unwrap();

        sender.send(
            WebhookEventType::Output,
            &serde_json::json!({"status": "processing", "n": 0}),
        );
        wait_for_requests(&server, 1).await;
        for n in 1..3 {
            sender.send(
                WebhookEventType::Output,
                &serde_json::json!({"status": "processing", "n": n}),
            );
        }
        wait_for_requests(&server, 2).await;
        tokio::time::sleep(Duration::from_millis(200)).await;

        assert_eq!(
            received_payloads(&server).await,
            [
                serde_json::json!({"status": "processing", "n": 0}),
                serde_json::json!({"status": "processing", "n": 2}),
            ]
        );
        sender
            .send_terminal(
                WebhookEventType::Completed,
                &serde_json::json!({"status": "succeeded"}),
            )
            .await;
    }

    #[tokio::test]
    async fn send_terminal_drops_waiting_webhook() {
        let server = MockServer::start().await;
        mount_slow_non_terminal_webhooks(&server, Duration::from_millis(100)).await;
        let sender =
            WebhookSender::new(format!("{}/webhook", server.uri()), test_config()).unwrap();

        sender.send(
            WebhookEventType::Output,
            &serde_json::json!({"status": "processing", "n": 0}),
        );
        wait_for_requests(&server, 1).await;
        sender.send(
            WebhookEventType::Output,
            &serde_json::json!({"status": "processing", "n": 1}),
        );
        sender
            .send_terminal(
                WebhookEventType::Completed,
                &serde_json::json!({"status": "succeeded"}),
            )
            .await;
        // Nothing is sent after the terminal webhook.
        sender.send(
            WebhookEventType::Output,
            &serde_json::json!({"status": "processing", "n": 2}),
        );
        tokio::time::sleep(Duration::from_millis(200)).await;

        assert_eq!(
            received_payloads(&server).await,
            [
                serde_json::json!({"status": "processing", "n": 0}),
                serde_json::json!({"status": "succeeded"}),
            ]
        );
    }

    #[tokio::test]
    async fn send_terminal_sync_posts_json() {
        let server = MockServer::start().await;

        Mock::given(method("POST"))
            .and(path("/webhook"))
            .respond_with(ResponseTemplate::new(200))
            .expect(1)
            .mount(&server)
            .await;

        let url = format!("{}/webhook", server.uri());
        let sender = WebhookSender::new(url, test_config()).unwrap();

        sender.send_terminal_sync(&serde_json::json!({"id": "pred_123", "status": "succeeded"}));
    }

    #[tokio::test]
    async fn send_terminal_sync_retries_on_500() {
        let server = MockServer::start().await;

        Mock::given(method("POST"))
            .and(path("/webhook"))
            .respond_with(ResponseTemplate::new(500))
            .up_to_n_times(1)
            .mount(&server)
            .await;

        Mock::given(method("POST"))
            .and(path("/webhook"))
            .respond_with(ResponseTemplate::new(200))
            .expect(1)
            .mount(&server)
            .await;

        let url = format!("{}/webhook", server.uri());
        let sender = WebhookSender::new(url, test_config()).unwrap();

        sender.send_terminal_sync(&serde_json::json!({"status": "succeeded"}));
    }
}
