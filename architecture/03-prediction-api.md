# Prediction API

The Prediction API is the HTTP interface for running model inference. It uses a fixed **envelope format** that wraps model-specific inputs and outputs, allowing a uniform API across all Cog models.

## Endpoints

| Endpoint | Method | Purpose |
|----------|--------|---------|
| `POST /predictions` | Create | Start a new prediction |
| `PUT /predictions/{id}` | Create (idempotent) | Start or retrieve existing prediction |
| `POST /predictions/{id}/cancel` | Cancel | Cancel a running prediction |
| `GET /health-check` | Health | Check server status |
| `GET /` | Index | List available endpoints |
| `GET /openapi.json` | Schema | OpenAPI specification |

By default, `POST /predictions` blocks until completion. For long-running predictions, use async mode with `Prefer: respond-async` header -- the response returns immediately with status `processing`, and progress updates are delivered via webhook.

## The Envelope Pattern

Every Cog model exposes the same endpoints with the same request/response structure. The model-specific parts (input fields, output type) are defined by the [Schema](./02-schema.md) and validated at runtime.

```mermaid
flowchart TB
    subgraph envelope ["Fixed Envelope (same for all models)"]
        direction TB
        fixed["id, status, created_at, logs, metrics, ..."]
        input["input#colon; { ... } — model-specific (from schema)"]
        output["output#colon; ... — model-specific (from schema)"]
    end
```

This pattern means:
- Clients use the same code to call any Cog model
- Platforms can route requests without understanding model internals
- Input validation is schema-driven, not hardcoded

## PredictionRequest

What clients send to start a prediction:

```json
{
  "id": "abc-123",
  "input": {
    "prompt": "A photo of a cat",
    "steps": 50
  },
  "webhook": "https://example.com/webhook",
  "webhook_events_filter": ["start", "output", "logs", "completed"]
}
```

| Field | Type | Purpose |
|-------|------|---------|
| `id` | string (optional) | Client-provided ID for idempotency |
| `input` | object | **Model-specific** -- validated against schema |
| `webhook` | URL (optional) | Where to send progress updates |
| `webhook_events_filter` | array (optional) | Which events to send |
| `created_at` | datetime (optional) | Client-provided timestamp |

The `input` object is validated against the `Input` schema generated from the predictor's `predict()` signature. Unknown fields are rejected; missing required fields raise validation errors.

## PredictionResponse

What comes back from the API:

```json
{
  "id": "abc-123",
  "status": "succeeded",
  "input": {
    "prompt": "A photo of a cat",
    "steps": 50
  },
  "output": "https://storage.example.com/output.png",
  "logs": "Loading model...\nGenerating image...\nDone.",
  "error": null,
  "metrics": {
    "predict_time": 4.52
  },
  "created_at": "2024-01-15T10:30:00Z",
  "started_at": "2024-01-15T10:30:01Z",
  "completed_at": "2024-01-15T10:30:05Z"
}
```

| Field | Type | Purpose |
|-------|------|---------|
| `id` | string | Prediction identifier |
| `status` | enum | `starting`, `processing`, `succeeded`, `canceled`, `failed` |
| `input` | object | Echo of the input (for reference) |
| `output` | any | **Model-specific** -- type defined by schema |
| `logs` | string | Captured stdout/stderr from predict() |
| `error` | string | Error message if status is `failed` |
| `metrics` | object | Timing and other metrics |
| `created_at` | datetime | When request was received |
| `started_at` | datetime | When prediction began |
| `completed_at` | datetime | When prediction finished |

## Status Lifecycle

```mermaid
stateDiagram-v2
    [*] --> starting: Request received
    starting --> processing: predict() called
    processing --> succeeded: predict() returns
    processing --> failed: predict() raises exception
    processing --> canceled: Cancel requested
    succeeded --> [*]
    failed --> [*]
    canceled --> [*]
```

State transitions on the `Prediction` struct fire webhooks as a side effect -- calling `set_processing()` sends the Start webhook, `set_succeeded()` sends the terminal Completed webhook.

## Connection Drop Handling

Synchronous predictions automatically cancel when the client connection drops. This prevents wasted computation on predictions where the client is no longer listening.

```rust
// SyncPredictionGuard is RAII -- drops when connection closes
let guard = handle.sync_guard();
let result = service.predict(slot, input).await;
// If connection drops here, guard.drop() cancels the prediction
```

Async predictions (via `Prefer: respond-async`) are unaffected -- they continue running regardless of client connection state, delivering results via webhook.

## Health States

The `/health-check` endpoint always returns HTTP 200 with the status in the JSON body. This allows load balancers and orchestrators to distinguish between "server is running but not ready" vs "server is down."

| State | JSON `status` | Condition |
|-------|---------------|-----------|
| `UNKNOWN` | `"UNKNOWN"` | Process just started, not yet serving |
| `STARTING` | `"STARTING"` | Worker subprocess initializing, running setup() |
| `READY` | `"READY"` | Worker ready, slots available |
| `BUSY` | `"BUSY"` | All slots occupied (backpressure) |
| `SETUP_FAILED` | `"SETUP_FAILED"` | `setup()` threw exception |
| `DEFUNCT` | `"DEFUNCT"` | Fatal error, worker crashed |
| `UNHEALTHY` | `"UNHEALTHY"` | User-defined healthcheck failed (transient) |

When all concurrency slots are occupied, new predictions receive `409 Conflict` instead of queuing. Clients should implement retry with backoff.

Prediction endpoints return 503 when health is not `READY`.

## Idempotent PUT

`PUT /predictions/{id}` is idempotent -- if the prediction already exists, it returns the current state. If not, it creates a new one. This is backed by a concurrent `DashMap`, so it's thread-safe without locks and safe under concurrent requests with the same ID.

## Concurrency Model

### Slot-Based Permits

The runtime uses explicit permit tokens for concurrency control:

```rust
// Acquire permit (blocks if all slots busy)
let permit = permit_pool.acquire().await?;

// Permit is held during prediction
let slot_id = permit.slot_id();
let result = orchestrator.predict(slot_id, input).await;

// Permit automatically returned on drop
drop(permit);
```

Advantages:
- Fixed, predictable concurrency
- Fair queuing (FIFO permit acquisition)
- Observable slot usage in metrics
- No task explosion

### Configuration

```yaml
# cog.yaml
concurrency:
  max: 5
```

This creates 5 slots in the PermitPool. Each slot corresponds to one Unix socket connection to the worker subprocess.

## Dynamic Payload Handling

### Input Validation Flow

```mermaid
flowchart LR
    subgraph request["Incoming Request"]
        json["JSON body"]
    end
    
    subgraph validation["Validation"]
        schema["Schema (Input type)"]
        validate["Schema Validation"]
    end
    
    subgraph transform["Transformation"]
        download["Download URLs → Files"]
        coerce["Type Coercion"]
    end
    
    subgraph predict["predict()"]
        kwargs["**kwargs"]
    end
    
    json --> validate
    schema --> validate
    validate --> download
    download --> coerce
    coerce --> kwargs
```

1. **Parse JSON** -- Extract `input` from request body
2. **Validate against schema** -- Coglet validates types, required fields, and constraints at the HTTP edge using the OpenAPI schema
3. **Download files** -- URLs in `cog.Path` fields are fetched to local temp files
4. **Coerce types** -- Strings become Paths, etc.
5. **Call predict()** -- Validated input passed as `**kwargs`

### Output Handling Flow

```mermaid
flowchart LR
    subgraph predict["predict()"]
        result["Return value / yields"]
    end
    
    subgraph transform["Transformation"]
        upload["Upload files → URLs"]
        serialize["JSON serialization"]
    end
    
    subgraph response["Response"]
        output["output field"]
    end
    
    result --> upload
    upload --> serialize
    serialize --> output
```

1. **Capture output** -- Return value or yielded values from predict()
2. **Upload files** -- `cog.Path` outputs are uploaded, replaced with URLs
3. **Serialize** -- Convert to JSON-compatible format
4. **Return** -- Place in `output` field of response

### File Handling

Input files (cog.Path):
```
Client sends:    {"input": {"image": "https://example.com/photo.jpg"}}
Server downloads: /tmp/inputabc123.jpg
predict() sees:  image = Path("/tmp/inputabc123.jpg")
```

Output files (cog.Path):
```
predict() returns: Path("/tmp/output.png")
Server uploads:    https://storage.example.com/output-xyz.png
Client receives:   {"output": "https://storage.example.com/output-xyz.png"}
```

## Cancellation

Cancellation uses IPC messages with different strategies for sync vs async predictors:

```mermaid
flowchart TD
    parent["Parent#colon; ControlRequest#colon;#colon;Cancel { slot }"]
    parent --> worker["Worker#colon; handler.cancel(slot)"]

    subgraph sync ["Sync predictors"]
        direction TB
        s1["Set CANCEL_REQUESTED flag for slot"]
        s2["Send SIGUSR1 to self"]
        s3["Signal handler#colon; raise KeyboardInterrupt\n(if in cancelable region)"]
    end

    subgraph async_p ["Async predictors"]
        direction TB
        a1["Get future from slot state"]
        a2["future.cancel() → Python raises\nasyncio.CancelledError"]
    end

    worker --> sync
    worker --> async_p
```

## Webhooks

For async predictions, progress is delivered via webhooks:

```mermaid
sequenceDiagram
    participant Client
    participant Cog
    participant Webhook
    
    Client->>Cog: POST /predictions (Prefer: respond-async)
    Cog-->>Client: 202 {status: "starting"}
    
    Cog->>Webhook: {status: "starting"}
    Note over Cog: predict() starts
    Cog->>Webhook: {status: "processing"}
    
    loop Output yields
        Cog->>Webhook: {output: "partial...", logs: "..."}
    end
    
    Cog->>Webhook: {status: "succeeded", output: "final"}
```

### Webhook Events

| Event | When | Payload Contains |
|-------|------|------------------|
| `start` | Prediction begins | `status: starting` |
| `output` | Each yield from iterator | Partial `output` |
| `logs` | Log lines captured | Updated `logs` |
| `completed` | Prediction finishes | Final `status`, `output`, `metrics` |

Filter events with `webhook_events_filter`:
```json
{
  "input": {...},
  "webhook": "https://...",
  "webhook_events_filter": ["completed"]
}
```

Webhook delivery includes structured retry with exponential backoff and automatic OpenTelemetry trace context propagation in headers.

## Streaming Output

For models that yield output progressively:

```python
def predict(self, prompt: str) -> Iterator[str]:
    for token in generate(prompt):
        yield token
```

The API can deliver these as:

1. **Webhooks** -- Each yield triggers an `output` webhook
2. **Server-Sent Events** -- Stream via `Accept: text/event-stream`
3. **Final array** -- Sync response collects all yields into `output: ["a", "b", "c"]`

## Where to Look

- `crates/coglet/src/transport/http/` -- HTTP route handlers, request parsing, response construction
- `crates/coglet/src/prediction.rs` -- the `Prediction` state machine (status transitions, webhook firing)
- `crates/coglet/src/webhook.rs` -- webhook delivery, retry logic, trace context propagation
- `crates/coglet/src/bridge/` -- IPC protocol definitions and transport
- `crates/coglet/src/permit/` -- slot-based concurrency control
