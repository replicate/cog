"""Unit tests for the Input() function and InputSpec dataclass."""

import pytest

from coglet.api import FieldInfo, Input


class TestInputSpec:
    """Test the InputSpec dataclass."""

    def test_default_values(self):
        """Test InputSpec with all default values."""
        spec = FieldInfo()
        assert spec.default is None
        assert spec.description is None
        assert spec.ge is None
        assert spec.le is None
        assert spec.min_length is None
        assert spec.max_length is None
        assert spec.regex is None
        assert spec.choices is None
        assert spec.deprecated is None

    def test_custom_values(self):
        """Test InputSpec with custom values."""
        spec = FieldInfo(
            default='test',
            description='A test field',
            ge=1,
            le=10,
            min_length=2,
            max_length=20,
            regex=r'\d+',
            choices=['a', 'b', 'c'],
            deprecated=True,
        )
        assert spec.default == 'test'
        assert spec.description == 'A test field'
        assert spec.ge == 1
        assert spec.le == 10
        assert spec.min_length == 2
        assert spec.max_length == 20
        assert spec.regex == r'\d+'
        assert spec.choices == ['a', 'b', 'c']
        assert spec.deprecated is True

    def test_frozen_dataclass(self):
        """Test that InputSpec is frozen (immutable)."""
        spec = FieldInfo(default='test')
        with pytest.raises(AttributeError):
            spec.default = 'changed'


class TestInputFunction:
    """Test the Input() function."""

    def test_input_no_args(self):
        """Test Input() with no arguments."""
        result = Input()
        assert isinstance(result, FieldInfo)
        assert result.default is None
        assert result.description is None

    def test_input_default_only(self):
        """Test Input() with default value only."""
        result = Input('test_default')
        assert isinstance(result, FieldInfo)
        assert result.default == 'test_default'
        assert result.description is None

    def test_input_all_params(self):
        """Test Input() with all parameters."""
        result = Input(
            default=42,
            description='Test field',
            ge=0,
            le=100,
            min_length=1,
            max_length=50,
            regex=r'^\d+$',
            choices=[1, 2, 3],
            deprecated=False,
        )
        assert isinstance(result, FieldInfo)
        assert result.default == 42
        assert result.description == 'Test field'
        assert result.ge == 0
        assert result.le == 100
        assert result.min_length == 1
        assert result.max_length == 50
        assert result.regex == r'^\d+$'
        assert result.choices == [1, 2, 3]
        assert result.deprecated is False

    def test_input_keyword_only(self):
        """Test that non-default parameters are keyword-only."""
        # This should work
        result = Input(default='test', description='desc')
        assert result.default == 'test'
        assert result.description == 'desc'

        # This would be a syntax error if description wasn't keyword-only
        # Input("test", "desc")  # Would fail if not keyword-only

    def test_input_return_type_is_any(self):
        """Test that Input() has return type Any for type checkers."""
        # This is more of a static analysis test, but we can verify
        # that the function returns InputSpec at runtime
        result = Input(description='test')
        assert isinstance(result, FieldInfo)


class TestInputUsagePatterns:
    """Test common usage patterns of Input()."""

    def test_basic_field(self):
        """Test basic field definition pattern."""
        # Simulate: name: str = Input()
        field_spec = Input()
        assert isinstance(field_spec, FieldInfo)
        assert field_spec.default is None

    def test_field_with_default(self):
        """Test field with default value."""
        # Simulate: name: str = Input(default="foo")
        field_spec = Input(default='foo')
        assert field_spec.default == 'foo'

    def test_field_with_description(self):
        """Test field with description."""
        # Simulate: name: str = Input(description="User's name")
        field_spec = Input(description="User's name")
        assert field_spec.description == "User's name"

    def test_numeric_constraints(self):
        """Test numeric constraint fields."""
        # Simulate: age: int = Input(ge=0, le=120)
        field_spec = Input(ge=0, le=120)
        assert field_spec.ge == 0
        assert field_spec.le == 120

    def test_string_constraints(self):
        """Test string constraint fields."""
        # Simulate: name: str = Input(min_length=1, max_length=50)
        field_spec = Input(min_length=1, max_length=50)
        assert field_spec.min_length == 1
        assert field_spec.max_length == 50

    def test_regex_constraint(self):
        """Test regex constraint field."""
        # Simulate: email: str = Input(regex=r'.*@.*')
        field_spec = Input(regex=r'.*@.*')
        assert field_spec.regex == r'.*@.*'

    def test_choices_constraint(self):
        """Test choices constraint field."""
        # Simulate: color: str = Input(choices=['red', 'green', 'blue'])
        field_spec = Input(choices=['red', 'green', 'blue'])
        assert field_spec.choices == ['red', 'green', 'blue']

    def test_deprecated_field(self):
        """Test deprecated field."""
        # Simulate: old_param: str = Input(deprecated=True)
        field_spec = Input(deprecated=True)
        assert field_spec.deprecated is True

    def test_complex_field(self):
        """Test field with multiple constraints."""
        # Simulate: score: int = Input(
        #     default=50,
        #     description="Score between 0 and 100",
        #     ge=0,
        #     le=100
        # )
        field_spec = Input(
            default=50, description='Score between 0 and 100', ge=0, le=100
        )
        assert field_spec.default == 50
        assert field_spec.description == 'Score between 0 and 100'
        assert field_spec.ge == 0
        assert field_spec.le == 100


class TestTypeCompatibility:
    """Test that Input() works with type annotations."""

    def test_string_annotation(self):
        """Test Input() used with string type annotation."""
        # This simulates: name: str = Input()
        # The type checker should see this as valid
        field_spec = Input()
        assert isinstance(field_spec, FieldInfo)

    def test_optional_annotation(self):
        """Test Input() used with Optional type annotation."""
        # This simulates: name: Optional[str] = Input()
        field_spec = Input()
        assert isinstance(field_spec, FieldInfo)

    def test_list_annotation(self):
        """Test Input() used with List type annotation."""
        # This simulates: names: List[str] = Input()
        field_spec = Input()
        assert isinstance(field_spec, FieldInfo)

    def test_with_defaults_various_types(self):
        """Test Input() with defaults of various types."""
        str_field = Input(default='string')
        int_field = Input(default=42)
        float_field = Input(default=3.14)
        bool_field = Input(default=True)
        tuple_field = Input(default=(1, 2, 3))  # tuple is immutable
        none_field = Input(default=None)

        assert str_field.default == 'string'
        assert int_field.default == 42
        assert float_field.default == 3.14
        assert bool_field.default is True
        assert tuple_field.default == (1, 2, 3)
        assert none_field.default is None


class TestMutableDefaults:
    """Test automatic conversion of mutable defaults in Input()."""

    def test_empty_list_auto_converts(self):
        """Test that empty list automatically converts to default_factory=list."""
        from dataclasses import MISSING, Field

        field = Input(default=[])
        assert isinstance(field.default, Field)
        assert field.default.default_factory is list
        assert field.default.default is MISSING

    def test_populated_list_auto_converts(self):
        """Test that populated list automatically converts to lambda factory."""
        from dataclasses import MISSING, Field

        field = Input(default=[1, 2, 3])
        assert isinstance(field.default, Field)
        assert field.default.default is MISSING
        # Verify the factory produces the expected value
        result = field.default.default_factory()
        assert result == [1, 2, 3]
        # Verify it creates a new instance each time
        assert field.default.default_factory() is not field.default.default_factory()

    def test_empty_dict_auto_converts(self):
        """Test that empty dict automatically converts to default_factory=dict."""
        from dataclasses import MISSING, Field

        field = Input(default={})
        assert isinstance(field.default, Field)
        assert field.default.default_factory is dict
        assert field.default.default is MISSING

    def test_populated_dict_auto_converts(self):
        """Test that populated dict automatically converts to lambda factory."""
        from dataclasses import MISSING, Field

        field = Input(default={'key': 'value'})
        assert isinstance(field.default, Field)
        assert field.default.default is MISSING
        # Verify the factory produces the expected value
        result = field.default.default_factory()
        assert result == {'key': 'value'}
        # Verify it creates a new instance each time
        assert field.default.default_factory() is not field.default.default_factory()

    def test_empty_set_auto_converts(self):
        """Test that empty set automatically converts to default_factory=set."""
        from dataclasses import MISSING, Field

        field = Input(default=set())
        assert isinstance(field.default, Field)
        assert field.default.default_factory is set
        assert field.default.default is MISSING

    def test_populated_set_auto_converts(self):
        """Test that populated set automatically converts to lambda factory."""
        from dataclasses import MISSING, Field

        field = Input(default={1, 2})
        assert isinstance(field.default, Field)
        assert field.default.default is MISSING
        # Verify the factory produces the expected value
        result = field.default.default_factory()
        assert result == {1, 2}
        # Verify it creates a new instance each time
        assert field.default.default_factory() is not field.default.default_factory()

    def test_custom_object_auto_converts(self):
        """Test that custom objects automatically convert to lambda factory."""
        from dataclasses import MISSING, Field

        class CustomObject:
            def __init__(self, value):
                self.value = value

            def __repr__(self):
                return f'CustomObject({self.value})'

            def __eq__(self, other):
                return isinstance(other, CustomObject) and self.value == other.value

        obj = CustomObject(42)
        field = Input(default=obj)
        assert isinstance(field.default, Field)
        assert field.default.default is MISSING
        # Verify the factory produces the expected value
        result = field.default.default_factory()
        assert result == obj
        # Verify it creates a new instance each time
        assert field.default.default_factory() is not field.default.default_factory()

    def test_immutable_defaults_allowed(self):
        """Test that immutable types are allowed as defaults."""
        from enum import Enum

        from cog import Path, Secret

        class TestEnum(Enum):
            VALUE = 'test'

        # These should not raise errors
        Input(default='string')
        Input(default=42)
        Input(default=3.14)
        Input(default=True)
        Input(default=False)
        Input(default=None)
        Input(default=(1, 2, 3))  # tuple
        Input(default=frozenset([1, 2, 3]))
        Input(default=b'bytes')
        Input(default=Path('test.txt'))  # Cog Path
        Input(default=Secret('secret'))  # Cog Secret
        Input(default=TestEnum.VALUE)  # Enum

    def test_default_factory_works(self):
        """Test that default_factory parameter works correctly."""
        from dataclasses import MISSING, Field

        # Test with empty list factory
        field = Input(default_factory=list)
        assert isinstance(field.default, Field)
        assert field.default.default_factory is list
        assert field.default.default is MISSING

        # Test with lambda factory
        def func():
            return [1, 2, 3]

        field = Input(default_factory=func)
        assert isinstance(field.default, Field)
        assert field.default.default_factory is func
        assert field.default.default is MISSING

    def test_default_and_default_factory_mutual_exclusion(self):
        """Test that default and default_factory are mutually exclusive."""
        with pytest.raises(
            ValueError,
            match="Cannot specify both 'default' and 'default_factory' parameters",
        ):
            Input(default='value', default_factory=lambda: 'other')
