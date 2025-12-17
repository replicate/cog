import inspect
import pathlib
from typing import Any, Type

import pydantic
from pydantic import BaseModel

from coglet import api


def patch_path() -> None:
    # We do not support pydantic v1
    if pydantic.__version__.startswith('1.'):
        return

    # Custom serialization for output path handling
    def _display(self) -> str:
        return self.absolute().as_uri()

    setattr(api.Path, '_display', _display)

    # Add pydantic class methods
    from pydantic_core import CoreSchema

    def __get_pydantic_core_schema__(
        cls,
        source: Type[Any],
        handler: pydantic.GetCoreSchemaHandler,
    ) -> CoreSchema:
        from pydantic_core import core_schema

        return core_schema.is_instance_schema(pathlib.Path)

    setattr(
        api.Path,
        '__get_pydantic_core_schema__',
        classmethod(__get_pydantic_core_schema__),
    )


patch_path()


class BaseModelCoder(api.Coder):
    @staticmethod
    def factory(cls: Type):
        try:
            if cls is not BaseModel and any(
                c is BaseModel for c in inspect.getmro(cls)
            ):
                return BaseModelCoder(cls)
        except (AttributeError, TypeError):
            # Generic types like Set[Any] don't have __mro__ in newer Python versions
            pass
        return None

    def __init__(self, cls: Type[BaseModel]):
        self.cls = cls

    def encode(self, x: BaseModel) -> dict[str, Any]:
        return x.model_dump(exclude_unset=True)

    def decode(self, x: dict[str, Any]) -> BaseModel:
        return self.cls.model_construct(**x)
