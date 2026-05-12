"""Tests for cog.predictor module (BasePredictor)."""

from pathlib import Path as FilePath
from typing import Optional

import pytest

from cog import BasePredictor, BaseRunner, Path


def test_base_runner_run_and_predict_bridge() -> None:
    class MyRunner(BaseRunner):
        def run(self, text: str) -> str:
            return text.upper()

    runner = MyRunner()
    assert runner.run(text="hello") == "HELLO"
    assert runner.predict("hello") == "HELLO"
    assert runner.predict(text="hello") == "HELLO"


def test_base_runner_run_delegates_to_legacy_predict_with_positional_args() -> None:
    class MyRunner(BaseRunner):
        def predict(self, text: str) -> str:
            return text.upper()

    runner = MyRunner()
    assert runner.run("hello") == "HELLO"
    assert runner.run(text="hello") == "HELLO"


def test_base_predictor_is_legacy_subclass() -> None:
    assert issubclass(BasePredictor, BaseRunner)


def test_load_predictor_from_ref_defaults_to_runner(tmp_path: FilePath) -> None:
    model = tmp_path / "run.py"
    model.write_text(
        "from cog import BaseRunner\n"
        "class Runner(BaseRunner):\n"
        "    def run(self, text: str) -> str:\n"
        "        return text.upper()\n"
    )

    from cog.predictor import load_predictor_from_ref

    runner = load_predictor_from_ref(str(model))
    assert runner.run(text="hello") == "HELLO"


def test_load_predictor_from_ref_warns_for_legacy_predictor_class(
    tmp_path: FilePath,
) -> None:
    model = tmp_path / "predict.py"
    model.write_text(
        "from cog import BaseRunner\n"
        "class Predictor(BaseRunner):\n"
        "    def run(self, text: str) -> str:\n"
        "        return text.upper()\n"
    )

    from cog.predictor import load_predictor_from_ref

    with pytest.warns(DeprecationWarning, match="Predictor is deprecated"):
        runner = load_predictor_from_ref(str(model))
    assert runner.run(text="hello") == "HELLO"


def test_load_predictor_from_ref_prefers_runner_when_both_default_classes_exist(
    tmp_path: FilePath,
) -> None:
    model = tmp_path / "run.py"
    model.write_text(
        "from cog import BaseRunner\n"
        "class Runner(BaseRunner):\n"
        "    def run(self, text: str) -> str:\n"
        "        return 'runner:' + text\n"
        "class Predictor(BaseRunner):\n"
        "    def run(self, text: str) -> str:\n"
        "        return 'predictor:' + text\n"
    )

    from cog.predictor import load_predictor_from_ref

    with pytest.warns(UserWarning, match="Both Runner and Predictor"):
        runner = load_predictor_from_ref(str(model))
    assert runner.run(text="hello") == "runner:hello"


def test_load_predictor_from_ref_rejects_run_and_predict(
    tmp_path: FilePath,
) -> None:
    model = tmp_path / "run.py"
    model.write_text(
        "from cog import BaseRunner\n"
        "class Runner(BaseRunner):\n"
        "    def run(self, text: str) -> str:\n"
        "        return text\n"
        "    def predict(self, text: str) -> str:\n"
        "        return text\n"
    )

    from cog.predictor import load_predictor_from_ref

    with pytest.raises(
        ValueError, match=r"define either run\(\) or predict\(\), not both"
    ):
        load_predictor_from_ref(str(model))


def test_load_predictor_from_ref_rejects_missing_run_or_predict(
    tmp_path: FilePath,
) -> None:
    model = tmp_path / "run.py"
    model.write_text(
        "from cog import BaseRunner\nclass Runner(BaseRunner):\n    pass\n"
    )

    from cog.predictor import load_predictor_from_ref

    with pytest.raises(ValueError, match="run or predict"):
        load_predictor_from_ref(str(model))


def test_load_predictor_from_ref_warns_for_class_predict_method(
    tmp_path: FilePath,
) -> None:
    model = tmp_path / "run.py"
    model.write_text(
        "from cog import BaseRunner\n"
        "class Runner(BaseRunner):\n"
        "    def predict(self, text: str) -> str:\n"
        "        return text.upper()\n"
    )

    from cog.predictor import load_predictor_from_ref

    with pytest.warns(DeprecationWarning, match=r"Runner\.predict\(\) is deprecated"):
        runner = load_predictor_from_ref(str(model))
    assert runner.predict(text="hello") == "HELLO"


def test_load_predictor_from_ref_warns_for_base_predictor_inheritance(
    tmp_path: FilePath,
) -> None:
    model = tmp_path / "run.py"
    model.write_text(
        "from cog import BasePredictor\n"
        "class Runner(BasePredictor):\n"
        "    def run(self, text: str) -> str:\n"
        "        return text.upper()\n"
    )

    from cog.predictor import load_predictor_from_ref

    with pytest.warns(DeprecationWarning, match="BasePredictor is deprecated"):
        runner = load_predictor_from_ref(str(model))
    assert runner.run(text="hello") == "HELLO"


def test_load_predictor_from_ref_supports_inherited_run(
    tmp_path: FilePath,
) -> None:
    model = tmp_path / "run.py"
    model.write_text(
        "from cog import BaseRunner\n"
        "class Shared(BaseRunner):\n"
        "    def run(self, text: str) -> str:\n"
        "        return text.upper()\n"
        "class Runner(Shared):\n"
        "    pass\n"
    )

    from cog.predictor import load_predictor_from_ref

    runner = load_predictor_from_ref(str(model))
    assert runner.run(text="hello") == "HELLO"


def test_load_predictor_from_ref_rejects_inherited_run_and_direct_predict(
    tmp_path: FilePath,
) -> None:
    model = tmp_path / "run.py"
    model.write_text(
        "from cog import BaseRunner\n"
        "class Shared(BaseRunner):\n"
        "    def run(self, text: str) -> str:\n"
        "        return text\n"
        "class Runner(Shared):\n"
        "    def predict(self, text: str) -> str:\n"
        "        return text\n"
    )

    from cog.predictor import load_predictor_from_ref

    with pytest.raises(ValueError, match=r"either run\(\) or predict\(\)"):
        load_predictor_from_ref(str(model))


def test_load_predictor_from_ref_warns_for_inherited_legacy_predict(
    tmp_path: FilePath,
) -> None:
    model = tmp_path / "predict.py"
    model.write_text(
        "from cog import BasePredictor\n"
        "class Shared(BasePredictor):\n"
        "    def predict(self, text: str) -> str:\n"
        "        return text.upper()\n"
        "class Predictor(Shared):\n"
        "    pass\n"
    )

    from cog.predictor import load_predictor_from_ref

    with pytest.warns(DeprecationWarning, match=r"predict\(\) is deprecated"):
        runner = load_predictor_from_ref(str(model))
    assert runner.predict(text="hello") == "HELLO"


class TestBasePredictor:
    """Tests for BasePredictor class."""

    def test_subclass_can_override_predict(self) -> None:
        class MyPredictor(BasePredictor):
            def predict(self, text: str) -> str:
                return text.upper()

        predictor = MyPredictor()
        result = predictor.predict(text="hello")
        assert result == "HELLO"

    def test_default_predict_raises(self) -> None:
        predictor = BasePredictor()
        try:
            predictor.predict()
            assert False, "Should have raised NotImplementedError"
        except NotImplementedError as e:
            assert "run has not been implemented" in str(e)

    def test_setup_is_optional(self) -> None:
        class MyPredictor(BasePredictor):
            def predict(self, x: int) -> int:
                return x * 2

        predictor = MyPredictor()
        # setup() should not raise
        predictor.setup()
        assert predictor.predict(x=5) == 10

    def test_setup_with_weights(self) -> None:
        class MyPredictor(BasePredictor):
            weights_path: Optional[str] = None

            def setup(self, weights: Optional[str] = None) -> None:
                self.weights_path = weights

            def predict(self, x: int) -> int:
                return x

        predictor = MyPredictor()
        predictor.setup(weights="/path/to/weights")
        assert predictor.weights_path == "/path/to/weights"

    def test_setup_with_path_weights(self) -> None:
        class MyPredictor(BasePredictor):
            weights_path: Optional[Path] = None

            def setup(self, weights: Optional[Path] = None) -> None:
                self.weights_path = weights

            def predict(self, x: int) -> int:
                return x

        predictor = MyPredictor()
        predictor.setup(weights=Path("/path/to/weights"))
        assert str(predictor.weights_path) == "/path/to/weights"

    def test_predictor_with_multiple_inputs(self) -> None:
        class MyPredictor(BasePredictor):
            def predict(self, a: int, b: int, c: str = "default") -> str:
                return f"{a + b}: {c}"

        predictor = MyPredictor()
        result = predictor.predict(a=1, b=2, c="test")
        assert result == "3: test"

        result_default = predictor.predict(a=1, b=2)
        assert result_default == "3: default"

    def test_predictor_with_state(self) -> None:
        class StatefulPredictor(BasePredictor):
            count: int = 0

            def setup(self, weights: Optional[str] = None) -> None:
                self.count = 0

            def predict(self, x: int) -> int:
                self.count += 1
                return x * self.count

        predictor = StatefulPredictor()
        predictor.setup()
        assert predictor.predict(x=10) == 10
        assert predictor.predict(x=10) == 20
        assert predictor.predict(x=10) == 30
