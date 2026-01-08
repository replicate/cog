import os.path
import pkgutil
import tempfile
from typing import List

import pytest

from coglet import inspector, runner, scope
from tests.util import PythonVersionError


def get_predictors() -> List[str]:
    runners_dir = os.path.join(os.path.dirname(__file__), 'runners')
    return [name for _, name, _ in pkgutil.iter_modules([runners_dir])]


@pytest.mark.asyncio
@pytest.mark.parametrize('predictor', get_predictors())
async def test_test_inputs(predictor):
    module_name = f'tests.runners.{predictor}'

    entrypoint = 'Predictor'
    if predictor.startswith('function_'):
        entrypoint = 'predict'

    try:
        p = inspector.create_predictor(module_name, entrypoint)
        r = runner.Runner(p)

        # Some predictors calls current_scope() and requires ctx_pid
        scope.ctx_pid.set(predictor)

        # Use temporary directory for predictors that create files
        original_cwd = os.getcwd()
        with tempfile.TemporaryDirectory() as temp_dir:
            try:
                os.chdir(temp_dir)
                assert await r.test()
            finally:
                os.chdir(original_cwd)
    except PythonVersionError as e:
        pytest.skip(reason=str(e))
