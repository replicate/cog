from dataclasses import dataclass, is_dataclass

import pytest

from cog import BaseModel


def test_simple_basemodel_inheritance() -> None:
    """Test that simple BaseModel subclasses become dataclasses automatically."""

    class SimpleOutput(BaseModel):
        x: int
        y: str

    assert is_dataclass(SimpleOutput)

    # Test instantiation and field access
    output = SimpleOutput(x=1, y='test')
    assert output.x == 1
    assert output.y == 'test'

    # Test dataclass features
    assert "SimpleOutput(x=1, y='test')" in repr(output)
    assert output == SimpleOutput(x=1, y='test')


def test_auto_dataclass_false_disables_auto() -> None:
    """Test auto_dataclass=False disables automatic dataclass application."""

    class ManualControl(BaseModel, auto_dataclass=False):
        def __init__(self, value: str) -> None:
            self.value = value

    assert not is_dataclass(ManualControl)
    assert hasattr(ManualControl, '_BaseModel__auto_dataclass')
    assert ManualControl._BaseModel__auto_dataclass is False

    # Should work with custom init
    obj = ManualControl('test')
    assert obj.value == 'test'


def test_custom_init_with_default_init_true() -> None:
    """Test that custom __init__ works with init=True (default behavior)."""

    class CustomInitOutput(BaseModel):  # init=True by default
        x: int
        y: str
        computed: int

        def __init__(self, x: int, y: str) -> None:
            self.x = x
            self.y = y
            self.computed = x * len(y)  # Custom logic

    assert is_dataclass(CustomInitOutput)

    # Should use custom init, dataclass init should be ignored per dataclass docs
    output = CustomInitOutput(5, 'hello')
    assert output.x == 5
    assert output.y == 'hello'
    assert output.computed == 25  # 5 * len("hello")

    # Dataclass features should still work
    assert hasattr(output, '__dataclass_fields__')

    # Standard dataclass constructor should NOT work since custom __init__ exists
    with pytest.raises(TypeError):
        CustomInitOutput(x=1, y='test', computed=42)


def test_init_false_custom_init() -> None:
    """Test init=False allows custom __init__ while keeping dataclass features."""

    class CustomInitOutput(BaseModel, init=False):
        x: int
        y: str

        def __init__(self, combined_value: str) -> None:
            parts = combined_value.split(',')
            self.x = int(parts[0])
            self.y = parts[1]

    assert is_dataclass(CustomInitOutput)

    # Should use custom init
    output = CustomInitOutput('42,hello')
    assert output.x == 42
    assert output.y == 'hello'

    # Dataclass features should still work (except auto-generated init)
    assert hasattr(output, '__dataclass_fields__')


def test_inheritance_chain_all_auto() -> None:
    """Test that inheritance chains work when all use auto dataclass."""

    class BaseOutput(BaseModel):
        x: int

    class ChildOutput(BaseOutput):
        y: str

    class GrandchildOutput(ChildOutput):
        z: float

    # All should be dataclasses
    assert is_dataclass(BaseOutput)
    assert is_dataclass(ChildOutput)
    assert is_dataclass(GrandchildOutput)

    # Test field inheritance
    grandchild = GrandchildOutput(x=1, y='test', z=3.14)
    assert grandchild.x == 1
    assert grandchild.y == 'test'
    assert grandchild.z == 3.14


def test_disabled_inheritance_propagation() -> None:
    """Test that auto_dataclass=False propagates to children."""

    class DisabledParent(BaseModel, auto_dataclass=False):
        def __init__(self, value: str) -> None:
            self.value = value

    # Child should also be disabled
    class DisabledChild(DisabledParent, auto_dataclass=False):
        def __init__(self, value: str, extra: str) -> None:
            super().__init__(value)
            self.extra = extra

    # Grandchild should also remain disabled
    class DisabledGrandchild(DisabledChild, auto_dataclass=False):
        def __init__(self, value: str, extra: str, more: str) -> None:
            super().__init__(value, extra)
            self.more = more

    assert not is_dataclass(DisabledParent)
    assert not is_dataclass(DisabledChild)
    assert not is_dataclass(DisabledGrandchild)

    # All should have the auto_dataclass flag set to False
    assert DisabledParent._BaseModel__auto_dataclass is False  # type: ignore[attr-defined]
    assert DisabledChild._BaseModel__auto_dataclass is False  # type: ignore[attr-defined]
    assert DisabledGrandchild._BaseModel__auto_dataclass is False  # type: ignore[attr-defined]


def test_dangerous_disable_after_auto_parent() -> None:
    """Test that disabling auto_dataclass after parent was auto-dataclassed raises error."""

    class AutoParent(BaseModel):
        x: int
        y: str

    assert is_dataclass(AutoParent)
    assert AutoParent._BaseModel__auto_dataclass is True  # type: ignore[attr-defined]

    # This should raise an error - dangerous inheritance pattern
    with pytest.raises(
        ValueError, match='has auto_dataclass=True, but.*has auto_dataclass=False'
    ):

        class DangerousChild(AutoParent, auto_dataclass=False):
            def __init__(self, custom_value: str) -> None:
                self.custom_value = custom_value


def test_safe_disabled_inheritance() -> None:
    """Test that disabled inheritance works when parent was also disabled."""

    class DisabledBase(BaseModel, auto_dataclass=False):
        def __init__(self, x: int) -> None:
            self.x = x

    # Child of disabled parent can also be disabled - this is safe
    class DisabledChild(DisabledBase, auto_dataclass=False):
        def __init__(self, x: int, y: str) -> None:
            super().__init__(x)
            self.y = y

    assert not is_dataclass(DisabledBase)
    assert not is_dataclass(DisabledChild)
    assert DisabledBase._BaseModel__auto_dataclass is False  # type: ignore[attr-defined]
    assert DisabledChild._BaseModel__auto_dataclass is False  # type: ignore[attr-defined]

    child = DisabledChild(1, 'test')
    assert child.x == 1
    assert child.y == 'test'


def test_manual_dataclass_with_basemodel() -> None:
    """Test that manually applied @dataclass works with BaseModel."""

    @dataclass
    class ManualOutput(BaseModel):
        x: int
        y: str = 'default'

    assert is_dataclass(ManualOutput)

    # Should work normally
    output = ManualOutput(x=5)
    assert output.x == 5
    assert output.y == 'default'

    # Child of manual dataclass should get auto dataclass
    class ChildOfManual(ManualOutput):
        z: int = 0  # Must have default since parent has default field

    assert is_dataclass(ChildOfManual)
    # mypy doesn't understand that BaseModel metaclass creates proper dataclass constructors
    child = ChildOfManual(x=1, y='test', z=42)  # type: ignore[call-arg]
    assert child.z == 42


def test_dataclass_kwargs_passthrough() -> None:
    """Test that kwargs are passed to dataclass decorator."""

    class FrozenOutput(BaseModel, frozen=True):
        x: int
        y: str

    assert is_dataclass(FrozenOutput)

    output = FrozenOutput(x=1, y='test')

    # Should be frozen
    with pytest.raises(Exception):  # FrozenInstanceError
        output.x = 2


def test_complex_inheritance_scenarios() -> None:
    """Test complex real-world inheritance patterns."""

    # Auto base
    class BaseResult(BaseModel):
        success: bool

    # Auto child with init=False
    class ProcessResult(BaseResult, init=False):
        process_id: int

        def __init__(self, success: bool, process_id: int) -> None:
            self.success = success
            self.process_id = process_id * 10  # Custom logic

    # Auto grandchild
    class DetailedResult(ProcessResult):
        details: str

        def __init__(self, success: bool, process_id: int, details: str) -> None:
            super().__init__(success, process_id)
            self.details = details.upper()  # Custom logic

    assert is_dataclass(BaseResult)
    assert is_dataclass(ProcessResult)
    assert is_dataclass(DetailedResult)

    # Test the chain without the dangerous disable
    result = DetailedResult(True, 5, 'info')
    assert result.success is True
    assert result.process_id == 50  # 5 * 10 from ProcessResult
    assert result.details == 'INFO'  # uppercased from DetailedResult


def test_complex_disabled_inheritance() -> None:
    """Test complex inheritance with disabled auto_dataclass from the start."""

    # Start with disabled base
    class DisabledBase(BaseModel, auto_dataclass=False):
        def __init__(self, success: bool) -> None:
            self.success = success

    # Child with disabled can add custom logic
    class DisabledChild(DisabledBase, auto_dataclass=False):
        def __init__(self, success: bool, process_id: int) -> None:
            super().__init__(success)
            self.process_id = process_id * 10

    # Grandchild still disabled
    class DisabledGrandchild(DisabledChild, auto_dataclass=False):
        def __init__(self, success: bool, process_id: int, extra: str) -> None:
            super().__init__(success, process_id)
            self.extra = extra

    assert not is_dataclass(DisabledBase)
    assert not is_dataclass(DisabledChild)
    assert not is_dataclass(DisabledGrandchild)

    result = DisabledGrandchild(True, 5, 'bonus')
    assert result.success is True
    assert result.process_id == 50
    assert result.extra == 'bonus'


def test_no_fields_class() -> None:
    """Test BaseModel subclass with no fields."""

    class EmptyOutput(BaseModel):
        pass

    assert is_dataclass(EmptyOutput)

    # Should be instantiable
    output = EmptyOutput()
    assert isinstance(output, EmptyOutput)


def test_invalid_primary_base() -> None:
    """Test that primary base must inherit from BaseModel."""

    class RegularClass:
        pass

    with pytest.raises(
        TypeError, match='Primary base class.*must inherit from BaseModel'
    ):

        class BadOutput(RegularClass, BaseModel):
            x: int


def test_dataclass_mixin_prevention() -> None:
    """Test that dataclass mixins are prevented."""

    @dataclass
    class DataclassMixin:
        shared_field: str

    with pytest.raises(
        TypeError, match='Cannot mixin dataclass.*while inheriting from.*BaseModel'
    ):

        class BadOutput(BaseModel, DataclassMixin):
            x: int


def test_auto_dataclass_tracking() -> None:
    """Test that __auto_dataclass attribute is set correctly."""

    class AutoOutput(BaseModel):
        x: int

    class DisabledOutput(BaseModel, auto_dataclass=False):
        def __init__(self, value: str) -> None:
            self.value = value

    assert is_dataclass(AutoOutput)
    assert AutoOutput._BaseModel__auto_dataclass is True  # type: ignore[attr-defined]

    assert not is_dataclass(DisabledOutput)
    assert DisabledOutput._BaseModel__auto_dataclass is False  # type: ignore[attr-defined]
