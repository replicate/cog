"""Public exception classes raised by the Cog runtime.

These exceptions may be encountered in user predict/train code and are
intentionally part of the public SDK surface.
"""

from coglet import CancelationException as CancelationException

__all__ = ["CancelationException"]
