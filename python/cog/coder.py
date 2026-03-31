"""
Cog SDK Coder system for custom type encoding/decoding.

This module provides the Coder base class for defining custom type
serialization between Python types and JSON.
"""

from abc import abstractmethod
from typing import Any, Dict, Optional, Set, Type


class Coder:
    """
    Base class for custom type encoders/decoders.

    Implement this to add support for custom types in predictor inputs/outputs.
    Register your coder with Coder.register() to make it available.

    Example:
        from cog import Coder
        from myapp import MyCustomType

        class MyCustomCoder(Coder):
            @staticmethod
            def factory(tpe: Type) -> Optional["MyCustomCoder"]:
                if tpe is MyCustomType:
                    return MyCustomCoder()
                return None

            def encode(self, value: MyCustomType) -> dict:
                return {"data": value.to_dict()}

            def decode(self, value: dict) -> MyCustomType:
                return MyCustomType.from_dict(value["data"])

        # Register the coder
        Coder.register(MyCustomCoder)
    """

    _coders: Set[Type["Coder"]] = set()

    @staticmethod
    def register(coder: Type["Coder"]) -> None:
        """
        Register a coder class for custom type handling.

        Args:
            coder: A Coder subclass to register.
        """
        Coder._coders.add(coder)

    @staticmethod
    def lookup(tpe: type | Any) -> Optional["Coder"]:
        """
        Find a coder that can handle the given type.

        Args:
            tpe: The type to find a coder for.

        Returns:
            A Coder instance if one is found, None otherwise.
        """
        for cls in Coder._coders:
            c = cls.factory(tpe)
            if c is not None:
                return c
        return None

    @staticmethod
    @abstractmethod
    def factory(tpe: Type[Any]) -> Optional["Coder"]:
        """
        Factory method to create a coder for a given type.

        Override this to check if your coder can handle the type and
        return an instance if so.

        Args:
            tpe: The type to potentially handle.

        Returns:
            A Coder instance if this coder can handle the type, None otherwise.
        """
        pass

    @abstractmethod
    def encode(self, x: Any) -> Dict[str, Any]:
        """
        Encode a value to a JSON-serializable dictionary.

        Args:
            x: The value to encode.

        Returns:
            A dictionary representation of the value.
        """
        pass

    @abstractmethod
    def decode(self, x: Dict[str, Any]) -> Any:
        """
        Decode a dictionary back to the original type.

        Args:
            x: The dictionary to decode.

        Returns:
            The decoded value.
        """
        pass
