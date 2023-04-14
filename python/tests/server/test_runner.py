import os
import pytest
import threading
from datetime import datetime
from unittest import mock

from cog.schema import (
    JobRequest,
    JobResponse,
    Status,
    WebhookEvent,
    PredictionRequest,
    PredictionResponse,
    TrainingRequest,
    TrainingResponse,
)
from cog.server.eventtypes import (
    Done,
    Heartbeat,
    Log,
    JobOutput,
    JobOutputType,
)
from cog.server.runner import (
    JobEventHandler,
    Runner,
    RunnerBusyError,
    UnknownPredictionError,
    work,
)
from tests.server.conftest import _fixture_path


@pytest.fixture
def runner():
    runner = Runner(
        runnable_ref=_fixture_path("sleep"), shutdown_event=threading.Event()
    )
    try:
        runner.setup().get(5)
        yield runner
    finally:
        runner.shutdown()


def test_runner_setup():
    runner = Runner(
        runnable_ref=_fixture_path("sleep"), shutdown_event=threading.Event()
    )
    try:
        result = runner.setup().get(5)

        assert result["status"] == Status.SUCCEEDED
        assert result["logs"] == ""
        assert isinstance(result["started_at"], datetime)
        assert isinstance(result["completed_at"], datetime)
    finally:
        runner.shutdown()


def test_runner(runner):
    request = JobRequest(input={"sleep": 0.1})
    _, async_result = runner.run(request)
    response = async_result.get(timeout=1)
    assert response.output == "done in 0.1 seconds"
    assert response.status == "succeeded"
    assert response.error is None
    assert response.logs == ""
    assert isinstance(response.started_at, datetime)
    assert isinstance(response.completed_at, datetime)


def test_runner_called_while_busy(runner):
    request = JobRequest(input={"sleep": 0.1})
    _, async_result = runner.run(request)

    assert runner.is_busy()
    with pytest.raises(RunnerBusyError):
        runner.run(request)

    # Call .get() to ensure that the first prediction is scheduled before we
    # attempt to shut down the runner.
    async_result.get()


def test_runner_called_while_busy_idempotent(runner):
    request = JobRequest(id="abcd1234", input={"sleep": 0.1})

    runner.run(request)
    runner.run(request)
    _, async_result = runner.run(request)

    response = async_result.get(timeout=1)
    assert response.id == "abcd1234"
    assert response.output == "done in 0.1 seconds"
    assert response.status == "succeeded"


def test_runner_called_while_busy_idempotent_wrong_id(runner):
    request1 = JobRequest(id="abcd1234", input={"sleep": 0.1})
    request2 = JobRequest(id="5678efgh", input={"sleep": 0.1})

    _, async_result = runner.run(request1)
    with pytest.raises(RunnerBusyError):
        runner.run(request2)

    response = async_result.get(timeout=1)
    assert response.id == "abcd1234"
    assert response.output == "done in 0.1 seconds"
    assert response.status == "succeeded"


def test_runner_cancel(runner):
    request = JobRequest(input={"sleep": 0.5})
    _, async_result = runner.run(request)

    runner.cancel()

    response = async_result.get(timeout=1)
    assert response.output == None
    assert response.status == "canceled"
    assert response.error is None
    assert response.logs == ""
    assert isinstance(response.started_at, datetime)
    assert isinstance(response.completed_at, datetime)


def test_runner_cancel_matching_id(runner):
    request = JobRequest(id="abcd1234", input={"sleep": 0.5})
    _, async_result = runner.run(request)

    runner.cancel(id="abcd1234")

    response = async_result.get(timeout=1)
    assert response.output == None
    assert response.status == "canceled"


def test_runner_cancel_by_mismatched_id(runner):
    request = JobRequest(id="abcd1234", input={"sleep": 0.5})
    _, async_result = runner.run(request)

    with pytest.raises(UnknownPredictionError):
        runner.cancel(id="5678efgh")

    response = async_result.get(timeout=1)
    assert response.output == "done in 0.5 seconds"
    assert response.status == "succeeded"


# list of (events, calls)
JOB_TESTS = [
    ([Heartbeat()], []),
    ([Done()], [mock.call.succeeded()]),
    ([Done(canceled=True)], [mock.call.canceled()]),
    ([Done(error=True, error_detail="foo")], [mock.call.failed(error="foo")]),
    ([Log(source="stdout", message="help")], [mock.call.append_logs("help")]),
    (
        [JobOutputType(multi=False), JobOutput(payload="hello world")],
        [mock.call.set_output("hello world")],
    ),
    (
        [
            JobOutputType(multi=True),
            JobOutput(payload="hello"),
            JobOutput(payload="world"),
        ],
        [
            mock.call.set_output([]),
            mock.call.append_output("hello"),
            mock.call.append_output("world"),
        ],
    ),
    (
        [
            JobOutputType(multi=False),
            JobOutputType(multi=False),
            JobOutput(payload="hello world"),
        ],
        [mock.call.failed(error="Unexpected output returned")],
    ),
    (
        [JobOutput(payload="hello world"), Done()],
        [mock.call.failed(error="Unexpected output returned")],
    ),
]


def fake_worker(events):
    class FakeWorker:
        def run(self, input_, poll=None):
            for e in events:
                yield e

    return FakeWorker()


@pytest.mark.parametrize("events,calls", JOB_TESTS)
def test_predict(events, calls):
    worker = fake_worker(events)
    request = PredictionRequest(input={"text": "hello"}, foo="bar")
    event_handler = mock.Mock()
    should_cancel = threading.Event()

    response = work(
        worker=worker,
        request=request,
        event_handler=event_handler,
        should_cancel=should_cancel,
    )

    assert event_handler.method_calls == calls


@pytest.mark.parametrize("events,calls", JOB_TESTS)
def test_train(events, calls):
    worker = fake_worker(events)
    request = TrainingRequest(input={"text": "hello"}, foo="bar")
    event_handler = mock.Mock()
    should_cancel = threading.Event()

    response = work(
        worker=worker,
        request=request,
        event_handler=event_handler,
        should_cancel=should_cancel,
    )

    assert event_handler.method_calls == calls


def test_event_handler():
    p = JobResponse(input={"hello": "there"})
    h = JobEventHandler(p)

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
    h = JobEventHandler(p, webhook_sender=s)

    h.set_output([])
    h.append_output("elephant")
    h.append_output("duck")

    h.append_logs("running a prediction\n")
    h.append_logs("still running\n")

    s.reset_mock()
    h.succeeded()

    s.assert_called_once_with(
        match(
            {
                "input": {"hello": "there"},
                "output": ["elephant", "duck"],
                "logs": "running a prediction\nstill running\n",
                "status": "succeeded",
                "metrics": {"predict_time": mock.ANY},
            }
        ),
        WebhookEvent.COMPLETED,
    )


def test_training_event_handler_webhook_sender(match):
    """
    Metric name should be 'training_time' rather than 'predict_time'.
    """
    s = mock.Mock()
    p = TrainingResponse(input={})
    h = JobEventHandler(p, webhook_sender=s)
    s.reset_mock()
    h.succeeded()

    s.assert_called_once_with(
        match(
            {
                "input": {},
                "status": "succeeded",
                "metrics": {"training_time": mock.ANY},
            }
        ),
        WebhookEvent.COMPLETED,
    )


def test_event_handler_webhook_sender_intermediate(match):
    s = mock.Mock()
    p = JobResponse(input={"hello": "there"})
    h = JobEventHandler(p, webhook_sender=s)

    s.assert_called_once_with(match({"status": "processing"}), WebhookEvent.START)

    s.reset_mock()
    h.set_output("giraffes")
    assert s.call_count == 0

    # cheat and reset output behind event handler's back
    p.output = None
    s.reset_mock()
    h.set_output([])
    h.append_output("elephant")
    h.append_output("duck")
    s.assert_has_calls(
        [
            mock.call(
                match(
                    {
                        "output": ["elephant"],
                    }
                ),
                WebhookEvent.OUTPUT,
            ),
            mock.call(
                match(
                    {
                        "output": ["elephant", "duck"],
                    }
                ),
                WebhookEvent.OUTPUT,
            ),
        ]
    )

    s.reset_mock()
    h.append_logs("running a prediction\n")
    h.append_logs("still running\n")
    s.assert_has_calls(
        [
            mock.call(
                match(
                    {
                        "logs": "running a prediction\n",
                    }
                ),
                WebhookEvent.LOGS,
            ),
            mock.call(
                match(
                    {
                        "logs": "running a prediction\nstill running\n",
                    }
                ),
                WebhookEvent.LOGS,
            ),
        ]
    )

    s.reset_mock()
    h.succeeded()
    s.assert_called_once_with(match({"status": "succeeded"}), WebhookEvent.COMPLETED)

    s.reset_mock()
    h.failed("oops")
    s.assert_called_once_with(
        match({"status": "failed", "error": "oops"}), WebhookEvent.COMPLETED
    )

    s.reset_mock()
    h.canceled()
    s.assert_called_once_with(match({"status": "canceled"}), WebhookEvent.COMPLETED)


def test_event_handler_file_uploads():
    u = mock.Mock()
    p = JobResponse(input={"hello": "there"})
    h = JobEventHandler(p, file_uploader=u)

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
