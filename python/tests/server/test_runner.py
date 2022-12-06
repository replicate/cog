import os
import pytest
import time

from cog.schema import PredictionRequest
from cog.server.runner import PredictionRunner


def _fixture_path(name):
    test_dir = os.path.dirname(os.path.realpath(__file__))
    return os.path.join(test_dir, f"fixtures/{name}.py") + ":Predictor"


# FIXME: this needs more tests!
def test_prediction_runner():
    runner = PredictionRunner(predictor_ref=_fixture_path("sleep"))
    try:
        runner.setup()
        request = PredictionRequest(input={"sleep": 0.1})
        async_result = runner.predict(request)
        response = async_result.get(timeout=1)
        assert response.output == "done in 0.1 seconds"
    finally:
        runner.shutdown()
