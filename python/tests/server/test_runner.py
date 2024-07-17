import os
import threading
from datetime import datetime
from unittest import mock

import pytest
from cog.schema import PredictionRequest, PredictionResponse, Status, WebhookEvent
from cog.server.eventtypes import (
    Done,
    Heartbeat,
    Log,
    PredictionOutput,
    PredictionOutputType,
)
from cog.server.runner import (
    PredictionEventHandler,
    PredictionRunner,
    RunnerBusyError,
    UnknownPredictionError,
    predict,
)


def _fixture_path(name):
    test_dir = os.path.dirname(os.path.realpath(__file__))
    return os.path.join(test_dir, f"fixtures/{name}.py") + ":Predictor"


@pytest.fixture
def runner():
    runner = PredictionRunner(
        predictor_ref=_fixture_path("sleep"), shutdown_event=threading.Event()
    )
    try:
        runner.setup().get(5)
        yield runner
    finally:
        runner.shutdown()


def test_prediction_runner_setup():
    runner = PredictionRunner(
        predictor_ref=_fixture_path("sleep"), shutdown_event=threading.Event()
    )
    try:
        result = runner.setup().get(5)

        assert result.status == Status.SUCCEEDED
        assert result.logs == ""
        assert isinstance(result.started_at, datetime)
        assert isinstance(result.completed_at, datetime)
    finally:
        runner.shutdown()


def test_prediction_runner(runner):
    request = PredictionRequest(input={"sleep": 0.1})
    _, async_result = runner.predict(request)
    response = async_result.get(timeout=1)
    assert response.output == "done in 0.1 seconds"
    assert response.status == "succeeded"
    assert response.error is None
    assert response.logs == ""
    assert isinstance(response.started_at, datetime)
    assert isinstance(response.completed_at, datetime)


def test_prediction_runner_called_while_busy(runner):
    request = PredictionRequest(input={"sleep": 0.1})
    _, async_result = runner.predict(request)

    assert runner.is_busy()
    with pytest.raises(RunnerBusyError):
        runner.predict(request)

    # Call .get() to ensure that the first prediction is scheduled before we
    # attempt to shut down the runner.
    async_result.get()


def test_prediction_runner_called_while_busy_idempotent(runner):
    request = PredictionRequest(id="abcd1234", input={"sleep": 0.1})

    runner.predict(request)
    runner.predict(request)
    _, async_result = runner.predict(request)

    response = async_result.get(timeout=1)
    assert response.id == "abcd1234"
    assert response.output == "done in 0.1 seconds"
    assert response.status == "succeeded"


def test_prediction_runner_called_while_busy_idempotent_wrong_id(runner):
    request1 = PredictionRequest(id="abcd1234", input={"sleep": 0.1})
    request2 = PredictionRequest(id="5678efgh", input={"sleep": 0.1})

    _, async_result = runner.predict(request1)
    with pytest.raises(RunnerBusyError):
        runner.predict(request2)

    response = async_result.get(timeout=1)
    assert response.id == "abcd1234"
    assert response.output == "done in 0.1 seconds"
    assert response.status == "succeeded"


def test_prediction_runner_cancel(runner):
    request = PredictionRequest(input={"sleep": 0.5})
    _, async_result = runner.predict(request)

    runner.cancel()

    response = async_result.get(timeout=1)
    assert response.output is None
    assert response.status == "canceled"
    assert response.error is None
    assert response.logs == ""
    assert isinstance(response.started_at, datetime)
    assert isinstance(response.completed_at, datetime)


def test_prediction_runner_cancel_matching_id(runner):
    request = PredictionRequest(id="abcd1234", input={"sleep": 0.5})
    _, async_result = runner.predict(request)

    runner.cancel(prediction_id="abcd1234")

    response = async_result.get(timeout=1)
    assert response.output is None
    assert response.status == "canceled"


def test_prediction_runner_cancel_by_mismatched_id(runner):
    request = PredictionRequest(id="abcd1234", input={"sleep": 0.5})
    _, async_result = runner.predict(request)

    with pytest.raises(UnknownPredictionError):
        runner.cancel(prediction_id="5678efgh")

    response = async_result.get(timeout=1)
    assert response.output == "done in 0.5 seconds"
    assert response.status == "succeeded"


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
            yield from events

    return FakeWorker()


@pytest.mark.parametrize("events,calls", PREDICT_TESTS)
def test_predict(events, calls):
    worker = fake_worker(events)
    request = PredictionRequest(input={"text": "hello"}, foo="bar")
    event_handler = mock.Mock()
    should_cancel = threading.Event()

    predict(
        worker=worker,
        request=request,
        event_handler=event_handler,
        should_cancel=should_cancel,
    )

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


def test_prediction_event_handler_webhook_sender(match):
    s = mock.Mock()
    p = PredictionResponse(input={"hello": "there"})
    h = PredictionEventHandler(p, webhook_sender=s)

    h.set_output([])
    h.append_output("elephant")
    h.append_output("duck")

    h.append_logs("running a prediction\n")
    h.append_logs("still running\n")

    s.reset_mock()
    h.succeeded()

    s.assert_called_once_with(
        mock.ANY,
        WebhookEvent.COMPLETED,
    )
    actual = s.call_args[0][0]
    assert actual.input == {"hello": "there"}
    assert actual.output == ["elephant", "duck"]
    assert actual.logs == "running a prediction\nstill running\n"
    assert actual.status == "succeeded"
    assert "predict_time" in actual.metrics


def test_prediction_event_handler_webhook_sender_intermediate():
    s = mock.Mock()
    p = PredictionResponse(input={"hello": "there"})
    h = PredictionEventHandler(p, webhook_sender=s)

    s.assert_called_once_with(mock.ANY, WebhookEvent.START)
    actual = s.call_args[0][0]
    assert actual.status == "processing"

    s.reset_mock()
    h.set_output("giraffes")
    assert s.call_count == 0

    # cheat and reset output behind event handler's back
    p.output = None
    s.reset_mock()
    h.set_output([])
    h.append_output("elephant")
    assert s.call_count == 1
    actual = s.call_args_list[0][0][0]
    assert actual.output == ["elephant"]
    assert s.call_args_list[0][0][1] == WebhookEvent.OUTPUT

    s.reset_mock()
    h.append_output("duck")
    assert s.call_count == 1
    actual = s.call_args_list[0][0][0]
    assert actual.output == ["elephant", "duck"]
    assert s.call_args_list[0][0][1] == WebhookEvent.OUTPUT

    s.reset_mock()
    h.append_logs("running a prediction\n")
    assert s.call_count == 1
    actual = s.call_args_list[0][0][0]
    assert actual.logs == "running a prediction\n"
    assert s.call_args_list[0][0][1] == WebhookEvent.LOGS

    s.reset_mock()
    h.append_logs("still running\n")
    assert s.call_count == 1
    actual = s.call_args_list[0][0][0]
    assert actual.logs == "running a prediction\nstill running\n"
    assert s.call_args_list[0][0][1] == WebhookEvent.LOGS

    s.reset_mock()
    h.succeeded()
    s.assert_called_once()
    actual = s.call_args[0][0]
    assert actual.status == "succeeded"
    assert s.call_args[0][1] == WebhookEvent.COMPLETED

    s.reset_mock()
    h.failed("oops")
    s.assert_called_once()
    actual = s.call_args[0][0]
    assert actual.status == "failed"
    assert actual.error == "oops"
    assert s.call_args[0][1] == WebhookEvent.COMPLETED

    s.reset_mock()
    h.canceled()
    s.assert_called_once()
    actual = s.call_args[0][0]
    assert actual.status == "canceled"
    assert s.call_args[0][1] == WebhookEvent.COMPLETED


def test_prediction_event_handler_file_uploads():
    u = mock.Mock()
    p = PredictionResponse(input={"hello": "there"})
    h = PredictionEventHandler(p, file_uploader=u)

    # in reality this would be a Path object, but in this test we just care it
    # passes the output into the upload files function and uses whatever comes
    # back as final output.
    u.return_value = "http://example.com/output-image.png"
    h.set_output("Path(to/my/file)")

    u.assert_called_once_with("Path(to/my/file)")
    assert p.output == "http://example.com/output-image.png"

    # cheat and reset output behind event handler's back
    p.output = None
    u.reset_mock()

    u.return_value = []
    h.set_output([])

    u.return_value = "http://example.com/hello.jpg"
    h.append_output("hello.jpg")

    u.return_value = "http://example.com/world.jpg"
    h.append_output("world.jpg")

    u.assert_has_calls([mock.call([]), mock.call("hello.jpg"), mock.call("world.jpg")])
    assert p.output == ["http://example.com/hello.jpg", "http://example.com/world.jpg"]
