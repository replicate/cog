"""Tests for cog.model module (BaseModel)."""

from dataclasses import is_dataclass
from typing import Optional

from cog import BaseModel


class TestBaseModel:
    """Tests for BaseModel auto-dataclass behavior."""

    def test_subclass_becomes_dataclass(self) -> None:
        class Output(BaseModel):
            text: str
            score: float

        assert is_dataclass(Output)

    def test_subclass_can_be_instantiated(self) -> None:
        class Output(BaseModel):
            text: str
            score: float

        output = Output(text="hello", score=0.9)
        assert output.text == "hello"
        assert output.score == 0.9

    def test_subclass_with_defaults(self) -> None:
        class Output(BaseModel):
            text: str
            score: float = 0.5

        output = Output(text="hello")
        assert output.text == "hello"
        assert output.score == 0.5

    def test_subclass_with_optional(self) -> None:
        class Output(BaseModel):
            text: str
            metadata: Optional[str] = None

        output = Output(text="hello")
        assert output.text == "hello"
        assert output.metadata is None

    def test_nested_models(self) -> None:
        class Inner(BaseModel):
            value: int

        class Outer(BaseModel):
            inner: Inner
            name: str

        inner = Inner(value=42)
        outer = Outer(inner=inner, name="test")
        assert outer.inner.value == 42
        assert outer.name == "test"

    def test_inheritance(self) -> None:
        class Base(BaseModel):
            x: int

        class Derived(Base):
            y: str

        derived = Derived(x=1, y="two")
        assert derived.x == 1
        assert derived.y == "two"

    def test_auto_dataclass_false(self) -> None:
        class Manual(BaseModel, auto_dataclass=False):
            x: int

            def __init__(self, x: int) -> None:
                self.x = x

        # Should not be auto-dataclassed
        assert not is_dataclass(Manual)

        # But should still be usable
        m = Manual(x=5)
        assert m.x == 5

    def test_primary_base_must_be_basemodel(self) -> None:
        class NotBaseModel:
            pass

        try:

            class Bad(NotBaseModel, BaseModel):  # type: ignore[misc]
                x: int

            assert False, "Should have raised TypeError"
        except TypeError as e:
            assert "must inherit from BaseModel" in str(e)

    def test_cannot_mixin_dataclass(self) -> None:
        from dataclasses import dataclass

        @dataclass
        class SomeDataclass:
            y: int

        try:

            class Bad(BaseModel, SomeDataclass):  # type: ignore[misc]
                x: int

            assert False, "Should have raised TypeError"
        except TypeError as e:
            assert "Cannot mixin dataclass" in str(e)

    def test_auto_dataclass_inheritance_mismatch(self) -> None:
        class Parent(BaseModel):
            x: int

        try:

            class Child(Parent, auto_dataclass=False):
                y: str

            assert False, "Should have raised ValueError"
        except ValueError as e:
            assert "auto_dataclass=True" in str(e)
            assert "auto_dataclass=False" in str(e)
