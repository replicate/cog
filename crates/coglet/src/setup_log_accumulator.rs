//! Tracing layer that accumulates all logs from coglet server during setup.
//!
//! Captures every tracing event from the moment the server starts until setup completes.
//! This includes:
//! - Initial server startup logs ("coglet <version>")
//! - Orchestrator logs ("Spawning worker subprocess")
//! - Re-emitted worker logs (via emit_worker_log)
//! - Transport logs, codec warnings, everything
//!
//! Uses unbounded mpsc channel for lock-free accumulation.

use tokio::sync::mpsc;
use tracing::Subscriber;
use tracing_subscriber::layer::{Context, Layer};

pub struct SetupLogAccumulator {
    tx: mpsc::UnboundedSender<String>,
}

impl SetupLogAccumulator {
    pub fn new(tx: mpsc::UnboundedSender<String>) -> Self {
        Self { tx }
    }
}

impl<S> Layer<S> for SetupLogAccumulator
where
    S: Subscriber,
{
    fn on_event(&self, event: &tracing::Event<'_>, _ctx: Context<'_, S>) {
        if self.tx.is_closed() {
            return;
        }

        let metadata = event.metadata();
        let target = metadata.target();

        let mut visitor = MessageVisitor::default();
        event.record(&mut visitor);

        let log_line = format!("[{}] {}", target, visitor.message);
        let _ = self.tx.send(log_line);
    }
}

#[derive(Default)]
struct MessageVisitor {
    message: String,
}

impl tracing::field::Visit for MessageVisitor {
    fn record_debug(&mut self, field: &tracing::field::Field, value: &dyn std::fmt::Debug) {
        if field.name() == "message" {
            self.message = format!("{:?}", value);
            if self.message.starts_with('"') && self.message.ends_with('"') {
                self.message = self.message[1..self.message.len() - 1].to_string();
            }
        }
    }

    fn record_str(&mut self, field: &tracing::field::Field, value: &str) {
        if field.name() == "message" {
            self.message = value.to_string();
        }
    }
}

pub fn drain_accumulated_logs(rx: &mut mpsc::UnboundedReceiver<String>) -> String {
    let mut lines = Vec::new();
    while let Ok(line) = rx.try_recv() {
        lines.push(line);
    }

    if lines.is_empty() {
        String::new()
    } else {
        let mut result = lines.join("\n");
        result.push('\n');
        result
    }
}
