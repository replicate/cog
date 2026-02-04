"""Coder for JSON-like dict types."""

import typing
from typing import Any, Dict, Optional, Type

from ..coder import Coder


class JsonCoder(Coder):
    """Coder that handles Dict[str, Any] and dict subclasses."""

    @staticmethod
    def factory(tpe: Type[Any]) -> Optional[Coder]:
        origin = typing.get_origin(tpe)
        if origin in (dict, Dict):
            args = typing.get_args(tpe)
            if args and args[0] is str:
                return JsonCoder()

        try:
            if issubclass(tpe, dict):
                return JsonCoder()
        except TypeError:
            # Generic types like Set[Any] can't be used with issubclass
            pass

        return None

    def encode(self, x: Any) -> Dict[str, Any]:
        return x

    def decode(self, x: Dict[str, Any]) -> Any:
        return x
