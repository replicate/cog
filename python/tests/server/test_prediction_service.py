import os
import uuid
from concurrent.futures import Future
from datetime import datetime
from unittest import mock

import pytest

from cog.schema import PredictionRequest, PredictionResponse, Status, WebhookEvent
from cog.server.eventtypes import Done, Log
from cog.server.prediction_service import (
    BusyError,
    PredictTask,
    PredictionService,
    SetupResult,
    SetupTask,
    UnknownPredictionError,
)
from cog.server.worker import Worker


def _fixture_path(name):
    test_dir = os.path.dirname(os.path.realpath(__file__))
    return os.path.join(test_dir, f"fixtures/{name}.py") + ":Predictor"


class FakeClock:
    def __init__(self, t):
        self.t = t

    def __call__(self):
        return self.t


tick = mock.sentinel.tick


class FakeWorker:
    def __init__(self):
        self.subscribers = {}
        self.last_prediction_payload = None

        self._setup_future = None
        self._predict_future = None

    def subscribe(self, subscriber):
        sid = uuid.uuid4()
        self.subscribers[sid] = subscriber
        return sid

    def unsubscribe(self, sid):
        del self.subscribers[sid]

    def setup(self):
        assert self._setup_future is None
        self._setup_future = Future()
        return self._setup_future

    def run_setup(self, events):
        for event in events:
            if isinstance(event, Exception):
                self._setup_future.set_exception(event)
                return
            for subscriber in self.subscribers.values():
                subscriber(event)
            if isinstance(event, Done):
                self._setup_future.set_result(event)

    def predict(self, payload):
        assert self._predict_future is None or self._predict_future.done()
        self.last_prediction_payload = payload
        self._predict_future = Future()
        return self._predict_future

    def run_predict(self, events):
        for event in events:
            if isinstance(event, Exception):
                self._predict_future.set_exception(event)
                return
            for subscriber in self.subscribers.values():
                subscriber(event)
            if isinstance(event, Done):
                self._predict_future.set_result(event)

    def cancel(self):
        done = Done(canceled=True)
        for subscriber in self.subscribers.values():
            subscriber(done)
        self._predict_future.set_result(done)


def test_prediction_service_setup_success():
    w = FakeWorker()
    s = PredictionService(worker=w)

    task = s.setup()
    assert not task.done()

    w.run_setup([Log(message="Setting up...", source="stdout")])
    assert task.result.logs == ["Setting up..."]
    assert not task.done()

    w.run_setup([Done()])
    assert task.done()
    assert task.result.status == Status.SUCCEEDED


def test_prediction_service_setup_failure():
    w = FakeWorker()
    s = PredictionService(worker=w)
    task = s.setup()

    w.run_setup([Done(error=True)])
    assert task.done()
    assert task.result.status == Status.FAILED


def test_prediction_service_setup_exception():
    w = FakeWorker()
    s = PredictionService(worker=w)

    task = s.setup()

    w.run_setup([RuntimeError("kaboom!")])
    assert task.done()
    assert task.result.status == Status.FAILED
    assert task.result.logs[0].startswith("Traceback")
    assert task.result.logs[0].endswith("kaboom!\n")


def test_prediction_service_predict_success():
    w = FakeWorker()
    s = PredictionService(worker=w)

    s.setup()
    w.run_setup([Done()])

    task = s.predict(PredictionRequest(input={"text": "giraffes"}))
    assert w.last_prediction_payload == {"text": "giraffes"}
    assert task.result.input == {"text": "giraffes"}
    assert task.result.status == Status.PROCESSING

    w.run_predict([Log(message="Predicting...", source="stdout")])
    assert task.result.logs == "Predicting..."

    w.run_predict([Done()])
    assert task.result.status == Status.SUCCEEDED


def test_prediction_service_predict_failure():
    w = FakeWorker()
    s = PredictionService(worker=w)

    s.setup()
    w.run_setup([Done()])

    task = s.predict(PredictionRequest(input={"text": "giraffes"}))
    assert w.last_prediction_payload == {"text": "giraffes"}
    assert task.result.input == {"text": "giraffes"}
    assert task.result.status == Status.PROCESSING

    w.run_predict([Done(error=True, error_detail="ErrNeckTooLong")])
    assert task.result.status == Status.FAILED
    assert task.result.error == "ErrNeckTooLong"


def test_prediction_service_predict_exception():
    w = FakeWorker()
    s = PredictionService(worker=w)

    s.setup()
    w.run_setup([Done()])

    task = s.predict(PredictionRequest(input={"text": "giraffes"}))
    assert w.last_prediction_payload == {"text": "giraffes"}
    assert task.result.input == {"text": "giraffes"}
    assert task.result.status == Status.PROCESSING

    w.run_predict(
        [
            Log(message="counting shards\n", source="stdout"),
            Log(message="reticulating splines\n", source="stdout"),
            ValueError("splines not reticulable"),
        ]
    )

    assert task.result.logs.startswith("counting shards\nreticulating splines\n")
    assert "Traceback" in task.result.logs
    assert "ValueError: splines not reticulable" in task.result.logs
    assert task.result.status == Status.FAILED
    assert task.result.error == "splines not reticulable"


def test_prediction_service_predict_before_setup():
    w = FakeWorker()
    s = PredictionService(worker=w)

    with pytest.raises(BusyError):
        s.predict(PredictionRequest(input={"text": "giraffes"}))


def test_prediction_service_predict_before_setup_completes():
    w = FakeWorker()
    s = PredictionService(worker=w)

    s.setup()

    with pytest.raises(BusyError):
        s.predict(PredictionRequest(input={"text": "giraffes"}))


def test_prediction_service_predict_before_predict_completes():
    w = FakeWorker()
    s = PredictionService(worker=w)

    s.setup()
    w.run_setup([Done()])

    s.predict(PredictionRequest(input={"text": "giraffes"}))

    with pytest.raises(BusyError):
        s.predict(PredictionRequest(input={"text": "giraffes"}))


def test_prediction_service_predict_after_predict_completes():
    w = FakeWorker()
    s = PredictionService(worker=w)

    s.setup()
    w.run_setup([Done()])

    s.predict(PredictionRequest(input={"text": "giraffes"}))
    w.run_predict([Done()])

    s.predict(PredictionRequest(input={"text": "elephants"}))
    w.run_predict([Done()])

    assert w.last_prediction_payload == {"text": "elephants"}


def test_prediction_service_is_busy():
    w = FakeWorker()
    s = PredictionService(worker=w)

    assert s.is_busy()

    s.setup()
    assert s.is_busy()

    w.run_setup([Done()])
    assert not s.is_busy()

    s.predict(PredictionRequest(input={"text": "elephants"}))
    assert s.is_busy()

    w.run_predict([Done()])
    assert not s.is_busy()


def test_prediction_service_predict_cancelation():
    w = FakeWorker()
    s = PredictionService(worker=w)

    s.setup()
    w.run_setup([Done()])

    task = s.predict(PredictionRequest(id="abcd1234", input={"text": "giraffes"}))

    with pytest.raises(ValueError):
        s.cancel(None)
    with pytest.raises(ValueError):
        s.cancel("")
    with pytest.raises(UnknownPredictionError):
        s.cancel("wxyz5678")

    w.run_predict([Log(message="Predicting...", source="stdout")])
    assert task.result.status == Status.PROCESSING

    s.cancel("abcd1234")
    assert task.result.status == Status.CANCELED


def test_prediction_service_predict_cancelation_multiple_predictions():
    w = FakeWorker()
    s = PredictionService(worker=w)

    s.setup()
    w.run_setup([Done()])

    task1 = s.predict(PredictionRequest(id="abcd1234", input={"text": "giraffes"}))
    w.run_predict([Done()])

    task2 = s.predict(PredictionRequest(id="defg6789", input={"text": "elephants"}))
    with pytest.raises(UnknownPredictionError):
        s.cancel("abcd1234")

    s.cancel("defg6789")
    assert task1.result.status == Status.SUCCEEDED
    assert task2.result.status == Status.CANCELED


def test_prediction_service_setup_e2e():
    w = Worker(predictor_ref=_fixture_path("sleep"))
    s = PredictionService(worker=w)

    try:
        task = s.setup()
        task.wait(timeout=5)
    finally:
        w.shutdown()

    assert task.result.status == Status.SUCCEEDED
    assert task.result.logs == []
    assert isinstance(task.result.started_at, datetime)
    assert isinstance(task.result.completed_at, datetime)


def test_prediction_service_predict_e2e():
    w = Worker(predictor_ref=_fixture_path("sleep"))
    s = PredictionService(worker=w)

    try:
        s.setup().wait(timeout=5)
        task = s.predict(PredictionRequest(input={"sleep": 0.1}))
        task.wait(timeout=1)
    finally:
        w.shutdown()

    assert task.result.output == "done in 0.1 seconds"
    assert task.result.status == "succeeded"
    assert task.result.error is None
    assert task.result.logs == "starting\n"
    assert isinstance(task.result.started_at, datetime)
    assert isinstance(task.result.completed_at, datetime)


@pytest.mark.parametrize(
    "log,result",
    [
        (
            [],
            SetupResult(started_at=1),
        ),
        (
            [tick, Done()],
            SetupResult(started_at=1, completed_at=2, status=Status.SUCCEEDED),
        ),
        (
            [
                tick,
                Log("running 1\n", source="stdout"),
                Log("running 2\n", source="stdout"),
                Done(),
            ],
            SetupResult(
                started_at=1,
                completed_at=2,
                logs=["running 1\n", "running 2\n"],
                status=Status.SUCCEEDED,
            ),
        ),
        (
            [
                tick,
                tick,
                Done(error=True, error_detail="kaboom!"),
            ],
            SetupResult(
                started_at=1,
                completed_at=3,
                status=Status.FAILED,
            ),
        ),
    ],
)
def test_setup_task(log, result):
    c = FakeClock(t=1)
    t = SetupTask(_clock=c)

    for event in log:
        if event == tick:
            c.t += 1
        else:
            t.handle_event(event)

    assert t.result == result


def test_predict_task():
    p = PredictionResponse(input={"hello": "there"})
    t = PredictTask(p)

    assert p.status == Status.PROCESSING
    assert p.output is None
    assert p.logs == ""
    assert isinstance(p.started_at, datetime)

    t.set_output_type(multi=False)
    t.append_output("giraffes")
    assert p.output == "giraffes"


def test_predict_task_multi():
    p = PredictionResponse(input={"hello": "there"})
    t = PredictTask(p)

    assert p.status == Status.PROCESSING
    assert p.output is None
    assert p.logs == ""
    assert isinstance(p.started_at, datetime)

    t.set_output_type(multi=True)
    t.append_output("elephant")
    t.append_output("duck")
    assert p.output == ["elephant", "duck"]

    t.append_logs("running a prediction\n")
    t.append_logs("still running\n")
    assert p.logs == "running a prediction\nstill running\n"

    t.succeeded()
    assert p.status == Status.SUCCEEDED
    assert isinstance(p.completed_at, datetime)

    t.failed("oops")
    assert p.status == Status.FAILED
    assert p.error == "oops"
    assert isinstance(p.completed_at, datetime)

    t.canceled()
    assert p.status == Status.CANCELED
    assert isinstance(p.completed_at, datetime)


def test_predict_task_webhook_sender():
    s = mock.Mock()
    p = PredictionResponse(input={"hello": "there"})
    t = PredictTask(p, webhook_sender=s)

    t.set_output_type(multi=True)
    t.append_output("elephant")
    t.append_output("duck")

    t.append_logs("running a prediction\n")
    t.append_logs("still running\n")

    s.reset_mock()
    t.succeeded()

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


def test_predict_task_webhook_sender_intermediate():
    s = mock.Mock()
    p = PredictionResponse(input={"hello": "there"})
    t = PredictTask(p, webhook_sender=s)

    s.assert_called_once_with(mock.ANY, WebhookEvent.START)
    actual = s.call_args[0][0]
    assert actual.status == "processing"

    s.reset_mock()
    t.set_output_type(multi=False)
    t.append_output("giraffes")
    assert s.call_count == 0


def test_predict_task_webhook_sender_intermediate_multi():
    s = mock.Mock()
    p = PredictionResponse(input={"hello": "there"})
    t = PredictTask(p, webhook_sender=s)

    s.assert_called_once_with(mock.ANY, WebhookEvent.START)
    actual = s.call_args[0][0]
    assert actual.status == "processing"

    s.reset_mock()
    t.set_output_type(multi=True)
    t.append_output("elephant")
    print(s.call_args_list)
    assert s.call_count == 1
    actual = s.call_args_list[0][0][0]
    assert actual.output == ["elephant"]
    assert s.call_args_list[0][0][1] == WebhookEvent.OUTPUT

    s.reset_mock()
    t.append_output("duck")
    assert s.call_count == 1
    actual = s.call_args_list[0][0][0]
    assert actual.output == ["elephant", "duck"]
    assert s.call_args_list[0][0][1] == WebhookEvent.OUTPUT

    s.reset_mock()
    t.append_logs("running a prediction\n")
    assert s.call_count == 1
    actual = s.call_args_list[0][0][0]
    assert actual.logs == "running a prediction\n"
    assert s.call_args_list[0][0][1] == WebhookEvent.LOGS

    s.reset_mock()
    t.append_logs("still running\n")
    assert s.call_count == 1
    actual = s.call_args_list[0][0][0]
    assert actual.logs == "running a prediction\nstill running\n"
    assert s.call_args_list[0][0][1] == WebhookEvent.LOGS

    s.reset_mock()
    t.succeeded()
    s.assert_called_once()
    actual = s.call_args[0][0]
    assert actual.status == "succeeded"
    assert s.call_args[0][1] == WebhookEvent.COMPLETED

    s.reset_mock()
    t.failed("oops")
    s.assert_called_once()
    actual = s.call_args[0][0]
    assert actual.status == "failed"
    assert actual.error == "oops"
    assert s.call_args[0][1] == WebhookEvent.COMPLETED

    s.reset_mock()
    t.canceled()
    s.assert_called_once()
    actual = s.call_args[0][0]
    assert actual.status == "canceled"
    assert s.call_args[0][1] == WebhookEvent.COMPLETED


def test_predict_task_file_uploads():
    u = mock.Mock()
    p = PredictionResponse(input={"hello": "there"})
    t = PredictTask(p, file_uploader=u)

    # in reality this would be a Path object, but in this test we just care it
    # passes the output into the upload files function and uses whatever comes
    # back as final output.
    u.return_value = "http://example.com/output-image.png"
    t.set_output_type(multi=False)
    t.append_output("Path(to/my/file)")

    u.assert_called_once_with("Path(to/my/file)")
    assert p.output == "http://example.com/output-image.png"


def test_predict_task_file_uploads_multi():
    u = mock.Mock()
    p = PredictionResponse(input={"hello": "there"})
    t = PredictTask(p, file_uploader=u)

    u.return_value = []
    t.set_output_type(multi=True)

    u.return_value = "http://example.com/hello.jpg"
    t.append_output("hello.jpg")

    u.return_value = "http://example.com/world.jpg"
    t.append_output("world.jpg")

    u.assert_has_calls([mock.call("hello.jpg"), mock.call("world.jpg")])
    assert p.output == ["http://example.com/hello.jpg", "http://example.com/world.jpg"]
