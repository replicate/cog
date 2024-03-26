import asyncio
import os
import threading
from datetime import datetime
from unittest import mock

import pytest
import pytest_asyncio
from cog.schema import PredictionRequest, PredictionResponse, Status, WebhookEvent
from cog.server.clients import ClientManager
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
)


def _fixture_path(name):
    test_dir = os.path.dirname(os.path.realpath(__file__))
    return os.path.join(test_dir, f"fixtures/{name}.py") + ":Predictor"


@pytest_asyncio.fixture
async def runner():
    runner = PredictionRunner(
        predictor_ref=_fixture_path("sleep"), shutdown_event=threading.Event()
    )
    try:
        await runner.setup()
        yield runner
    finally:
        runner.shutdown()


@pytest.mark.asyncio
async def test_prediction_runner_setup():
    runner = PredictionRunner(
        predictor_ref=_fixture_path("sleep"), shutdown_event=threading.Event()
    )
    try:
        result = await runner.setup()

        assert result.status == Status.SUCCEEDED
        assert result.logs == ""
        assert isinstance(result.started_at, datetime)
        assert isinstance(result.completed_at, datetime)
    finally:
        runner.shutdown()


@pytest.mark.asyncio
async def test_prediction_runner(runner):
    request = PredictionRequest(input={"sleep": 0.1})
    _, async_result = runner.predict(request)
    response = await async_result
    assert response.output == "done in 0.1 seconds"
    assert response.status == "succeeded"
    assert response.error is None
    assert response.logs == ""
    assert isinstance(response.started_at, datetime)
    assert isinstance(response.completed_at, datetime)


@pytest.mark.asyncio
async def test_prediction_runner_called_while_busy(runner):
    request = PredictionRequest(input={"sleep": 1})
    _, async_result = runner.predict(request)
    await asyncio.sleep(0)
    assert runner.is_busy()
    with pytest.raises(RunnerBusyError):
        request2 = PredictionRequest(input={"sleep": 1})
        _, task = runner.predict(request2)
        await task

    # Await to ensure that the first prediction is scheduled before we
    # attempt to shut down the runner.
    await async_result


@pytest.mark.asyncio
async def test_prediction_runner_called_while_busy_idempotent(runner):
    request = PredictionRequest(id="abcd1234", input={"sleep": 0.1})

    runner.predict(request)
    runner.predict(request)
    _, async_result = runner.predict(request)

    response = await asyncio.wait_for(async_result, timeout=1)
    assert response.id == "abcd1234"
    assert response.output == "done in 0.1 seconds"
    assert response.status == "succeeded"


@pytest.mark.asyncio
async def test_prediction_runner_called_while_busy_idempotent_wrong_id(runner):
    request1 = PredictionRequest(id="abcd1234", input={"sleep": 0.1})
    request2 = PredictionRequest(id="5678efgh", input={"sleep": 0.1})

    _, async_result = runner.predict(request1)
    with pytest.raises(RunnerBusyError):
        runner.predict(request2)

    response = await async_result
    assert response.id == "abcd1234"
    assert response.output == "done in 0.1 seconds"
    assert response.status == "succeeded"


@pytest.mark.asyncio
async def test_prediction_runner_cancel(runner):
    request = PredictionRequest(input={"sleep": 0.5})
    _, async_result = runner.predict(request)
    await asyncio.sleep(0.001)

    runner.cancel(request.id)

    response = await async_result
    assert response.output is None
    assert response.status == "canceled"
    assert response.error is None
    assert response.logs == ""
    assert isinstance(response.started_at, datetime)
    assert isinstance(response.completed_at, datetime)


@pytest.mark.asyncio
async def test_prediction_runner_cancel_matching_id(runner):
    request = PredictionRequest(id="abcd1234", input={"sleep": 0.5})
    _, async_result = runner.predict(request)
    await asyncio.sleep(0.001)

    runner.cancel(request.id)

    response = await async_result
    assert response.output is None
    assert response.status == "canceled"


@pytest.mark.asyncio
async def test_prediction_runner_cancel_by_mismatched_id(runner):
    request = PredictionRequest(id="abcd1234", input={"sleep": 0.5})
    _, async_result = runner.predict(request)

    with pytest.raises(UnknownPredictionError):
        runner.cancel(prediction_id="5678efgh")

    response = await async_result
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
        async def predict(self, input_, poll=None, eager=False):
            for event in events:
                yield event

    return FakeWorker()


class FakeEventHandler(mock.AsyncMock):
    handle_event_stream = PredictionEventHandler.handle_event_stream
    event_to_handle_future = PredictionEventHandler.event_to_handle_future


# this ought to almost work with AsyncMark
@pytest.mark.xfail
@pytest.mark.asyncio
@pytest.mark.parametrize("events,calls", PREDICT_TESTS)
async def test_predict(events, calls):
    worker = fake_worker(events)
    request = PredictionRequest(input={"text": "hello"}, foo="bar")
    event_handler = FakeEventHandler()
    await event_handler.handle_event_stream(worker.predict(request))

    assert event_handler.method_calls == calls


@pytest.mark.asyncio
async def test_prediction_event_handler():
    request = PredictionRequest(input={"hello": "there"}, webhook=None)
    h = PredictionEventHandler(request, ClientManager(), upload_url=None)
    p = h.p
    await asyncio.sleep(0.0001)

    assert p.status == Status.PROCESSING
    assert p.output is None
    assert p.logs == ""
    assert isinstance(p.started_at, datetime)

    await h.set_output("giraffes")
    assert p.output == "giraffes"

    # cheat and reset output behind event handler's back
    p.output = None
    await h.set_output([])
    await h.append_output("elephant")
    await h.append_output("duck")
    assert p.output == ["elephant", "duck"]

    await h.append_logs("running a prediction\n")
    await h.append_logs("still running\n")
    assert p.logs == "running a prediction\nstill running\n"

    await h.succeeded()
    assert p.status == Status.SUCCEEDED
    assert isinstance(p.completed_at, datetime)

    await h.failed("oops")
    assert p.status == Status.FAILED
    assert p.error == "oops"
    assert isinstance(p.completed_at, datetime)

    await h.canceled()
    assert p.status == Status.CANCELED
    assert isinstance(p.completed_at, datetime)


@pytest.mark.xfail  # ClientManager refactor
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


@pytest.mark.xfail
def test_prediction_event_handler_webhook_sender_intermediate(match):
    s = mock.Mock()
    p = PredictionResponse(input={"hello": "there"})
    h = PredictionEventHandler(p, webhook_sender=s)

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


@pytest.mark.xfail  # ClientManager refactor
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
