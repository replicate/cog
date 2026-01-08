import typing
from typing import Any, Dict, Optional, Type

from coglet import api


class JsonCoder(api.Coder):
    @staticmethod
    def factory(cls: Type) -> Optional[api.Coder]:
        origin = typing.get_origin(cls)
        if (origin in (dict, Dict)) and typing.get_args(cls)[0] is str:
            return JsonCoder()

        try:
            if issubclass(cls, dict):
                return JsonCoder()
        except TypeError:
            # Generic types like Set[Any] can't be used with issubclass in newer Python
            pass

        return None

    def encode(self, x: Any) -> dict[str, Any]:
        return x

    def decode(self, x: dict[str, Any]) -> Any:
        return x
