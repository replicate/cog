import typing
from typing import Any, Optional, Set, Type

from coglet import api


class SetCoder(api.Coder):
    @staticmethod
    def factory(cls: Type) -> Optional[api.Coder]:
        origin = typing.get_origin(cls)
        if origin in (set, Set):
            return SetCoder()
        else:
            return None

    def encode(self, x: Any) -> dict[str, Any]:
        return {'items': list(x)}

    def decode(self, x: dict[str, Any]) -> Any:
        return set(x['items'])
