"""Tests for cog._adt module (FieldType, PrimitiveType)."""

from typing import Any, Dict, List, Optional, TypedDict

from cog._adt import FieldType, PrimitiveType, Repetition


class ExampleTypedDict(TypedDict):
    name: str
    count: int


class ExampleDictSubclass(dict[str, int]):
    pass


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

    def test_list_of_typeddict_input(self) -> None:
        """list[TypedDict] should produce a repeated ANY field."""
        ft = FieldType.from_type(list[ExampleTypedDict])
        assert ft.primitive is PrimitiveType.ANY
        assert ft.repetition is Repetition.REPEATED
        assert ft.coder is None

    def test_optional_typeddict_input(self) -> None:
        """Optional[TypedDict] should produce an optional ANY field."""
        ft = FieldType.from_type(Optional[ExampleTypedDict])
        assert ft.primitive is PrimitiveType.ANY
        assert ft.repetition is Repetition.OPTIONAL
        assert ft.coder is None

    def test_optional_list_of_typeddict_input(self) -> None:
        """Optional[list[TypedDict]] should produce optional repeated ANY."""
        ft = FieldType.from_type(Optional[list[ExampleTypedDict]])
        assert ft.primitive is PrimitiveType.ANY
        assert ft.repetition is Repetition.OPTIONAL_REPEATED
        assert ft.coder is None

    def test_pep604_list_of_typeddict_or_none_input(self) -> None:
        """list[TypedDict] | None should produce optional repeated ANY."""
        ft = FieldType.from_type(list[ExampleTypedDict] | None)
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
