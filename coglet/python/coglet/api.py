import copy
import pathlib
import sys
from abc import ABC, abstractmethod
from dataclasses import MISSING, Field, dataclass, is_dataclass
from enum import Enum
from typing import (
    Any,
    AsyncIterator,
    Callable,
    Generic,
    Iterator,
    List,
    Optional,
    Type,
    TypeVar,
    Union,
    overload,
)

from typing_extensions import ParamSpec

########################################
# Custom encoding
########################################


# Encoding between a custom type and JSON dict[str, Any]
class Coder:
    _coders: set = set()

    @staticmethod
    def register(coder) -> None:
        Coder._coders.add(coder)

    @staticmethod
    def lookup(tpe: Type) -> Optional[Any]:
        for cls in Coder._coders:
            c = cls.factory(tpe)
            if c is not None:
                return c
        return None

    @staticmethod
    @abstractmethod
    def factory(cls: Type) -> Optional[Any]:
        pass

    @abstractmethod
    def encode(self, x: Any) -> dict[str, Any]:
        pass

    @abstractmethod
    def decode(self, x: dict[str, Any]) -> Any:
        pass


########################################
# Data types
########################################


class CancelationException(Exception):
    pass


class Path(pathlib.PosixPath):
    pass


@dataclass(frozen=True)
class Secret:
    secret_value: Optional[str] = None

    def __repr__(self):
        return f'Secret({str(self)})'

    def __str__(self):
        return '**********' if self.secret_value is not None else ''

    def get_secret_value(self) -> Optional[str]:
        return self.secret_value


_T_co = TypeVar('_T_co', covariant=True)


class ConcatenateIterator(Iterator[_T_co]):
    @abstractmethod
    def __next__(self) -> _T_co: ...


class AsyncConcatenateIterator(AsyncIterator[_T_co]):
    @abstractmethod
    async def __anext__(self) -> _T_co: ...


########################################
# Input, Output
########################################


class Representation:
    """Base class for custom object representations, similar to Pydantic's approach."""

    def __repr__(self) -> str:
        """Generate a detailed string representation."""
        return f'{self.__class__.__name__}({self.__repr_str__(", ")})'

    def __repr_str__(self, join_str: str) -> str:
        """Generate representation string for attributes."""
        return join_str.join(
            f'{k}={v!r}' if v is not None else k for k, v in self.__repr_args__()
        )

    def __repr_args__(self):
        """Generate arguments for representation."""
        # Override in subclasses
        return []


@dataclass(frozen=True)
class FieldInfo(Representation):
    """Internal dataclass to hold Input metadata."""

    default: Any = None
    description: Optional[str] = None
    ge: Optional[Union[int, float]] = None
    le: Optional[Union[int, float]] = None
    min_length: Optional[int] = None
    max_length: Optional[int] = None
    regex: Optional[str] = None
    choices: Optional[List[Union[str, int]]] = None
    deprecated: Optional[bool] = None

    def __repr_args__(self):
        """Generate arguments for representation."""
        args = []
        if self.default is not None:
            args.append(('default', self.default))
        if self.description is not None:
            args.append(('description', self.description))
        if self.ge is not None:
            args.append(('ge', self.ge))
        if self.le is not None:
            args.append(('le', self.le))
        if self.min_length is not None:
            args.append(('min_length', self.min_length))
        if self.max_length is not None:
            args.append(('max_length', self.max_length))
        if self.regex is not None:
            args.append(('regex', self.regex))
        if self.choices is not None:
            args.append(('choices', self.choices))
        if self.deprecated is not None:
            args.append(('deprecated', self.deprecated))
        return args


# Type variable for preserving input types
_T = TypeVar('_T')


@overload
def Input() -> Any:
    """Create an input field with no constraints."""
    ...


@overload
def Input(
    *,
    description: Optional[str] = None,
    ge: Optional[Union[int, float]] = None,
    le: Optional[Union[int, float]] = None,
    min_length: Optional[int] = None,
    max_length: Optional[int] = None,
    regex: Optional[str] = None,
    choices: Optional[List[Union[str, int]]] = None,
    deprecated: Optional[bool] = None,
) -> Any:
    """Create an input field with keyword-only constraints."""
    ...


@overload
def Input(
    default: _T,
    *,
    description: Optional[str] = None,
    ge: Optional[Union[int, float]] = None,
    le: Optional[Union[int, float]] = None,
    min_length: Optional[int] = None,
    max_length: Optional[int] = None,
    regex: Optional[str] = None,
    choices: Optional[List[Union[str, int]]] = None,
    deprecated: Optional[bool] = None,
) -> _T:
    """Create an input field with default value and optional constraints."""
    ...


@overload
def Input(
    *,
    default_factory: Callable[[], Any],
    description: Optional[str] = None,
    ge: Optional[Union[int, float]] = None,
    le: Optional[Union[int, float]] = None,
    min_length: Optional[int] = None,
    max_length: Optional[int] = None,
    regex: Optional[str] = None,
    choices: Optional[List[Union[str, int]]] = None,
    deprecated: Optional[bool] = None,
) -> Any:
    """Create an input field with default_factory and optional constraints."""
    ...


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
    """Create an input field specification.

    For type checkers, this returns Any to allow usage on type-annotated fields.
    At runtime, returns an FieldInfo instance with the field metadata.

    Args:
        default: Default value for the field
        description: Human-readable description
        ge: Minimum value (greater than or equal)
        le: Maximum value (less than or equal)
        min_length: Minimum length for strings/lists
        max_length: Maximum length for strings/lists
        regex: Regular expression pattern for strings
        choices: List of allowed values
        deprecated: Whether the field is deprecated

    Returns:
        FieldInfo instance containing the field metadata
    """
    # Validate that default and default_factory are mutually exclusive
    if default is not None and default_factory is not None:
        raise ValueError(
            "Cannot specify both 'default' and 'default_factory' parameters. "
            "Use either 'default' for immutable values or 'default_factory' for mutable values."
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

                def _create_default():
                    return copy.deepcopy(original_value)

                default_factory = _create_default

            # Clear the default since we're using factory instead
            default = None

    # If default_factory is provided, create a proper dataclass Field
    if default_factory is not None:
        # kw_only parameter was added in Python 3.10
        if sys.version_info >= (3, 10):
            computed_default = Field(
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
            computed_default = Field(
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


class BaseModel:
    def __init_subclass__(
        cls, *, auto_dataclass: bool = True, init: bool = True, **kwargs
    ):
        # BaseModel is parented to `object` so we have nothing to pass up to it, we pass the kwargs to dataclass() only.
        super().__init_subclass__()

        # For sanity, the primary base class must inherit from BaseModel
        if not issubclass(cls.__bases__[0], BaseModel):
            raise TypeError(
                f'Primary base class of "{cls.__name__}" must inherit from BaseModel'
            )
        elif not auto_dataclass:
            try:
                if (
                    cls.__bases__[0] != BaseModel
                    and cls.__bases__[0].__auto_dataclass is True  # type: ignore[attr-defined]
                ):
                    raise ValueError(
                        f'Primary base class of "{cls.__name__}" ("{cls.__bases__[0].__name__}") has auto_dataclass=True, but "{cls.__name__}" has auto_dataclass=False. This creates broken field inheritance.'
                    )
            except AttributeError:
                raise RuntimeError(
                    f'Primary base class of "{cls.__name__}" is a child of a child of `BaseModel`, but `auto_dataclass` tracking does not exist. This is likely a bug or other programming error.'
                )

        for base in cls.__bases__[1:]:
            if is_dataclass(base):
                raise TypeError(
                    f'Cannot mixin dataclass "{base.__name__}" while inheriting from `BaseModel`'
                )

        # Once manual dataclass handling is enabled, we never apply the auto dataclass logic again,
        # it becomes the responsibility of the user to ensure that all dataclass semantics are handled.
        if not auto_dataclass:
            cls.__auto_dataclass = False  # type: ignore[attr-defined]
            return

        # all children should be dataclass'd, this is the only way to ensure that the dataclass inheritence
        # is handled properly.
        dataclass(init=init, **kwargs)(cls)
        cls.__auto_dataclass = True  # type: ignore[attr-defined]


########################################
# Predict
########################################


P = ParamSpec('P')
R = TypeVar('R')


class BasePredictor(ABC, Generic[P, R]):
    def setup(
        self,
        weights: Optional[Union[Path, str]] = None,
    ) -> None:
        return

    @abstractmethod
    def predict(self, *args: P.args, **kwargs: P.kwargs) -> R:
        raise NotImplementedError('predict has not been implemented by parent class.')


########################################
# Logging
########################################


# Compat: for current_scope warning
# https://github.com/replicate/cog/blob/main/python/cog/types.py#L41
class ExperimentalFeatureWarning(Warning):
    pass
