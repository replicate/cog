"""
Cog SDK Input definition.

This module provides the Input() function and FieldInfo class for defining
predictor input parameters with constraints and metadata.
"""

import copy
import sys
from dataclasses import MISSING, Field, dataclass, field
from enum import Enum
from typing import Any, Callable, List, Optional, Union


class Representation:
    """Base class for custom object representations."""

    def __repr__(self) -> str:
        """Generate a detailed string representation."""
        return f"{self.__class__.__name__}({self.__repr_str__(', ')})"

    def __repr_str__(self, join_str: str) -> str:
        """Generate representation string for attributes."""
        return join_str.join(
            f"{k}={v!r}" if v is not None else k for k, v in self.__repr_args__()
        )

    def __repr_args__(self) -> List[tuple[str, Any]]:
        """Generate arguments for representation. Override in subclasses."""
        return []


@dataclass(frozen=True, repr=False)
class FieldInfo(Representation):
    """
    Internal dataclass to hold Input metadata.

    This stores the constraints and metadata for a predictor input parameter.
    Users don't typically create this directly - use Input() instead.
    """

    default: Any = None
    description: Optional[str] = None
    ge: Optional[Union[int, float]] = None
    le: Optional[Union[int, float]] = None
    min_length: Optional[int] = None
    max_length: Optional[int] = None
    regex: Optional[str] = None
    choices: Optional[List[Union[str, int]]] = None
    deprecated: Optional[bool] = None

    def __repr_args__(self) -> List[tuple[str, Any]]:
        """Generate arguments for representation."""
        args: List[tuple[str, Any]] = []
        if self.default is not None:
            args.append(("default", self.default))
        if self.description is not None:
            args.append(("description", self.description))
        if self.ge is not None:
            args.append(("ge", self.ge))
        if self.le is not None:
            args.append(("le", self.le))
        if self.min_length is not None:
            args.append(("min_length", self.min_length))
        if self.max_length is not None:
            args.append(("max_length", self.max_length))
        if self.regex is not None:
            args.append(("regex", self.regex))
        if self.choices is not None:
            args.append(("choices", self.choices))
        if self.deprecated is not None:
            args.append(("deprecated", self.deprecated))
        return args


def Input(
    default: Any = None,
    *,
    default_factory: Optional[Callable[[], Any]] = None,
    description: Optional[str] = None,
    ge: Optional[Union[int, float]] = None,
    le: Optional[Union[int, float]] = None,
    min_length: Optional[int] = None,
    max_length: Optional[int] = None,
    regex: Optional[str] = None,
    choices: Optional[List[Union[str, int]]] = None,
    deprecated: Optional[bool] = None,
) -> Any:
    """
    Create an input field specification for a predictor parameter.

    Use this to add metadata and constraints to predictor inputs.

    Example:
        from cog import BasePredictor, Input

        class Predictor(BasePredictor):
            def predict(
                self,
                prompt: str = Input(description="The input prompt"),
                temperature: float = Input(default=0.7, ge=0.0, le=2.0),
                max_tokens: int = Input(default=100, ge=1, le=4096),
            ) -> str:
                ...

    Args:
        default: Default value for the field. For mutable defaults (lists, dicts),
            this is automatically converted to a default_factory.
        default_factory: A callable that returns the default value. Use this for
            mutable defaults. Cannot be used together with default.
        description: Human-readable description of the input.
        ge: Minimum value (greater than or equal) for numeric inputs.
        le: Maximum value (less than or equal) for numeric inputs.
        min_length: Minimum length for string inputs.
        max_length: Maximum length for string inputs.
        regex: Regular expression pattern for string inputs.
        choices: List of allowed values.
        deprecated: Whether the input is deprecated.

    Returns:
        A FieldInfo instance containing the field metadata.
    """
    # Import here to avoid circular imports
    from .types import Path, Secret

    # Validate that default and default_factory are mutually exclusive
    if default is not None and default_factory is not None:
        raise ValueError(
            "Cannot specify both 'default' and 'default_factory' parameters. "
            "Use either 'default' for immutable values or 'default_factory' "
            "for mutable values."
        )

    # Automatically convert mutable defaults to default_factory
    if default is not None:
        # Known immutable types that are safe to use as defaults
        immutable_types = (str, int, float, bool, type(None), tuple, frozenset, bytes)

        # Also allow Cog-specific types and enums
        if not isinstance(default, immutable_types) and not isinstance(
            default, (Path, Secret, Enum)
        ):
            # Automatically convert to default_factory
            if isinstance(default, list) and not default:
                default_factory = list
            elif isinstance(default, dict) and not default:
                default_factory = dict
            elif isinstance(default, set) and not default:
                default_factory = set
            else:
                # For populated collections or complex objects, use deepcopy
                # Capture the value before clearing default
                original_value = default

                def _create_default(val: Any = original_value) -> Any:
                    return copy.deepcopy(val)

                default_factory = _create_default

            # Clear the default since we're using factory instead
            default = None

    # If default_factory is provided, create a proper dataclass Field
    if default_factory is not None:
        # kw_only parameter was added in Python 3.10
        if sys.version_info >= (3, 10):
            computed_default: Any = field(
                default=MISSING,
                default_factory=default_factory,
                init=True,
                repr=True,
                hash=None,
                compare=True,
                metadata={},
                kw_only=False,
            )
        else:
            computed_default = field(
                default=MISSING,
                default_factory=default_factory,
                init=True,
                repr=True,
                hash=None,
                compare=True,
                metadata={},
            )
    else:
        computed_default = default

    return FieldInfo(
        default=computed_default,
        description=description,
        ge=ge,
        le=le,
        min_length=min_length,
        max_length=max_length,
        regex=regex,
        choices=choices,
        deprecated=deprecated,
    )
