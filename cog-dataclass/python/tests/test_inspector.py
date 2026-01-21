"""Tests for cog._inspector module."""

from typing import List, Optional

from cog import BaseModel, BasePredictor, Input
from cog._adt import OutputKind, PrimitiveType, Repetition
from cog._inspector import check_input, _create_input_field, _create_output_type
from cog.input import FieldInfo


class TestCreateInputField:
    """Tests for _create_input_field function."""

    def test_basic_input(self) -> None:
        field = _create_input_field(0, "text", str, None)
        assert field.name == "text"
        assert field.order == 0
        assert field.type.primitive is PrimitiveType.STRING
        assert field.type.repetition is Repetition.REQUIRED
        assert field.default is None

    def test_input_with_default(self) -> None:
        info = FieldInfo(default="hello")
        field = _create_input_field(0, "text", str, info)
        assert field.default == "hello"

    def test_input_with_constraints(self) -> None:
        info = FieldInfo(ge=0, le=100, description="A number")
        field = _create_input_field(0, "count", int, info)
        assert field.ge == 0.0
        assert field.le == 100.0
        assert field.description == "A number"

    def test_input_with_choices(self) -> None:
        info = FieldInfo(choices=["a", "b", "c"])
        field = _create_input_field(0, "option", str, info)
        assert field.choices == ["a", "b", "c"]

    def test_optional_input(self) -> None:
        field = _create_input_field(0, "text", Optional[str], None)
        assert field.type.repetition is Repetition.OPTIONAL

    def test_list_input(self) -> None:
        field = _create_input_field(0, "items", List[str], None)
        assert field.type.repetition is Repetition.REPEATED


class TestCreateOutputType:
    """Tests for _create_output_type function."""

    def test_string_output(self) -> None:
        out = _create_output_type(str)
        assert out.kind is OutputKind.SINGLE
        assert out.type is PrimitiveType.STRING

    def test_int_output(self) -> None:
        out = _create_output_type(int)
        assert out.kind is OutputKind.SINGLE
        assert out.type is PrimitiveType.INTEGER

    def test_list_output(self) -> None:
        out = _create_output_type(List[str])
        assert out.kind is OutputKind.LIST
        assert out.type is PrimitiveType.STRING

    def test_basemodel_output(self) -> None:
        class Output(BaseModel):
            text: str
            score: float

        out = _create_output_type(Output)
        assert out.kind is OutputKind.OBJECT
        assert out.fields is not None
        assert "text" in out.fields
        assert "score" in out.fields


class TestCheckInput:
    """Tests for check_input function."""

    def test_basic_check_input(self) -> None:
        field = _create_input_field(0, "text", str, None)
        inputs = {"text": field}
        result = check_input(inputs, {"text": "hello"})
        assert result == {"text": "hello"}

    def test_check_input_with_default(self) -> None:
        info = FieldInfo(default="default_value")
        field = _create_input_field(0, "text", str, info)
        inputs = {"text": field}
        result = check_input(inputs, {})
        assert result == {"text": "default_value"}

    def test_check_input_optional_none(self) -> None:
        field = _create_input_field(0, "text", Optional[str], None)
        inputs = {"text": field}
        result = check_input(inputs, {})
        assert result == {"text": None}

    def test_check_input_ge_constraint(self) -> None:
        info = FieldInfo(ge=0)
        field = _create_input_field(0, "count", int, info)
        inputs = {"count": field}

        # Valid
        result = check_input(inputs, {"count": 5})
        assert result == {"count": 5}

        # Invalid
        try:
            check_input(inputs, {"count": -1})
            assert False, "Should have raised ValueError"
        except ValueError as e:
            assert "fails constraint >= 0" in str(e)

    def test_check_input_le_constraint(self) -> None:
        info = FieldInfo(le=100)
        field = _create_input_field(0, "count", int, info)
        inputs = {"count": field}

        # Valid
        result = check_input(inputs, {"count": 50})
        assert result == {"count": 50}

        # Invalid
        try:
            check_input(inputs, {"count": 101})
            assert False, "Should have raised ValueError"
        except ValueError as e:
            assert "fails constraint <= 100" in str(e)

    def test_check_input_min_length_constraint(self) -> None:
        info = FieldInfo(min_length=3)
        field = _create_input_field(0, "text", str, info)
        inputs = {"text": field}

        # Valid
        result = check_input(inputs, {"text": "hello"})
        assert result == {"text": "hello"}

        # Invalid
        try:
            check_input(inputs, {"text": "hi"})
            assert False, "Should have raised ValueError"
        except ValueError as e:
            assert "fails constraint len() >= 3" in str(e)

    def test_check_input_max_length_constraint(self) -> None:
        info = FieldInfo(max_length=5)
        field = _create_input_field(0, "text", str, info)
        inputs = {"text": field}

        # Valid
        result = check_input(inputs, {"text": "hello"})
        assert result == {"text": "hello"}

        # Invalid
        try:
            check_input(inputs, {"text": "hello world"})
            assert False, "Should have raised ValueError"
        except ValueError as e:
            assert "fails constraint len() <= 5" in str(e)

    def test_check_input_choices_constraint(self) -> None:
        info = FieldInfo(choices=["a", "b", "c"])
        field = _create_input_field(0, "option", str, info)
        inputs = {"option": field}

        # Valid
        result = check_input(inputs, {"option": "a"})
        assert result == {"option": "a"}

        # Invalid
        try:
            check_input(inputs, {"option": "d"})
            assert False, "Should have raised ValueError"
        except ValueError as e:
            assert "does not match choices" in str(e)

    def test_check_input_regex_constraint(self) -> None:
        info = FieldInfo(regex=r"^\d{3}-\d{4}$")
        field = _create_input_field(0, "phone", str, info)
        inputs = {"phone": field}

        # Valid
        result = check_input(inputs, {"phone": "123-4567"})
        assert result == {"phone": "123-4567"}

        # Invalid
        try:
            check_input(inputs, {"phone": "invalid"})
            assert False, "Should have raised ValueError"
        except ValueError as e:
            assert "does not match regex" in str(e)

    def test_check_input_missing_required(self) -> None:
        field = _create_input_field(0, "text", str, None)
        inputs = {"text": field}

        try:
            check_input(inputs, {})
            assert False, "Should have raised ValueError"
        except ValueError as e:
            assert "missing required input field" in str(e)

    def test_check_input_unknown_field_warning(self, capsys) -> None:
        field = _create_input_field(0, "text", str, None)
        inputs = {"text": field}

        result = check_input(inputs, {"text": "hello", "unknown": "value"})
        assert result == {"text": "hello"}

        captured = capsys.readouterr()
        assert "WARNING unknown input field ignored: unknown" in captured.out
