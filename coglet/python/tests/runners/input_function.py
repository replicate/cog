"""Test predictor specifically for testing the Input() function changes in Go test hardness."""

from typing import Optional

from cog import BasePredictor, Input


class Predictor(BasePredictor):
    """Test predictor demonstrating Input() function usage."""

    test_inputs = {
        'message': 'hello world',
        'repeat_count': 2,
        'format_type': 'uppercase',
        'prefix': 'Test: ',
    }

    def predict(
        self,
        # Basic required field
        message: str = Input(description='Message to process'),
        # Field with default
        repeat_count: int = Input(
            default=1, description='Number of times to repeat message', ge=1, le=10
        ),
        # Field with choices
        format_type: str = Input(
            default='plain',
            description='Output format',
            choices=['plain', 'uppercase', 'title'],
        ),
        # String constraints
        prefix: str = Input(
            default='Result: ',
            description='Prefix for output',
            min_length=1,
            max_length=20,
        ),
        # Optional field
        suffix: Optional[str] = Input(
            default=None, description='Optional suffix for output'
        ),
        # Deprecated field
        deprecated_option: str = Input(
            default='deprecated_value',
            description='This field is deprecated',
            deprecated=True,
        ),
    ) -> str:
        """Process the message according to the input parameters."""

        # Apply formatting
        if format_type == 'uppercase':
            processed_message = message.upper()
        elif format_type == 'title':
            processed_message = message.title()
        else:
            processed_message = message

        # Repeat the message
        repeated = (processed_message + ' ') * repeat_count
        repeated = repeated.rstrip()

        # Add prefix and suffix
        result = prefix + repeated
        if suffix:
            result += suffix

        return result
