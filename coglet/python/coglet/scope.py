import contextvars
import sys
from collections import defaultdict
from typing import Any, Callable, Dict, Optional

ctx_pid: contextvars.ContextVar[Optional[str]] = contextvars.ContextVar(
    'pid', default=None
)
metrics: Dict[str, Dict[str, Any]] = defaultdict(dict)
contexts: Dict[str, Dict[str, Any]] = defaultdict(dict)
ctx_write_buf: Dict[str, str] = {}


class Scope:
    def __init__(self, pid: str):
        self.pid = pid

    def record_metric(self, name: str, value: Any) -> None:
        metrics[self.pid][name] = value

    @property
    def context(self) -> Dict[str, Any]:
        return contexts[self.pid]

    @context.setter
    def context(self, value: Dict[str, Any]) -> None:
        contexts[self.pid] = value


# Compat: for internal model metrics
# https://github.com/replicate/cog/blob/main/python/cog/server/scope.py
def current_scope() -> Scope:
    pid = ctx_pid.get()
    assert pid is not None
    return Scope(pid)


def flush_ctx_write_buf(pid: str, write_fn=None) -> None:
    """Flush any remaining buffered output for a prediction ID"""
    if pid in ctx_write_buf and ctx_write_buf[pid]:
        remaining = ctx_write_buf[pid]
        if write_fn is None:
            write_fn = sys.stdout.write
        write_fn(remaining)
        del ctx_write_buf[pid]


def flush_all_buffers(write_fn=None) -> None:
    """Flush all remaining buffered output"""
    if write_fn is None:
        write_fn = sys.stdout.write

    for pid in list(
        ctx_write_buf.keys()
    ):  # Copy keys to avoid modification during iteration
        if ctx_write_buf[pid]:
            write_fn(ctx_write_buf[pid])
        del ctx_write_buf[pid]


def cleanup_prediction_context(pid: str) -> None:
    """Clean up all context for a prediction, flushing any remaining output"""
    # Flush any remaining buffered output first
    flush_ctx_write_buf(pid)

    # Clean up other prediction context
    metrics.pop(pid, None)
    contexts.pop(pid, None)


def ctx_write(write_fn) -> Callable[[str], int]:
    def _write(s: str) -> int:
        if len(s) == 0:
            return 0
        pid = ctx_pid.get()
        prefix = f'[pid={pid}] ' if pid is not None else ''
        if pid is None:
            pid = 'logger'

        # Large input, bypass buffer and write truncated line directly
        if len(s) > 16384:
            return write_fn(prefix + s[:16384] + ' ... truncated\n')

        n = 0
        # s = s.replace('\r', '\n')
        if s[-1] in {'\n', '\r'}:
            # Input ends with new line, flush buffer and input
            b = ctx_write_buf.pop(pid, '')
            lines = s.splitlines()
            n += write_fn(b + lines[0] + '\n')
            for line in lines[1:]:
                n += write_fn(prefix + line + '\n')
            ctx_write_buf[pid] = prefix
        elif '\n' in s or '\r' in s:
            # Input contains new line but does not end
            b = ctx_write_buf.pop(pid, '')
            lines = s.splitlines()
            n += write_fn(b + lines[0] + '\n')
            # Flush all but last partial line
            for line in lines[1:-1]:
                n += write_fn(prefix + line + '\n')
            ctx_write_buf[pid] = prefix + lines[-1]
        else:
            # No new line, append to buffer
            if pid not in ctx_write_buf:
                ctx_write_buf[pid] = prefix + s
            else:
                ctx_write_buf[pid] += s
        return n

    return _write
