import cog
import json

try:
    import numpy as np  # type: ignore

    has_numpy = True
except ImportError:
    has_numpy = False


# https://pydantic-docs.helpmanual.io/usage/exporting_models/#json_encoders
JSON_ENCODERS = {
    cog.File: cog.File.encode,
    cog.Path: cog.Path.encode,
}

if has_numpy:
    JSON_ENCODERS.update(
        {
            np.integer: int,
            np.floating: float,
            np.ndarray: lambda obj: obj.tolist(),
        }
    )
