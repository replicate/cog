"""
Built-in coders for common types.

This module registers the default coders for dataclasses, JSON-like types,
and sets.
"""

from .dataclass import DataclassCoder
from .json import JsonCoder
from .set import SetCoder

__all__ = ["DataclassCoder", "JsonCoder", "SetCoder"]
