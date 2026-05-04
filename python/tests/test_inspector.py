from typing import Annotated, List, Optional

import pytest

from cog import Opaque
from cog import _adt as adt
from cog._inspector import _create_predictor_info


class ExternalObject:
    pass


def test_inspector_preserves_opaque_input_metadata() -> None:
    class Predictor:
        def predict(self, value: Annotated[ExternalObject, Opaque]) -> str:
            return "ok"

    info = _create_predictor_info(
        "predict", "Predictor", Predictor().predict, "predict", True
    )
    field = info.inputs["value"]
    assert field.type.primitive is adt.PrimitiveType.ANY
    assert field.type.repetition is adt.Repetition.REQUIRED


def test_inspector_preserves_opaque_list_input_metadata() -> None:
    class Predictor:
        def predict(self, value: Annotated[List[ExternalObject], Opaque]) -> str:
            return "ok"

    info = _create_predictor_info(
        "predict", "Predictor", Predictor().predict, "predict", True
    )
    field = info.inputs["value"]
    assert field.type.primitive is adt.PrimitiveType.ANY
    assert field.type.repetition is adt.Repetition.REPEATED


def test_inspector_supports_opaque_output_metadata() -> None:
    class Predictor:
        def predict(self, value: str) -> Annotated[ExternalObject, Opaque]:
            return ExternalObject()

    info = _create_predictor_info(
        "predict", "Predictor", Predictor().predict, "predict", True
    )
    assert info.output.kind is adt.OutputKind.SINGLE
    assert info.output.type is adt.PrimitiveType.ANY


def test_inspector_supports_opaque_list_output_metadata() -> None:
    class Predictor:
        def predict(self, value: str) -> Annotated[List[ExternalObject], Opaque]:
            return [ExternalObject()]

    info = _create_predictor_info(
        "predict", "Predictor", Predictor().predict, "predict", True
    )
    assert info.output.kind is adt.OutputKind.LIST
    assert info.output.type is adt.PrimitiveType.ANY


def test_inspector_rejects_optional_opaque_output_metadata() -> None:
    class Predictor:
        def predict(self, value: str) -> Optional[Annotated[ExternalObject, Opaque]]:
            return ExternalObject()

    with pytest.raises(ValueError, match="output must not be Optional"):
        _create_predictor_info(
            "predict", "Predictor", Predictor().predict, "predict", True
        )


def test_inspector_preserves_non_opaque_annotated_behavior() -> None:
    class Predictor:
        def predict(self, value: Annotated[str, "metadata"]) -> Annotated[
            str, "metadata"
        ]:
            return value

    info = _create_predictor_info(
        "predict", "Predictor", Predictor().predict, "predict", True
    )
    field = info.inputs["value"]
    assert field.type.primitive is adt.PrimitiveType.STRING
    assert field.type.repetition is adt.Repetition.REQUIRED
    assert info.output.kind is adt.OutputKind.SINGLE
    assert info.output.type is adt.PrimitiveType.STRING
