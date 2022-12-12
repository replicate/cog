import os
import pytest
import time
from unittest import mock

from cog.schema import PredictionRequest, PredictionResponse
from cog.server.eventtypes import (
    Done,
    Heartbeat,
    Log,
    PredictionOutput,
    PredictionOutputType,
)
from cog.server.runner import PredictionRunner, predict


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


# list of (events, calls)
PREDICT_TESTS = [
    ([Heartbeat()], []),
    ([Done()], [mock.call.succeeded()]),
    ([Done(canceled=True)], [mock.call.canceled()]),
    ([Done(error=True, error_detail="foo")], [mock.call.failed(error="foo")]),
    ([Log(source="stdout", message="help")], [mock.call.append_logs("help")]),
    (
        [PredictionOutputType(multi=False), PredictionOutput(payload="hello world")],
        [mock.call.set_output("hello world")],
    ),
    (
        [
            PredictionOutputType(multi=True),
            PredictionOutput(payload="hello"),
            PredictionOutput(payload="world"),
        ],
        [
            mock.call.set_output([]),
            mock.call.append_output("hello"),
            mock.call.append_output("world"),
        ],
    ),
    (
        [
            PredictionOutputType(multi=False),
            PredictionOutputType(multi=False),
            PredictionOutput(payload="hello world"),
        ],
        [mock.call.failed(error="Predictor returned unexpected output")],
    ),
    (
        [PredictionOutput(payload="hello world"), Done()],
        [mock.call.failed(error="Predictor returned unexpected output")],
    ),
]


def fake_worker(events):
    class FakeWorker:
        def predict(self, input_):
            for e in events:
                yield e

    return FakeWorker()


@pytest.mark.parametrize("events,calls", PREDICT_TESTS)
def test_predict(events, calls):
    worker = fake_worker(events)
    request = PredictionRequest(input={"text": "hello"}, foo="bar")
    event_handler_class = mock.Mock()

    expected_response = PredictionResponse(**request.dict())
    response = predict(
        worker=worker, request=request, event_handler_class=event_handler_class
    )
    assert response == expected_response

    event_handler_class.assert_called_once_with(expected_response)

    event_handler = event_handler_class.return_value
    assert event_handler.method_calls == calls
