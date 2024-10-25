from pathlib import Path

import pydantic
from pydantic import BaseModel

from .types import PYDANTIC_V2, URLPath


# Base class for inputs, constructed dynamically in get_input_type().
# (This can't be a docstring or it gets passed through to the schema.)
class BaseInput(BaseModel):
    if PYDANTIC_V2:
        model_config = pydantic.ConfigDict(use_enum_values=True)  # type: ignore
    else:

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
                value.unlink(missing_ok=True)
