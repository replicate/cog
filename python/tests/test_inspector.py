"""Tests for _inspector module."""

import pytest

from cog._inspector import _create_output_type

try:
    from pydantic import BaseModel as PydanticBaseModel

    HAS_PYDANTIC = True
except ImportError:
    HAS_PYDANTIC = False


@pytest.mark.skipif(not HAS_PYDANTIC, reason="pydantic not installed")
def test_pydantic_basemodel_output_raises_error():
    """Detect pydantic.BaseModel in output types and error with migration guidance (#2922)."""

    class MyOutput(PydanticBaseModel):
        text: str
        score: float

    with pytest.raises(ValueError, match="inherits from pydantic.BaseModel"):
        _create_output_type(MyOutput)


@pytest.mark.skipif(not HAS_PYDANTIC, reason="pydantic not installed")
def test_pydantic_basemodel_error_message_includes_guidance():
    """Error message should include migration guidance to cog.BaseModel."""

    class Result(PydanticBaseModel):
        name: str

    with pytest.raises(ValueError, match="from cog import BaseModel"):
        _create_output_type(Result)


def test_cog_basemodel_output_works():
    """cog.BaseModel should still work as output type."""
    from cog.model import BaseModel

    class Output(BaseModel):
        text: str
        score: float

    result = _create_output_type(Output)
    assert result.kind.name == "OBJECT"
    assert "text" in result.fields
    assert "score" in result.fields
