//! Prediction state tracking.

use std::collections::HashMap;
use std::sync::Arc;
use std::time::Instant;

use tokio::sync::Notify;
pub use tokio_util::sync::CancellationToken;

use crate::bridge::protocol::MetricMode;
use crate::webhook::WebhookSender;

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum PredictionStatus {
    Starting,
    Processing,
    Succeeded,
    Failed,
    Canceled,
}

impl PredictionStatus {
    pub fn is_terminal(&self) -> bool {
        matches!(self, Self::Succeeded | Self::Failed | Self::Canceled)
    }

    pub fn as_str(&self) -> &'static str {
        match self {
            Self::Starting => "starting",
            Self::Processing => "processing",
            Self::Succeeded => "succeeded",
            Self::Failed => "failed",
            Self::Canceled => "canceled",
        }
    }
}

/// Prediction output - single value or streamed chunks.
#[derive(Debug, Clone, serde::Serialize)]
#[serde(untagged)]
pub enum PredictionOutput {
    Single(serde_json::Value),
    Stream(Vec<serde_json::Value>),
}

impl PredictionOutput {
    pub fn is_stream(&self) -> bool {
        matches!(self, PredictionOutput::Stream(_))
    }

    pub fn into_values(self) -> Vec<serde_json::Value> {
        match self {
            PredictionOutput::Single(v) => vec![v],
            PredictionOutput::Stream(v) => v,
        }
    }

    /// Get the final/only output value (last for stream, the value for single).
    pub fn final_value(&self) -> &serde_json::Value {
        match self {
            PredictionOutput::Single(v) => v,
            PredictionOutput::Stream(v) => v.last().unwrap_or(&serde_json::Value::Null),
        }
    }
}

/// Prediction lifecycle state.
pub struct Prediction {
    id: String,
    cancel_token: CancellationToken,
    started_at: Instant,
    status: PredictionStatus,
    logs: String,
    outputs: Vec<serde_json::Value>,
    output: Option<PredictionOutput>,
    error: Option<String>,
    webhook: Option<WebhookSender>,
    completion: Arc<Notify>,
    /// User-emitted metrics. Merged with system metrics (predict_time) in terminal response.
    metrics: HashMap<String, serde_json::Value>,
}

impl Prediction {
    pub fn new(id: String, webhook: Option<WebhookSender>) -> Self {
        Self {
            id,
            cancel_token: CancellationToken::new(),
            started_at: Instant::now(),
            status: PredictionStatus::Starting,
            logs: String::new(),
            outputs: Vec::new(),
            output: None,
            error: None,
            webhook,
            completion: Arc::new(Notify::new()),
            metrics: HashMap::new(),
        }
    }

    pub fn id(&self) -> &str {
        &self.id
    }

    pub fn cancel_token(&self) -> CancellationToken {
        self.cancel_token.clone()
    }

    pub fn is_canceled(&self) -> bool {
        self.cancel_token.is_cancelled()
    }

    pub fn status(&self) -> PredictionStatus {
        self.status
    }

    pub fn is_terminal(&self) -> bool {
        self.status.is_terminal()
    }

    pub fn set_processing(&mut self) {
        self.status = PredictionStatus::Processing;
    }

    pub fn set_succeeded(&mut self, output: PredictionOutput) {
        self.status = PredictionStatus::Succeeded;
        self.output = Some(output);
        // notify_one stores a permit so a future .notified().await will
        // consume it immediately.  notify_waiters only wakes currently-
        // registered waiters and would race with the service task that
        // checks is_terminal() then awaits — the notification can fire
        // in between.  There is exactly one waiter per prediction
        // (service.rs predict()), so notify_one is semantically correct.
        self.completion.notify_one();
    }

    pub fn set_failed(&mut self, error: String) {
        self.status = PredictionStatus::Failed;
        self.error = Some(error);
        self.completion.notify_one();
    }

    pub fn set_canceled(&mut self) {
        self.status = PredictionStatus::Canceled;
        self.completion.notify_one();
    }

    pub fn elapsed(&self) -> std::time::Duration {
        self.started_at.elapsed()
    }

    pub fn append_log(&mut self, data: &str) {
        self.logs.push_str(data);
    }

    pub fn logs(&self) -> &str {
        &self.logs
    }

    /// Set a user metric with the given accumulation mode.
    ///
    /// - `Replace`: overwrites the value (or deletes if null).
    /// - `Increment`: adds to an existing numeric value. Errors silently if types mismatch.
    /// - `Append`: pushes onto an existing array, creating one if needed.
    ///
    /// Dot-path keys (e.g., "timing.preprocess") are resolved into nested objects.
    pub fn set_metric(&mut self, name: String, value: serde_json::Value, mode: MetricMode) {
        // Dot-path resolution: "a.b.c" → nested objects
        let parts: Vec<&str> = name.split('.').collect();
        if parts.len() > 1 {
            self.set_metric_dotpath(&parts, value, mode);
            return;
        }

        match mode {
            MetricMode::Replace => {
                if value.is_null() {
                    self.metrics.remove(&name);
                } else {
                    self.metrics.insert(name, value);
                }
            }
            MetricMode::Increment => {
                let entry = self.metrics.entry(name).or_insert(serde_json::json!(0));
                if let (Some(existing), Some(delta)) = (entry.as_f64(), value.as_f64()) {
                    // Preserve integer type if both are integers
                    if entry.is_i64() && value.is_i64() {
                        *entry = serde_json::json!(existing as i64 + delta as i64);
                    } else if entry.is_u64() && value.is_u64() {
                        *entry = serde_json::json!(existing as u64 + delta as u64);
                    } else {
                        *entry = serde_json::json!(existing + delta);
                    }
                }
                // Non-numeric increment is silently ignored
            }
            MetricMode::Append => {
                let entry = self
                    .metrics
                    .entry(name)
                    .or_insert(serde_json::Value::Array(vec![]));
                if let Some(arr) = entry.as_array_mut() {
                    arr.push(value);
                } else {
                    // Existing value is not an array — wrap it and append
                    let existing = entry.take();
                    *entry = serde_json::json!([existing, value]);
                }
            }
        }
    }

    /// Resolve a dot-path key into nested objects and apply the metric.
    fn set_metric_dotpath(&mut self, parts: &[&str], value: serde_json::Value, mode: MetricMode) {
        debug_assert!(parts.len() > 1);

        let root_key = parts[0].to_string();

        // Navigate/create nested structure
        let entry = self
            .metrics
            .entry(root_key)
            .or_insert_with(|| serde_json::json!({}));

        let mut current = entry;
        for &part in &parts[1..parts.len() - 1] {
            // Ensure intermediate nodes are objects
            if !current.is_object() {
                *current = serde_json::json!({});
            }
            current = current
                .as_object_mut()
                .unwrap()
                .entry(part)
                .or_insert_with(|| serde_json::json!({}));
        }

        let leaf_key = parts[parts.len() - 1];

        // Ensure the parent is an object
        if !current.is_object() {
            *current = serde_json::json!({});
        }
        let obj = current.as_object_mut().unwrap();

        match mode {
            MetricMode::Replace => {
                if value.is_null() {
                    obj.remove(leaf_key);
                } else {
                    obj.insert(leaf_key.to_string(), value);
                }
            }
            MetricMode::Increment => {
                let entry = obj.entry(leaf_key).or_insert(serde_json::json!(0));
                if let (Some(existing), Some(delta)) = (entry.as_f64(), value.as_f64()) {
                    if entry.is_i64() && value.is_i64() {
                        *entry = serde_json::json!(existing as i64 + delta as i64);
                    } else if entry.is_u64() && value.is_u64() {
                        *entry = serde_json::json!(existing as u64 + delta as u64);
                    } else {
                        *entry = serde_json::json!(existing + delta);
                    }
                }
            }
            MetricMode::Append => {
                let entry = obj
                    .entry(leaf_key)
                    .or_insert(serde_json::Value::Array(vec![]));
                if let Some(arr) = entry.as_array_mut() {
                    arr.push(value);
                } else {
                    let existing = entry.take();
                    *entry = serde_json::json!([existing, value]);
                }
            }
        }
    }

    pub fn metrics(&self) -> &HashMap<String, serde_json::Value> {
        &self.metrics
    }

    pub fn append_output(&mut self, output: serde_json::Value) {
        self.outputs.push(output);
    }

    pub fn outputs(&self) -> &[serde_json::Value] {
        &self.outputs
    }

    pub fn take_outputs(&mut self) -> Vec<serde_json::Value> {
        std::mem::take(&mut self.outputs)
    }

    pub fn output(&self) -> Option<&PredictionOutput> {
        self.output.as_ref()
    }

    pub fn error(&self) -> Option<&str> {
        self.error.as_deref()
    }

    pub async fn wait(&self) {
        if self.status.is_terminal() {
            return;
        }
        self.completion.notified().await;
    }

    pub fn completion(&self) -> Arc<Notify> {
        Arc::clone(&self.completion)
    }

    /// Take the webhook sender (for sending on drop).
    pub fn take_webhook(&mut self) -> Option<WebhookSender> {
        self.webhook.take()
    }

    /// Build merged metrics object: user metrics + system metrics (predict_time).
    /// System metrics (predict_time) always win on conflict.
    fn build_metrics(&self) -> serde_json::Value {
        let predict_time = self.elapsed().as_secs_f64();
        let mut merged = serde_json::Map::new();

        // User metrics first
        for (k, v) in &self.metrics {
            merged.insert(k.clone(), v.clone());
        }

        // System metrics override — predict_time is always authoritative
        merged.insert("predict_time".to_string(), serde_json::json!(predict_time));

        serde_json::Value::Object(merged)
    }

    pub fn build_terminal_response(&self) -> serde_json::Value {
        let metrics = self.build_metrics();

        match self.status {
            PredictionStatus::Succeeded => {
                serde_json::json!({
                    "id": self.id,
                    "status": "succeeded",
                    "output": self.output,
                    "metrics": metrics
                })
            }
            PredictionStatus::Failed => {
                serde_json::json!({
                    "id": self.id,
                    "status": "failed",
                    "error": self.error,
                    "metrics": metrics
                })
            }
            PredictionStatus::Canceled => {
                serde_json::json!({
                    "id": self.id,
                    "status": "canceled",
                    "metrics": metrics
                })
            }
            _ => {
                serde_json::json!({
                    "id": self.id,
                    "status": "failed",
                    "error": "Prediction in non-terminal state",
                    "metrics": metrics
                })
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn status_is_terminal() {
        assert!(!PredictionStatus::Starting.is_terminal());
        assert!(!PredictionStatus::Processing.is_terminal());
        assert!(PredictionStatus::Succeeded.is_terminal());
        assert!(PredictionStatus::Failed.is_terminal());
        assert!(PredictionStatus::Canceled.is_terminal());
    }

    #[test]
    fn new_starts_in_starting_status() {
        let pred = Prediction::new("test".to_string(), None);
        assert_eq!(pred.status(), PredictionStatus::Starting);
        assert_eq!(pred.id(), "test");
    }

    #[test]
    fn set_succeeded() {
        let mut pred = Prediction::new("test".to_string(), None);
        pred.set_succeeded(PredictionOutput::Single(serde_json::json!("hello")));
        assert_eq!(pred.status(), PredictionStatus::Succeeded);
    }

    #[test]
    fn set_failed() {
        let mut pred = Prediction::new("test".to_string(), None);
        pred.set_failed("something went wrong".to_string());
        assert_eq!(pred.status(), PredictionStatus::Failed);
    }

    #[test]
    fn set_canceled() {
        let mut pred = Prediction::new("test".to_string(), None);
        pred.set_canceled();
        assert_eq!(pred.status(), PredictionStatus::Canceled);
    }

    #[test]
    fn cancel_token_works() {
        let pred = Prediction::new("test".to_string(), None);
        let token = pred.cancel_token();

        assert!(!pred.is_canceled());
        token.cancel();
        assert!(pred.is_canceled());
    }

    #[test]
    fn elapsed_time_increases() {
        let pred = Prediction::new("test".to_string(), None);
        let t1 = pred.elapsed();
        std::thread::sleep(std::time::Duration::from_millis(10));
        let t2 = pred.elapsed();
        assert!(t2 > t1);
    }

    #[test]
    fn append_log() {
        let mut pred = Prediction::new("test".to_string(), None);
        pred.append_log("line 1\n");
        pred.append_log("line 2\n");
        assert_eq!(pred.logs(), "line 1\nline 2\n");
    }

    #[test]
    fn append_output() {
        let mut pred = Prediction::new("test".to_string(), None);
        pred.append_output(serde_json::json!("chunk1"));
        pred.append_output(serde_json::json!("chunk2"));
        assert_eq!(pred.outputs().len(), 2);
    }

    #[tokio::test]
    async fn wait_returns_immediately_if_terminal() {
        let mut pred = Prediction::new("test".to_string(), None);
        pred.set_succeeded(PredictionOutput::Single(serde_json::json!("done")));

        pred.wait().await;
        assert_eq!(pred.status(), PredictionStatus::Succeeded);
    }

    #[test]
    fn prediction_output_single() {
        let output = PredictionOutput::Single(serde_json::json!("hello"));
        assert!(!output.is_stream());
        assert_eq!(output.into_values(), vec![serde_json::json!("hello")]);
    }

    #[test]
    fn prediction_output_stream() {
        let output = PredictionOutput::Stream(vec![serde_json::json!("a"), serde_json::json!("b")]);
        assert!(output.is_stream());
    }

    // ====================================================================
    // Metric tests
    // ====================================================================

    #[test]
    fn metric_replace_sets_value() {
        let mut pred = Prediction::new("test".to_string(), None);
        pred.set_metric("temp".into(), serde_json::json!(0.7), MetricMode::Replace);
        assert_eq!(pred.metrics()["temp"], serde_json::json!(0.7));
    }

    #[test]
    fn metric_replace_overwrites() {
        let mut pred = Prediction::new("test".to_string(), None);
        pred.set_metric("temp".into(), serde_json::json!(0.7), MetricMode::Replace);
        pred.set_metric("temp".into(), serde_json::json!(0.9), MetricMode::Replace);
        assert_eq!(pred.metrics()["temp"], serde_json::json!(0.9));
    }

    #[test]
    fn metric_replace_null_deletes() {
        let mut pred = Prediction::new("test".to_string(), None);
        pred.set_metric("temp".into(), serde_json::json!(0.7), MetricMode::Replace);
        pred.set_metric(
            "temp".into(),
            serde_json::Value::Null,
            MetricMode::Replace,
        );
        assert!(!pred.metrics().contains_key("temp"));
    }

    #[test]
    fn metric_increment_integers() {
        let mut pred = Prediction::new("test".to_string(), None);
        pred.set_metric("count".into(), serde_json::json!(1), MetricMode::Increment);
        pred.set_metric("count".into(), serde_json::json!(3), MetricMode::Increment);
        assert_eq!(pred.metrics()["count"], serde_json::json!(4));
    }

    #[test]
    fn metric_increment_floats() {
        let mut pred = Prediction::new("test".to_string(), None);
        pred.set_metric(
            "score".into(),
            serde_json::json!(1.5),
            MetricMode::Increment,
        );
        pred.set_metric(
            "score".into(),
            serde_json::json!(2.5),
            MetricMode::Increment,
        );
        assert_eq!(pred.metrics()["score"], serde_json::json!(4.0));
    }

    #[test]
    fn metric_increment_creates_from_zero() {
        let mut pred = Prediction::new("test".to_string(), None);
        pred.set_metric("count".into(), serde_json::json!(5), MetricMode::Increment);
        assert_eq!(pred.metrics()["count"], serde_json::json!(5));
    }

    #[test]
    fn metric_append_creates_array() {
        let mut pred = Prediction::new("test".to_string(), None);
        pred.set_metric(
            "logprobs".into(),
            serde_json::json!(-1.2),
            MetricMode::Append,
        );
        pred.set_metric(
            "logprobs".into(),
            serde_json::json!(-0.3),
            MetricMode::Append,
        );
        assert_eq!(
            pred.metrics()["logprobs"],
            serde_json::json!([-1.2, -0.3])
        );
    }

    #[test]
    fn metric_append_to_non_array_wraps() {
        let mut pred = Prediction::new("test".to_string(), None);
        pred.set_metric("val".into(), serde_json::json!(1), MetricMode::Replace);
        pred.set_metric("val".into(), serde_json::json!(2), MetricMode::Append);
        assert_eq!(pred.metrics()["val"], serde_json::json!([1, 2]));
    }

    #[test]
    fn metric_dotpath_creates_nested() {
        let mut pred = Prediction::new("test".to_string(), None);
        pred.set_metric(
            "timing.preprocess".into(),
            serde_json::json!(0.1),
            MetricMode::Replace,
        );
        assert_eq!(
            pred.metrics()["timing"],
            serde_json::json!({"preprocess": 0.1})
        );
    }

    #[test]
    fn metric_dotpath_deep() {
        let mut pred = Prediction::new("test".to_string(), None);
        pred.set_metric(
            "a.b.c".into(),
            serde_json::json!(42),
            MetricMode::Replace,
        );
        assert_eq!(
            pred.metrics()["a"],
            serde_json::json!({"b": {"c": 42}})
        );
    }

    #[test]
    fn metric_dotpath_multiple_leaves() {
        let mut pred = Prediction::new("test".to_string(), None);
        pred.set_metric(
            "timing.preprocess".into(),
            serde_json::json!(0.1),
            MetricMode::Replace,
        );
        pred.set_metric(
            "timing.inference".into(),
            serde_json::json!(0.8),
            MetricMode::Replace,
        );
        assert_eq!(
            pred.metrics()["timing"],
            serde_json::json!({"preprocess": 0.1, "inference": 0.8})
        );
    }

    #[test]
    fn metric_dotpath_delete_leaf() {
        let mut pred = Prediction::new("test".to_string(), None);
        pred.set_metric(
            "timing.preprocess".into(),
            serde_json::json!(0.1),
            MetricMode::Replace,
        );
        pred.set_metric(
            "timing.preprocess".into(),
            serde_json::Value::Null,
            MetricMode::Replace,
        );
        // Parent object should still exist but be empty
        assert_eq!(pred.metrics()["timing"], serde_json::json!({}));
    }

    #[test]
    fn metric_dotpath_increment() {
        let mut pred = Prediction::new("test".to_string(), None);
        pred.set_metric(
            "stats.tokens".into(),
            serde_json::json!(10),
            MetricMode::Increment,
        );
        pred.set_metric(
            "stats.tokens".into(),
            serde_json::json!(5),
            MetricMode::Increment,
        );
        assert_eq!(
            pred.metrics()["stats"],
            serde_json::json!({"tokens": 15})
        );
    }

    #[test]
    fn metric_complex_values() {
        let mut pred = Prediction::new("test".to_string(), None);
        pred.set_metric(
            "config".into(),
            serde_json::json!({"layers": 12, "heads": 8}),
            MetricMode::Replace,
        );
        pred.set_metric(
            "scores".into(),
            serde_json::json!([0.9, 0.8, 0.7]),
            MetricMode::Replace,
        );
        assert_eq!(
            pred.metrics()["config"],
            serde_json::json!({"layers": 12, "heads": 8})
        );
        assert_eq!(
            pred.metrics()["scores"],
            serde_json::json!([0.9, 0.8, 0.7])
        );
    }

    #[test]
    fn build_metrics_merges_with_predict_time() {
        let mut pred = Prediction::new("test".to_string(), None);
        pred.set_metric("temp".into(), serde_json::json!(0.7), MetricMode::Replace);
        pred.set_metric("count".into(), serde_json::json!(42), MetricMode::Replace);

        let metrics = pred.build_metrics();
        let obj = metrics.as_object().unwrap();
        assert_eq!(obj["temp"], serde_json::json!(0.7));
        assert_eq!(obj["count"], serde_json::json!(42));
        assert!(obj.contains_key("predict_time"));
    }

    #[test]
    fn build_metrics_predict_time_overrides_user() {
        let mut pred = Prediction::new("test".to_string(), None);
        // User tries to set predict_time — system should override
        pred.set_metric(
            "predict_time".into(),
            serde_json::json!(999.0),
            MetricMode::Replace,
        );

        let metrics = pred.build_metrics();
        let obj = metrics.as_object().unwrap();
        // predict_time should be the actual elapsed, not 999.0
        assert_ne!(obj["predict_time"], serde_json::json!(999.0));
    }
}
