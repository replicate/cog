import pytest

from coglet import inspector, schemas

from .test_predictors import run_fixture

predictors = [
    pytest.param('procedure', 'predict'),
    pytest.param('repetition', 'Predictor'),
]


@pytest.mark.asyncio
@pytest.mark.parametrize('module,predictor', predictors)
async def test_predictor(module: str, predictor: str):
    await run_fixture(f'tests.cases.{module}', predictor)


def test_repetition_schema():
    module_name = 'tests.cases.repetition'
    predictor_name = 'Predictor'
    p = inspector.create_predictor(module_name, predictor_name)

    schema = schemas.to_json_schema(p)
    schema_in = schema['components']['schemas']['Input']

    # Fields with default=None or missing default are required
    # Unless if type hint is `Optional[T]`
    assert set(schema_in['required']) == {'rs', 'ls', 'rd0', 'ld0'}
    for name, prop in schema_in['properties'].items():
        # x: Optional[T] implies nullable, regardless of default
        if name in {'os', 'od0', 'od1', 'od2'}:
            assert prop['nullable'] is True
        else:
            assert 'nullable' not in prop
        # Only fields with default=X where X is not None are preserved
        if name in {'rd2', 'od2', 'ld2'}:
            assert 'default' in prop
        else:
            assert 'default' not in prop
