import pydantic

if pydantic.__version__.startswith("1."):
    from .types_v1 import *
else:
    from .types_v2 import *
