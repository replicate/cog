"""
Cog SDK BaseModel definition.

This module provides the BaseModel class that users can subclass to define
structured output types. BaseModel automatically converts subclasses into
dataclasses.
"""

from dataclasses import dataclass, is_dataclass


class BaseModel:
    """
    Base class for structured output types.

    Subclasses are automatically converted to dataclasses. This provides
    a clean way to define output schemas without explicit dataclass decorators.

    Example:
        from cog import BaseModel

        class Output(BaseModel):
            text: str
            confidence: float

        # Use as return type
        def predict(self, prompt: str) -> Output:
            return Output(text="hello", confidence=0.9)

    By default, auto_dataclass=True, which means all subclasses are
    automatically wrapped with @dataclass. You can disable this with
    auto_dataclass=False if you need manual control:

        class ManualModel(BaseModel, auto_dataclass=False):
            # You must apply @dataclass yourself or handle initialization
            pass
    """

    def __init_subclass__(
        cls,
        *,
        auto_dataclass: bool = True,
        init: bool = True,
        **kwargs: object,
    ) -> None:
        """
        Hook called when BaseModel is subclassed.

        This automatically wraps subclasses with @dataclass unless
        auto_dataclass=False is specified.

        Args:
            auto_dataclass: If True, automatically apply @dataclass to the class.
            init: If True (and auto_dataclass=True), generate __init__.
            **kwargs: Additional arguments passed to @dataclass.
        """
        # BaseModel is parented to `object` so we have nothing to pass up to it,
        # we pass the kwargs to dataclass() only.
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
                        f'Primary base class of "{cls.__name__}" '
                        f'("{cls.__bases__[0].__name__}") has auto_dataclass=True, '
                        f'but "{cls.__name__}" has auto_dataclass=False. '
                        "This creates broken field inheritance."
                    )
            except AttributeError:
                raise RuntimeError(
                    f'Primary base class of "{cls.__name__}" is a child of a child '
                    "of `BaseModel`, but `auto_dataclass` tracking does not exist. "
                    "This is likely a bug or other programming error."
                ) from None

        for base in cls.__bases__[1:]:
            if is_dataclass(base):
                raise TypeError(
                    f'Cannot mixin dataclass "{base.__name__}" while inheriting '
                    "from `BaseModel`"
                )

        # Once manual dataclass handling is enabled, we never apply the auto
        # dataclass logic again. It becomes the responsibility of the user to
        # ensure that all dataclass semantics are handled.
        if not auto_dataclass:
            cls.__auto_dataclass = False  # type: ignore[attr-defined]
            return

        # All children should be dataclass'd. This is the only way to ensure
        # that the dataclass inheritance is handled properly.
        dataclass(init=init, **kwargs)(cls)  # type: ignore[call-overload]
        cls.__auto_dataclass = True  # type: ignore[attr-defined]
