"""Type stubs for the coglet native module.

coglet is the Rust execution engine for cog models. It runs predictions
in a subprocess with IPC for crash isolation and memory isolation.
"""

from typing import Optional

# Module version
__version__: str

def active() -> bool:
    """Check if running inside a worker subprocess.

    Returns:
        True when running inside the worker subprocess (after coglet._run_worker()),
        False in the parent process (when calling coglet.serve()).
    """
    ...

def serve(
    predictor: Optional[str] = None,
    host: str = "0.0.0.0",
    port: int = 5000,
    await_explicit_shutdown: bool = False,
    is_train: bool = False,
) -> None:
    """Start the coglet HTTP server.

    Args:
        predictor: Path to predictor like "predict.py:Predictor".
                   If None, only health endpoints are served.
        host: Host to bind to.
        port: Port to listen on.
        await_explicit_shutdown: If True, ignore SIGTERM and wait for
                                  SIGINT or /shutdown endpoint.
        is_train: If True, call train() instead of predict().
    """
    ...

def _run_worker() -> None:
    """Internal: Run as a worker subprocess.

    Called by the orchestrator via:
        python -c "import coglet; coglet._run_worker()"

    Reads Init message from stdin, runs setup, then processes predictions.
    Do not call directly.
    """
    ...

def _is_cancelable() -> bool:
    """Internal: Check if we're in a cancelable section.

    Used by Python signal handlers for cancellation.
    """
    ...
