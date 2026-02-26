# FFI Runtime (Rust + PyO3)

This directory documents the next-generation Cog runtime implementation using Rust with PyO3 FFI bindings.

## Status

This is an **experimental** runtime implementation currently in development. It provides significant improvements in:
- Performance and resource management
- Worker process isolation and stability
- Concurrency control with slot-based permits
- Graceful cancellation and connection drop handling

## When to Use

Enable this implementation by setting the `USE_COGLET` environment variable when running Cog containers.

## Key Improvements

- **Rust HTTP server (Axum)**: Faster, better backpressure handling
- **Worker isolation**: Python crashes don't kill the server
- **Slot-based concurrency**: Predictable resource management with permit pool
- **Subprocess reuse**: Predictor stays loaded between requests
- **Better cancellation**: Sync predictions cancel on connection drop via RAII guards

## Architecture Overview

```
HTTP Server (Rust/Axum)
  ↓
PredictionService (state, webhooks, DashMap)
  ↓
PermitPool (slot-based concurrency)
  ↓
Orchestrator → Worker Subprocess (Python)
  ↓ (Unix socket + pipes)
Predictor (setup/predict)
```

## Documentation

- [Prediction API](./03-prediction-api.md) - HTTP endpoints with coglet-specific behavior
- [Container Runtime](./04-container-runtime.md) - Complete FFI architecture and flow

## Implementation

Primary code location: `crates/coglet/`
- `src/transport/http/` - Axum HTTP server
- `src/service.rs` - PredictionService (single owner of prediction state)
- `src/permit/` - Slot-based concurrency control
- `src/orchestrator.rs` - Worker subprocess management
- `src/bridge/` - IPC protocol and transport
- `src/worker/` - Worker implementation

Python bindings: `crates/coglet-python/src/lib.rs`
