//! Webhook sender for async predictions.
//!
//! Implements the cog webhook protocol:
//! - Throttling (default 500ms between non-terminal updates)
//! - Terminal webhooks retried with exponential backoff
//! - WEBHOOK_AUTH_TOKEN bearer authentication
//! - Events filtering (start, output, logs, completed)

use std::collections::HashSet;
use std::sync::Mutex;
use std::time::{Duration, Instant};

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
}

pub struct WebhookSender {
    url: String,
    config: WebhookConfig,
    client: reqwest::Client,
    last_sent: Mutex<Instant>,
    trace_context: TraceContext,
}

impl WebhookSender {
    pub fn new(url: String, config: WebhookConfig) -> Self {
        Self::with_trace_context(url, config, TraceContext::default())
    }

    pub fn with_trace_context(
        url: String,
        config: WebhookConfig,
        trace_context: TraceContext,
    ) -> Self {
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
            .build()
            .expect("Failed to create HTTP client");

        Self {
            url,
            config,
            client,
            // Allow immediate first send
            last_sent: Mutex::new(Instant::now() - Duration::from_secs(10)),
            trace_context,
        }
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

        let last = self.last_sent.lock().unwrap();
        last.elapsed() >= self.config.response_interval
    }

    fn update_last_sent(&self) {
        let mut last = self.last_sent.lock().unwrap();
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

        request
    }

    /// Send a non-terminal webhook (fire and forget, no retry).
    pub fn send(&self, event: WebhookEventType, payload: &serde_json::Value) {
        if !self.should_send(event) {
            return;
        }

        let request = self.build_request(payload);
        self.update_last_sent();

        tokio::spawn(async move {
            if let Err(e) = request.send().await {
                tracing::warn!(error = %e, "Failed to send webhook (non-terminal)");
            }
        });
    }

    /// Send a terminal webhook with exponential backoff retries.
    pub async fn send_terminal(&self, event: WebhookEventType, payload: &serde_json::Value) {
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
    use wiremock::matchers::{method, path};
    use wiremock::{Mock, MockServer, ResponseTemplate};

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
        let sender = WebhookSender::new(url, test_config());

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
        let sender = WebhookSender::new(url, test_config());

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
        let sender = WebhookSender::new(url, test_config());

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
        let sender = WebhookSender::new(url, config);

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
        let sender = WebhookSender::new(url, test_config());

        sender.send(
            WebhookEventType::Start,
            &serde_json::json!({"status": "starting"}),
        );

        tokio::time::sleep(Duration::from_millis(50)).await;
    }

    #[tokio::test]
    async fn send_non_terminal_throttled() {
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
        let sender = WebhookSender::new(url, config);

        sender.send(
            WebhookEventType::Output,
            &serde_json::json!({"output": "1"}),
        );
        // Second send should be throttled
        sender.send(
            WebhookEventType::Output,
            &serde_json::json!({"output": "2"}),
        );

        tokio::time::sleep(Duration::from_millis(50)).await;
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
        let sender = WebhookSender::new(url, test_config());

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
        let sender = WebhookSender::new(url, test_config());

        sender.send_terminal_sync(&serde_json::json!({"status": "succeeded"}));
    }
}
