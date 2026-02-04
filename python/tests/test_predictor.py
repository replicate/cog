"""Tests for cog.predictor module (BasePredictor)."""

from typing import Optional

from cog import BasePredictor, Path


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
            assert "predict has not been implemented" in str(e)

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
