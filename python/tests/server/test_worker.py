import os
import threading
import time
from concurrent.futures import TimeoutError
from typing import Any, List, Optional

import pytest
from attrs import define, field
from hypothesis import HealthCheck, given, settings
from hypothesis import strategies as st
from hypothesis.stateful import RuleBasedStateMachine, precondition, rule

from cog.server.eventtypes import Done, Log, PredictionOutput, PredictionOutputType
from cog.server.exceptions import FatalWorkerException, InvalidStateException
from cog.server.worker import make_worker

from .conftest import WorkerConfig, _fixture_path, uses_worker

# Set a longer deadline on CI as the instances are a bit slower.
settings.register_profile("ci", max_examples=100, deadline=2000)
settings.register_profile("default", max_examples=10, deadline=1500)
settings.register_profile("slow", max_examples=10, deadline=2000)
settings.load_profile(os.getenv("HYPOTHESIS_PROFILE", "default"))

HYPOTHESIS_TEST_TIMEOUT = (
    settings().max_examples * settings().deadline
).total_seconds() + 5

ST_NAMES = st.sampled_from(["John", "Barry", "Elspeth", "Hamid", "Ronnie", "Yasmeen"])

SETUP_FATAL_FIXTURES = [
    "exc_in_setup",
    "exc_in_setup_and_predict",
    "exc_on_import",
    "exit_in_setup",
    "exit_on_import",
    "missing_predictor",
    "nonexistent_file",
]

PREDICTION_FATAL_FIXTURES = [
    "exit_in_predict",
    "killed_in_predict",
]

RUNNABLE_FIXTURES = [
    "simple",
    "exc_in_predict",
    "missing_predict",
]

OUTPUT_FIXTURES = [
    (
        WorkerConfig("hello_world"),
        {"name": ST_NAMES},
        lambda x: f"hello, {x['name']}",
    ),
    (
        WorkerConfig("count_up"),
        {"upto": st.integers(min_value=0, max_value=100)},
        lambda x: list(range(x["upto"])),
    ),
    (
        WorkerConfig("complex_output"),
        {},
        lambda _: {"number": 42, "text": "meaning of life"},
    ),
]

SETUP_LOGS_FIXTURES = [
    (
        (
            "writing some stuff from C at import time\n"
            "writing to stdout at import time\n"
            "setting up predictor\n"
        ),
        "writing to stderr at import time\n",
    )
]

PREDICT_LOGS_FIXTURES = [
    (
        ("writing from C\n" "writing with print\n"),
        ("WARNING:root:writing log message\n" "writing to stderr\n"),
    )
]


@define
class Result:
    stdout_lines: List[str] = field(factory=list)
    stderr_lines: List[str] = field(factory=list)
    heartbeat_count: int = 0
    output_type: Optional[PredictionOutputType] = None
    output: Any = None
    done: Optional[Done] = None
    exception: Optional[Exception] = None

    @property
    def stdout(self):
        return "".join(self.stdout_lines)

    @property
    def stderr(self):
        return "".join(self.stderr_lines)


def _handle_event(result, event):
    if isinstance(event, Log) and event.source == "stdout":
        result.stdout_lines.append(event.message)
    elif isinstance(event, Log) and event.source == "stderr":
        result.stderr_lines.append(event.message)
    elif isinstance(event, Done):
        assert not result.done
        result.done = event
    elif isinstance(event, PredictionOutput):
        assert result.output_type, "Should get output type before any output"
        if result.output_type.multi:
            result.output.append(event.payload)
        else:
            assert (
                result.output is None
            ), "Should not get multiple outputs for output type single"
            result.output = event.payload
    elif isinstance(event, PredictionOutputType):
        assert result.output_type is None, "Should not get multiple output type events"
        result.output_type = event
        if result.output_type.multi:
            result.output = []
    else:
        pytest.fail(f"saw unexpected event: {event}")


def _process(worker, work, swallow_exceptions=False):
    """
    Helper function to collect events generated by Worker during tests.
    """
    result = Result()
    subid = worker.subscribe(lambda event: _handle_event(result, event))
    try:
        work().result()
    except Exception as exc:
        result.exception = exc
        if not swallow_exceptions:
            raise
    finally:
        worker.unsubscribe(subid)
    return result


@uses_worker(SETUP_FATAL_FIXTURES, setup=False)
def test_fatalworkerexception_from_setup_failures(worker):
    """
    Any failure during setup is fatal and should raise FatalWorkerException.
    """
    with pytest.raises(FatalWorkerException):
        _process(worker, worker.setup)


@uses_worker(PREDICTION_FATAL_FIXTURES)
def test_fatalworkerexception_from_irrecoverable_failures(worker):
    """
    Certain kinds of failure during predict (crashes, unexpected exits) are
    irrecoverable and should raise FatalWorkerException.
    """
    with pytest.raises(FatalWorkerException):
        _process(worker, lambda: worker.predict({}))

    with pytest.raises(InvalidStateException):
        _process(worker, lambda: worker.predict({}))


@uses_worker(RUNNABLE_FIXTURES)
def test_no_exceptions_from_recoverable_failures(worker):
    """
    Well-behaved predictors, or those that only throw exceptions, should not
    raise.
    """
    for _ in range(5):
        _process(worker, lambda: worker.predict({}))


@uses_worker("stream_redirector_race_condition")
def test_stream_redirector_race_condition(worker):
    """
    StreamRedirector and ChildWorker are using the same pipe to send data. When
    there are multiple threads trying to write to the same pipe, it can cause
    data corruption by race condition. The data corruption will cause pipe
    receiver to raise an exception due to unpickling error.
    """
    for _ in range(5):
        result = _process(worker, lambda: worker.predict({}))
        assert not result.done.error


@pytest.mark.timeout(HYPOTHESIS_TEST_TIMEOUT)
@pytest.mark.parametrize(
    "worker,payloads,output_generator", OUTPUT_FIXTURES, indirect=["worker"]
)
@settings(suppress_health_check=[HealthCheck.function_scoped_fixture])
@given(data=st.data())
def test_output(worker, payloads, output_generator, data):
    """
    We should get the outputs we expect from predictors that generate output.

    Note that most of the validation work here is actually done in _process.
    """
    payload = data.draw(st.fixed_dictionaries(payloads))
    expected_output = output_generator(payload)

    result = _process(worker, lambda: worker.predict(payload))

    assert result.output == expected_output


@uses_worker("logging", setup=False)
@pytest.mark.parametrize("expected_stdout,expected_stderr", SETUP_LOGS_FIXTURES)
def test_setup_logging(worker, expected_stdout, expected_stderr):
    """
    We should get the logs we expect from predictors that generate logs during
    setup.
    """
    result = _process(worker, worker.setup)
    assert not result.done.error

    assert result.stdout == expected_stdout
    assert result.stderr == expected_stderr


@uses_worker("logging")
@pytest.mark.parametrize("expected_stdout,expected_stderr", PREDICT_LOGS_FIXTURES)
def test_predict_logging(worker, expected_stdout, expected_stderr):
    """
    We should get the logs we expect from predictors that generate logs during
    predict.
    """
    result = _process(worker, lambda: worker.predict({}))

    assert result.stdout == expected_stdout
    assert result.stderr == expected_stderr


@uses_worker("sleep", setup=False)
def test_cancel_is_safe(worker):
    """
    Calls to cancel at any time should not result in unexpected things
    happening or the cancelation of unexpected predictions.
    """

    for _ in range(50):
        worker.cancel()

    result = _process(worker, worker.setup)
    assert not result.done.error

    for _ in range(50):
        worker.cancel()

    result1 = _process(
        worker, lambda: worker.predict({"sleep": 0.5}), swallow_exceptions=True
    )

    for _ in range(50):
        worker.cancel()

    result2 = _process(
        worker, lambda: worker.predict({"sleep": 0.1}), swallow_exceptions=True
    )

    assert not result1.exception
    assert not result1.done.canceled
    assert not result2.exception
    assert not result2.done.canceled
    assert result2.output == "done in 0.1 seconds"


@uses_worker("sleep", setup=False)
def test_cancel_idempotency(worker):
    """
    Multiple calls to cancel within the same prediction, while not necessary or
    recommended, should still only result in a single cancelled prediction, and
    should not affect subsequent predictions.
    """

    def cancel_a_bunch(_):
        for _ in range(100):
            worker.cancel()

    result = _process(worker, worker.setup)
    assert not result.done.error

    fut = worker.predict({"sleep": 0.5})
    # We call cancel a WHOLE BUNCH to make sure that we don't propagate any
    # of those cancelations to subsequent predictions, regardless of the
    # internal implementation of exceptions raised inside signal handlers.
    for _ in range(5):
        time.sleep(0.05)
        for _ in range(100):
            worker.cancel()
    result1 = fut.result()
    assert result1.canceled

    result2 = _process(worker, lambda: worker.predict({"sleep": 0.1}))

    assert not result2.done.canceled
    assert result2.output == "done in 0.1 seconds"


@uses_worker("sleep")
def test_cancel_multiple_predictions(worker):
    """
    Multiple predictions cancelled in a row shouldn't be a problem. This test
    is mainly ensuring that the _allow_cancel latch in Worker is correctly
    reset every time a prediction starts.
    """
    dones: list[Done] = []
    for _ in range(5):
        fut = worker.predict({"sleep": 1})
        time.sleep(0.1)
        worker.cancel()
        dones.append(fut.result())
    assert dones == [Done(canceled=True)] * 5

    assert not worker.predict({"sleep": 0}).result().canceled


@uses_worker("sleep")
def test_graceful_shutdown(worker):
    """
    On shutdown, the worker should finish running the current prediction, and
    then exit.
    """

    saw_first_event = threading.Event()

    # When we see the first event, we'll start the shutdown process.
    worker.subscribe(lambda event: saw_first_event.set())

    fut = worker.predict({"sleep": 1})

    saw_first_event.wait(timeout=1)
    worker.shutdown(timeout=2)

    assert fut.result() == Done()


class WorkerState(RuleBasedStateMachine):
    """
    This is a Hypothesis-driven rule-based state machine test. It is intended
    to ensure that any sequence of calls to the public API of Worker leaves the
    instance in an expected state.

    In short: any call should either throw InvalidStateException or should do
    what the caller asked.

    See https://hypothesis.readthedocs.io/en/latest/stateful.html for more on
    stateful testing with Hypothesis.
    """

    def __init__(self):
        super().__init__()

        self.events = []

        self.predict_canceled = False
        self.predict_payload = None
        self.predict_result = None
        self.setup_result = None

        self.worker = make_worker(_fixture_path("steps"), tee_output=False)
        self.worker.subscribe(self.events.append)

    @rule(sleep=st.floats(min_value=0, max_value=0.1))
    def wait(self, sleep):
        time.sleep(sleep)

    @rule()
    def setup(self):
        try:
            self.setup_result = self.worker.setup()
        except InvalidStateException:
            pass

    @precondition(lambda x: x.setup_result)
    @rule(timeout=st.floats(min_value=0, max_value=0.1))
    def await_setup_complete(self, timeout):
        try:
            res = self.setup_result.result(timeout=timeout)
        except TimeoutError:
            pass
        else:
            assert isinstance(res, Done)
            self._check_events()

    # For now, don't run another prediction until we've read the result. This
    # is solely a limitation of the tests: predictions don't (yet) have
    # identifiers, so we can't distinguish between them.
    @precondition(lambda x: not x.predict_result)
    @rule(name=ST_NAMES, steps=st.integers(min_value=0, max_value=5))
    def predict(self, name, steps):
        try:
            payload = {"name": name, "steps": steps}
            self.predict_result = self.worker.predict(payload)
            self.predict_payload = payload
        except InvalidStateException:
            pass

    @precondition(lambda x: x.predict_result)
    @rule(timeout=st.floats(min_value=0, max_value=0.1))
    def await_predict_complete(self, timeout):
        try:
            res = self.predict_result.result(timeout=timeout)
        except TimeoutError:
            pass
        else:
            assert isinstance(res, Done)
            self._check_events()

    @precondition(lambda x: x.predict_result)
    @rule()
    def cancel(self):
        self.worker.cancel()
        self.predict_canceled = True

    def teardown(self):
        self.worker.shutdown()
        # self.worker.terminate()

    def _check_events(self):
        if self.setup_result and self.setup_result.done():
            self.setup_result = None
            self._check_setup_events()

        if self.predict_result and self.predict_result.done():
            canceled = self.predict_canceled
            payload = self.predict_payload
            self.predict_canceled = False
            self.predict_payload = None
            self.predict_result = None
            self._check_predict_events(payload, canceled)

    def _check_setup_events(self):
        result = self._consume_result()
        assert result.stdout == "did setup\n"
        assert result.stderr == ""
        assert result.done == Done()

    def _check_predict_events(self, payload, canceled=False):
        result = self._consume_result()

        if canceled:
            # Requesting cancelation does not guarantee that the prediction is
            # canceled. It may complete before the cancelation is processed.
            assert result.done == Done() or result.done == Done(canceled=True)

            # If it was canceled, we can't make any other assertions.
            return

        expected_stdout = ["START\n"]
        for i in range(payload["steps"]):
            expected_stdout.append(f"STEP {i+1}\n")
        expected_stdout.append("END\n")

        assert result.stdout == "".join(expected_stdout)
        assert result.stderr == ""
        assert result.output == f"NAME={payload['name']}"
        assert result.done == Done()

    def _consume_result(self):
        print(self.events)
        r = Result()
        while self.events:
            event = self.events.pop(0)
            _handle_event(r, event)
            if isinstance(event, Done):
                break
        return r


TestWorkerState = pytest.mark.timeout(HYPOTHESIS_TEST_TIMEOUT)(WorkerState.TestCase)
