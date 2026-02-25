"""Tests for cog.input module (Input, FieldInfo)."""

import pytest

from cog import Input
from cog.input import FieldInfo


class TestInput:
    """Tests for Input() function."""

    def test_input_returns_fieldinfo(self) -> None:
        result = Input(description="Test input")
        assert isinstance(result, FieldInfo)

    def test_input_with_default(self) -> None:
        result = Input(default="hello", description="A string")
        assert result.default == "hello"
        assert result.description == "A string"

    def test_input_with_numeric_constraints(self) -> None:
        result = Input(default=5, ge=0, le=10)
        assert result.default == 5
        assert result.ge == 0
        assert result.le == 10

    def test_input_with_string_constraints(self) -> None:
        result = Input(min_length=1, max_length=100, regex=r"^\w+$")
        assert result.min_length == 1
        assert result.max_length == 100
        assert result.regex == r"^\w+$"

    def test_input_with_choices(self) -> None:
        result = Input(default="a", choices=["a", "b", "c"])
        assert result.default == "a"
        assert result.choices == ["a", "b", "c"]

    def test_input_with_deprecated(self) -> None:
        result = Input(deprecated=True)
        assert result.deprecated is True

    def test_input_default_factory_raises_error(self) -> None:
        with pytest.raises(TypeError, match="default_factory is not supported"):
            Input(default_factory=list)

    def test_input_immutable_defaults_stored_directly(self) -> None:
        for default in ["string", 42, 3.14, True, None, (1, 2), frozenset([1, 2])]:
            result = Input(default=default)
            assert result.default == default

    def test_input_no_default(self) -> None:
        # No default means the parameter is required
        result = Input(description="Required input")
        assert result.default is None
        assert result.description == "Required input"


class TestFieldInfo:
    """Tests for FieldInfo dataclass."""

    def test_fieldinfo_is_frozen(self) -> None:
        info = FieldInfo(default="test")
        with pytest.raises(Exception):
            info.default = "new"  # type: ignore[misc]

    def test_fieldinfo_defaults(self) -> None:
        info = FieldInfo(default=5, ge=0, le=10, description="A number")
        assert info.default == 5
        assert info.ge == 0
        assert info.le == 10
        assert info.description == "A number"

    def test_fieldinfo_none_defaults(self) -> None:
        info = FieldInfo(description="Just a description")
        assert info.default is None
        assert info.ge is None
        assert info.le is None
