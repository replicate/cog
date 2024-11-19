import os
from functools import wraps
from typing import Any, Callable, Optional, TypeVar, Union, get_args, get_origin

R = TypeVar("R")


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
                    get_origin(expected_type) is Optional
                    or get_origin(expected_type) is Union
                ):
                    expected_type = get_args(expected_type)[0]
                return expected_type(result)
            result = func(*args, **kwargs)
            return result

        return wrapper

    return decorator
