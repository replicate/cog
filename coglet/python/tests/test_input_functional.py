"""Functional tests for Input() with real predictors and schema generation."""

from typing import List, Optional

import pytest

from coglet import inspector, schemas
from coglet.api import BasePredictor, FieldInfo, Input


class TestInputWithPredictors:
    """Test Input() function with real predictor classes."""

    def test_basic_predictor_with_input(self):
        """Test predictor with basic Input() fields."""

        class TestPredictor(BasePredictor):
            def predict(
                self,
                message: str = Input(description='Message to process'),
                count: int = Input(default=1, description='Number of times to repeat'),
            ) -> str:
                return message * count

        # Test that predictor can be inspected
        pred = inspector._predictor_adt(
            'test_module', 'TestPredictor', TestPredictor.predict, 'predict', True
        )

        # Check inputs were processed correctly
        assert len(pred.inputs) == 2
        assert 'message' in pred.inputs
        assert 'count' in pred.inputs

        # Check message field
        message_input = pred.inputs['message']
        assert message_input.description == 'Message to process'
        assert message_input.default is None

        # Check count field
        count_input = pred.inputs['count']
        assert count_input.description == 'Number of times to repeat'
        assert count_input.default == 1

    def test_predictor_with_constraints(self):
        """Test predictor with Input() validation constraints."""

        class TestPredictor(BasePredictor):
            def predict(
                self,
                temperature: float = Input(
                    default=0.7,
                    description='Temperature for generation',
                    ge=0.0,
                    le=2.0,
                ),
                max_tokens: int = Input(
                    default=100, description='Maximum tokens to generate', ge=1, le=2000
                ),
                prompt: str = Input(
                    description='Input prompt', min_length=1, max_length=1000
                ),
            ) -> str:
                return f"Generated with temp={temperature}, max_tokens={max_tokens}, prompt='{prompt}'"

        pred = inspector._predictor_adt(
            'test_module', 'TestPredictor', TestPredictor.predict, 'predict', True
        )

        # Check temperature constraints
        temp_input = pred.inputs['temperature']
        assert temp_input.ge == 0.0
        assert temp_input.le == 2.0
        assert temp_input.default == 0.7

        # Check max_tokens constraints
        tokens_input = pred.inputs['max_tokens']
        assert tokens_input.ge == 1
        assert tokens_input.le == 2000
        assert tokens_input.default == 100

        # Check prompt constraints
        prompt_input = pred.inputs['prompt']
        assert prompt_input.min_length == 1
        assert prompt_input.max_length == 1000
        assert prompt_input.default is None

    def test_predictor_with_choices(self):
        """Test predictor with Input() choices constraint."""

        class TestPredictor(BasePredictor):
            def predict(
                self,
                model: str = Input(
                    default='gpt-3.5-turbo',
                    description='Model to use',
                    choices=['gpt-3.5-turbo', 'gpt-4', 'claude-3'],
                ),
                format: str = Input(
                    description='Output format', choices=['json', 'text', 'markdown']
                ),
            ) -> str:
                return f'Using {model} with {format} format'

        pred = inspector._predictor_adt(
            'test_module', 'TestPredictor', TestPredictor.predict, 'predict', True
        )

        # Check model choices
        model_input = pred.inputs['model']
        assert model_input.choices == ['gpt-3.5-turbo', 'gpt-4', 'claude-3']
        assert model_input.default == 'gpt-3.5-turbo'

        # Check format choices
        format_input = pred.inputs['format']
        assert format_input.choices == ['json', 'text', 'markdown']
        assert format_input.default is None

    def test_predictor_with_optional_types(self):
        """Test predictor with Optional type annotations."""

        class TestPredictor(BasePredictor):
            def predict(
                self,
                required_input: str = Input(description='Required field'),
                optional_input: Optional[str] = Input(description='Optional field'),
                optional_with_default: Optional[str] = Input(
                    default='default_value', description='Optional with default'
                ),
            ) -> str:
                return f'Required: {required_input}, Optional: {optional_input}, Default: {optional_with_default}'

        pred = inspector._predictor_adt(
            'test_module', 'TestPredictor', TestPredictor.predict, 'predict', True
        )

        # Check required input
        required = pred.inputs['required_input']
        assert required.default is None
        assert required.description == 'Required field'

        # Check optional input
        optional = pred.inputs['optional_input']
        assert optional.default is None
        assert optional.description == 'Optional field'

        # Check optional with default
        optional_default = pred.inputs['optional_with_default']
        assert optional_default.default == 'default_value'
        assert optional_default.description == 'Optional with default'

    def test_predictor_with_list_types(self):
        """Test predictor with List type annotations."""

        class TestPredictor(BasePredictor):
            def predict(
                self,
                tags: List[str] = Input(
                    default_factory=lambda: ['default'], description='List of tags'
                ),
                numbers: List[int] = Input(description='List of numbers'),
            ) -> str:
                return f'Tags: {tags}, Numbers: {numbers}'

        pred = inspector._predictor_adt(
            'test_module', 'TestPredictor', TestPredictor.predict, 'predict', True
        )

        # Check tags list
        from dataclasses import Field

        tags_input = pred.inputs['tags']
        assert isinstance(tags_input.default, Field)
        assert tags_input.default.default_factory() == ['default']
        assert tags_input.description == 'List of tags'

        # Check numbers list
        numbers_input = pred.inputs['numbers']
        assert numbers_input.default is None
        assert numbers_input.description == 'List of numbers'


class TestSchemaGeneration:
    """Test JSON schema generation with Input() fields."""

    def test_schema_generation_with_input(self):
        """Test that JSON schema is generated correctly from Input() fields."""

        class TestPredictor(BasePredictor):
            def predict(
                self,
                text: str = Input(
                    description='Input text to process', min_length=1, max_length=1000
                ),
                temperature: float = Input(
                    default=0.7, description='Temperature parameter', ge=0.0, le=2.0
                ),
                model: str = Input(
                    default='base',
                    description='Model to use',
                    choices=['base', 'large', 'xl'],
                ),
                deprecated_field: str = Input(
                    default='old_value',
                    description='This field is deprecated',
                    deprecated=True,
                ),
            ) -> str:
                return 'processed'

        pred = inspector._predictor_adt(
            'test_module', 'TestPredictor', TestPredictor.predict, 'predict', True
        )

        # Generate JSON schema
        schema = schemas.to_json_schema(pred)

        # Check overall structure
        assert 'openapi' in schema
        assert 'components' in schema
        assert 'schemas' in schema['components']
        assert 'Input' in schema['components']['schemas']

        input_schema = schema['components']['schemas']['Input']
        assert input_schema['type'] == 'object'
        assert 'properties' in input_schema

        # Check text field schema
        text_prop = input_schema['properties']['text']
        assert text_prop['type'] == 'string'
        assert text_prop['title'] == 'Text'
        assert text_prop['description'] == 'Input text to process'
        assert text_prop['minLength'] == 1
        assert text_prop['maxLength'] == 1000

        # Check temperature field schema
        temp_prop = input_schema['properties']['temperature']
        assert temp_prop['type'] == 'number'
        assert temp_prop['title'] == 'Temperature'
        assert temp_prop['description'] == 'Temperature parameter'
        assert temp_prop['minimum'] == 0.0
        assert temp_prop['maximum'] == 2.0
        assert temp_prop['default'] == 0.7

        # Check model field schema (choices create separate enum schema)
        model_prop = input_schema['properties']['model']
        assert model_prop['default'] == 'base'
        # For choices fields, the enum is in a separate schema referenced by allOf
        if 'allOf' in model_prop:
            assert '$ref' in model_prop['allOf'][0]
            # The actual enum schema should be in the components
            assert 'model' in schema['components']['schemas']
            model_enum_schema = schema['components']['schemas']['model']
            assert model_enum_schema['enum'] == ['base', 'large', 'xl']

        # Check deprecated field
        deprecated_prop = input_schema['properties']['deprecated_field']
        assert deprecated_prop['deprecated'] is True
        assert deprecated_prop['description'] == 'This field is deprecated'
        assert deprecated_prop['default'] == 'old_value'

        # Check required fields
        assert 'text' in input_schema['required']
        assert 'temperature' not in input_schema['required']  # has default
        assert 'model' not in input_schema['required']  # has default
        assert 'deprecated_field' not in input_schema['required']  # has default


class TestInputValidation:
    """Test input validation with Input() constraints."""

    def test_validation_with_constraints(self):
        """Test that input validation works with Input() constraints."""

        class TestPredictor(BasePredictor):
            def predict(
                self,
                score: int = Input(ge=0, le=100),
                name: str = Input(min_length=2, max_length=50),
                category: str = Input(choices=['A', 'B', 'C']),
            ) -> str:
                return f'Score: {score}, Name: {name}, Category: {category}'

        pred = inspector._predictor_adt(
            'test_module', 'TestPredictor', TestPredictor.predict, 'predict', True
        )

        # Test valid inputs
        valid_inputs = {'score': 85, 'name': 'John', 'category': 'A'}
        result = inspector.check_input(pred.inputs, valid_inputs)
        assert result['score'] == 85
        assert result['name'] == 'John'
        assert result['category'] == 'A'

        # Test invalid score (too high)
        with pytest.raises(AssertionError, match='fails constraint <= 100'):
            inspector.check_input(
                pred.inputs, {'score': 150, 'name': 'John', 'category': 'A'}
            )

        # Test invalid score (too low)
        with pytest.raises(AssertionError, match='fails constraint >= 0'):
            inspector.check_input(
                pred.inputs, {'score': -5, 'name': 'John', 'category': 'A'}
            )

        # Test invalid name (too short)
        with pytest.raises(AssertionError, match='fails constraint len\\(\\) >= 2'):
            inspector.check_input(
                pred.inputs, {'score': 85, 'name': 'J', 'category': 'A'}
            )

        # Test invalid name (too long)
        long_name = 'J' * 51
        with pytest.raises(AssertionError, match='fails constraint len\\(\\) <= 50'):
            inspector.check_input(
                pred.inputs, {'score': 85, 'name': long_name, 'category': 'A'}
            )

        # Test invalid category (not in choices)
        with pytest.raises(AssertionError, match='does not match choices'):
            inspector.check_input(
                pred.inputs, {'score': 85, 'name': 'John', 'category': 'D'}
            )

    def test_validation_with_regex(self):
        """Test input validation with regex constraints."""

        class TestPredictor(BasePredictor):
            def predict(
                self,
                email: str = Input(
                    regex=r'^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$'
                ),
                phone: str = Input(regex=r'^\d{3}-\d{3}-\d{4}$'),
            ) -> str:
                return f'Email: {email}, Phone: {phone}'

        pred = inspector._predictor_adt(
            'test_module', 'TestPredictor', TestPredictor.predict, 'predict', True
        )

        # Test valid inputs
        valid_inputs = {'email': 'test@example.com', 'phone': '555-123-4567'}
        result = inspector.check_input(pred.inputs, valid_inputs)
        assert result['email'] == 'test@example.com'
        assert result['phone'] == '555-123-4567'

        # Test invalid email
        with pytest.raises(AssertionError, match='does not match regex'):
            inspector.check_input(
                pred.inputs, {'email': 'invalid-email', 'phone': '555-123-4567'}
            )

        # Test invalid phone
        with pytest.raises(AssertionError, match='does not match regex'):
            inspector.check_input(
                pred.inputs, {'email': 'test@example.com', 'phone': '555-1234567'}
            )


class TestBackwardCompatibility:
    """Test that Input() is backward compatible with existing code."""

    def test_import_compatibility(self):
        """Test that Input can still be imported the same way."""
        from cog import Input as CogInput
        from coglet.api import Input

        # Both should work and be the same function
        assert Input is CogInput

        # Test they both return InputSpec instances
        spec1 = Input(description='test')
        spec2 = CogInput(description='test')

        assert isinstance(spec1, FieldInfo)
        assert isinstance(spec2, FieldInfo)
        assert spec1.description == spec2.description

    def test_existing_predictor_patterns(self):
        """Test that existing predictor patterns still work."""

        # This mimics existing test patterns from the codebase
        class Predictor(BasePredictor):
            def predict(
                self,
                b: bool = Input(default=False),
                f: float = Input(default=0.0),
                i: int = Input(default=0),
                s: str = Input(default='foo'),
            ) -> str:
                return f'{b},{f:.2f},{i},{s}'

        # Test inspection works
        pred = inspector._predictor_adt(
            'test', 'Predictor', Predictor.predict, 'predict', True
        )

        # Test defaults are preserved
        assert pred.inputs['b'].default is False
        assert pred.inputs['f'].default == 0.0
        assert pred.inputs['i'].default == 0
        assert pred.inputs['s'].default == 'foo'

        # Test validation works with defaults
        result = inspector.check_input(pred.inputs, {})
        assert result == {'b': False, 'f': 0.0, 'i': 0, 's': 'foo'}
