"""Tests for cog._adt module."""

from typing import Any, Dict, List, Optional, Set

from cog import Path, Secret
from cog._adt import (
    FieldType,
    InputField,
    OutputKind,
    OutputType,
    PredictorInfo,
    PrimitiveType,
    Repetition,
)


class TestPrimitiveType:
    """Tests for PrimitiveType enum."""

    def test_from_type_basic_types(self) -> None:
        assert PrimitiveType.from_type(bool) is PrimitiveType.BOOL
        assert PrimitiveType.from_type(int) is PrimitiveType.INTEGER
        assert PrimitiveType.from_type(float) is PrimitiveType.FLOAT
        assert PrimitiveType.from_type(str) is PrimitiveType.STRING

    def test_from_type_cog_types(self) -> None:
        assert PrimitiveType.from_type(Path) is PrimitiveType.PATH
        assert PrimitiveType.from_type(Secret) is PrimitiveType.SECRET

    def test_from_type_any(self) -> None:
        assert PrimitiveType.from_type(Any) is PrimitiveType.ANY

    def test_from_type_custom(self) -> None:
        class CustomClass:
            pass

        assert PrimitiveType.from_type(CustomClass) is PrimitiveType.CUSTOM

    def test_normalize_int(self) -> None:
        assert PrimitiveType.INTEGER.normalize(42) == 42
        assert PrimitiveType.INTEGER.normalize(42.0) == 42

    def test_normalize_float(self) -> None:
        assert PrimitiveType.FLOAT.normalize(3.14) == 3.14
        assert PrimitiveType.FLOAT.normalize(3) == 3.0

    def test_normalize_string(self) -> None:
        assert PrimitiveType.STRING.normalize("hello") == "hello"

    def test_normalize_bool(self) -> None:
        assert PrimitiveType.BOOL.normalize(True) is True
        assert PrimitiveType.BOOL.normalize(False) is False

    def test_normalize_path(self) -> None:
        result = PrimitiveType.PATH.normalize("/tmp/file.txt")
        assert isinstance(result, Path)
        assert str(result) == "/tmp/file.txt"

    def test_normalize_secret(self) -> None:
        result = PrimitiveType.SECRET.normalize("api-key")
        assert isinstance(result, Secret)
        assert result.get_secret_value() == "api-key"

    def test_json_type(self) -> None:
        assert PrimitiveType.INTEGER.json_type() == {"type": "integer"}
        assert PrimitiveType.FLOAT.json_type() == {"type": "number"}
        assert PrimitiveType.STRING.json_type() == {"type": "string"}
        assert PrimitiveType.BOOL.json_type() == {"type": "boolean"}

    def test_json_type_path(self) -> None:
        jt = PrimitiveType.PATH.json_type()
        assert jt["type"] == "string"
        assert jt["format"] == "uri"

    def test_json_type_secret(self) -> None:
        jt = PrimitiveType.SECRET.json_type()
        assert jt["type"] == "string"
        assert jt["format"] == "password"
        assert jt["writeOnly"] is True
        assert jt["x-cog-secret"] is True


class TestFieldType:
    """Tests for FieldType class."""

    def test_from_type_required(self) -> None:
        ft = FieldType.from_type(str)
        assert ft.primitive is PrimitiveType.STRING
        assert ft.repetition is Repetition.REQUIRED
        assert ft.coder is None

    def test_from_type_optional(self) -> None:
        ft = FieldType.from_type(Optional[str])
        assert ft.primitive is PrimitiveType.STRING
        assert ft.repetition is Repetition.OPTIONAL
        assert ft.coder is None

    def test_from_type_list(self) -> None:
        ft = FieldType.from_type(List[int])
        assert ft.primitive is PrimitiveType.INTEGER
        assert ft.repetition is Repetition.REPEATED
        assert ft.coder is None

    def test_from_type_bare_list(self) -> None:
        ft = FieldType.from_type(list)
        assert ft.primitive is PrimitiveType.ANY
        assert ft.repetition is Repetition.REPEATED

    def test_from_type_bare_dict(self) -> None:
        ft = FieldType.from_type(dict)
        assert ft.primitive is PrimitiveType.CUSTOM
        assert ft.repetition is Repetition.REQUIRED
        assert ft.coder is not None

    def test_from_type_dict_str_any(self) -> None:
        ft = FieldType.from_type(Dict[str, Any])
        assert ft.primitive is PrimitiveType.CUSTOM
        assert ft.coder is not None

    def test_from_type_set(self) -> None:
        ft = FieldType.from_type(Set[int])
        assert ft.primitive is PrimitiveType.CUSTOM
        assert ft.coder is not None

    def test_normalize_required(self) -> None:
        ft = FieldType.from_type(int)
        assert ft.normalize(42) == 42

    def test_normalize_optional(self) -> None:
        ft = FieldType.from_type(Optional[int])
        assert ft.normalize(42) == 42
        assert ft.normalize(None) is None

    def test_normalize_repeated(self) -> None:
        ft = FieldType.from_type(List[int])
        assert ft.normalize([1, 2, 3]) == [1, 2, 3]

    def test_json_type_required(self) -> None:
        ft = FieldType.from_type(str)
        assert ft.json_type() == {"type": "string"}

    def test_json_type_repeated(self) -> None:
        ft = FieldType.from_type(List[str])
        assert ft.json_type() == {"type": "array", "items": {"type": "string"}}

    def test_python_type_name(self) -> None:
        assert FieldType.from_type(str).python_type_name() == "str"
        assert FieldType.from_type(Optional[str]).python_type_name() == "Optional[str]"
        assert FieldType.from_type(List[str]).python_type_name() == "List[str]"


class TestInputField:
    """Tests for InputField class."""

    def test_basic_input_field(self) -> None:
        ft = FieldType.from_type(str)
        inp = InputField(name="text", order=0, type=ft)
        assert inp.name == "text"
        assert inp.order == 0
        assert inp.default is None
        assert inp.description is None

    def test_input_field_with_constraints(self) -> None:
        ft = FieldType.from_type(int)
        inp = InputField(
            name="count",
            order=0,
            type=ft,
            default=5,
            description="The count",
            ge=0,
            le=100,
        )
        assert inp.default == 5
        assert inp.description == "The count"
        assert inp.ge == 0
        assert inp.le == 100


class TestOutputType:
    """Tests for OutputType class."""

    def test_single_output(self) -> None:
        out = OutputType(kind=OutputKind.SINGLE, type=PrimitiveType.STRING)
        jt = out.json_type()
        assert jt["title"] == "Output"
        assert jt["type"] == "string"

    def test_list_output(self) -> None:
        out = OutputType(kind=OutputKind.LIST, type=PrimitiveType.INTEGER)
        jt = out.json_type()
        assert jt["title"] == "Output"
        assert jt["type"] == "array"
        assert jt["items"]["type"] == "integer"

    def test_iterator_output(self) -> None:
        out = OutputType(kind=OutputKind.ITERATOR, type=PrimitiveType.STRING)
        jt = out.json_type()
        assert jt["x-cog-array-type"] == "iterator"
        assert "x-cog-array-display" not in jt

    def test_concat_iterator_output(self) -> None:
        out = OutputType(kind=OutputKind.CONCAT_ITERATOR, type=PrimitiveType.STRING)
        jt = out.json_type()
        assert jt["x-cog-array-type"] == "iterator"
        assert jt["x-cog-array-display"] == "concatenate"

    def test_object_output(self) -> None:
        fields = {
            "text": FieldType.from_type(str),
            "score": FieldType.from_type(float),
        }
        out = OutputType(kind=OutputKind.OBJECT, fields=fields)
        jt = out.json_type()
        assert jt["type"] == "object"
        assert "text" in jt["properties"]
        assert "score" in jt["properties"]
        assert set(jt["required"]) == {"text", "score"}


class TestPredictorInfo:
    """Tests for PredictorInfo class."""

    def test_basic_predictor_info(self) -> None:
        inputs = {
            "text": InputField(
                name="text",
                order=0,
                type=FieldType.from_type(str),
            )
        }
        output = OutputType(kind=OutputKind.SINGLE, type=PrimitiveType.STRING)
        info = PredictorInfo(
            module_name="mymodule",
            predictor_name="Predictor",
            inputs=inputs,
            output=output,
        )
        assert info.module_name == "mymodule"
        assert info.predictor_name == "Predictor"
        assert len(info.inputs) == 1
        assert info.output.kind is OutputKind.SINGLE
