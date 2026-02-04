"""Coder for set types."""

import typing
from typing import Any, Dict, Optional, Set, Type

from ..coder import Coder


class SetCoder(Coder):
    """Coder that handles Set types by converting to/from lists."""

    @staticmethod
    def factory(tpe: Type[Any]) -> Optional[Coder]:
        origin = typing.get_origin(tpe)
        if origin in (set, Set):
            return SetCoder()
        return None

    def encode(self, x: Any) -> Dict[str, Any]:
        return {"items": list(x)}

    def decode(self, x: Dict[str, Any]) -> Any:
        return set(x["items"])
