import os
import pytest
import time
from datetime import datetime
from multiprocessing import Event
from unittest import mock

from cog.schema import PredictionRequest, PredictionResponse, Status
from cog.server.eventtypes import (
    Done,
    Heartbeat,
    Log,
    PredictionOutput,
    PredictionOutputType,
)
from cog.server.runner import PredictionEventHandler, PredictionRunner, predict


def _fixture_path(name):
    test_dir = os.path.dirname(os.path.realpath(__file__))
    return os.path.join(test_dir, f"fixtures/{name}.py") + ":Predictor"


@pytest.fixture
def runner():
    runner = PredictionRunner(predictor_ref=_fixture_path("sleep"))
    try:
        yield runner
    finally:
        runner.shutdown()


def test_prediction_runner(runner):
    runner.setup()
    request = PredictionRequest(input={"sleep": 0.1})
    async_result = runner.predict(request)
    response = async_result.get(timeout=1)
    assert response.output == "done in 0.1 seconds"
    assert response.status == "succeeded"
    assert response.error is None
    assert response.logs == ""
    assert isinstance(response.started_at, datetime)
    assert isinstance(response.completed_at, datetime)


def test_prediction_runner_called_while_busy(runner):
    runner.setup()
    request = PredictionRequest(input={"sleep": 0.1})
    runner.predict(request)

    assert runner.is_busy()
    with pytest.raises(Exception):
        runner.predict(request)


def test_prediction_runner_called_while_busy(runner):
    runner.setup()
    request = PredictionRequest(input={"sleep": 0.5})
    async_result = runner.predict(request)

    runner.cancel()

    response = async_result.get(timeout=1)
    assert response.output == None
    assert response.status == "canceled"
    assert response.error is None
    assert response.logs == ""
    assert isinstance(response.started_at, datetime)
    assert isinstance(response.completed_at, datetime)


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
        def predict(self, input_, poll=None):
            for e in events:
                yield e

    return FakeWorker()


@pytest.mark.parametrize("events,calls", PREDICT_TESTS)
def test_predict(events, calls):
    worker = fake_worker(events)
    request = PredictionRequest(input={"text": "hello"}, foo="bar")
    should_cancel = Event()
    event_handler_class = mock.Mock()

    expected_response = PredictionResponse(**request.dict())
    response = predict(
        worker=worker,
        request=request,
        should_cancel=should_cancel,
        event_handler_class=event_handler_class,
    )
    assert response == expected_response

    event_handler_class.assert_called_once_with(expected_response)

    event_handler = event_handler_class.return_value
    assert event_handler.method_calls == calls


def test_prediction_event_handler():
    p = PredictionResponse(input={"hello": "there"})
    h = PredictionEventHandler(p)

    assert p.status == Status.PROCESSING
    assert p.output is None
    assert p.logs == ""
    assert isinstance(p.started_at, datetime)

    h.set_output("giraffes")
    assert p.output == "giraffes"

    # cheat and reset output behind event handler's back
    p.output = None
    h.set_output([])
    h.append_output("elephant")
    h.append_output("duck")
    assert p.output == ["elephant", "duck"]

    h.append_logs("running a prediction\n")
    h.append_logs("still running\n")
    assert p.logs == "running a prediction\nstill running\n"

    h.succeeded()
    assert p.status == Status.SUCCEEDED
    assert isinstance(p.completed_at, datetime)

    h.failed("oops")
    assert p.status == Status.FAILED
    assert p.error == "oops"
    assert isinstance(p.completed_at, datetime)

    h.canceled()
    assert p.status == Status.CANCELED
    assert isinstance(p.completed_at, datetime)
