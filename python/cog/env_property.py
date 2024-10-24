import os
from functools import wraps
from typing import Any, Callable, Optional, TypeVar, Union

R = TypeVar("R")


def _get_origin(typ: Any) -> Any:
    if hasattr(typ, "__origin__"):
        return typ.__origin__
    return None


def _get_args(typ: Any) -> Any:
    if hasattr(typ, "__args__"):
        return typ.__args__
    return ()


def env_property(
    env_var: str,
) -> Callable[[Callable[[Any], R]], Callable[[Any], R]]:
    """Wraps a class property in an environment variable check."""

    def decorator(func: Callable[[Any], R]) -> Callable[[Any], R]:
        @wraps(func)
        def wrapper(*args: Any, **kwargs: Any) -> R:
            result = os.environ.get(env_var)
            if result is not None:
                expected_type = func.__annotations__.get("return", str)
                if (
                    _get_origin(expected_type) is Optional
                    or _get_origin(expected_type) is Union
                ):
                    expected_type = _get_args(expected_type)[0]
                return expected_type(result)
            result = func(*args, **kwargs)
            return result

        return wrapper

    return decorator
