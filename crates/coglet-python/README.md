# coglet-python

PyO3 bindings that bridge the Rust coglet library to Python. This crate implements
the `PredictHandler` trait by wrapping Python predictor classes.

## Overview

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                            coglet-python                                     │
│                                                                              │
│  ┌──────────────────────────────────────────────────────────────────────┐   │
│  │                           lib.rs                                     │   │
│  │  Python module: serve(), active(), _run_worker(), _is_cancelable()  │   │
│  └──────────────────────────────────────────────────────────────────────┘   │
│                                      │                                       │
│              ┌───────────────────────┼───────────────────────┐              │
│              ▼                       ▼                       ▼              │
│  ┌─────────────────────┐  ┌─────────────────────┐  ┌──────────────────┐    │
│  │   worker_bridge.rs  │  │    predictor.rs     │  │   log_writer.rs  │    │
│  │  PredictHandler     │  │  PythonPredictor    │  │  SlotLogWriter   │    │
│  │  impl for Python    │  │  load/setup/predict │  │  ContextVar      │    │
│  └─────────────────────┘  └─────────────────────┘  └──────────────────┘    │
│              │                       │                       │              │
│              │                       │                       │              │
│              ▼                       ▼                       ▼              │
│  ┌─────────────────────┐  ┌─────────────────────┐  ┌──────────────────┐    │
│  │      input.rs       │  │      output.rs      │  │     audit.rs     │    │
│  │  Pydantic/ADT       │  │  JSON serialization │  │  TeeWriter       │    │
│  │  input processing   │  │  make_encodeable    │  │  stream protect  │    │
│  └─────────────────────┘  └─────────────────────┘  └──────────────────┘    │
│                                                                              │
│  ┌──────────────────────────────────────────────────────────────────────┐   │
│  │                          cancel.rs                                   │   │
│  │  SIGUSR1 handling, CancelableGuard, KeyboardInterrupt injection     │   │
│  └──────────────────────────────────────────────────────────────────────┘   │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

## Directory Structure

```
coglet-python/
├── Cargo.toml
├── coglet.pyi              # Type stubs for Python IDE support
└── src/
    ├── lib.rs              # Python module definition, serve/active/_run_worker
    ├── predictor.rs        # PythonPredictor: wraps Python Predictor class
    ├── worker_bridge.rs    # PythonPredictHandler: implements PredictHandler
    ├── input.rs            # Input processing (Pydantic validation, ADT)
    ├── output.rs           # Output processing (make_encodeable, upload_files)
    ├── log_writer.rs       # SlotLogWriter, ContextVar routing, SetupLogSender
    ├── audit.rs            # Audit hook, TeeWriter for stream protection
    └── cancel.rs           # Cancellation: SIGUSR1, CancelableGuard
```

## Critical Concepts

### The `active()` Flag

```python
import coglet

if coglet.server.active:
    # Running inside worker subprocess
    # stdout/stderr are captured, print goes to slot routing
else:
    # Running in parent or standalone
    # Normal stdout/stderr behavior
```

Set to `True` at the start of `_run_worker()`. Used by user code and cog internals
to detect worker context.

### Single Async Event Loop

Async predictors run on a **single** Python asyncio event loop, created at worker
startup. All slots share this loop.

```
Worker Subprocess
┌─────────────────────────────────────────────────────────────┐
│  Tokio Runtime (Rust)                                       │
│  └─ run_worker event loop                                   │
│      └─ For each SlotRequest::Predict:                      │
│          └─ tokio::spawn prediction task                    │
│              └─ Python::attach (acquire GIL)                │
│                  └─ asyncio.run_coroutine_threadsafe()      │
│                      └─ Predictor.predict() coroutine       │
└─────────────────────────────────────────────────────────────┘

asyncio event loop (Python)
┌─────────────────────────────────────────────────────────────┐
│  Single event loop, started once at worker init             │
│  - concurrent.futures.Future per async prediction           │
│  - ContextVar propagates prediction_id to spawned tasks     │
│  - Cancellation via future.cancel()                         │
└─────────────────────────────────────────────────────────────┘
```

**Why single loop?** 
- Python asyncio has one event loop per thread
- We use `run_coroutine_threadsafe` to submit from Rust/Tokio
- Multiple slots can have concurrent predictions (up to `max_concurrency`)

### Prediction Execution

**Sync Predictors:**
```
SlotRequest::Predict arrives
    │
    ├─▶ Python::attach (acquire GIL)
    ├─▶ set_sync_prediction_id(id)        # For log routing
    ├─▶ predictor.predict(input)          # Blocking call
    ├─▶ set_sync_prediction_id(None)
    └─▶ Return PredictResult
```

**Async Predictors:**
```
SlotRequest::Predict arrives
    │
    ├─▶ Python::attach (acquire GIL)
    ├─▶ Create wrapped coroutine:
    │       async def _ctx_wrapper(coro, prediction_id, contextvar):
    │           contextvar.set(prediction_id)  # Set in this task's context
    │           return await coro
    │
    ├─▶ asyncio.run_coroutine_threadsafe(wrapper, loop)
    ├─▶ py.detach() (release GIL)
    ├─▶ future.result() (block Rust task, Python runs)
    └─▶ Return PredictResult
```

### STDOUT/STDERR Routing

All output from user code must be captured and routed through the slot socket.

**Architecture:**
```
sys.stdout = SlotLogWriter(stdout)
sys.stderr = SlotLogWriter(stderr)

SlotLogWriter.write(data)
    │
    ├─▶ Get current prediction_id from:
    │       1. SYNC_PREDICTION_ID static (for sync predictors)
    │       2. ContextVar (for async predictors/spawned tasks)
    │
    ├─▶ Look up SlotSender in PREDICTION_REGISTRY
    │
    └─▶ Route:
            Found sender → slot_sender.send_log(source, data)
            No sender → Check setup sender (during setup)
            Neither → Log as orphan to stderr
```

**Line Buffering:**
SlotLogWriter buffers writes until a newline. This coalesces Python's `print()`
which does separate writes for content and `\n`.

### Audit Hook Protection

User code might replace `sys.stdout`:
```python
sys.stdout = open("mylog.txt", "w")
```

We can't prevent this, but we can intercept it with a Python audit hook.

**Strategy: TeeWriter**
```
User replaces sys.stdout
    │
    ├─▶ Audit hook fires on object.__setattr__(sys, "stdout", value)
    │
    ├─▶ Check: is value already SlotLogWriter? → Allow (it's us)
    │
    ├─▶ Check: is value already TeeWriter? → Allow (already wrapped)
    │
    ├─▶ Create TeeWriter(inner=SlotLogWriter, user_stream=value)
    │
    └─▶ Schedule: sys.stdout = tee (via Timer to avoid recursion)

TeeWriter.write(data)
    │
    ├─▶ inner.write(data)      # Our SlotLogWriter (routing works)
    └─▶ user_stream.write(data) # User's stream (their code works)
```

**Result:** Both our log routing AND the user's stream receive the data.

### Cancellation

**Sync Predictors:**
```
Parent: ControlRequest::Cancel { slot }
    │
    ├─▶ Worker: handler.cancel(slot)
    │       └─▶ Set CANCEL_REQUESTED flag for slot
    │
    ├─▶ Worker: send SIGUSR1 to self
    │
    └─▶ Signal handler: raise KeyboardInterrupt (if in cancelable region)

Prediction code:
    with CancelableGuard():  # Sets CANCELABLE=true
        predictor.predict()  # Can be interrupted
    # CANCELABLE=false on exit
```

**Async Predictors:**
```
Parent: ControlRequest::Cancel { slot }
    │
    └─▶ Worker: handler.cancel(slot)
            │
            ├─▶ Get future from slot state
            └─▶ future.cancel()
                    │
                    └─▶ Python raises asyncio.CancelledError
```

### Setup Log Routing

During setup (before any prediction), logs go through the control channel:

```
worker_bridge.setup()
    │
    ├─▶ register_setup_sender(tx)  # Control channel sender
    │
    ├─▶ predictor.load() + predictor.setup()
    │       │
    │       └─▶ print("Loading model...")
    │               │
    │               └─▶ SlotLogWriter.write()
    │                       │
    │                       ├─▶ No prediction_id (not in prediction)
    │                       └─▶ get_setup_sender() → ControlResponse::Log
    │
    └─▶ unregister_setup_sender()
```

### Behaviors

**Worker Startup:**
1. `set_active()` - Mark as worker subprocess
2. `init_tracing()` - Configure logging (stderr, COG_LOG_LEVEL env)
3. `install_slot_log_writers()` - Replace sys.stdout/stderr
4. `install_audit_hook()` - Protect streams
5. `install_signal_handler()` - SIGUSR1 for cancellation
6. Read Init message from stdin
7. Connect to slot sockets
8. `handler.setup()` - Load and initialize predictor
9. Send Ready message
10. Enter event loop

**Shutdown:**
- ControlRequest::Shutdown → Send ShuttingDown, exit
- stdin closes (parent died) → Exit immediately
- All slots poisoned → Exit

**Error Handling:**
- SetupError::Load - Failed to import/instantiate predictor
- SetupError::Setup - setup() raised exception
- PredictionError - Prediction failed, slot stays healthy
- Slot write error → Slot poisoned (no more predictions on that slot)
