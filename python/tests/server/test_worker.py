import multiprocessing
import os
import threading
import time
import uuid
from typing import TYPE_CHECKING, Any, Dict, List, Optional

import pytest
from attrs import define, evolve, field, frozen
from hypothesis import HealthCheck, given, settings
from hypothesis import strategies as st
from hypothesis.stateful import (
    Bundle,
    RuleBasedStateMachine,
    consumes,
    multiple,
    rule,
)

from cog.server.eventtypes import (
    Cancel,
    Done,
    Envelope,
    Log,
    PredictionInput,
    PredictionMetric,
    PredictionOutput,
    PredictionOutputType,
)
from cog.server.exceptions import FatalWorkerException, InvalidStateException
from cog.server.worker import Worker, _PublicEventType

from .conftest import WorkerConfig, uses_worker, uses_worker_configs

if TYPE_CHECKING:
    from concurrent.futures import Future

# Set a longer deadline on CI as the instances are a bit slower.
settings.register_profile("ci", max_examples=200, deadline=2000)
settings.register_profile("default", max_examples=50, deadline=1500)
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

METRICS_FIXTURES = [
    (
        WorkerConfig("record_metric"),
        {"name": ST_NAMES},
        {
            "foo": 123,
        },
    ),
    (
        WorkerConfig("record_metric_async", is_async=True),
        {"name": ST_NAMES},
        {
            "foo": 123,
        },
    ),
    (
        WorkerConfig("emit_metric"),
        {"name": ST_NAMES},
        {
            "foo": 123,
        },
    ),
    (
        WorkerConfig("emit_metric_async", is_async=True),
        {"name": ST_NAMES},
        {
            "foo": 123,
        },
    ),
]

OUTPUT_FIXTURES = [
    (
        WorkerConfig("hello_world"),
        {"name": ST_NAMES},
        lambda x: f"hello, {x['name']}",
    ),
    (
        WorkerConfig("hello_world_async", is_async=True),
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
        WorkerConfig("logging", setup=False),
        (
            "writing some stuff from C at import time\n"
            "writing to stdout at import time\n"
            "setting up predictor\n"
        ),
        "writing to stderr at import time\n",
    ),
    (
        WorkerConfig("logging_async", is_async=True, setup=False),
        ("writing to stdout at import time\n" "setting up predictor\n"),
        "writing to stderr at import time\n",
    ),
]

PREDICT_LOGS_FIXTURES = [
    (
        WorkerConfig("logging"),
        ("writing from C\n" "writing with print\n"),
        ("WARNING:root:writing log message\n" "writing to stderr\n"),
    ),
    (
        WorkerConfig("logging_async", is_async=True),
        ("writing with print\n"),
        ("WARNING:root:writing log message\n" "writing to stderr\n"),
    ),
]


@define
class Result:
    stdout_lines: List[str] = field(factory=list)
    stderr_lines: List[str] = field(factory=list)
    heartbeat_count: int = 0
    metrics: Optional[Dict[str, Any]] = None
    output_type: Optional[PredictionOutputType] = None
    output: Any = None
    done: Optional[Done] = None
    exception: Optional[Exception] = None

    event_seen: threading.Event = field(factory=threading.Event)

    @property
    def stdout(self):
        return "".join(self.stdout_lines)

    @property
    def stderr(self):
        return "".join(self.stderr_lines)

    def handle_event(self, event: _PublicEventType):
        if isinstance(event, Log) and event.source == "stdout":
            self.stdout_lines.append(event.message)
        elif isinstance(event, Log) and event.source == "stderr":
            self.stderr_lines.append(event.message)
        elif isinstance(event, Done):
            assert not self.done
            self.done = event
        elif isinstance(event, PredictionMetric):
            if self.metrics is None:
                self.metrics = {}
            self.metrics[event.name] = event.value
        elif isinstance(event, PredictionOutput):
            assert self.output_type, "Should get output type before any output"
            if self.output_type.multi:
                self.output.append(event.payload)
            else:
                assert (
                    self.output is None
                ), "Should not get multiple outputs for output type single"
                self.output = event.payload
        elif isinstance(event, PredictionOutputType):
            assert (
                self.output_type is None
            ), "Should not get multiple output type events"
            self.output_type = event
            if self.output_type.multi:
                self.output = []
        else:
            pytest.fail(f"saw unexpected event: {event}")
        self.event_seen.set()


def _process(worker, work, swallow_exceptions=False, tag=None):
    """
    Helper function to collect events generated by Worker during tests.
    """
    result = Result()
    subid = worker.subscribe(result.handle_event, tag=tag)
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


# TODO test this works with errors and cancelations and the like
@uses_worker_configs(
    [WorkerConfig("simple"), WorkerConfig("simple_async", is_async=True)]
)
def test_can_subscribe_for_a_specific_tag(worker):
    tag = "123"

    result = Result()
    subid = worker.subscribe(result.handle_event, tag=tag)

    try:
        worker.predict({}, tag="not-my-tag").result()
        assert not result.done

        worker.predict({}, tag=tag).result()
        assert result.done
        assert not result.done.canceled
        assert not result.exception
        assert result.stdout == "did predict\n"
        assert result.output == "prediction output"

    finally:
        worker.unsubscribe(subid)


@uses_worker("sleep_async", is_async=True, max_concurrency=5)
def test_can_run_predictions_concurrently_on_async_predictor(worker):
    subids = []

    try:
        start = time.time()
        futures = []
        results = []
        for i in range(5):
            result = Result()
            results.append(result)
            tag = f"tag-{i}"
            subids.append(worker.subscribe(result.handle_event, tag=tag))
            futures.append(worker.predict({"sleep": 0.5}, tag=tag))
            assert not result.done

        for fut in futures:
            fut.result()

        end = time.time()

        duration = end - start
        # we should take at least 0.5 seconds (the time for 1 prediction) but
        # not more than double that
        assert duration >= 0.5
        assert duration <= 1.0

        for result in results:
            assert result.done
            assert not result.done.canceled
            assert not result.exception
            assert result.stdout == "starting\n"
            assert result.output == "done in 0.5 seconds"

    finally:
        for subid in subids:
            worker.unsubscribe(subid)


@uses_worker("stream_redirector_race_condition")
def test_stream_redirector_race_condition(worker):
    """
    StreamRedirector and _ChildWorker are using the same pipe to send data. When
    there are multiple threads trying to write to the same pipe, it can cause
    data corruption by race condition. The data corruption will cause pipe
    receiver to raise an exception due to unpickling error.
    """
    for _ in range(5):
        result = _process(worker, lambda: worker.predict({}))
        assert not result.done.error


@pytest.mark.timeout(HYPOTHESIS_TEST_TIMEOUT)
@pytest.mark.parametrize(
    "worker,payloads,expected_metrics", METRICS_FIXTURES, indirect=["worker"]
)
@settings(suppress_health_check=[HealthCheck.function_scoped_fixture])
@given(data=st.data())
def test_metrics(worker, payloads, expected_metrics, data):
    """
    We should get the metrics we expect from predictors that emit metrics.
    """
    payload = data.draw(st.fixed_dictionaries(payloads))
    tag = uuid.uuid4().hex

    result = _process(worker, lambda: worker.predict(payload, tag=tag), tag=tag)

    assert result.metrics == expected_metrics


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


@pytest.mark.parametrize(
    "worker,expected_stdout,expected_stderr",
    SETUP_LOGS_FIXTURES,
    indirect=["worker"],
)
def test_setup_logging(worker, expected_stdout, expected_stderr):
    """
    We should get the logs we expect from predictors that generate logs during
    setup.
    """
    result = _process(worker, worker.setup)
    assert not result.done.error

    assert result.stdout == expected_stdout
    assert result.stderr == expected_stderr


@pytest.mark.parametrize(
    "worker,expected_stdout,expected_stderr",
    PREDICT_LOGS_FIXTURES,
    indirect=["worker"],
)
def test_predict_logging(worker, expected_stdout, expected_stderr):
    """
    We should get the logs we expect from predictors that generate logs during
    predict.
    """
    result = _process(worker, lambda: worker.predict({}))

    assert result.stdout == expected_stdout
    assert result.stderr == expected_stderr


@uses_worker_configs(
    [
        WorkerConfig("sleep", setup=False),
        WorkerConfig("sleep_async", is_async=True, setup=False),
    ]
)
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


@uses_worker_configs(
    [
        WorkerConfig("sleep", setup=False),
        WorkerConfig("sleep_async", is_async=True, setup=False),
    ]
)
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


@uses_worker_configs(
    [WorkerConfig("sleep"), WorkerConfig("sleep_async", is_async=True)]
)
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


@uses_worker_configs(
    [WorkerConfig("sleep"), WorkerConfig("sleep_async", is_async=True)]
)
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


@frozen
class SetupState:
    fut: "Future[Done]"
    result: Result
    sid: int

    error: bool = False


@frozen
class PredictState:
    tag: Optional[str]
    payload: Dict[str, Any]
    fut: "Future[Done]"
    result: Result
    sid: int

    canceled: bool = False
    error: bool = False


class FakeChildWorker:
    exitcode = None
    alive = True
    pid: int = 0

    def start(self):
        pass

    def is_alive(self):
        return self.alive

    def send_cancel(self):
        pass

    def terminate(self):
        pass

    def join(self):
        pass


class WorkerStateMachine(RuleBasedStateMachine):
    """
    This is a Hypothesis-driven rule-based state machine test. It is intended
    to ensure that any sequence of calls to the public API of Worker leaves the
    instance in an expected state.

    In short: any call should either throw InvalidStateException or should do
    what the caller asked.

    See https://hypothesis.readthedocs.io/en/latest/stateful.html for more on
    stateful testing with Hypothesis.
    """

    predict_pending = Bundle("predict_pending")
    predict_complete = Bundle("predict_complete")
    setup_pending = Bundle("setup_pending")
    setup_complete = Bundle("setup_complete")

    def __init__(self):
        super().__init__()

        parent_conn, child_conn = multiprocessing.get_context("spawn").Pipe()

        self.child = FakeChildWorker()
        self.child_events = child_conn

        self.pending = threading.Semaphore(0)

        self.worker = Worker(child=self.child, events=parent_conn, max_concurrency=4)

    def simulate_events(self, events, event_seen: threading.Event, *, tag=None):
        for event in events:
            event_seen.clear()
            self.child_events.send(Envelope(event, tag=tag))
            event_seen.wait(timeout=0.5)

    @rule(target=setup_pending)
    def setup(self):
        try:
            fut = self.worker.setup()
        except InvalidStateException:
            return multiple()
        else:
            result = Result()
            sid = self.worker.subscribe(result.handle_event)
            return SetupState(fut=fut, result=result, sid=sid)

    @rule(
        state=setup_pending,
        text=st.text(),
        source=st.sampled_from(["stdout", "stderr"]),
    )
    def simulate_setup_logs(self, state: SetupState, text: str, source: str):
        events = [Log(source=source, message=text)]
        self.simulate_events(events, event_seen=state.result.event_seen)

    @rule(state=consumes(setup_pending), target=setup_complete)
    def simulate_setup_success(self, state: SetupState):
        try:
            self.simulate_events(events=[Done()], event_seen=state.result.event_seen)
            return state
        finally:
            self.worker.unsubscribe(state.sid)

    @rule(state=consumes(setup_pending), target=setup_complete)
    def simulate_setup_failure(self, state: SetupState):
        try:
            self.simulate_events(
                events=[Done(error=True, error_detail="Setup failed!")],
                event_seen=state.result.event_seen,
            )
            return evolve(state, error=True)
        finally:
            self.worker.unsubscribe(state.sid)

    @rule(state=consumes(setup_complete))
    def await_setup(self, state: SetupState):
        if state.error:
            with pytest.raises(FatalWorkerException):
                state.fut.result()
            assert state.result.done.error
            assert state.result.done.error_detail == "Setup failed!"
        else:
            ev = state.fut.result()
            assert isinstance(ev, Done)
            assert state.result.done == Done()

    @rule(
        target=predict_pending,
        name=ST_NAMES,
        tag=st.uuids(),
        steps=st.integers(min_value=0, max_value=5),
    )
    def predict(self, name: str, steps: int, tag: uuid.UUID) -> PredictState:
        payload = {"name": name, "steps": steps}
        try:
            fut = self.worker.predict(payload, tag=tag.hex)
        except InvalidStateException:
            return multiple()
        else:
            # ensure the PredictionInput event has been sent; this needs to
            # happen before any further rules fire so we don't simulate a
            # prediction Done event before it has even started - that really
            # confuses the Worker
            assert self.child_events.poll(timeout=0.5)
            e = self.child_events.recv()
            assert isinstance(e, Envelope)
            assert isinstance(e.event, PredictionInput)
            assert e.tag == tag.hex
            assert not self.child_events.poll(timeout=0.1)

            result = Result()
            sid = self.worker.subscribe(result.handle_event, tag=tag.hex)
            return PredictState(
                tag=tag.hex, payload=payload, fut=fut, result=result, sid=sid
            )

    @rule(
        state=predict_pending,
        text=st.text(),
        source=st.sampled_from(["stdout", "stderr"]),
    )
    def simulate_predict_logs(self, state: PredictState, text: str, source: str):
        events = [Log(source=source, message=text)]
        self.simulate_events(events, event_seen=state.result.event_seen, tag=state.tag)

    @rule(state=consumes(predict_pending), target=predict_complete)
    def simulate_predict_success(self, state: PredictState):
        events = []

        steps = state.payload["steps"]
        name = state.payload["name"]

        if steps == 1:
            events.append(PredictionOutputType(multi=False))
            events.append(PredictionOutput(payload=f"NAME={name}"))

        elif steps > 1:
            events.append(PredictionOutputType(multi=True))
            for i in range(steps):
                events.append(
                    PredictionOutput(payload=f"NAME={name},STEP={i+1}"),
                )

        events.append(Done(canceled=state.canceled))

        self.simulate_events(events, event_seen=state.result.event_seen, tag=state.tag)
        return state

    @rule(state=consumes(predict_pending), target=predict_complete)
    def simulate_predict_failure(self, state: PredictState):
        events = [
            Done(
                error=True,
                error_detail="Kaboom!",
                canceled=state.canceled,
            ),
        ]

        self.simulate_events(events, event_seen=state.result.event_seen, tag=state.tag)
        return evolve(state, error=True)

    @rule(state=consumes(predict_complete))
    def await_predict(self, state: PredictState):
        try:
            ev = state.fut.result()
            assert isinstance(ev, Done)
            assert state.result.done

            if state.canceled:
                assert state.result.done.canceled
                return

            if state.error:
                assert state.result.done.error
                assert state.result.done.error_detail == "Kaboom!"
                return

            steps = state.payload["steps"]
            name = state.payload["name"]

            if steps == 0:
                assert not state.result.output
            elif steps == 1:
                assert state.result.output == f"NAME={name}"
            else:
                assert state.result.output == [
                    f"NAME={name},STEP={i+1}" for i in range(steps)
                ]

            assert state.result.done == Done()
        finally:
            self.worker.unsubscribe(state.sid)

    # For now, we only try canceling when we know a prediction is running.
    @rule(
        target=predict_pending,
        state=consumes(predict_pending),
    )
    def cancel(self, state: PredictState):
        self.worker.cancel(tag=state.tag)

        if not state.canceled:
            # if this prediction has not previously been canceled, Worker will
            # send a Cancel event to the child. We need to consume this event to
            # ensure we stay synced up on the child connection
            assert self.child_events.poll(timeout=0.5)
            e = self.child_events.recv()
            assert isinstance(e, Envelope)
            assert isinstance(e.event, Cancel)
            assert e.tag == state.tag

        return evolve(state, canceled=True)

    def teardown(self):
        self.child.alive = False
        self.worker.shutdown()


# Set a longer timeout for the state machine test. It can take a little while,
# particularly in CI, and particularly if it finds a failure, as shrinking
# might not happen all that quickly.
TestWorkerState = pytest.mark.timeout(600)(WorkerStateMachine.TestCase)
