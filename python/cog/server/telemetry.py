from contextlib import contextmanager
from contextvars import ContextVar
from typing import Generator, Optional

# TypedDict was added in 3.8
from typing_extensions import TypedDict


# See: https://www.w3.org/TR/trace-context/
class TraceContext(TypedDict, total=False):
    traceparent: str
    tracestate: str


TRACE_CONTEXT: ContextVar[Optional[TraceContext]] = ContextVar(
    "trace_context", default=None
)


def make_trace_context(
    traceparent: Optional[str] = None, tracestate: Optional[str] = None
) -> TraceContext:
    """
    Creates a trace context dictionary from the given traceparent and tracestate
    headers. This is used to pass the trace context between services.
    """
    ctx: TraceContext = {}
    if traceparent:
        ctx["traceparent"] = traceparent
    if tracestate:
        ctx["tracestate"] = tracestate
    return ctx


def current_trace_context() -> Optional[TraceContext]:
    """
    Returns the current trace context, this needs to be added via HTTP headers
    to all outgoing HTTP requests.
    """
    return TRACE_CONTEXT.get()


@contextmanager
def trace_context(ctx: TraceContext) -> Generator[None, None, None]:
    """
    A helper for managing the current trace context provided by the inbound
    HTTP request. This context is used to link requests across the system and
    needs to be added to all internal outgoing HTTP requests.
    """
    t = TRACE_CONTEXT.set(ctx)
    try:
        yield
    finally:
        TRACE_CONTEXT.reset(t)
