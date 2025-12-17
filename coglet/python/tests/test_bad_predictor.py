import importlib
import os.path
import pkgutil
import re
from typing import List

import pytest

from coglet import inspector
from tests.util import PythonVersionError


def get_predictors() -> List[str]:
    bad_predictors_dir = os.path.join(os.path.dirname(__file__), 'bad_predictors')
    return [name for _, name, _ in pkgutil.iter_modules([bad_predictors_dir])]


def run(module_name: str, predictor_name: str) -> None:
    try:
        m = importlib.import_module(module_name)
        err_msg = getattr(m, 'ERROR')
        with pytest.raises(AssertionError, match=re.escape(err_msg)):
            inspector.create_predictor(module_name, predictor_name)
    except PythonVersionError as e:
        pytest.skip(reason=str(e))


@pytest.mark.parametrize('predictor', get_predictors())
def test_bad_predictor(predictor):
    module_name = f'tests.bad_predictors.{predictor}'
    if predictor.startswith('procedure_'):
        run(module_name, 'run')
    else:
        run(module_name, 'Predictor')
