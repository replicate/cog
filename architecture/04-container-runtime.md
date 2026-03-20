# Container Runtime

This document covers what happens when a Cog container runs. It's where the [Model Source](./01-model-source.md), [Schema](./02-schema.md), and [Prediction API](./03-prediction-api.md) come together.

## Overview

When a Cog container runs, it executes a **two-process architecture**: a Rust parent process (HTTP server + orchestrator) and a Python worker subprocess (predictor execution). The design isolates user model code from the HTTP server for stability, resource management, and clean shutdown handling.

The runtime is implemented in Rust using Axum for HTTP and PyO3 for Python integration, distributed as a Python wheel (`coglet`).

## High-Level Architecture

```
┌─────────────────────────────────────────────────────────────────────────────────┐
│                              HTTP Transport (axum)                               │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────────┐ │
│  │ POST        │  │ PUT         │  │ POST        │  │ GET                     │ │
│  │ /predictions│  │ /predictions│  │ /cancel     │  │ /health-check           │ │
│  │             │  │ /{id}       │  │             │  │ /openapi.json           │ │
│  └──────┬──────┘  └──────┬──────┘  └──────┬──────┘  └───────────┬─────────────┘ │
└─────────┼────────────────┼────────────────┼─────────────────────┼───────────────┘
          │                │                │                     │
          ▼                ▼                ▼                     ▼
┌─────────────────────────────────────────────────────────────────────────────────┐
│                            PredictionService                                     │
│  ┌────────────────────────────────────────────────────────────────────────────┐ │
│  │                    Active Predictions (DashMap)                            │ │
│  │  ┌─────────────────┐  ┌─────────────────┐  ┌─────────────────┐            │ │
│  │  │ PredictionEntry │  │ PredictionEntry │  │ PredictionEntry │  ...       │ │
│  │  │ ─────────────── │  │ ─────────────── │  │ ─────────────── │            │ │
│  │  │ prediction (Arc)│  │ prediction (Arc)│  │ prediction (Arc)│            │ │
│  │  │ cancel_token    │  │ cancel_token    │  │ cancel_token    │            │ │
│  │  │ input           │  │ input           │  │ input           │            │ │
│  │  └─────────────────┘  └─────────────────┘  └─────────────────┘            │ │
│  └────────────────────────────────────────────────────────────────────────────┘ │
│                                                                                  │
│  ┌────────────────────────────────────────────────────────────────────────────┐ │
│  │                           PermitPool                                       │ │
│  │  ┌────────┐  ┌────────┐  ┌────────┐                                       │ │
│  │  │ Permit │  │ Permit │  │ Permit │  (concurrency control)                │ │
│  │  │ slot_0 │  │ slot_1 │  │ slot_2 │                                       │ │
│  │  └────────┘  └────────┘  └────────┘                                       │ │
│  └────────────────────────────────────────────────────────────────────────────┘ │
│                                                                                  │
│  ┌────────────────────────────────────────────────────────────────────────────┐ │
│  │                        OrchestratorHandle                                  │ │
│  │  (slot_ids, control_tx for worker comms)                                   │ │
│  └────────────────────────────────────────────────────────────────────────────┘ │
└──────────────────────────────────┬──────────────────────────────────────────────┘
                                   │
                    Unix Socket (slot) + stdin/stdout (control)
                                   │
                                   ▼
┌─────────────────────────────────────────────────────────────────────────────────┐
│                         Worker Subprocess (Python)                               │
│  ┌────────────────────────────────────────────────────────────────────────────┐ │
│  │                              Predictor                                     │ │
│  │  ┌─────────────────────────────────────────────────────────────────────┐  │ │
│  │  │  setup()    →  runs once at startup                                 │  │ │
│  │  │  predict()  →  handles SlotRequest::Predict                         │  │ │
│  │  └─────────────────────────────────────────────────────────────────────┘  │ │
│  └────────────────────────────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────────────────────────┘
```

## Component Ownership

```
═══════════════════════════════════════════════════════════════════════════════════
                           COMPONENT OWNERSHIP
═══════════════════════════════════════════════════════════════════════════════════
  PredictionService (single owner of prediction state)
  ├── owns: DashMap<String, PredictionEntry> (active predictions)
  ├── owns: OrchestratorState (pool + orchestrator handle)
  ├── owns: health, setup_result, schema
  └── method: cancel() fires token + delegates to orchestrator

  PredictionEntry (in DashMap)
  ├── has: Arc<Mutex<Prediction>> (the real state -- single source of truth)
  ├── has: CancellationToken
  └── has: input (for API responses)

  Prediction (state machine -- webhooks fire from mutation methods)
  ├── owns: status, logs, outputs, output, error, metrics
  ├── owns: WebhookSender (fires on set_processing, append_log, etc.)
  └── owns: completion notifier (for waiting on result)

  PredictionSlot (RAII container)
  ├── owns: Arc<Mutex<Prediction>> (shared with DashMap entry)
  ├── owns: Permit (concurrency token, returns to pool on drop)
  └── Drop: marks permit idle, releases back to pool

  PredictionHandle (returned to route handler)
  ├── has: CancellationToken clone
  └── method: sync_guard(service) → SyncPredictionGuard (cancels on drop)

  Cancellation (via OrchestratorHandle)
  ├── Sync predictors: ControlRequest::Cancel → SIGUSR1 → KeyboardInterrupt
  └── Async predictors: ControlRequest::Cancel → future.cancel() → CancelledError
═══════════════════════════════════════════════════════════════════════════════════
```

## Process Roles

### tini (PID 1)
- **What**: Minimal init system (~30KB binary)
- **Why**: Proper signal forwarding to children, zombie process reaping
- **Entry**: `ENTRYPOINT ["/sbin/tini", "--"]`

### Parent Process (Rust HTTP Server)
- **Entry**: `CMD ["python", "-m", "cog.server.http"]` -- this thin Python launcher calls `coglet.server.serve()`
- **Responsibilities**:
  - HTTP API on port 5000 (Axum)
  - Request validation
  - Input file downloading (from URLs)
  - Webhook delivery with retry and trace context propagation
  - Output file uploads
  - Health state management
  - Worker subprocess lifecycle

### Worker Subprocess (Python)
- **Spawned via**: `python -c "import coglet; coglet.server._run_worker()"`
- **Responsibilities**:
  - Load user's predictor module
  - Run `setup()` once at startup
  - Execute `predict()` / `train()` methods
  - Capture stdout/stderr via ContextVar-based log routing
  - Send events back to parent via slot sockets

## Why Two Processes?

1. **Isolation**: User code crashes don't bring down the HTTP server
2. **Memory**: Fresh address space for model loading
3. **CUDA**: Clean GPU context initialization in worker
4. **Stability**: Server marks health as `DEFUNCT` and continues serving other endpoints if worker crashes
5. **Monitoring**: Parent tracks worker health independently

## Worker Subprocess Protocol

Communication between the Rust server and Python worker uses two channels:

### Control Channel (stdin/stdout -- JSON framed)

| Parent → Child | Child → Parent |
|----------------|----------------|
| `Init { predictor_ref, num_slots, ... }` | `Ready { slots, schema }` |
| `Cancel { slot }` | `Log { source, data }` |
| `Shutdown` | `Idle { slot }` |
| | `Failed { slot, error }` |
| | `ShuttingDown` |

### Slot Channel (Unix socket per slot -- JSON framed)

| Parent → Child | Child → Parent |
|----------------|----------------|
| `Predict { id, input }` | `Log { data }` |
| | `Output { value }` (streaming) |
| | `Done { output }` |
| | `Failed { error }` |
| | `Cancelled` |

## Health State Machine

```mermaid
stateDiagram-v2
    [*] --> STARTING: Container start
    note right of STARTING: Predictions return 503
    
    STARTING --> READY: setup() succeeds
    STARTING --> SETUP_FAILED: setup() raises exception
    
    READY --> BUSY: All slots occupied
    note right of BUSY: New predictions get 409
    
    BUSY --> READY: Slot freed
    
    READY --> DEFUNCT: Fatal error / worker crash
    BUSY --> DEFUNCT: Fatal error / worker crash
    note right of DEFUNCT: Predictions return 503
    
    SETUP_FAILED --> [*]
    DEFUNCT --> [*]
```

## Prediction Flow

### Sync Request (POST /predictions)

```mermaid
sequenceDiagram
    participant Client
    participant Routes
    participant Service
    participant Worker
    
    Client->>Routes: POST /predictions
    Routes->>Service: submit_prediction(id, input, webhook)
    Service-->>Routes: PredictionHandle + slot
    
    Note over Routes: SyncPredictionGuard held<br/>(cancels on connection drop)
    
    Routes->>Service: predict(slot, input)
    Service->>Worker: predict(slot, input)
    Worker-->>Service: result
    Note over Service: Prediction.set_succeeded() fires webhook
    
    Routes-->>Client: 200 {output}
```

**Key behavior**: The `SyncPredictionGuard` is held for the duration of the request. If the client connection drops, the guard is dropped and the prediction is automatically cancelled.

### Async Request (Prefer: respond-async)

```mermaid
sequenceDiagram
    participant Client
    participant Routes
    participant Service
    participant Worker
    
    Client->>Routes: POST + respond-async
    Routes->>Service: submit_prediction(id, input, webhook)
    Service-->>Routes: PredictionHandle + slot
    
    Routes-->>Client: 202 {status: "starting"}
    
    Note over Routes,Worker: spawned task continues independently
    
    par Background Task
        Service->>Worker: predict(slot, input)
        Worker-->>Service: result
        Note over Service: Prediction mutations fire webhooks automatically
    end
    
    Service-->>Client: webhook (completed)
```

**Key behavior**: No guard is held. The prediction continues even if the client disconnects.

### Connection Drop (Sync Mode)

```mermaid
sequenceDiagram
    participant Client
    participant Routes
    participant Service
    participant Worker
    
    Client->>Routes: POST /predictions
    Note over Routes: SyncPredictionGuard armed
    Routes->>Worker: predict(slot)
    
    Client-xRoutes: ✕ connection drops
    
    Note over Routes: guard.drop()
    Routes->>Service: cancel(id)
    Service->>Worker: Cancel
    Worker-->>Service: Cancelled
```

## Invocation Path

How coglet gets invoked when running a Cog container:

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                        cog predict / cog run                                │
│                               (CLI)                                         │
└─────────────────────────────────┬───────────────────────────────────────────┘
                                  │
                                  ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                     python -m cog.server.http                               │
│                                                                             │
│   import coglet                                                             │
│   coglet.server.serve(predictor_ref, port=5000)                             │
└─────────────────────────────────┬───────────────────────────────────────────┘
                                  │
                                  ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                          coglet (Rust)                                      │
│                                                                             │
│   ┌───────────────────────────────────────────────────────────────────┐     │
│   │  HTTP Server (axum)  :5000                                        │     │
│   │    /predictions, /health-check, etc.                              │     │
│   └───────────────────────────────────────────────────────────────────┘     │
│                              │                                              │
│                              ▼                                              │
│   ┌───────────────────────────────────────────────────────────────────┐     │
│   │  PredictionService (state, webhooks, permits)                      │     │
│   └───────────────────────────────────────────────────────────────────┘     │
│                              │                                              │
│                    Unix socket + pipes                                      │
│                              │                                              │
│                              ▼                                              │
│   ┌───────────────────────────────────────────────────────────────────┐     │
│   │  Worker subprocess (Python)                                       │     │
│   │    - loads predictor_ref                                          │     │
│   │    - runs setup()                                                 │     │
│   │    - handles predict() requests                                   │     │
│   └───────────────────────────────────────────────────────────────────┘     │
└─────────────────────────────────────────────────────────────────────────────┘
```

## Key Design Decisions

### Why Rust?
- **Performance**: Axum is faster than Python HTTP frameworks for request handling
- **Stability**: Server doesn't crash when user code fails
- **Resource management**: Better backpressure and concurrency control
- **Memory safety**: No Python GIL contention in HTTP layer

### Why PyO3?
- **ABI3 wheel**: Single wheel works across Python 3.10-3.13
- **Native performance**: Direct C API calls, no serialization overhead
- **Same predictor code**: Users don't change anything
- **Drop-in**: Same HTTP API, same behavior

### Why Subprocess (not in-process)?
- **Isolation**: Python crashes/segfaults don't kill server
- **CUDA context**: Clean GPU initialization per worker
- **Memory**: Fresh address space for model loading

### Why Slots (not async tasks)?
- **Predictable**: Fixed number of concurrent predictions
- **Fair**: Permits prevent starvation
- **Observable**: Easy to monitor slot usage
- **Simple**: No async complexity in worker subprocess

## Environment Variables

| Variable | Default | Purpose |
|----------|---------|---------|
| `PORT` | 5000 | HTTP server port |
| `COG_LOG_LEVEL` | INFO | Logging verbosity |
| `COG_MAX_CONCURRENCY` | 1 | Number of concurrent prediction slots |

## File Structure

```
crates/coglet/src/
├── lib.rs                    # Public API exports
├── service.rs                # PredictionService (single owner of prediction state)
├── prediction.rs             # Prediction state (logs, outputs, status)
├── health.rs                 # Health enum + SetupResult
├── orchestrator.rs           # Worker subprocess management
├── permit/
│   ├── pool.rs               # PermitPool (concurrency control)
│   └── slot.rs               # PredictionSlot (Prediction + Permit RAII)
├── bridge/
│   ├── protocol.rs           # Control/Slot request/response types
│   ├── codec.rs              # JSON length-delimited framing
│   └── transport.rs          # Unix socket transport
├── transport/
│   └── http/
│       ├── server.rs         # Axum server setup
│       └── routes.rs         # HTTP handlers (uses service)
├── webhook.rs                # WebhookSender (retry logic, trace context)
└── worker.rs                 # Worker event loop (child side)

crates/coglet-python/src/
├── lib.rs                    # PyO3 module: serve(), _run_worker()
├── predictor.rs              # PythonPredictor wrapper (sync/async)
├── worker_bridge.rs          # PredictHandler trait implementation
├── log_writer.rs             # ContextVar-based stdout/stderr routing
├── audit.rs                  # Audit hook, TeeWriter (protects streams)
├── cancel.rs                 # SIGUSR1-based cancellation for sync predictors
├── input.rs                  # Input processing (file downloads, normalization)
└── output.rs                 # Output serialization
```

## Code References

| File | Purpose |
|------|---------|
| `crates/coglet/src/service.rs` | Main orchestrator: PredictionService |
| `crates/coglet/src/prediction.rs` | Prediction state machine + webhook firing |
| `crates/coglet/src/transport/http/routes.rs` | HTTP endpoint handlers |
| `crates/coglet/src/permit/pool.rs` | Slot-based concurrency control |
| `crates/coglet/src/orchestrator.rs` | Worker subprocess spawn/management |
| `crates/coglet/src/bridge/protocol.rs` | IPC message definitions |
| `crates/coglet-python/src/lib.rs` | PyO3 Python bindings |
| `python/cog/server/http.py` | Thin launcher that calls coglet |
