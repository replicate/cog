//! Custom tracing layer for worker subprocess.
//!
//! Ships structured tracing events over IPC to orchestrator, preserving target and level.
//! Optionally writes to fd 101 for direct debugging (controlled by RUST_WORKER_DIRECT_LOG=1).

use std::io::Write;
use std::sync::{Arc, Mutex};

use tokio::sync::mpsc;
use tracing::{Level, Subscriber};
use tracing_subscriber::layer::{Context, Layer};

use crate::bridge::protocol::ControlResponse;

pub struct WorkerTracingLayer {
    tx: mpsc::Sender<ControlResponse>,
    direct_log_fd: Option<Arc<Mutex<std::fs::File>>>,
}

impl WorkerTracingLayer {
    pub fn new(tx: mpsc::Sender<ControlResponse>) -> Self {
        let direct_log_fd = if std::env::var("RUST_WORKER_DIRECT_LOG").as_deref() == Ok("1") {
            // fd 101 is the original stderr preserved during fd_redirect
            let fd = unsafe { std::fs::File::from_raw_fd(101) };
            Some(Arc::new(Mutex::new(fd)))
        } else {
            None
        };

        Self { tx, direct_log_fd }
    }

    fn level_to_string(level: &Level) -> &'static str {
        match *level {
            Level::TRACE => "trace",
            Level::DEBUG => "debug",
            Level::INFO => "info",
            Level::WARN => "warn",
            Level::ERROR => "error",
        }
    }
}

impl<S> Layer<S> for WorkerTracingLayer
where
    S: Subscriber,
{
    fn on_event(
        &self,
        event: &tracing::Event<'_>,
        _ctx: Context<'_, S>,
    ) {
        let metadata = event.metadata();
        let target = metadata.target();
        let level = Self::level_to_string(metadata.level());

        let mut visitor = MessageVisitor::default();
        event.record(&mut visitor);
        let message = visitor.message;

        let _ = self.tx.try_send(ControlResponse::WorkerLog {
            target: target.to_string(),
            level: level.to_string(),
            message: message.clone(),
        });

        if let Some(ref fd) = self.direct_log_fd
            && let Ok(mut file) = fd.lock()
        {
            let _ = writeln!(
                file,
                "worker_direct_log::{} [{}] {}",
                target, level, message
            );
        }
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

#[cfg(unix)]
use std::os::unix::io::FromRawFd;
