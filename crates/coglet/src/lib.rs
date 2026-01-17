//! coglet: Rust execution engine for cog models.
//!
//! This crate provides the core runtime for coglet, including:
//! - HTTP server for serving predictions
//! - Worker subprocess management
//! - IPC protocol for parent-worker communication
//! - Prediction service with slot-based concurrency

// Core types
mod health;
pub mod permit;
mod prediction;
mod predictor;
mod service;
mod supervisor;
mod version;
pub mod webhook;

// IPC protocol (was coglet-bridge)
pub mod bridge;

// Transport (HTTP, future: gRPC)
pub mod transport;

// Worker subprocess (was coglet-worker)
pub mod worker;

// Orchestrator (spawns worker, runs event loop)
pub mod orchestrator;

// Re-exports from core
pub use health::{Health, SetupResult, SetupStatus};
pub use permit::{PermitError, PermitInUse, PermitPool, PredictionSlot};
pub use prediction::{Prediction, PredictionStatus};
pub use predictor::{
    AsyncPredictFn, CancellationToken, PredictFn, PredictFuture, PredictionError, PredictionGuard,
    PredictionMetrics, PredictionOutput, PredictionResult,
};
pub use service::{CreatePredictionError, HealthSnapshot, PredictionService};
pub use supervisor::PredictionStatus as SupervisorPredictionStatus;
pub use supervisor::{
    PredictionHandle, PredictionState, PredictionSupervisor, SyncPredictionGuard,
};
pub use version::{COGLET_VERSION, VersionInfo};
pub use webhook::{TraceContext, WebhookConfig, WebhookEventType, WebhookSender};

// Re-exports from transport
pub use transport::{ServerConfig, serve};

// Re-exports from bridge
pub use bridge::{
    codec::JsonCodec,
    protocol::{ControlRequest, ControlResponse, LogSource, SlotId, SlotRequest, SlotResponse},
    transport::{ChildTransportInfo, SlotTransport, create_transport},
};

// Re-exports from worker
pub use worker::{
    PredictHandler, PredictResult, SetupLogHook, SlotSender, SpawnConfig, Worker, WorkerConfig,
    WorkerError, run_worker,
};

// Re-exports from orchestrator
pub use orchestrator::{
    OrchestratorConfig,
    OrchestratorError,
    OrchestratorHandle,
    OrchestratorReady,
    SimpleSpawner,
    SpawnError,
    WorkerSpawnConfig,
    // Extension point for custom spawners
    WorkerSpawner,
    spawn_worker,
};
