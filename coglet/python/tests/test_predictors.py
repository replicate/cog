import importlib
import json
import os.path
import pkgutil
from typing import List

import pytest

from coglet import inspector, runner, schemas, util

# Test predictors in tests/schemas
# * run prediction with input/output fixture
# * produce same Open API schema as CogPy


def get_predictors() -> List[str]:
    schemas_dir = os.path.join(os.path.dirname(__file__), 'schemas')
    return [name for _, name, _ in pkgutil.iter_modules([schemas_dir])]


async def run_fixture(module_name: str, predictor_name: str) -> None:
    p = inspector.create_predictor(module_name, predictor_name)
    r = runner.Runner(p)

    # Only check setup_done if it's defined on the class
    has_setup_done = hasattr(r.predictor.__class__, 'setup_done')
    if has_setup_done:
        assert not getattr(r.predictor, 'setup_done', None)

    await r.setup()

    if has_setup_done:
        assert getattr(r.predictor, 'setup_done', None)

    m = importlib.import_module(module_name)
    fixture = getattr(m, 'FIXTURE')
    for inputs, output in fixture:
        if r.is_iter():
            result = [x async for x in r.predict_iter(inputs)]
            assert result == output
        else:
            result = await r.predict(inputs)
            assert result == output


@pytest.mark.asyncio
@pytest.mark.parametrize('predictor', get_predictors())
async def test_predictor(predictor):
    module_name = f'tests.schemas.{predictor}'
    await run_fixture(module_name, 'Predictor')


@pytest.mark.parametrize('predictor', get_predictors())
def test_schema(predictor):
    module_name = f'tests.schemas.{predictor}'
    predictor_name = 'Predictor'
    p = inspector.create_predictor(module_name, predictor_name)

    path = os.path.join(os.path.dirname(__file__), 'schemas', f'{predictor}.json')
    with open(path, 'r') as f:
        schema = json.load(f)

    # Compat: Cog handles secret differently
    if predictor == 'secret':
        props = schema['components']['schemas']['Input']['properties']
        # Default Secret should be redacted
        props['s3']['default'] = '**********'
        # List[Secret] missing defaults
        props['ss']['default'] = ['**********', '**********']

    # Round trip schema to encode Path & Secret
    def rt(s):
        return json.loads(json.dumps(s, default=util.schema_json))

    assert rt(schemas.to_json_input(p)) == schema['components']['schemas']['Input']
    assert rt(schemas.to_json_output(p)) == schema['components']['schemas']['Output']

    assert rt(schemas.to_json_schema(p)) == schema


@pytest.mark.asyncio
async def test_output_complex_types():
    """Test that output models can have complex nested types when assertion is removed."""
    module_name = 'tests.schemas.output_complex_types'
    predictor_name = 'Predictor'
    await run_fixture(module_name, predictor_name)
