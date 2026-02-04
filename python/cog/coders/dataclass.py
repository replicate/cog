"""Coder for dataclass types."""

import dataclasses
from typing import Any, Dict, Optional, Type

from ..coder import Coder
from ..types import Path, Secret


class DataclassCoder(Coder):
    """Coder that handles dataclass encoding/decoding."""

    @staticmethod
    def factory(tpe: Type[Any]) -> Optional[Coder]:
        if dataclasses.is_dataclass(tpe):
            return DataclassCoder(tpe)
        return None

    def __init__(self, cls: Type[Any]) -> None:
        assert dataclasses.is_dataclass(cls)
        self.cls = cls

    def encode(self, x: Any) -> Dict[str, Any]:
        # Secret is a dataclass and dataclasses.asdict recursively converts
        # its internals, so we handle it specially
        return self._to_dict(self.cls, x)

    def _to_dict(self, cls: Type[Any], x: Any) -> Dict[str, Any]:
        r: Dict[str, Any] = {}
        for f in dataclasses.fields(cls):
            v = getattr(x, f.name)
            # Keep Path and Secret as is and let json.dumps(default=fn) handle them
            if f.type is Path:
                v = Path(v) if type(v) is str else v
            elif f.type is Secret:
                v = Secret(v) if type(v) is str else v
            elif dataclasses.is_dataclass(v):
                v = self._to_dict(f.type, v)  # type: ignore[arg-type]
            r[f.name] = v
        return r

    def decode(self, x: Dict[str, Any]) -> Any:
        kwargs = self._from_dict(self.cls, x)
        return self.cls(**kwargs)

    def _from_dict(self, cls: Type[Any], x: Dict[str, Any]) -> Dict[str, Any]:
        r: Dict[str, Any] = {}
        for f in dataclasses.fields(cls):
            if f.name not in x:
                continue
            elif f.type is Path:
                r[f.name] = Path(x[f.name]) if type(x[f.name]) is str else x[f.name]
            # Secret is a dataclass and must be handled before other dataclasses
            elif f.type is Secret:
                r[f.name] = Secret(x[f.name]) if type(x[f.name]) is str else x[f.name]
            elif dataclasses.is_dataclass(f.type):
                kwargs = self._from_dict(f.type, x[f.name])  # type: ignore[arg-type]
                r[f.name] = f.type(**kwargs)  # type: ignore[misc]
            else:
                r[f.name] = x[f.name]
        return r
