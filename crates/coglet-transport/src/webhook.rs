//! Webhook sender for async predictions.
//!
//! Implements the cog webhook protocol:
//! - Throttling (default 500ms between non-terminal updates)
//! - Terminal webhooks are retried with exponential backoff
//! - WEBHOOK_AUTH_TOKEN bearer authentication
//! - Events filtering (start, output, logs, completed)

use std::collections::HashSet;
use std::sync::Mutex;
use std::time::{Duration, Instant};

use crate::routes::WebhookEventFilter;

/// Webhook event types.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub enum WebhookEvent {
    Start,
    Output,
    Logs,
    Completed,
}

impl WebhookEvent {
    /// Check if this is a terminal event.
    pub fn is_terminal(&self) -> bool {
        matches!(self, Self::Completed)
    }
    
    /// Convert to filter type for comparison.
    fn to_filter(&self) -> WebhookEventFilter {
        match self {
            Self::Start => WebhookEventFilter::Start,
            Self::Output => WebhookEventFilter::Output,
            Self::Logs => WebhookEventFilter::Logs,
            Self::Completed => WebhookEventFilter::Completed,
        }
    }
}

/// Configuration for webhook sender.
#[derive(Debug, Clone)]
pub struct WebhookConfig {
    /// Minimum interval between non-terminal webhooks (default 500ms).
    pub response_interval: Duration,
    /// Events to send (default: all).
    pub events_filter: HashSet<WebhookEventFilter>,
    /// Maximum retries for terminal webhooks (default 12).
    pub max_retries: u32,
    /// Base backoff factor for retries (default 100ms).
    pub backoff_base: Duration,
    /// HTTP status codes that trigger a retry.
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
                    .unwrap_or(500)
            ),
            events_filter: [
                WebhookEventFilter::Start,
                WebhookEventFilter::Output,
                WebhookEventFilter::Logs,
                WebhookEventFilter::Completed,
            ].into_iter().collect(),
            max_retries: 12,
            backoff_base: Duration::from_millis(100),
            retry_status_codes: vec![429, 500, 502, 503, 504],
        }
    }
}

/// Webhook sender with throttling and retry logic.
pub struct WebhookSender {
    url: String,
    config: WebhookConfig,
    client: reqwest::Client,
    last_sent: Mutex<Instant>,
}

impl WebhookSender {
    /// Create a new webhook sender.
    pub fn new(url: String, config: WebhookConfig) -> Self {
        let mut headers = reqwest::header::HeaderMap::new();
        
        // Add bearer auth if WEBHOOK_AUTH_TOKEN is set
        if let Ok(token) = std::env::var("WEBHOOK_AUTH_TOKEN") {
            if let Ok(value) = reqwest::header::HeaderValue::from_str(&format!("Bearer {}", token)) {
                headers.insert(reqwest::header::AUTHORIZATION, value);
            }
        }
        
        // Add user agent
        let user_agent = format!("coglet/{}", env!("CARGO_PKG_VERSION"));
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
            last_sent: Mutex::new(Instant::now() - Duration::from_secs(10)), // Allow immediate first send
        }
    }
    
    /// Check if this event should be sent based on filter and throttling.
    fn should_send(&self, event: WebhookEvent) -> bool {
        // Check event filter
        if !self.config.events_filter.contains(&event.to_filter()) {
            return false;
        }
        
        // Terminal events always sent
        if event.is_terminal() {
            return true;
        }
        
        // Check throttle
        let last = self.last_sent.lock().unwrap();
        last.elapsed() >= self.config.response_interval
    }
    
    /// Update last sent time.
    fn update_last_sent(&self) {
        let mut last = self.last_sent.lock().unwrap();
        *last = Instant::now();
    }
    
    /// Send a non-terminal webhook (no retry, errors ignored).
    pub fn send(&self, event: WebhookEvent, payload: &serde_json::Value) {
        if !self.should_send(event) {
            return;
        }
        
        let url = self.url.clone();
        let client = self.client.clone();
        let payload = payload.clone();
        
        self.update_last_sent();
        
        // Fire and forget - spawn a task but don't wait
        tokio::spawn(async move {
            if let Err(e) = client.post(&url).json(&payload).send().await {
                tracing::warn!(error = %e, "Failed to send webhook (non-terminal)");
            }
        });
    }
    
    /// Send a terminal webhook with retries.
    pub async fn send_terminal(&self, event: WebhookEvent, payload: &serde_json::Value) {
        if !self.config.events_filter.contains(&event.to_filter()) {
            return;
        }
        
        let mut attempt = 0;
        loop {
            match self.client.post(&self.url).json(payload).send().await {
                Ok(response) => {
                    let status = response.status().as_u16();
                    if response.status().is_success() {
                        tracing::debug!(status = %status, "Terminal webhook sent successfully");
                        return;
                    }
                    
                    // Check if we should retry this status
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
                        
                        // Exponential backoff: base * 2^attempt
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
                    
                    // Non-retryable error status
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
}

#[cfg(test)]
mod tests {
    use super::*;
    use wiremock::matchers::{method, path};
    use wiremock::{Mock, MockServer, ResponseTemplate};
    
    #[test]
    fn webhook_config_defaults() {
        let config = WebhookConfig::default();
        assert_eq!(config.response_interval, Duration::from_millis(500));
        assert_eq!(config.max_retries, 12);
        assert!(config.events_filter.contains(&WebhookEventFilter::Start));
        assert!(config.events_filter.contains(&WebhookEventFilter::Output));
        assert!(config.events_filter.contains(&WebhookEventFilter::Logs));
        assert!(config.events_filter.contains(&WebhookEventFilter::Completed));
    }
    
    #[test]
    fn webhook_event_is_terminal() {
        assert!(!WebhookEvent::Start.is_terminal());
        assert!(!WebhookEvent::Output.is_terminal());
        assert!(!WebhookEvent::Logs.is_terminal());
        assert!(WebhookEvent::Completed.is_terminal());
    }
    
    /// Helper to create a WebhookConfig for tests (no throttling, fast retries).
    fn test_config() -> WebhookConfig {
        WebhookConfig {
            response_interval: Duration::ZERO,
            max_retries: 2,
            backoff_base: Duration::from_millis(1),
            ..Default::default()
        }
    }
    
    #[tokio::test]
    async fn send_terminal_posts_json_payload() {
        let server = MockServer::start().await;
        
        Mock::given(method("POST"))
            .and(path("/webhook"))
            .respond_with(ResponseTemplate::new(200))
            .expect(1)
            .mount(&server)
            .await;
        
        let url = format!("{}/webhook", server.uri());
        let sender = WebhookSender::new(url, test_config());
        
        let payload = serde_json::json!({
            "id": "pred_123",
            "status": "succeeded",
            "output": "hello"
        });
        
        sender.send_terminal(WebhookEvent::Completed, &payload).await;
        
        // wiremock verifies expect(1) on drop
    }
    
    #[tokio::test]
    async fn send_terminal_retries_on_500() {
        let server = MockServer::start().await;
        
        // First request fails with 500, second succeeds
        // up_to_n_times(1) makes the mock deactivate after one match
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
        
        sender.send_terminal(WebhookEvent::Completed, &serde_json::json!({"status": "succeeded"})).await;
    }
    
    #[tokio::test]
    async fn send_terminal_does_not_retry_on_400() {
        let server = MockServer::start().await;
        
        // 400 is not in retry_status_codes, should not retry
        Mock::given(method("POST"))
            .and(path("/webhook"))
            .respond_with(ResponseTemplate::new(400))
            .expect(1)
            .mount(&server)
            .await;
        
        let url = format!("{}/webhook", server.uri());
        let sender = WebhookSender::new(url, test_config());
        
        sender.send_terminal(WebhookEvent::Completed, &serde_json::json!({"status": "succeeded"})).await;
    }
    
    #[tokio::test]
    async fn send_terminal_respects_event_filter() {
        let server = MockServer::start().await;
        
        // Should NOT be called - Completed is filtered out
        Mock::given(method("POST"))
            .and(path("/webhook"))
            .respond_with(ResponseTemplate::new(200))
            .expect(0)
            .mount(&server)
            .await;
        
        let url = format!("{}/webhook", server.uri());
        let config = WebhookConfig {
            events_filter: [WebhookEventFilter::Start].into_iter().collect(), // Only Start, not Completed
            ..test_config()
        };
        let sender = WebhookSender::new(url, config);
        
        sender.send_terminal(WebhookEvent::Completed, &serde_json::json!({"status": "succeeded"})).await;
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
        
        sender.send(WebhookEvent::Start, &serde_json::json!({"status": "starting"}));
        
        // Give the spawned task time to complete
        tokio::time::sleep(Duration::from_millis(50)).await;
    }
    
    #[tokio::test]
    async fn send_non_terminal_throttled() {
        let server = MockServer::start().await;
        
        // Only expect 1 call due to throttling
        Mock::given(method("POST"))
            .and(path("/webhook"))
            .respond_with(ResponseTemplate::new(200))
            .expect(1)
            .mount(&server)
            .await;
        
        let url = format!("{}/webhook", server.uri());
        let config = WebhookConfig {
            response_interval: Duration::from_secs(10), // Long throttle
            ..test_config()
        };
        let sender = WebhookSender::new(url, config);
        
        // First should send
        sender.send(WebhookEvent::Output, &serde_json::json!({"output": "1"}));
        // Second should be throttled
        sender.send(WebhookEvent::Output, &serde_json::json!({"output": "2"}));
        
        tokio::time::sleep(Duration::from_millis(50)).await;
    }
}
