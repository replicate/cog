"""Simple test runner for Input() function validation."""

from cog import BasePredictor, Input


class Predictor(BasePredictor):
    """Simple test predictor for validating Input() function works correctly."""

    test_inputs = {
        'message': 'test',
        'count': 3,
    }

    def predict(
        self,
        message: str = Input(description='Test message'),
        count: int = Input(default=1, description='Repeat count', ge=1, le=5),
    ) -> str:
        """Simple predictor that repeats a message."""
        return message * count
