"""Public exception classes raised by the Cog runtime.

These exceptions may be encountered in user predict/train code and are
intentionally part of the public SDK surface.
"""

try:
    from coglet import CancelationException as CancelationException
except ImportError:
    # When coglet is not installed (e.g. SDK-only usage outside a container),
    # provide a pure-Python definition so that user code can still import and
    # reference the exception for type annotations or catch clauses.

    class CancelationException(BaseException):  # type: ignore[no-redef]
        """Raised when a running prediction or training is cancelled.

        This is a ``BaseException`` subclass (not ``Exception``) so that
        bare ``except Exception`` blocks do not accidentally swallow
        cancellation signals.  This matches the precedent set by
        ``KeyboardInterrupt`` and ``asyncio.CancelledError``.

        You do **not** need to handle this exception in normal predictor
        code — the Cog runtime manages cancellation automatically.  If you
        need to run cleanup logic when a prediction is cancelled, catch it
        explicitly::

            from cog.exceptions import CancelationException

            try:
                result = long_running_inference()
            except CancelationException:
                cleanup_resources()
                raise  # always re-raise
        """


__all__ = ["CancelationException"]
