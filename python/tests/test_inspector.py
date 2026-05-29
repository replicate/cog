from pathlib import Path
from typing import Annotated, List, Optional

import pytest

from cog import BaseModel, Opaque
from cog import _adt as adt
from cog._inspector import _create_predictor_info, create_predictor


def test_inspector_uses_run_method_for_classes(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    module_name = "runner_module_run_method"
    (tmp_path / f"{module_name}.py").write_text(
        "class Runner:\n    def run(self, value: str) -> str:\n        return value\n"
    )
    monkeypatch.syspath_prepend(str(tmp_path))

    info = create_predictor(module_name, "Runner")
    assert "value" in info.inputs


def test_inspector_warns_for_legacy_predict_method(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    module_name = "runner_module_predict_method"
    (tmp_path / f"{module_name}.py").write_text(
        "class Runner:\n"
        "    def predict(self, value: str) -> str:\n"
        "        return value\n"
    )
    monkeypatch.syspath_prepend(str(tmp_path))

    with pytest.warns(DeprecationWarning, match=r"Runner\.predict\(\) is deprecated"):
        info = create_predictor(module_name, "Runner")
    assert "value" in info.inputs


def test_inspector_warns_for_base_predictor_inheritance(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    module_name = "runner_module_base_predictor"
    (tmp_path / f"{module_name}.py").write_text(
        "from cog import BasePredictor\n"
        "class Runner(BasePredictor):\n"
        "    def run(self, value: str) -> str:\n"
        "        return value\n"
    )
    monkeypatch.syspath_prepend(str(tmp_path))

    with pytest.warns(DeprecationWarning, match="BasePredictor is deprecated"):
        info = create_predictor(module_name, "Runner")
    assert "value" in info.inputs


def test_inspector_supports_inherited_run(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    module_name = "runner_module_inherited_run"
    (tmp_path / f"{module_name}.py").write_text(
        "from cog import BaseRunner\n"
        "class Shared(BaseRunner):\n"
        "    def run(self, value: str) -> str:\n"
        "        return value\n"
        "class Runner(Shared):\n"
        "    pass\n"
    )
    monkeypatch.syspath_prepend(str(tmp_path))

    info = create_predictor(module_name, "Runner")
    assert "value" in info.inputs


def test_inspector_rejects_inherited_run_and_direct_predict(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    module_name = "runner_module_inherited_conflict"
    (tmp_path / f"{module_name}.py").write_text(
        "from cog import BaseRunner\n"
        "class Shared(BaseRunner):\n"
        "    def run(self, value: str) -> str:\n"
        "        return value\n"
        "class Runner(Shared):\n"
        "    def predict(self, value: str) -> str:\n"
        "        return value\n"
    )
    monkeypatch.syspath_prepend(str(tmp_path))

    with pytest.raises(ValueError, match=r"either run\(\) or predict\(\)"):
        create_predictor(module_name, "Runner")


def test_inspector_warns_for_inherited_legacy_predict(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    module_name = "runner_module_inherited_predict"
    (tmp_path / f"{module_name}.py").write_text(
        "from cog import BasePredictor\n"
        "class Shared(BasePredictor):\n"
        "    def predict(self, value: str) -> str:\n"
        "        return value\n"
        "class Predictor(Shared):\n"
        "    pass\n"
    )
    monkeypatch.syspath_prepend(str(tmp_path))

    with pytest.warns(DeprecationWarning, match=r"predict\(\) is deprecated"):
        info = create_predictor(module_name, "Predictor")
    assert "value" in info.inputs


def test_inspector_rejects_class_with_run_and_predict(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    module_name = "runner_module_conflict"
    (tmp_path / f"{module_name}.py").write_text(
        "class Runner:\n"
        "    def run(self, value: str) -> str:\n"
        "        return value\n"
        "    def predict(self, value: str) -> str:\n"
        "        return value\n"
    )
    monkeypatch.syspath_prepend(str(tmp_path))

    with pytest.raises(ValueError, match=r"either run\(\) or predict\(\)"):
        create_predictor(module_name, "Runner")


def test_inspector_errors_when_class_has_no_run_or_predict(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    module_name = "runner_module_missing_method"
    (tmp_path / f"{module_name}.py").write_text(
        "class Runner:\n    def setup(self) -> None:\n        pass\n"
    )
    monkeypatch.syspath_prepend(str(tmp_path))

    with pytest.raises(ValueError, match="run.*predict|predict.*run"):
        create_predictor(module_name, "Runner")


class ExternalObject:
    pass


def test_inspector_preserves_opaque_input_metadata_with_run() -> None:
    class Runner:
        def run(self, value: Annotated[ExternalObject, Opaque]) -> str:
            return "ok"

    info = _create_predictor_info("run", "Runner", Runner.run, "run", True)
    field = info.inputs["value"]
    assert field.type.primitive is adt.PrimitiveType.ANY
    assert field.type.repetition is adt.Repetition.REQUIRED


def test_inspector_preserves_opaque_input_metadata() -> None:
    class Predictor:
        def predict(self, value: Annotated[ExternalObject, Opaque]) -> str:
            return "ok"

    info = _create_predictor_info(
        "predict", "Predictor", Predictor.predict, "predict", True
    )
    field = info.inputs["value"]
    assert field.type.primitive is adt.PrimitiveType.ANY
    assert field.type.repetition is adt.Repetition.REQUIRED


def test_inspector_preserves_opaque_list_input_metadata() -> None:
    class Predictor:
        def predict(self, value: Annotated[List[ExternalObject], Opaque]) -> str:
            return "ok"

    info = _create_predictor_info(
        "predict", "Predictor", Predictor.predict, "predict", True
    )
    field = info.inputs["value"]
    assert field.type.primitive is adt.PrimitiveType.ANY
    assert field.type.repetition is adt.Repetition.REPEATED


def test_inspector_supports_opaque_output_metadata() -> None:
    class Predictor:
        def predict(self, value: str) -> Annotated[ExternalObject, Opaque]:
            return ExternalObject()

    info = _create_predictor_info(
        "predict", "Predictor", Predictor.predict, "predict", True
    )
    assert info.output.kind is adt.OutputKind.SINGLE
    assert info.output.type is adt.PrimitiveType.ANY


def test_inspector_supports_opaque_list_output_metadata() -> None:
    class Predictor:
        def predict(self, value: str) -> Annotated[List[ExternalObject], Opaque]:
            return [ExternalObject()]

    info = _create_predictor_info(
        "predict", "Predictor", Predictor.predict, "predict", True
    )
    assert info.output.kind is adt.OutputKind.LIST
    assert info.output.type is adt.PrimitiveType.ANY


def test_inspector_supports_basemodel_opaque_output_field_with_run() -> None:
    class Output(BaseModel):
        payload: Annotated[ExternalObject, Opaque]

    class Runner:
        def run(self, value: str) -> Output:
            return Output(payload=ExternalObject())

    info = _create_predictor_info("run", "Runner", Runner.run, "run", True)
    assert info.output.kind is adt.OutputKind.OBJECT
    assert info.output.fields is not None
    field = info.output.fields["payload"]
    assert field.primitive is adt.PrimitiveType.ANY
    assert field.repetition is adt.Repetition.REQUIRED


def test_inspector_supports_basemodel_opaque_output_field() -> None:
    class Output(BaseModel):
        payload: Annotated[ExternalObject, Opaque]

    class Predictor:
        def predict(self, value: str) -> Output:
            return Output(payload=ExternalObject())

    info = _create_predictor_info(
        "predict", "Predictor", Predictor.predict, "predict", True
    )
    assert info.output.kind is adt.OutputKind.OBJECT
    assert info.output.fields is not None
    field = info.output.fields["payload"]
    assert field.primitive is adt.PrimitiveType.ANY
    assert field.repetition is adt.Repetition.REQUIRED


def test_inspector_supports_basemodel_opaque_list_output_field_schema() -> None:
    class Output(BaseModel):
        payload: Annotated[List[ExternalObject], Opaque]

    class Predictor:
        def predict(self, value: str) -> Output:
            return Output(payload=[ExternalObject()])

    info = _create_predictor_info(
        "predict", "Predictor", Predictor.predict, "predict", True
    )
    payload_schema = info.output.json_type()["properties"]["payload"]
    assert payload_schema["type"] == "array"
    assert payload_schema["items"] == {"type": "object"}


def test_inspector_basemodel_optional_output_fields_schema() -> None:
    class Output(BaseModel):
        required: str
        maybe: Optional[str]
        maybe_values: Optional[List[str]]

    class Predictor:
        def predict(self, value: str) -> Output:
            return Output(required=value, maybe=None, maybe_values=None)

    info = _create_predictor_info(
        "predict", "Predictor", Predictor.predict, "predict", True
    )
    output_schema = info.output.json_type()

    assert output_schema["required"] == ["required"]

    maybe_schema = output_schema["properties"]["maybe"]
    assert maybe_schema["type"] == "string"
    assert maybe_schema["nullable"] is True
    assert maybe_schema["title"] == "Maybe"

    maybe_values_schema = output_schema["properties"]["maybe_values"]
    assert maybe_values_schema["type"] == "array"
    assert maybe_values_schema["items"] == {"type": "string"}
    assert maybe_values_schema["nullable"] is True
    assert maybe_values_schema["title"] == "Maybe Values"


def test_inspector_basemodel_all_optional_output_fields_omits_required_schema() -> None:
    class Output(BaseModel):
        maybe: Optional[str]
        maybe_values: Optional[List[str]]

    class Predictor:
        def predict(self, value: str) -> Output:
            return Output(maybe=value, maybe_values=None)

    info = _create_predictor_info(
        "predict", "Predictor", Predictor.predict, "predict", True
    )
    output_schema = info.output.json_type()

    assert "required" not in output_schema


def test_inspector_supports_basemodel_string_opaque_output_field() -> None:
    class Output(BaseModel):
        payload: "Annotated[ExternalObject, Opaque]"

    class Predictor:
        def predict(self, value: str) -> Output:
            return Output(payload=ExternalObject())

    info = _create_predictor_info(
        "predict", "Predictor", Predictor.predict, "predict", True
    )
    assert info.output.kind is adt.OutputKind.OBJECT
    assert info.output.fields is not None
    field = info.output.fields["payload"]
    assert field.primitive is adt.PrimitiveType.ANY
    assert field.repetition is adt.Repetition.REQUIRED


def test_inspector_supports_pydantic_opaque_list_output_field_schema() -> None:
    pydantic = pytest.importorskip("pydantic")

    class Output(pydantic.BaseModel):
        payload: Annotated[List[ExternalObject], Opaque]

        model_config = pydantic.ConfigDict(arbitrary_types_allowed=True)

    class Predictor:
        def predict(self, value: str) -> Output:
            return Output(payload=[ExternalObject()])

    info = _create_predictor_info(
        "predict", "Predictor", Predictor.predict, "predict", True
    )
    payload_schema = info.output.json_type()["properties"]["payload"]
    assert payload_schema["type"] == "array"
    assert payload_schema["items"] == {"type": "object"}


def test_inspector_rejects_optional_opaque_output_metadata() -> None:
    class Predictor:
        def predict(self, value: str) -> Optional[Annotated[ExternalObject, Opaque]]:
            return ExternalObject()

    with pytest.raises(ValueError, match="output must not be Optional"):
        _create_predictor_info(
            "predict", "Predictor", Predictor.predict, "predict", True
        )


def test_inspector_preserves_non_opaque_annotated_behavior() -> None:
    class Predictor:
        def predict(
            self, value: Annotated[str, "metadata"]
        ) -> Annotated[str, "metadata"]:
            return value

    info = _create_predictor_info(
        "predict", "Predictor", Predictor.predict, "predict", True
    )
    field = info.inputs["value"]
    assert field.type.primitive is adt.PrimitiveType.STRING
    assert field.type.repetition is adt.Repetition.REQUIRED
    assert info.output.kind is adt.OutputKind.SINGLE
    assert info.output.type is adt.PrimitiveType.STRING


def test_inspector_preserves_nested_non_opaque_annotated_list_behavior() -> None:
    class Predictor:
        def predict(self, value: List[Annotated[str, "metadata"]]) -> str:
            return value[0]

    info = _create_predictor_info(
        "predict", "Predictor", Predictor.predict, "predict", True
    )
    field = info.inputs["value"]
    assert field.type.primitive is adt.PrimitiveType.STRING
    assert field.type.repetition is adt.Repetition.REPEATED


def test_inspector_preserves_nested_non_opaque_annotated_optional_behavior() -> None:
    class Predictor:
        def predict(self, value: Optional[Annotated[str, "metadata"]]) -> str:
            return value or ""

    info = _create_predictor_info(
        "predict", "Predictor", Predictor.predict, "predict", True
    )
    field = info.inputs["value"]
    assert field.type.primitive is adt.PrimitiveType.STRING
    assert field.type.repetition is adt.Repetition.OPTIONAL


def test_inspector_accepts_setup_with_none_return_annotation_and_future_annotations(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    """setup() -> None must be accepted even when from __future__ import annotations is active.

    With PEP 563 string annotations, -> None is stored as the string "None" rather than
    the NoneType object, so a naive `is not None` check incorrectly rejects it.
    """
    module_name = "future_annotations_setup"
    (tmp_path / f"{module_name}.py").write_text(
        "from __future__ import annotations\n"
        "class Runner:\n"
        "    def setup(self) -> None:\n"
        "        pass\n"
        "    def run(self, value: str) -> str:\n"
        "        return value\n"
    )
    monkeypatch.syspath_prepend(str(tmp_path))

    info = create_predictor(module_name, "Runner")
    assert "value" in info.inputs


def test_inspector_rejects_predict_with_none_return_annotation_and_future_annotations(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    """predict() -> None must be rejected even when from __future__ import annotations is active.

    With PEP 563 string annotations, -> None is stored as the string "None" rather than
    the NoneType object, so a naive `is None` check incorrectly accepts it.
    """
    module_name = "future_annotations_predict_none"
    (tmp_path / f"{module_name}.py").write_text(
        "from __future__ import annotations\n"
        "class Runner:\n"
        "    def run(self, value: str) -> None:\n"
        "        pass\n"
    )
    monkeypatch.syspath_prepend(str(tmp_path))

    with pytest.raises(ValueError, match="return type annotation"):
        create_predictor(module_name, "Runner")
