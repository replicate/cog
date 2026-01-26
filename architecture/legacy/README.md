# Legacy Python Runtime (FastAPI)

This directory documents the original Cog runtime implementation using Python's FastAPI/Uvicorn HTTP server.

## Status

This is the **current default** runtime implementation. It uses a two-process architecture with:
- Parent process: FastAPI/Uvicorn HTTP server
- Child process: User predictor code in isolated subprocess
- IPC: Python `multiprocessing.Pipe` with pickled events

## When to Use

This implementation is used by default when running Cog containers unless the `USE_COGLET` environment variable is set.

## Documentation

- [Prediction API](./03-prediction-api.md) - HTTP endpoints and request/response format
- [Container Runtime](./04-container-runtime.md) - Two-process architecture and execution flow

## Implementation

Primary code location: `python/cog/server/`
- `http.py` - FastAPI application and endpoints
- `worker.py` - Worker process management
- `runner.py` - Prediction orchestration
- `webhook.py` - Webhook delivery
- `stream_redirector.py` - Output capture
