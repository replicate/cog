# Container Runtime (FFI/Rust)

This document covers the FFI runtime implementation using Rust with PyO3 bindings. This is a complete rewrite of the HTTP server, moving from Python/FastAPI to Rust/Axum with a PyO3 ABI3 wheel.

## Overview

The FFI runtime provides significant improvements over the legacy Python runtime:
- **Rust HTTP server (Axum)**: Faster request handling, better backpressure management
- **Worker isolation**: Python predictor crashes don't kill the server
- **Slot-based concurrency**: Predictable resource control with permit pools
- **Same API surface**: Drop-in replacement for the legacy runtime
- **Subprocess reuse**: Predictor stays loaded between requests

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
│  │                        PredictionSupervisor (DashMap)                      │ │
│  │  ┌─────────────────┐  ┌─────────────────┐  ┌─────────────────┐            │ │
│  │  │ PredictionEntry │  │ PredictionEntry │  │ PredictionEntry │  ...       │ │
│  │  │ ─────────────── │  │ ─────────────── │  │ ─────────────── │            │ │
│  │  │ state           │  │ state           │  │ state           │            │ │
│  │  │ cancel_token    │  │ cancel_token    │  │ cancel_token    │            │ │
│  │  │ webhook         │  │ webhook         │  │ webhook         │            │ │
│  │  │ completion      │  │ completion      │  │ completion      │            │ │
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

The FFI runtime uses clear ownership patterns to manage prediction lifecycle:

```
═══════════════════════════════════════════════════════════════════════════════════
                           COMPONENT OWNERSHIP
═══════════════════════════════════════════════════════════════════════════════════
  PredictionSupervisor (DashMap - lock-free concurrent access)
  ├── owns: prediction state (id, status, input, output, logs, error, timestamps)
  ├── owns: webhook sender (sends terminal webhook, then cleans up entry)
  └── owns: completion notifier (for waiting on result)

  PredictionSlot (RAII container)
  ├── owns: Prediction (logs, outputs from worker event loop)
  ├── owns: Permit (concurrency token, returns to pool on drop)
  └── Drop: marks permit idle, releases back to pool

  PredictionHandle (returned to route handler)
  ├── has: reference to supervisor (for state queries)
  ├── has: completion notifier clone (for waiting)
  └── method: sync_guard() → SyncPredictionGuard (cancels on drop)

  Cancellation (via OrchestratorHandle)
  ├── Sync predictors: ControlRequest::Cancel → SIGUSR1 → KeyboardInterrupt
  └── Async predictors: ControlRequest::Cancel → future.cancel() → CancelledError
═══════════════════════════════════════════════════════════════════════════════════
```

## Worker Subprocess Protocol

Communication between the Rust server and Python worker uses two channels:

### Control Channel (stdin/stdout - JSON framed)

| Parent → Child | Child → Parent |
|----------------|----------------|
| `Init { predictor_ref, num_slots, ... }` | `Ready { slots, schema }` |
| `Cancel { slot }` | `Log { source, data }` |
| `Shutdown` | `Idle { slot }` |
| | `Failed { slot, error }` |
| | `ShuttingDown` |

### Slot Channel (Unix socket per slot - JSON framed)

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
    note right of STARTING: HTTP serves 503
    
    STARTING --> READY: setup() succeeds
    STARTING --> SETUP_FAILED: setup() raises exception
    
    READY --> BUSY: All slots occupied
    note right of BUSY: 409 for new predictions
    
    BUSY --> READY: Slot freed
    
    READY --> DEFUNCT: Fatal error / worker crash
    BUSY --> DEFUNCT: Fatal error / worker crash
    note right of DEFUNCT: HTTP serves 503
    
    SETUP_FAILED --> [*]
    DEFUNCT --> [*]
```

### Health States

| State | HTTP Behavior | Meaning |
|-------|--------------|---------|
| `STARTING` | 503 Service Unavailable | Worker subprocess initializing, `setup()` running |
| `READY` | 200 OK | Worker ready, at least one slot available |
| `BUSY` | 409 Conflict | All slots occupied, no capacity for new predictions |
| `SETUP_FAILED` | 503 Service Unavailable | `setup()` threw exception, cannot serve predictions |
| `DEFUNCT` | 503 Service Unavailable | Fatal error or worker crash, server unusable |

## Prediction Flow

### Sync Request (POST /predictions)

```mermaid
sequenceDiagram
    participant Client
    participant Routes
    participant Supervisor
    participant Worker
    
    Client->>Routes: POST /predictions
    Routes->>Supervisor: submit(id, input)
    Supervisor-->>Routes: PredictionHandle
    
    Routes->>Routes: acquire permit
    Note over Routes: SyncPredictionGuard held<br/>(cancels on connection drop)
    
    Routes->>Worker: predict(slot, input)
    Worker-->>Routes: result
    
    Routes->>Supervisor: update_status(terminal)
    Supervisor->>Supervisor: send webhook
    Supervisor->>Supervisor: cleanup entry
    
    Routes-->>Client: 200 {output}
```

**Key behavior**: The `SyncPredictionGuard` is held for the duration of the request. If the client connection drops, the guard is dropped and the prediction is automatically cancelled.

### Async Request (Prefer: respond-async)

```mermaid
sequenceDiagram
    participant Client
    participant Routes
    participant Supervisor
    participant Worker
    
    Client->>Routes: POST + respond-async
    Routes->>Supervisor: submit(id, input)
    Supervisor-->>Routes: PredictionHandle
    
    Routes-->>Client: 202 {status: "starting"}
    
    Note over Routes,Worker: spawned task continues independently
    
    par Background Task
        Routes->>Worker: predict(slot, input)
        Worker-->>Routes: result
        Routes->>Supervisor: update_status(result)
        Supervisor->>Supervisor: webhook + cleanup
    end
    
    Supervisor-->>Client: webhook (completed)
```

**Key behavior**: No guard is held. The prediction continues even if the client disconnects.

### Idempotent PUT (PUT /predictions/{id})

```mermaid
sequenceDiagram
    participant Client
    participant Routes
    participant Supervisor
    
    Client->>Routes: PUT /predictions/X
    Routes->>Supervisor: get_state("X")
    
    alt Prediction exists
        Supervisor-->>Routes: existing state
        Routes-->>Client: 202 + full state
    else Prediction doesn't exist
        Routes->>Supervisor: submit + run prediction
        Routes-->>Client: 202 + starting state
    end
```

### Connection Drop (Sync Mode)

```mermaid
sequenceDiagram
    participant Client
    participant Routes
    participant Supervisor
    participant Worker
    
    Client->>Routes: POST /predictions
    Note over Routes: SyncPredictionGuard armed
    Routes->>Worker: predict(slot)
    
    Client-xRoutes: ✕ connection drops
    
    Note over Routes: guard.drop()
    Routes->>Supervisor: cancel_token.cancel()
    Supervisor->>Worker: Cancel
    Worker-->>Supervisor: Cancelled
```

## File Structure

```
crates/coglet/src/
├── lib.rs                    # Public API exports
├── service.rs                # PredictionService (orchestrates everything)
├── supervisor.rs             # PredictionSupervisor (lifecycle + webhooks)
├── prediction.rs             # Prediction state (logs, outputs, status)
├── health.rs                 # Health enum + SetupResult
├── orchestrator.rs           # Worker subprocess management
├── permit/
│   ├── mod.rs
│   ├── pool.rs               # PermitPool (concurrency control)
│   └── slot.rs               # PredictionSlot (Prediction + Permit RAII)
├── bridge/
│   ├── mod.rs
│   ├── protocol.rs           # Control/Slot request/response types
│   ├── codec.rs              # JSON length-delimited framing
│   └── transport.rs          # Unix socket transport
├── transport/
│   └── http/
│       ├── mod.rs
│       ├── server.rs         # Axum server setup
│       └── routes.rs         # HTTP handlers (uses supervisor)
├── webhook.rs                # WebhookSender (retry logic, trace context)
├── worker.rs                 # run_worker, PredictHandler trait, SetupError
└── version.rs                # VersionInfo

crates/coglet-python/src/
└── lib.rs                    # PyO3 bindings (coglet.serve())
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
│   if USE_COGLET env var:                                                    │
│       import coglet                                                         │
│       coglet.serve(predictor_ref, port=5000)  ──────────────────────────┐   │
│   else:                                                                 │   │
│       # original Python FastAPI server                                  │   │
│       uvicorn.run(app, port=5000)                                       │   │
└─────────────────────────────────────────────────────────────────────────┼───┘
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
│   │  PredictionService + Supervisor                                   │     │
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
- **Performance**: Axum is faster than Uvicorn/FastAPI for HTTP handling
- **Stability**: Server doesn't crash when user code fails
- **Resource management**: Better backpressure and concurrency control
- **Memory safety**: No Python GIL contention in HTTP layer

### Why PyO3 FFI?
- **ABI3 wheel**: Single wheel works across Python 3.8-3.13
- **Native performance**: Direct C API calls, no serialization overhead
- **Same predictor code**: Users don't change anything
- **Drop-in replacement**: Same HTTP API, same behavior

### Why Subprocess (not in-process)?
- **Isolation**: Python crashes/segfaults don't kill server
- **CUDA context**: Clean GPU initialization per worker
- **Memory**: Fresh address space for model loading
- **Restart**: Server can restart worker on fatal errors

### Why Slots (not async tasks)?
- **Predictable**: Fixed number of concurrent predictions
- **Fair**: Permits prevent starvation
- **Observable**: Easy to monitor slot usage
- **Simple**: No async complexity in worker subprocess

## Environment Variables

| Variable | Default | Purpose |
|----------|---------|---------|
| `USE_COGLET` | unset | Enable FFI runtime (set to any value) |
| `PORT` | 5000 | HTTP server port |
| `COG_LOG_LEVEL` | INFO | Logging verbosity |
| `COG_CONCURRENCY_SLOTS` | 1 | Number of concurrent prediction slots |

## Comparison to Legacy Runtime

| Aspect | Legacy (Python) | FFI (Rust) |
|--------|----------------|------------|
| HTTP Server | FastAPI/Uvicorn | Axum |
| Language | Pure Python | Rust + PyO3 |
| IPC | multiprocessing.Pipe (pickled) | Unix socket + pipes (JSON) |
| Concurrency | async tasks | Slot-based permits |
| Cancellation | SIGUSR1 signal | IPC message + SIGUSR1 (sync) / future.cancel() (async) |
| Connection drop | No effect on prediction | Cancels sync predictions |
| Worker crash | Server unstable | Server stays up, marks DEFUNCT |
| Performance | Baseline | ~2x faster HTTP layer |

## Code References

| File | Purpose |
|------|---------|
| `crates/coglet/src/service.rs` | Main orchestrator: PredictionService |
| `crates/coglet/src/supervisor.rs` | Prediction lifecycle management |
| `crates/coglet/src/transport/http/routes.rs` | HTTP endpoint handlers |
| `crates/coglet/src/permit/pool.rs` | Slot-based concurrency control |
| `crates/coglet/src/orchestrator.rs` | Worker subprocess spawn/management |
| `crates/coglet/src/bridge/protocol.rs` | IPC message definitions |
| `crates/coglet-python/src/lib.rs` | PyO3 Python bindings |
| `python/cog/server/http.py` | Entry point (checks USE_COGLET) |
