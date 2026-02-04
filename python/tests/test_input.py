"""Tests for cog.input module (Input, FieldInfo)."""

from dataclasses import Field, MISSING

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

    def test_input_mutable_default_list_empty(self) -> None:
        # Empty list should become list factory
        result = Input(default=[])
        assert isinstance(result.default, Field)
        assert result.default.default_factory is list

    def test_input_mutable_default_dict_empty(self) -> None:
        # Empty dict should become dict factory
        result = Input(default={})
        assert isinstance(result.default, Field)
        assert result.default.default_factory is dict

    def test_input_mutable_default_list_populated(self) -> None:
        # Populated list should become deepcopy factory
        result = Input(default=[1, 2, 3])
        assert isinstance(result.default, Field)
        assert result.default.default_factory is not None
        # Factory should produce a copy
        value1 = result.default.default_factory()
        value2 = result.default.default_factory()
        assert value1 == [1, 2, 3]
        assert value1 is not value2  # Different instances

    def test_input_default_factory_explicit(self) -> None:
        result = Input(default_factory=list)
        assert isinstance(result.default, Field)
        assert result.default.default_factory is list

    def test_input_default_and_factory_mutually_exclusive(self) -> None:
        try:
            Input(default="value", default_factory=str)
            assert False, "Should have raised ValueError"
        except ValueError as e:
            assert "Cannot specify both" in str(e)

    def test_input_immutable_defaults_not_converted(self) -> None:
        # Immutable defaults should stay as-is
        for default in ["string", 42, 3.14, True, None, (1, 2), frozenset([1, 2])]:
            result = Input(default=default)
            assert result.default == default
            assert not isinstance(result.default, Field)


class TestFieldInfo:
    """Tests for FieldInfo dataclass."""

    def test_fieldinfo_is_frozen(self) -> None:
        info = FieldInfo(default="test")
        try:
            info.default = "new"  # type: ignore[misc]
            assert False, "Should have raised FrozenInstanceError"
        except Exception:
            pass  # Expected

    def test_fieldinfo_repr(self) -> None:
        info = FieldInfo(default=5, ge=0, le=10, description="A number")
        repr_str = repr(info)
        assert "FieldInfo" in repr_str
        assert "default=5" in repr_str
        assert "ge=0" in repr_str
        assert "le=10" in repr_str
        assert "description=" in repr_str

    def test_fieldinfo_repr_omits_none(self) -> None:
        info = FieldInfo(description="Just a description")
        repr_str = repr(info)
        assert "description=" in repr_str
        # None values should not appear
        assert "ge=" not in repr_str
        assert "le=" not in repr_str
