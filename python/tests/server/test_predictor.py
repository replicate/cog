import inspect
import os
import sys

import pytest
from cog.predictor import (
    get_predict,
    get_predictor,
    load_full_predictor_from_file,
    load_slim_predictor_from_file,
)

PREDICTOR_FIXTURES = [
    ("input_choices", "Predictor", "predict"),
    ("input_choices_integer", "Predictor", "predict"),
    ("input_file", "Predictor", "predict"),
    ("function", "predict", "predict"),
    ("input_ge_le", "Predictor", "predict"),
    ("input_integer", "Predictor", "predict"),
    ("input_integer_default", "Predictor", "predict"),
    ("input_multiple", "Predictor", "predict"),
    ("input_none", "Predictor", "predict"),
    ("input_path", "Predictor", "predict"),
    ("input_path_2", "Predictor", "predict"),
    ("input_string", "Predictor", "predict"),
    ("input_union_integer_or_list_of_integers", "Predictor", "predict"),
    ("input_union_string_or_list_of_strings", "Predictor", "predict"),
    ("complex_output", "Predictor", "predict"),
    ("output_complex", "Predictor", "predict"),
    ("output_file_named", "Predictor", "predict"),
    ("output_file", "Predictor", "predict"),
    ("output_path_image", "Predictor", "predict"),
    ("output_path_text", "Predictor", "predict"),
    ("output_numpy", "Predictor", "predict"),
    ("output_iterator_complex", "Predictor", "predict"),
    ("yield_concatenate_iterator", "Predictor", "predict"),
    ("yield_files", "Predictor", "predict"),
    ("yield_strings_file_input", "Predictor", "predict"),
    ("yield_strings", "Predictor", "predict"),
]


def _fixture_path(name):
    test_dir = os.path.dirname(os.path.realpath(__file__))
    return os.path.join(test_dir, f"fixtures/{name}.py")


@pytest.mark.skipif(sys.version_info < (3, 9), reason="Requires Python 3.9 or newer")
@pytest.mark.parametrize("fixture_name, class_name, method_name", PREDICTOR_FIXTURES)
def test_fast_slow_signatures(fixture_name, class_name, method_name):
    module_path = _fixture_path(fixture_name)
    # get signature from FAST loader
    module_fast = load_slim_predictor_from_file(module_path, class_name, method_name)
    assert hasattr(module_fast, class_name)
    predictor_fast = get_predictor(module_fast, class_name)
    predict_fast = get_predict(predictor_fast)
    signature_fast = inspect.signature(predict_fast)
    # get signature from SLOW loader
    module_slow = load_full_predictor_from_file(module_path, module_fast.__name__)
    assert hasattr(module_slow, class_name)
    predictor_slow = get_predictor(module_slow, class_name)
    predict_slow = get_predict(predictor_slow)
    signature_slow = inspect.signature(predict_slow)
    # compare predict signatures using str representation (good enough) as some custom Fields do not have __eq__
    assert str(signature_fast) == str(signature_slow)
