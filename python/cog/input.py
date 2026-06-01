"""
Cog SDK Input definition.

This module provides the Input() function and FieldInfo class for defining
predictor input parameters with constraints and metadata.
"""

from dataclasses import dataclass
from typing import Any, Callable, List, Optional, Union


@dataclass(frozen=True)
class FieldInfo:
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


def Input(
    default: Any = None,
    *,
    default_factory: Optional[Callable[..., Any]] = None,
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

    Example::

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
        default: Default value for the field. Must be an immutable literal value.
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
    if default_factory is not None:
        raise TypeError(
            "default_factory is not supported in Input(). "
            "Use a literal default value instead: Input(default=...). "
            "Mutable defaults like lists should use immutable alternatives "
            "(e.g. a comma-separated string) or be constructed in predict()."
        )

    return FieldInfo(
        default=default,
        description=description,
        ge=ge,
        le=le,
        min_length=min_length,
        max_length=max_length,
        regex=regex,
        choices=choices,
        deprecated=deprecated,
    )
