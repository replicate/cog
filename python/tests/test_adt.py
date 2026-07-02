"""Tests for cog._adt module (FieldType, PrimitiveType)."""

from typing import Annotated, Any, Dict, List, Optional, TypedDict

import pytest
from typing_extensions import TypedDict as ExtensionsTypedDict

from cog import Opaque
from cog._adt import FieldType, PrimitiveType, Repetition


class ExampleTypedDict(TypedDict):
    name: str
    count: int


class ExampleExtensionsTypedDict(ExtensionsTypedDict):
    name: str
    count: int


class ExampleDictSubclass(dict[str, int]):
    pass


class ThirdPartyObject:
    pass


def test_opaque_is_public_cog_export() -> None:
    cog_module = __import__("cog")

    assert repr(cog_module.Opaque) == "cog.Opaque"
    assert cog_module.Opaque is Opaque


def test_opaque_simple_field_type() -> None:
    ft = FieldType.from_type(Annotated[ThirdPartyObject, Opaque])
    assert ft.primitive is PrimitiveType.ANY
    assert ft.repetition is Repetition.REQUIRED
    assert ft.coder is None


def test_opaque_list_field_type() -> None:
    ft = FieldType.from_type(Annotated[List[ThirdPartyObject], Opaque])
    assert ft.primitive is PrimitiveType.ANY
    assert ft.repetition is Repetition.REPEATED
    assert ft.coder is None


def test_opaque_bare_list_field_type() -> None:
    ft = FieldType.from_type(Annotated[list, Opaque])
    assert ft.primitive is PrimitiveType.ANY
    assert ft.repetition is Repetition.REPEATED
    assert ft.coder is None


def test_opaque_inside_list_field_type() -> None:
    ft = FieldType.from_type(List[Annotated[ThirdPartyObject, Opaque]])
    assert ft.primitive is PrimitiveType.ANY
    assert ft.repetition is Repetition.REPEATED
    assert ft.coder is None


def test_opaque_optional_field_type() -> None:
    ft = FieldType.from_type(Annotated[ThirdPartyObject, Opaque] | None)
    assert ft.primitive is PrimitiveType.ANY
    assert ft.repetition is Repetition.OPTIONAL
    assert ft.coder is None


def test_opaque_optional_list_field_type() -> None:
    ft = FieldType.from_type(Annotated[List[ThirdPartyObject], Opaque] | None)
    assert ft.primitive is PrimitiveType.ANY
    assert ft.repetition is Repetition.OPTIONAL_REPEATED
    assert ft.coder is None


def test_opaque_optional_bare_list_field_type() -> None:
    ft = FieldType.from_type(Annotated[list, Opaque] | None)
    assert ft.primitive is PrimitiveType.ANY
    assert ft.repetition is Repetition.OPTIONAL_REPEATED
    assert ft.coder is None


class TestDictInputTypes:
    """Tests for FieldType.from_type() with dict types as inputs."""

    def test_bare_dict_input(self) -> None:
        """Bare dict should be accepted as an input type, mapped to ANY."""
        ft = FieldType.from_type(dict)
        assert ft.primitive is PrimitiveType.ANY
        assert ft.repetition is Repetition.REQUIRED
        assert ft.coder is None

    def test_dict_str_any_input(self) -> None:
        """Dict[str, Any] should be accepted as an input type."""
        ft = FieldType.from_type(Dict[str, Any])
        assert ft.primitive is PrimitiveType.ANY
        assert ft.repetition is Repetition.REQUIRED

    def test_list_of_dict_input(self) -> None:
        """list[dict] should produce a repeated ANY field."""
        ft = FieldType.from_type(list[dict])
        assert ft.primitive is PrimitiveType.ANY
        assert ft.repetition is Repetition.REPEATED
        assert ft.coder is None

    def test_list_of_typing_dict_input(self) -> None:
        """List[Dict[str, Any]] should produce a repeated ANY field."""
        ft = FieldType.from_type(List[Dict[str, Any]])
        assert ft.primitive is PrimitiveType.ANY
        assert ft.repetition is Repetition.REPEATED

    def test_optional_dict_input(self) -> None:
        """Optional[dict] should produce an optional ANY field."""
        ft = FieldType.from_type(Optional[dict])
        assert ft.primitive is PrimitiveType.ANY
        assert ft.repetition is Repetition.OPTIONAL
        assert ft.coder is None

    def test_optional_typing_dict_input(self) -> None:
        """Optional[Dict[str, Any]] should produce an optional ANY field."""
        ft = FieldType.from_type(Optional[Dict[str, Any]])
        assert ft.primitive is PrimitiveType.ANY
        assert ft.repetition is Repetition.OPTIONAL
        assert ft.coder is None

    def test_optional_list_of_dict_input(self) -> None:
        """Optional[list[dict]] should produce an optional repeated ANY field."""
        ft = FieldType.from_type(Optional[list[dict]])
        assert ft.primitive is PrimitiveType.ANY
        assert ft.repetition is Repetition.OPTIONAL_REPEATED

    def test_optional_list_of_typing_dict_input(self) -> None:
        """Optional[List[Dict[str, Any]]] should produce optional repeated ANY."""
        ft = FieldType.from_type(Optional[List[Dict[str, Any]]])
        assert ft.primitive is PrimitiveType.ANY
        assert ft.repetition is Repetition.OPTIONAL_REPEATED

    def test_pep604_dict_or_none(self) -> None:
        """dict | None (PEP 604) should produce optional ANY."""
        ft = FieldType.from_type(dict | None)
        assert ft.primitive is PrimitiveType.ANY
        assert ft.repetition is Repetition.OPTIONAL
        assert ft.coder is None

    def test_pep604_list_dict_or_none(self) -> None:
        """list[dict] | None (PEP 604) should produce optional repeated ANY."""
        ft = FieldType.from_type(list[dict] | None)
        assert ft.primitive is PrimitiveType.ANY
        assert ft.repetition is Repetition.OPTIONAL_REPEATED

    def test_dict_str_int_type_erasure(self) -> None:
        """dict[str, int] should be accepted as ANY (type params discarded)."""
        ft = FieldType.from_type(dict[str, int])
        assert ft.primitive is PrimitiveType.ANY
        assert ft.repetition is Repetition.REQUIRED

    def test_typeddict_input(self) -> None:
        """TypedDict subclasses should be accepted as dict-like inputs."""
        ft = FieldType.from_type(ExampleTypedDict)
        assert ft.primitive is PrimitiveType.ANY
        assert ft.repetition is Repetition.REQUIRED
        assert ft.coder is None

    def test_typing_extensions_typeddict_input(self) -> None:
        """typing_extensions.TypedDict should be accepted as dict-like inputs."""
        ft = FieldType.from_type(ExampleExtensionsTypedDict)
        assert ft.primitive is PrimitiveType.ANY
        assert ft.repetition is Repetition.REQUIRED
        assert ft.coder is None

    def test_list_of_typeddict_input(self) -> None:
        """list[TypedDict] should produce a repeated ANY field."""
        ft = FieldType.from_type(list[ExampleTypedDict])
        assert ft.primitive is PrimitiveType.ANY
        assert ft.repetition is Repetition.REPEATED
        assert ft.coder is None

    def test_list_of_typing_extensions_typeddict_input(self) -> None:
        """list[typing_extensions.TypedDict] should produce repeated ANY."""
        ft = FieldType.from_type(list[ExampleExtensionsTypedDict])
        assert ft.primitive is PrimitiveType.ANY
        assert ft.repetition is Repetition.REPEATED
        assert ft.coder is None

    def test_optional_typeddict_input(self) -> None:
        """Optional[TypedDict] should produce an optional ANY field."""
        ft = FieldType.from_type(Optional[ExampleTypedDict])
        assert ft.primitive is PrimitiveType.ANY
        assert ft.repetition is Repetition.OPTIONAL
        assert ft.coder is None

    def test_optional_typing_extensions_typeddict_input(self) -> None:
        """Optional[typing_extensions.TypedDict] should produce optional ANY."""
        ft = FieldType.from_type(Optional[ExampleExtensionsTypedDict])
        assert ft.primitive is PrimitiveType.ANY
        assert ft.repetition is Repetition.OPTIONAL
        assert ft.coder is None

    def test_optional_list_of_typeddict_input(self) -> None:
        """Optional[list[TypedDict]] should produce optional repeated ANY."""
        ft = FieldType.from_type(Optional[list[ExampleTypedDict]])
        assert ft.primitive is PrimitiveType.ANY
        assert ft.repetition is Repetition.OPTIONAL_REPEATED
        assert ft.coder is None

    def test_optional_list_of_typing_extensions_typeddict_input(self) -> None:
        """Optional[list[typing_extensions.TypedDict]] should be optional repeated ANY."""
        ft = FieldType.from_type(Optional[list[ExampleExtensionsTypedDict]])
        assert ft.primitive is PrimitiveType.ANY
        assert ft.repetition is Repetition.OPTIONAL_REPEATED
        assert ft.coder is None

    def test_pep604_list_of_typeddict_or_none_input(self) -> None:
        """list[TypedDict] | None should produce optional repeated ANY."""
        ft = FieldType.from_type(list[ExampleTypedDict] | None)
        assert ft.primitive is PrimitiveType.ANY
        assert ft.repetition is Repetition.OPTIONAL_REPEATED
        assert ft.coder is None

    def test_pep604_list_of_typing_extensions_typeddict_or_none_input(self) -> None:
        """list[typing_extensions.TypedDict] | None should be optional repeated ANY."""
        ft = FieldType.from_type(list[ExampleExtensionsTypedDict] | None)
        assert ft.primitive is PrimitiveType.ANY
        assert ft.repetition is Repetition.OPTIONAL_REPEATED
        assert ft.coder is None

    def test_plain_dict_subclass_is_not_treated_as_dict(self) -> None:
        """Plain dict subclasses should not be accepted as dict-like inputs."""
        try:
            FieldType.from_type(ExampleDictSubclass)
        except ValueError as exc:
            assert str(exc) == "unsupported Cog type ExampleDictSubclass"
        else:
            raise AssertionError("Expected ValueError for plain dict subclass")

    def test_list_dict_str_int_type_erasure(self) -> None:
        """list[dict[str, int]] should be accepted as repeated ANY."""
        ft = FieldType.from_type(list[dict[str, int]])
        assert ft.primitive is PrimitiveType.ANY
        assert ft.repetition is Repetition.REPEATED


class TestDictJsonSchema:
    """Tests for JSON schema generation with dict types."""

    def test_dict_json_type(self) -> None:
        """dict should generate {"type": "object"} schema."""
        ft = FieldType.from_type(dict)
        assert ft.json_type() == {"type": "object"}

    def test_list_of_dict_json_type(self) -> None:
        """list[dict] should generate array-of-objects schema."""
        ft = FieldType.from_type(list[dict])
        assert ft.json_type() == {"type": "array", "items": {"type": "object"}}


class TestDictNormalization:
    """Tests for normalize/encode/decode with dict types."""

    def test_dict_normalize_passthrough(self) -> None:
        """dict normalize should pass values through unchanged."""
        ft = FieldType.from_type(dict)
        data = {"key": "value", "nested": {"a": 1}}
        assert ft.normalize(data) == data

    def test_list_of_dict_normalize_passthrough(self) -> None:
        """list[dict] normalize should pass list elements through unchanged."""
        ft = FieldType.from_type(list[dict])
        data = [{"key": "value"}, {"a": 1}]
        assert ft.normalize(data) == data

    def test_optional_dict_normalize_none(self) -> None:
        """Optional[dict] should normalize None to None."""
        ft = FieldType.from_type(Optional[dict])
        assert ft.normalize(None) is None

    def test_optional_list_dict_normalize_none(self) -> None:
        """Optional[list[dict]] should normalize None to None."""
        ft = FieldType.from_type(Optional[list[dict]])
        assert ft.normalize(None) is None

    def test_dict_json_encode_passthrough(self) -> None:
        """dict json_encode should pass values through unchanged."""
        ft = FieldType.from_type(dict)
        data = {"key": "value", "nested": {"a": 1}}
        assert ft.json_encode(data) == data

    def test_list_of_dict_json_encode_passthrough(self) -> None:
        """list[dict] json_encode should pass list elements through unchanged."""
        ft = FieldType.from_type(list[dict])
        data = [{"key": "value"}, {"a": 1}]
        assert ft.json_encode(data) == data

    def test_dict_json_decode_passthrough(self) -> None:
        """dict json_decode should pass values through unchanged."""
        ft = FieldType.from_type(dict)
        data = {"key": "value"}
        assert ft.json_decode(data) == data


class TestFieldTypeExistingTypes:
    """Regression tests: ensure existing types still work after dict changes."""

    def test_bare_list(self) -> None:
        ft = FieldType.from_type(list)
        assert ft.primitive is PrimitiveType.ANY
        assert ft.repetition is Repetition.REPEATED

    def test_list_str(self) -> None:
        ft = FieldType.from_type(list[str])
        assert ft.primitive is PrimitiveType.STRING
        assert ft.repetition is Repetition.REPEATED

    def test_list_int(self) -> None:
        ft = FieldType.from_type(list[int])
        assert ft.primitive is PrimitiveType.INTEGER
        assert ft.repetition is Repetition.REPEATED

    def test_optional_str(self) -> None:
        ft = FieldType.from_type(Optional[str])
        assert ft.primitive is PrimitiveType.STRING
        assert ft.repetition is Repetition.OPTIONAL


class TestUnionInputTypes:
    def test_union_str_float_field_type(self) -> None:
        ft = FieldType.from_type(str | float)
        assert ft.primitive is PrimitiveType.ANY
        assert ft.repetition is Repetition.REQUIRED
        assert ft.union_variants is not None
        assert [v.primitive for v in ft.union_variants] == [
            PrimitiveType.STRING,
            PrimitiveType.FLOAT,
        ]

    def test_union_str_float_none_field_type(self) -> None:
        ft = FieldType.from_type(str | float | None)
        assert ft.repetition is Repetition.OPTIONAL
        assert ft.union_variants is not None

    def test_union_int_float_prefers_int(self) -> None:
        ft = FieldType.from_type(int | float)
        assert ft.normalize(1) == 1
        assert isinstance(ft.normalize(1), int)

    def test_union_bool_int_prefers_bool(self) -> None:
        ft = FieldType.from_type(bool | int)
        value = ft.normalize(True)
        assert value is True

    def test_union_int_float_rejects_bool(self) -> None:
        ft = FieldType.from_type(int | float)
        with pytest.raises(ValueError):
            ft.normalize(True)

    def test_union_str_bool_rejects_int(self) -> None:
        ft = FieldType.from_type(str | bool)
        with pytest.raises(ValueError):
            ft.normalize(1)

    def test_union_str_dict_rejects_scalar(self) -> None:
        ft = FieldType.from_type(str | dict)
        with pytest.raises(ValueError):
            ft.normalize(123)

    def test_union_list_int_float_rejects_bool_element(self) -> None:
        ft = FieldType.from_type(list[int] | list[float])
        with pytest.raises(ValueError):
            ft.normalize([True])

    def test_union_list_int_float_rejects_string_element(self) -> None:
        ft = FieldType.from_type(list[int] | list[float])
        with pytest.raises(ValueError):
            ft.normalize(["3"])

    def test_union_list_int_float_accepts_numeric_elements(self) -> None:
        ft = FieldType.from_type(list[int] | list[float])
        assert ft.normalize([1]) == [1]
        assert isinstance(ft.normalize([1])[0], int)
        assert ft.normalize([1.5]) == [1.5]

    def test_union_optional_normalize_none(self) -> None:
        ft = FieldType.from_type(str | float | None)
        assert ft.repetition is Repetition.OPTIONAL
        assert ft.normalize(None) is None

    def test_union_required_normalize_none_raises(self) -> None:
        ft = FieldType.from_type(str | float)
        assert ft.repetition is Repetition.REQUIRED
        with pytest.raises(ValueError):
            ft.normalize(None)

    def test_union_required_json_type_omits_nullable(self) -> None:
        ft = FieldType.from_type(int | str)
        assert ft.json_type() == {
            "anyOf": [{"type": "integer"}, {"type": "string"}],
        }

    def test_union_list_int_float_accepts_empty_list(self) -> None:
        ft = FieldType.from_type(list[int] | list[float])
        assert ft.normalize([]) == []

    def test_union_mixed_scalar_and_list(self) -> None:
        ft = FieldType.from_type(list[int] | int)
        assert ft.normalize(5) == 5
        assert isinstance(ft.normalize(5), int)
        assert ft.normalize([5]) == [5]

    def test_union_str_float_none_json_type(self) -> None:
        ft = FieldType.from_type(str | float | None)
        assert ft.json_type() == {
            "anyOf": [{"type": "string"}, {"type": "number"}],
            "nullable": True,
        }

    def test_union_rejects_path_string(self) -> None:
        from cog import Path

        try:
            FieldType.from_type(Path | str)
        except ValueError as exc:
            assert "Path" in str(exc)
            assert "union" in str(exc)
        else:
            raise AssertionError("Expected ValueError for Path | str")
