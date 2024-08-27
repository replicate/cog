import os
import uuid
from concurrent.futures import Future
from datetime import datetime
from unittest import mock

import pytest

from cog.schema import PredictionRequest, Status, WebhookEvent
from cog.server.eventtypes import Done, Log
from cog.server.runner import (
    PredictionRunner,
    PredictTask,
    RunnerBusyError,
    SetupResult,
    SetupTask,
    UnknownPredictionError,
)
from cog.server.worker import make_worker


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


def test_prediction_runner_setup_success():
    w = FakeWorker()
    r = PredictionRunner(worker=w)

    task = r.setup()
    assert not task.done()

    w.run_setup([Log(message="Setting up...", source="stdout")])
    assert task.result.logs == ["Setting up..."]
    assert not task.done()

    w.run_setup([Done()])
    assert task.done()
    assert task.result.status == Status.SUCCEEDED


def test_prediction_runner_setup_failure():
    w = FakeWorker()
    r = PredictionRunner(worker=w)
    task = r.setup()

    w.run_setup([Done(error=True)])
    assert task.done()
    assert task.result.status == Status.FAILED


def test_prediction_runner_setup_exception():
    w = FakeWorker()
    r = PredictionRunner(worker=w)

    task = r.setup()

    w.run_setup([RuntimeError("kaboom!")])
    assert task.done()
    assert task.result.status == Status.FAILED
    assert task.result.logs[0].startswith("Traceback")
    assert task.result.logs[0].endswith("kaboom!\n")


def test_prediction_runner_predict_success():
    w = FakeWorker()
    r = PredictionRunner(worker=w)

    r.setup()
    w.run_setup([Done()])

    task = r.predict(PredictionRequest(input={"text": "giraffes"}))
    assert w.last_prediction_payload == {"text": "giraffes"}
    assert task.result.input == {"text": "giraffes"}
    assert task.result.status == Status.PROCESSING

    w.run_predict([Log(message="Predicting...", source="stdout")])
    assert task.result.logs == "Predicting..."

    w.run_predict([Done()])
    assert task.result.status == Status.SUCCEEDED


def test_prediction_runner_predict_failure():
    w = FakeWorker()
    r = PredictionRunner(worker=w)

    r.setup()
    w.run_setup([Done()])

    task = r.predict(PredictionRequest(input={"text": "giraffes"}))
    assert w.last_prediction_payload == {"text": "giraffes"}
    assert task.result.input == {"text": "giraffes"}
    assert task.result.status == Status.PROCESSING

    w.run_predict([Done(error=True, error_detail="ErrNeckTooLong")])
    assert task.result.status == Status.FAILED
    assert task.result.error == "ErrNeckTooLong"


def test_prediction_runner_predict_exception():
    w = FakeWorker()
    r = PredictionRunner(worker=w)

    r.setup()
    w.run_setup([Done()])

    task = r.predict(PredictionRequest(input={"text": "giraffes"}))
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


def test_prediction_runner_predict_before_setup():
    w = FakeWorker()
    r = PredictionRunner(worker=w)

    with pytest.raises(RunnerBusyError):
        r.predict(PredictionRequest(input={"text": "giraffes"}))


def test_prediction_runner_predict_before_setup_completes():
    w = FakeWorker()
    r = PredictionRunner(worker=w)

    r.setup()

    with pytest.raises(RunnerBusyError):
        r.predict(PredictionRequest(input={"text": "giraffes"}))


def test_prediction_runner_predict_before_predict_completes():
    w = FakeWorker()
    r = PredictionRunner(worker=w)

    r.setup()
    w.run_setup([Done()])

    r.predict(PredictionRequest(input={"text": "giraffes"}))

    with pytest.raises(RunnerBusyError):
        r.predict(PredictionRequest(input={"text": "giraffes"}))


def test_prediction_runner_predict_after_predict_completes():
    w = FakeWorker()
    r = PredictionRunner(worker=w)

    r.setup()
    w.run_setup([Done()])

    r.predict(PredictionRequest(input={"text": "giraffes"}))
    w.run_predict([Done()])

    r.predict(PredictionRequest(input={"text": "elephants"}))
    w.run_predict([Done()])

    assert w.last_prediction_payload == {"text": "elephants"}


def test_prediction_runner_is_busy():
    w = FakeWorker()
    r = PredictionRunner(worker=w)

    assert r.is_busy()

    r.setup()
    assert r.is_busy()

    w.run_setup([Done()])
    assert not r.is_busy()

    r.predict(PredictionRequest(input={"text": "elephants"}))
    assert r.is_busy()

    w.run_predict([Done()])
    assert not r.is_busy()


def test_prediction_runner_predict_cancelation():
    w = FakeWorker()
    r = PredictionRunner(worker=w)

    r.setup()
    w.run_setup([Done()])

    task = r.predict(PredictionRequest(id="abcd1234", input={"text": "giraffes"}))

    with pytest.raises(ValueError):
        r.cancel(None)
    with pytest.raises(ValueError):
        r.cancel("")
    with pytest.raises(UnknownPredictionError):
        r.cancel("wxyz5678")

    w.run_predict([Log(message="Predicting...", source="stdout")])
    assert task.result.status == Status.PROCESSING

    r.cancel("abcd1234")
    assert task.result.status == Status.CANCELED


def test_prediction_runner_predict_cancelation_multiple_predictions():
    w = FakeWorker()
    r = PredictionRunner(worker=w)

    r.setup()
    w.run_setup([Done()])

    task1 = r.predict(PredictionRequest(id="abcd1234", input={"text": "giraffes"}))
    w.run_predict([Done()])

    task2 = r.predict(PredictionRequest(id="defg6789", input={"text": "elephants"}))
    with pytest.raises(UnknownPredictionError):
        r.cancel("abcd1234")

    r.cancel("defg6789")
    assert task1.result.status == Status.SUCCEEDED
    assert task2.result.status == Status.CANCELED


def test_prediction_runner_setup_e2e():
    w = make_worker(predictor_ref=_fixture_path("sleep"))
    r = PredictionRunner(worker=w)

    try:
        task = r.setup()
        task.wait(timeout=5)
    finally:
        w.shutdown()

    assert task.result.status == Status.SUCCEEDED
    assert task.result.logs == []
    assert isinstance(task.result.started_at, datetime)
    assert isinstance(task.result.completed_at, datetime)


def test_prediction_runner_predict_e2e():
    w = make_worker(predictor_ref=_fixture_path("sleep"))
    r = PredictionRunner(worker=w)

    try:
        r.setup().wait(timeout=5)
        task = r.predict(PredictionRequest(input={"sleep": 0.1}))
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
    p = PredictionRequest(
        input={"hello": "there"},
        id=None,
        created_at=None,
        output_file_prefix=None,
        webhook=None,
    )
    t = PredictTask(p)

    assert t.result.status == Status.PROCESSING
    assert t.result.output is None
    assert t.result.logs == ""
    assert isinstance(t.result.started_at, datetime)

    t.set_output_type(multi=False)
    t.append_output("giraffes")
    assert t.result.output == "giraffes"


def test_predict_task_multi():
    p = PredictionRequest(
        input={"hello": "there"},
        id=None,
        created_at=None,
        output_file_prefix=None,
        webhook=None,
    )
    t = PredictTask(p)

    assert t.result.status == Status.PROCESSING
    assert t.result.output is None
    assert t.result.logs == ""
    assert isinstance(t.result.started_at, datetime)

    t.set_output_type(multi=True)
    t.append_output("elephant")
    t.append_output("duck")
    assert t.result.output == ["elephant", "duck"]

    t.append_logs("running a prediction\n")
    t.append_logs("still running\n")
    assert t.result.logs == "running a prediction\nstill running\n"

    t.succeeded()
    assert t.result.status == Status.SUCCEEDED
    assert isinstance(t.result.completed_at, datetime)

    t.failed("oops")
    assert t.result.status == Status.FAILED
    assert t.result.error == "oops"
    assert isinstance(t.result.completed_at, datetime)

    t.canceled()
    assert t.result.status == Status.CANCELED
    assert isinstance(t.result.completed_at, datetime)


def test_predict_task_webhook_sender():
    p = PredictionRequest(
        input={"hello": "there"},
        id=None,
        created_at=None,
        output_file_prefix=None,
        webhook="https://a.url.honest",
    )
    t = PredictTask(p)
    t._webhook_sender = mock.Mock()
    t.track(Future())

    t._webhook_sender.assert_called_once_with(mock.ANY, WebhookEvent.START)
    actual = t._webhook_sender.call_args[0][0]
    assert actual.status == "processing"

    t.set_output_type(multi=True)
    t.append_output("elephant")
    t.append_output("duck")

    t.append_logs("running a prediction\n")
    t.append_logs("still running\n")

    t._webhook_sender.reset_mock()
    t.succeeded()

    t._webhook_sender.assert_called_once_with(
        mock.ANY,
        WebhookEvent.COMPLETED,
    )
    actual = t._webhook_sender.call_args[0][0]
    assert actual.input == {"hello": "there"}
    assert actual.output == ["elephant", "duck"]
    assert actual.logs == "running a prediction\nstill running\n"
    assert actual.status == "succeeded"
    assert "predict_time" in actual.metrics


def test_predict_task_webhook_sender_intermediate():
    p = PredictionRequest(
        input={"hello": "there"},
        id=None,
        created_at=None,
        output_file_prefix=None,
        webhook="https://a.url.honest",
    )
    t = PredictTask(p)
    t._webhook_sender = mock.Mock()
    t.track(Future())

    t._webhook_sender.assert_called_once_with(mock.ANY, WebhookEvent.START)
    actual = t._webhook_sender.call_args[0][0]
    assert actual.status == "processing"

    t._webhook_sender.reset_mock()
    t.set_output_type(multi=False)
    t.append_output("giraffes")
    assert t._webhook_sender.call_count == 0


def test_predict_task_webhook_sender_intermediate_multi():
    p = PredictionRequest(
        input={"hello": "there"},
        id=None,
        created_at=None,
        output_file_prefix=None,
        webhook="https://a.url.honest",
    )
    t = PredictTask(p)
    t._webhook_sender = mock.Mock()
    t.track(Future())

    t._webhook_sender.assert_called_once_with(mock.ANY, WebhookEvent.START)
    actual = t._webhook_sender.call_args[0][0]
    assert actual.status == "processing"

    t._webhook_sender.reset_mock()
    t.set_output_type(multi=True)
    t.append_output("elephant")
    print(t._webhook_sender.call_args_list)
    assert t._webhook_sender.call_count == 1
    actual = t._webhook_sender.call_args_list[0][0][0]
    assert actual.output == ["elephant"]
    assert t._webhook_sender.call_args_list[0][0][1] == WebhookEvent.OUTPUT

    t._webhook_sender.reset_mock()
    t.append_output("duck")
    assert t._webhook_sender.call_count == 1
    actual = t._webhook_sender.call_args_list[0][0][0]
    assert actual.output == ["elephant", "duck"]
    assert t._webhook_sender.call_args_list[0][0][1] == WebhookEvent.OUTPUT

    t._webhook_sender.reset_mock()
    t.append_logs("running a prediction\n")
    assert t._webhook_sender.call_count == 1
    actual = t._webhook_sender.call_args_list[0][0][0]
    assert actual.logs == "running a prediction\n"
    assert t._webhook_sender.call_args_list[0][0][1] == WebhookEvent.LOGS

    t._webhook_sender.reset_mock()
    t.append_logs("still running\n")
    assert t._webhook_sender.call_count == 1
    actual = t._webhook_sender.call_args_list[0][0][0]
    assert actual.logs == "running a prediction\nstill running\n"
    assert t._webhook_sender.call_args_list[0][0][1] == WebhookEvent.LOGS

    t._webhook_sender.reset_mock()
    t.succeeded()
    t._webhook_sender.assert_called_once()
    actual = t._webhook_sender.call_args[0][0]
    assert actual.status == "succeeded"
    assert t._webhook_sender.call_args[0][1] == WebhookEvent.COMPLETED

    t._webhook_sender.reset_mock()
    t.failed("oops")
    t._webhook_sender.assert_called_once()
    actual = t._webhook_sender.call_args[0][0]
    assert actual.status == "failed"
    assert actual.error == "oops"
    assert t._webhook_sender.call_args[0][1] == WebhookEvent.COMPLETED

    t._webhook_sender.reset_mock()
    t.canceled()
    t._webhook_sender.assert_called_once()
    actual = t._webhook_sender.call_args[0][0]
    assert actual.status == "canceled"
    assert t._webhook_sender.call_args[0][1] == WebhookEvent.COMPLETED


def test_predict_task_file_uploads():
    p = PredictionRequest(
        input={"hello": "there"},
        id=None,
        created_at=None,
        output_file_prefix=None,
        webhook=None,
    )
    t = PredictTask(p, upload_url="https://a.url.honest")
    t._file_uploader = mock.Mock()

    # in reality this would be a Path object, but in this test we just care it
    # passes the output into the upload files function and uses whatever comes
    # back as final output.
    t._file_uploader.return_value = "http://example.com/output-image.png"
    t.set_output_type(multi=False)
    t.append_output("Path(to/my/file)")

    t._file_uploader.assert_called_once_with("Path(to/my/file)")
    assert t.result.output == "http://example.com/output-image.png"


def test_predict_task_file_uploads_multi():
    p = PredictionRequest(
        input={"hello": "there"},
        id=None,
        created_at=None,
        output_file_prefix=None,
        webhook=None,
    )
    t = PredictTask(p, upload_url="https://a.url.honest")
    t._file_uploader = mock.Mock()

    t._file_uploader.return_value = []
    t.set_output_type(multi=True)

    t._file_uploader.return_value = "http://example.com/hello.jpg"
    t.append_output("hello.jpg")

    t._file_uploader.return_value = "http://example.com/world.jpg"
    t.append_output("world.jpg")

    t._file_uploader.assert_has_calls([mock.call("hello.jpg"), mock.call("world.jpg")])
    assert t.result.output == [
        "http://example.com/hello.jpg",
        "http://example.com/world.jpg",
    ]
