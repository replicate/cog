# ruff: noqa: F401
import pydantic

if pydantic.__version__.startswith("1."):
    from .types_v1 import ConcatenateIterator, File, Input, Path, Secret, URLPath
else:
    from .types_v2 import ConcatenateIterator, File, Input, Path, Secret, URLPath
