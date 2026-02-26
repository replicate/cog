# Coglet: Rust Runtime for Cog

Coglet is the Rust-based prediction server that powers Cog's subprocess isolation model.
It provides process isolation, concurrent slot management, and high-performance IPC for
running ML predictions.

## Architecture Overview

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              Parent Process                                  │
│  ┌─────────────┐    ┌──────────────┐    ┌─────────────────────────────────┐ │
│  │ HTTP Server │───▶│ Prediction   │───▶│        Orchestrator             │ │
│  │   (axum)    │    │   Service    │    │  - Spawns worker subprocess     │ │
│  └─────────────┘    └──────────────┘    │  - Routes predictions to slots  │ │
│                                          │  - Handles worker lifecycle     │ │
│                                          └───────────────┬─────────────────┘ │
│                                                          │                   │
│                          ┌───────────────────────────────┼───────────────┐   │
│                          │  Control Channel (stdin/stdout - JSON lines) │   │
│                          │  - Init, Ready, Cancel, Shutdown             │   │
│                          └───────────────────────────────┼───────────────┘   │
│                                                          │                   │
│                          ┌───────────────────────────────┼───────────────┐   │
│                          │  Slot Sockets (Unix domain - per slot)       │   │
│                          │  - Predict requests                          │   │
│                          │  - Streaming logs, outputs                   │   │
│                          │  - Done/Failed/Cancelled responses           │   │
│                          └───────────────────────────────┼───────────────┘   │
└──────────────────────────────────────────────────────────┼───────────────────┘
                                                           │
┌──────────────────────────────────────────────────────────┼───────────────────┐
│                           Worker Subprocess              │                   │
│  ┌─────────────────────────────────────────────────────────────────────────┐ │
│  │                        Python Runtime (GIL)                             │ │
│  │  ┌─────────────────┐   ┌─────────────────┐   ┌───────────────────────┐  │ │
│  │  │ PythonPredictor │   │  SlotLogWriter  │   │    Audit Hook        │  │ │
│  │  │ - load()        │   │ (sys.stdout/err)│   │ - Protects streams   │  │ │
│  │  │ - setup()       │   │  Routes via     │   │ - Tee pattern for    │  │ │
│  │  │ - predict()     │   │  ContextVar     │   │   user overrides     │  │ │
│  │  └─────────────────┘   └─────────────────┘   └───────────────────────┘  │ │
│  └─────────────────────────────────────────────────────────────────────────┘ │
│                                                                              │
│  ┌──────────────────────────────────────────────────────────────────────┐    │
│  │                         Tokio Runtime                                 │    │
│  │  - Async event loop for slot socket I/O                              │    │
│  │  - Releases GIL during I/O (py.detach)                               │    │
│  │  - Single async executor for async predictors                        │    │
│  └──────────────────────────────────────────────────────────────────────┘    │
└──────────────────────────────────────────────────────────────────────────────┘
```

## Prediction Flow

```
HTTP Request                     Parent Process                    Worker Subprocess
     │                                │                                  │
     │  POST /predictions             │                                  │
     ├───────────────────────────────▶│                                  │
     │                                │                                  │
     │                    ┌───────────┴───────────┐                      │
     │                    │ 1. Acquire slot permit│                      │
     │                    │ 2. Register prediction│                      │
     │                    └───────────┬───────────┘                      │
     │                                │                                  │
     │                                │  SlotRequest::Predict            │
     │                                │  {id, input}                     │
     │                                ├─────────────────────────────────▶│
     │                                │        (slot socket)             │
     │                                │                                  │
     │                                │                      ┌───────────┴───────────┐
     │                                │                      │ 3. Set ContextVar     │
     │                                │                      │ 4. Call predict()     │
     │                                │                      └───────────┬───────────┘
     │                                │                                  │
     │                                │  SlotResponse::Log               │
     │                                │◀─────────────────────────────────┤ (streaming)
     │                                │                                  │
     │                                │  SlotResponse::Output            │
     │                                │◀─────────────────────────────────┤ (generators)
     │                                │                                  │
     │                                │  SlotResponse::Done              │
     │                                │◀─────────────────────────────────┤
     │                                │  {id, output, predict_time}      │
     │                                │                                  │
     │                    ┌───────────┴───────────┐                      │
     │                    │ 5. Update prediction  │                      │
     │                    │ 6. Release permit     │                      │
     │                    │ 7. Send webhook       │                      │
     │                    └───────────┬───────────┘                      │
     │                                │                                  │
     │  200 OK {output}               │                                  │
     │◀───────────────────────────────┤                                  │
     │                                │                                  │
```

## Startup Sequence

```
1. coglet.server.serve() called from Python
   │
   ├─▶ Start HTTP server immediately (health returns STARTING until ready)
   │
   └─▶ Spawn orchestrator task
       │
       ├─▶ Create slot transport (Unix sockets)
       │
        ├─▶ Spawn worker: python -c "import coglet; coglet.server._run_worker()"
       │
       ├─▶ Send Init message (predictor_ref, num_slots, transport_info)
       │     │
       │     │   ┌────────────────────────────────────────────────┐
       │     └──▶│ Worker: connect sockets, install log writers, │
       │         │ install audit hook, load predictor, run setup │
       │         └────────────────────────────────────────────────┘
       │
       ├─▶ Wait for Ready {slots, schema} or Failed {error}
       │
       ├─▶ Populate PermitPool with slot sockets
       │
       ├─▶ Start event loop (routes responses to predictions)
       │
       └─▶ Set health = READY, start accepting predictions
```

## Components

### coglet (core library)
Pure Rust library with no Python dependencies. Provides:
- **orchestrator.rs** - Spawns worker, manages lifecycle, routes messages
- **worker.rs** - Child-side event loop, prediction execution
- **service.rs** - Transport-agnostic prediction service
- **permit/** - Slot-based concurrency control (PermitPool)
- **bridge/** - IPC protocol and transport (Unix sockets + JSON codec)
- **transport/http/** - Axum-based HTTP server and routes

### coglet-python (PyO3 bindings)
Bridges coglet to Python via PyO3. Provides:
- **lib.rs** - Python module with `serve()`, `active()`, `_run_worker()`
- **predictor.rs** - Wraps Python predictor class (sync/async detection)
- **worker_bridge.rs** - Implements `PredictHandler` trait for Python
- **log_writer.rs** - ContextVar-based stdout/stderr routing
- **audit.rs** - Protects runtime streams from user code
- **cancel.rs** - SIGUSR1-based cancellation for sync predictors

## Directory Structure

```
crates/
├── Cargo.toml              # Workspace manifest
├── Cargo.lock
├── deny.toml               # cargo-deny configuration
│
├── coglet/                 # Core Rust library
│   ├── Cargo.toml
│   └── src/
│       ├── lib.rs          # Public API exports
│       ├── health.rs       # Health/SetupStatus types
│       ├── prediction.rs   # Prediction state machine
│       ├── predictor.rs    # PredictionResult, PredictionError
│       ├── service.rs      # PredictionService
│       ├── webhook.rs      # WebhookSender (retry, trace context)
│       ├── version.rs      # Version info
│       ├── webhook.rs      # Webhook sender
│       ├── orchestrator.rs # Worker lifecycle, event loop (parent)
│       ├── worker.rs       # Worker event loop (child)
│       ├── bridge/
│       │   ├── mod.rs
│       │   ├── codec.rs    # JSON line codec
│       │   ├── protocol.rs # Message types (ControlRequest, SlotResponse, etc.)
│       │   └── transport.rs # Unix socket transport
│       ├── permit/
│       │   ├── mod.rs
│       │   ├── pool.rs     # PermitPool (concurrency control)
│       │   └── slot.rs     # PredictionSlot (permit + prediction)
│       └── transport/
│           ├── mod.rs
│           └── http/
│               ├── mod.rs
│               ├── server.rs  # Axum server setup
│               └── routes.rs  # HTTP handlers
│
└── coglet-python/          # PyO3 bindings
    ├── Cargo.toml
    ├── coglet.pyi          # Type stubs for Python
    └── src/
        ├── lib.rs          # Python module definition
        ├── predictor.rs    # PythonPredictor wrapper
        ├── worker_bridge.rs # PredictHandler impl
        ├── input.rs        # Input processing (Pydantic/ADT)
        ├── output.rs       # Output serialization
        ├── log_writer.rs   # SlotLogWriter, ContextVar routing
        ├── audit.rs        # Audit hook, TeeWriter
        └── cancel.rs       # Cancellation support
```

## Bridge Protocol

Two communication channels between parent and worker:

### Control Channel (stdin/stdout)

Used for lifecycle messages. JSON lines, one message per line.

**Parent → Worker:**
```json
{"type": "init", "predictor_ref": "predict.py:Predictor", "num_slots": 2, ...}
{"type": "cancel", "slot": "uuid"}
{"type": "shutdown"}
```

**Worker → Parent:**
```json
{"type": "ready", "slots": ["uuid1", "uuid2"], "schema": {...}}
{"type": "log", "source": "stdout", "data": "Loading model..."}
{"type": "idle", "slot": "uuid"}
{"type": "failed", "slot": "uuid", "error": "Setup failed: ..."}
{"type": "shutting_down"}
```

### Slot Sockets (Unix domain)

Per-slot bidirectional sockets for prediction data. Avoids head-of-line blocking.

**Parent → Worker:**
```json
{"type": "predict", "id": "pred_123", "input": {"prompt": "Hello"}}
```

**Worker → Parent:**
```json
{"type": "log", "source": "stdout", "data": "Processing..."}
{"type": "output", "output": "chunk"}
{"type": "done", "id": "pred_123", "output": "Hello, world!", "predict_time": 0.5}
{"type": "failed", "id": "pred_123", "error": "ValueError: ..."}
{"type": "cancelled", "id": "pred_123"}
```

## Key Design Decisions

### Subprocess Isolation
Worker runs in a separate process. Benefits:
- Crash isolation (worker crash → restart, parent survives)
- Memory isolation (GPU memory leaks don't accumulate)
- Clean shutdown (SIGKILL if needed)

### Single Worker Mode
Always exactly one worker subprocess. No dynamic scaling - the parent is
lightweight, all the heavy lifting happens in the worker.

### Slot-Based Concurrency
Each slot is a Unix socket pair. `max_concurrency` determines slot count.
Permits control access - at most one prediction per slot at a time.

### ContextVar-Based Log Routing
Async predictions may spawn tasks. ContextVar propagates prediction ID
through the call stack, allowing log routing even from spawned tasks.

### Audit Hook Protection
User code might replace `sys.stdout`. The audit hook intercepts this and
wraps their stream in a TeeWriter, preserving our log routing while
allowing their code to work as expected.
