"""Opaque marker for Cog input and output annotations.

Use inside ``typing.Annotated[T, Opaque]`` to tell Cog to treat ``T`` as an
opaque JSON object. Cog emits an object schema and passes values through
without custom encoding or decoding.
"""


class _OpaqueMarker:
    """Marker singleton exposed publicly as ``cog.Opaque``."""

    def __repr__(self) -> str:
        return "cog.Opaque"


Opaque = _OpaqueMarker()
