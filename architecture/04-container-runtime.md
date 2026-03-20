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

## Ownership Model

`PredictionService` is the single owner of all prediction state. Everything flows through it.

A prediction's lifecycle involves three key objects:

- **PredictionEntry** (in a concurrent DashMap) -- the source of truth for a prediction's state. Holds the `Prediction` state machine (shared via Arc), a cancellation token, and the original input.
- **PredictionSlot** -- RAII container that pairs a prediction with a concurrency permit. When the slot drops, the permit returns to the pool automatically.
- **PredictionHandle** -- returned to the HTTP route handler. For sync requests, calling `sync_guard()` creates a guard that cancels the prediction if the client connection drops.

The `Prediction` struct is itself a state machine -- its mutation methods (`set_processing`, `set_succeeded`, `append_log`, etc.) fire webhooks as a side effect. This keeps webhook delivery tightly coupled to state transitions rather than scattered across call sites.

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

Communication between the Rust server and Python worker uses two channels. All messages are JSON, one per line.

### Control Channel (stdin/stdout)

Lifecycle messages for the worker as a whole.

**Parent → Worker:**

| Message | Purpose |
|---------|---------|
| `Init { predictor_ref, num_slots, is_train, is_async, ... }` | Bootstrap worker -- load predictor, create slots |
| `Cancel { slot }` | Cancel a running prediction on a slot |
| `Healthcheck { id }` | Request a user-defined healthcheck |
| `Shutdown` | Graceful shutdown |

**Worker → Parent:**

| Message | Purpose |
|---------|---------|
| `Ready { slots, schema }` | Worker initialized, here are the slot IDs and OpenAPI schema |
| `Log { source, data }` | Setup-time log line (stdout or stderr) |
| `WorkerLog { target, level, message }` | Structured log from the worker runtime itself (not user code) |
| `Idle { slot }` | Slot finished a prediction and is available |
| `Cancelled { slot }` | Prediction on slot was cancelled |
| `Failed { slot, error }` | Prediction on slot failed |
| `Fatal { reason }` | Unrecoverable error -- worker is shutting down |
| `DroppedLogs { count, interval_millis }` | Worker dropped log messages due to backpressure |
| `HealthcheckResult { id, status, error }` | Result of a user-defined healthcheck |
| `ShuttingDown` | Worker is shutting down |

### Slot Channel (Unix socket per slot)

Per-prediction data. Using separate sockets per slot avoids head-of-line blocking between concurrent predictions.

**Parent → Worker:**

| Message | Purpose |
|---------|---------|
| `Predict { id, input, input_file, output_dir }` | Run a prediction. `input` is inline JSON; for large payloads (>6MiB) it's `null` and `input_file` points to a spill file on disk |

**Worker → Parent:**

| Message | Purpose |
|---------|---------|
| `Log { source, data }` | Log line from predict() |
| `Output { output }` | Yielded output value (for generators/streaming) |
| `FileOutput { filename, kind, mime_type }` | File produced by predict() -- referenced by path, uploaded by parent |
| `Metric { name, value, mode }` | Custom metric (mode: `replace`, `increment`, or `append`) |
| `Done { id, output, predict_time, is_stream }` | Prediction completed successfully |
| `Failed { id, error }` | Prediction failed |
| `Cancelled { id }` | Prediction was cancelled |

## Health State Machine

```mermaid
stateDiagram-v2
    [*] --> UNKNOWN: Process starts
    note right of UNKNOWN: Predictions return 503
    
    UNKNOWN --> STARTING: serve() called
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

There's a distinction between internal health state (`Health` enum) and what the HTTP response returns (`HealthResponse`). The HTTP response adds one extra state: `UNHEALTHY`, which is transient -- it's returned when a user-defined healthcheck fails but doesn't change the internal health state. See [User-Defined Healthchecks](#user-defined-healthchecks) below.

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

## Input Spilling

When a prediction input exceeds 6MiB, it's too large to send inline through the IPC socket. Instead, the parent writes it to a temporary file and sends the file path in `input_file` (with `input` set to null). The worker reads the file, deletes it, and proceeds normally. This is transparent to the predictor code.

## File Outputs

When predict() produces file outputs (`cog.Path`), the worker sends a `FileOutput` message with the filename and MIME type. The parent handles uploading the file (or base64-encoding it for inline responses). The `output_dir` field in the `Predict` request tells the worker where to write output files.

`FileOutputKind` distinguishes between normal file outputs (`FileType`) and oversized outputs (`Oversized`) that exceeded an inline size limit.

## Custom Metrics

Models can record custom metrics via `self.record_metric(name, value, mode)` in their predict method. These are sent as `Metric` messages on the slot channel. The `mode` controls how metrics aggregate:

- `replace` -- overwrite any existing value
- `increment` -- add to the current value (numeric)
- `append` -- append to a list

Metrics appear in the prediction response's `metrics` object alongside the built-in `predict_time`.

## User-Defined Healthchecks

Models can implement a custom healthcheck that runs alongside the built-in health state machine. The parent sends `Healthcheck { id }` on the control channel; the worker runs the user's healthcheck and responds with `HealthcheckResult { id, status, error }`.

If the healthcheck fails, the HTTP `/health-check` endpoint returns `UNHEALTHY` -- but this is transient and doesn't change the internal `Health` state. The model stays `READY` and continues accepting predictions.

## Environment Variables

| Variable | Default | Purpose |
|----------|---------|---------|
| `PORT` | 5000 | HTTP server port |
| `COG_LOG_LEVEL` | INFO | Logging verbosity (ignored if `RUST_LOG` is set) |
| `COG_MAX_CONCURRENCY` | 1 | Number of concurrent prediction slots |
| `COG_SETUP_TIMEOUT` | none | Setup timeout in seconds (0 is ignored) |
| `COG_THROTTLE_RESPONSE_INTERVAL` | 0.5s | Webhook response throttling interval |
| `LOG_FORMAT` | json | Set to `console` for human-readable log output |

## Where to Look

**coglet core** (`crates/coglet/src/`):
- `service.rs` -- `PredictionService`, the central coordinator. Start here.
- `orchestrator.rs` -- worker subprocess spawning and lifecycle
- `bridge/` -- IPC protocol definitions (`protocol.rs`) and Unix socket transport
- `permit/` -- slot-based concurrency control (`PermitPool`, `PredictionSlot`)
- `transport/http/` -- Axum HTTP server and route handlers
- `prediction.rs` -- prediction state machine, webhook firing on state transitions

**coglet-python** (`crates/coglet-python/src/`):
- `lib.rs` -- PyO3 module entry point: `serve()` and `_run_worker()`
- `predictor.rs` -- wraps the Python predictor class, handles sync/async detection
- `worker_bridge.rs` -- implements the `PredictHandler` trait for Python
- `log_writer.rs` -- ContextVar-based stdout/stderr routing per prediction slot

**Python launcher**: `python/cog/server/http.py` -- the thin entry point that calls `coglet.server.serve()`
