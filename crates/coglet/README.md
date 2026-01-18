# coglet

Core Rust library for the coglet prediction server. Pure Rust with no Python
dependencies - the Python bindings live in `coglet-python`.

## Architecture

```
                                    coglet
    ┌─────────────────────────────────────────────────────────────────┐
    │                                                                 │
    │  ┌─────────────────────────────────────────────────────────┐   │
    │  │                    transport/http                        │   │
    │  │  ┌──────────────┐  ┌─────────────────────────────────┐  │   │
    │  │  │   server.rs  │  │           routes.rs             │  │   │
    │  │  │  Axum setup  │  │ /health, /predictions, /cancel  │  │   │
    │  │  └──────────────┘  └─────────────────────────────────┘  │   │
    │  └───────────────────────────────┬─────────────────────────┘   │
    │                                  │                              │
    │  ┌───────────────────────────────▼─────────────────────────┐   │
    │  │                     service.rs                          │   │
    │  │  PredictionService: health, permits, predict routing    │   │
    │  └───────────────────────────────┬─────────────────────────┘   │
    │                                  │                              │
    │         ┌────────────────────────┼────────────────────────┐    │
    │         │                        │                        │    │
    │         ▼                        ▼                        ▼    │
    │  ┌─────────────┐    ┌────────────────────┐    ┌──────────────┐│
    │  │ permit/     │    │   orchestrator.rs  │    │ supervisor.rs││
    │  │ PermitPool  │    │   Parent-side:     │    │ State track  ││
    │  │ Slot alloc  │    │   spawn, route     │    │ Webhooks     ││
    │  └─────────────┘    └─────────┬──────────┘    └──────────────┘│
    │                               │                                │
    │  ┌────────────────────────────▼────────────────────────────┐   │
    │  │                      bridge/                            │   │
    │  │  ┌──────────────┐  ┌─────────────┐  ┌────────────────┐  │   │
    │  │  │ protocol.rs  │  │  codec.rs   │  │ transport.rs   │  │   │
    │  │  │ Message types│  │ JSON lines  │  │ Unix sockets   │  │   │
    │  │  └──────────────┘  └─────────────┘  └────────────────┘  │   │
    │  └─────────────────────────────────────────────────────────┘   │
    │                                                                 │
    │  ┌─────────────────────────────────────────────────────────┐   │
    │  │                      worker.rs                          │   │
    │  │  Child-side: PredictHandler trait, run_worker loop      │   │
    │  └─────────────────────────────────────────────────────────┘   │
    │                                                                 │
    └─────────────────────────────────────────────────────────────────┘
```

## Directory Structure

```
coglet/
└── src/
    ├── lib.rs              # Public API exports
    │
    │   # Core Types
    ├── health.rs           # Health, SetupStatus, SetupResult
    ├── prediction.rs       # Prediction state machine
    ├── predictor.rs        # PredictionResult, PredictionError, PredictionOutput
    ├── version.rs          # VersionInfo
    │
    │   # Service Layer
    ├── service.rs          # PredictionService - main entry point
    ├── supervisor.rs       # PredictionSupervisor - state/webhook management
    ├── webhook.rs          # WebhookSender, webhook types
    │
    │   # Orchestrator (Parent Process)
    ├── orchestrator.rs     # spawn_worker, OrchestratorHandle, event loop
    │
    │   # Worker (Child Process)  
    ├── worker.rs           # run_worker, PredictHandler trait, SetupError
    │
    │   # Concurrency Control
    ├── permit/
    │   ├── mod.rs
    │   ├── pool.rs         # PermitPool - slot permit management
    │   └── slot.rs         # PredictionSlot - permit + prediction binding
    │
    │   # IPC Bridge
    ├── bridge/
    │   ├── mod.rs
    │   ├── protocol.rs     # ControlRequest, ControlResponse, SlotRequest, SlotResponse
    │   ├── codec.rs        # JsonCodec - newline-delimited JSON
    │   └── transport.rs    # Unix socket transport, ChildTransportInfo
    │
    │   # HTTP Transport
    └── transport/
        ├── mod.rs
        └── http/
            ├── mod.rs
            ├── server.rs   # ServerConfig, serve()
            └── routes.rs   # Route handlers, request/response types
```

## Key Components

### PredictionService (`service.rs`)

Central coordination point. Owns:
- Health state (Unknown → Starting → Ready/SetupFailed)
- PermitPool or Orchestrator reference
- PredictionSupervisor for state tracking

Two modes:
- **Legacy mode**: Direct predict functions (testing)
- **Orchestrator mode**: Routes through worker subprocess

```rust
let service = PredictionService::new_no_pool()
    .with_health(Health::Starting)
    .with_version(version);

// Later, after worker is ready:
service.set_orchestrator(pool, handle).await;
service.set_health(Health::Ready).await;
```

### Orchestrator (`orchestrator.rs`)

Parent-side worker lifecycle management.

```
spawn_worker(config)
    │
    ├─▶ Create Unix socket transport (N slots)
    ├─▶ Spawn: python -c "import coglet; coglet._run_worker()"
    ├─▶ Send Init message via stdin
    ├─▶ Wait for worker to connect sockets
    ├─▶ Wait for Ready message (with timeout)
    ├─▶ Populate PermitPool with slot writers
    ├─▶ Spawn event loop task
    └─▶ Return OrchestratorReady {pool, schema, handle}
```

Event loop handles:
- `ControlResponse::Idle` - Slot ready for next prediction
- `ControlResponse::Failed` - Slot poisoned, mark unavailable  
- `SlotResponse::Log/Output/Done/Failed` - Route to prediction
- Worker crash - Fail all in-flight predictions

### Worker (`worker.rs`)

Child-side event loop. Implements `PredictHandler` trait.

```
run_worker(handler, config)
    │
    ├─▶ Connect to slot sockets (from env)
    ├─▶ Setup control channel (stdin/stdout)
    ├─▶ Run handler.setup() with log routing
    ├─▶ Send Ready {slots, schema}
    ├─▶ Enter event loop:
    │       - ControlRequest::Cancel → handler.cancel(slot)
    │       - ControlRequest::Shutdown → exit
    │       - SlotRequest::Predict → spawn prediction task
    └─▶ Exit on shutdown or all slots poisoned
```

### PermitPool (`permit/pool.rs`)

Slot-based concurrency control.

```rust
let pool = PermitPool::new(max_concurrency);

// Add slot with its socket writer
pool.add_permit(slot_id, writer);

// Acquire permit (returns None if at capacity)
let permit = pool.try_acquire()?;

// Send prediction request
permit.send(SlotRequest::Predict { id, input }).await?;

// Return permit when done
drop(permit);
```

### Bridge Protocol (`bridge/protocol.rs`)

Message types for parent-worker communication.

**Control Channel:**
- `ControlRequest`: Init, Cancel, Shutdown
- `ControlResponse`: Ready, Log, Idle, Failed, Cancelled, ShuttingDown

**Slot Channel:**
- `SlotRequest`: Predict
- `SlotResponse`: Log, Output, Done, Failed, Cancelled

All messages are JSON with `{"type": "..."}` discriminator.

## Behaviors

### Health States

```
Unknown ──▶ Starting ──┬──▶ Ready ◀──▶ Busy
                       │
                       └──▶ SetupFailed ──▶ Defunct
```

- **Unknown**: Initial state, health-check returns status in body
- **Starting**: Setup in progress
- **Ready**: Accepting predictions
- **Busy**: Ready but all slots in use (HTTP 409 on new predictions)
- **SetupFailed**: setup() raised exception
- **Defunct**: Unrecoverable error

### Prediction States

```
Starting ──▶ Processing ──┬──▶ Succeeded
                          ├──▶ Failed
                          └──▶ Canceled
```

### Cancellation

1. HTTP DELETE /predictions/{id} or PUT /predictions/{id}/cancel
2. Parent sends `ControlRequest::Cancel { slot }`
3. Worker calls `handler.cancel(slot)`
4. For sync: SIGUSR1 raises KeyboardInterrupt
5. For async: `future.cancel()` on the asyncio task
6. Prediction returns with `SlotResponse::Cancelled`

### Shutdown

**Graceful (SIGTERM with await_explicit_shutdown):**
1. Stop accepting new predictions
2. Wait for in-flight to complete
3. Send `ControlRequest::Shutdown`
4. Worker responds `ShuttingDown`, exits
5. Parent exits

**Immediate (SIGTERM without flag):**
1. Send `ControlRequest::Shutdown`
2. Cancel in-flight predictions
3. Exit

**Worker crash:**
1. Control channel closes
2. Event loop detects, fails all in-flight predictions
3. Health → Defunct

### Slot Poisoning

If a slot socket has an error (write fails, etc.), the slot is marked poisoned.
It won't receive new predictions. If all slots are poisoned, worker exits.

```rust
enum SlotOutcome {
    Idle(SlotId),              // Ready for next prediction
    Poisoned { slot, error },  // Slot is dead
}
```
