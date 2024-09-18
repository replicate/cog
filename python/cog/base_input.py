from pathlib import Path

from pydantic import BaseModel

from .types import (
    URLPath,
)


# Base class for inputs, constructed dynamically in get_input_type().
# (This can't be a docstring or it gets passed through to the schema.)
class BaseInput(BaseModel):
    class Config:
        # When using `choices`, the type is converted into an enum to validate
        # But, after validation, we want to pass the actual value to predict(), not the enum object
        use_enum_values = True

    def cleanup(self) -> None:
        """
        Cleanup any temporary files created by the input.
        """
        for _, value in self:
            # Handle URLPath objects specially for cleanup.
            # Also handle pathlib.Path objects, which cog.Path is a subclass of.
            # A pathlib.Path object shouldn't make its way here,
            # but both have an unlink() method, so we may as well be safe.
            if isinstance(value, (URLPath, Path)):
                # TODO: use unlink(missing_ok=...) when we drop Python 3.7 support.
                try:
                    value.unlink()
                except FileNotFoundError:
                    pass
