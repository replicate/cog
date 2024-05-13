# ruff: noqa: F401
import pydantic

if pydantic.__version__.startswith("1."):
    PYDANTIC_V2 = False
    from .types_v1 import ConcatenateIterator, File, Input, Path, Secret, URLPath
else:
    PYDANTIC_V2 = True
    from .types_v2 import ConcatenateIterator, File, Input, Path, Secret, URLPath
