import importlib
import os.path
import pkgutil
import re
from typing import List

import pytest

from coglet import inspector, runner


def get_predictors() -> List[str]:
    bad_inputs_dir = os.path.join(os.path.dirname(__file__), 'bad_inputs')
    return [name for _, name, _ in pkgutil.iter_modules([bad_inputs_dir])]


async def run(module_name: str, predictor_name: str) -> None:
    m = importlib.import_module(module_name)
    bad_inputs = getattr(m, 'BAD_INPUTS')
    p = inspector.create_predictor(module_name, predictor_name)
    r = runner.Runner(p)
    await r.setup()
    for inputs, err_msg in bad_inputs:
        with pytest.raises(AssertionError, match=re.escape(err_msg)):
            await r.predict(inputs)


@pytest.mark.asyncio
@pytest.mark.parametrize('predictor', get_predictors())
async def test_bad_inputs(predictor):
    module_name = f'tests.bad_inputs.{predictor}'
    await run(module_name, 'Predictor')
