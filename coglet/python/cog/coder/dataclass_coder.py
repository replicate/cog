import dataclasses
from typing import Any, Optional, Type

from coglet import api


class DataclassCoder(api.Coder):
    @staticmethod
    def factory(cls: Type) -> Optional[api.Coder]:
        if dataclasses.is_dataclass(cls):
            return DataclassCoder(cls)
        else:
            return None

    def __init__(self, cls: Type):
        assert dataclasses.is_dataclass(cls)
        self.cls = cls

    def encode(self, x: Any) -> dict[str, Any]:
        # Secret is a dataclass and dataclasses.asdict recursively converts its internals
        return self._to_dict(self.cls, x)

    def _to_dict(self, cls: Type, x: Any) -> dict[str, Any]:
        r: dict[str, Any] = {}
        for f in dataclasses.fields(cls):
            v = getattr(x, f.name)
            # Keep Path and Secret as is and let json.dumps(default=fn) handle them
            if f.type is api.Path:
                v = api.Path(v) if type(v) is str else v
            elif f.type is api.Secret:
                v = api.Secret(v) if type(v) is str else v
            elif dataclasses.is_dataclass(v):
                v = self._to_dict(f.type, v)  # type: ignore
            r[f.name] = v
        return r

    def decode(self, x: dict[str, Any]) -> Any:
        kwargs = self._from_dict(self.cls, x)
        return self.cls(**kwargs)  # type: ignore

    def _from_dict(self, cls: Type, x: dict[str, Any]) -> Any:
        r: dict[str, Any] = {}
        for f in dataclasses.fields(cls):
            if f.name not in x:
                continue
            elif f.type is api.Path:
                r[f.name] = api.Path(x[f.name]) if type(x[f.name]) is str else x[f.name]
            # Secret is a dataclass and must be handled before other dataclasses
            elif f.type is api.Secret:
                r[f.name] = (
                    api.Secret(x[f.name]) if type(x[f.name]) is str else x[f.name]
                )
            elif dataclasses.is_dataclass(f.type):
                kwargs = self._from_dict(f.type, x[f.name])  # type: ignore
                r[f.name] = f.type(**kwargs)  # type: ignore
            else:
                r[f.name] = x[f.name]
        return r
