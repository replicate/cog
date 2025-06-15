import multiprocessing
import os
import sys
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
        WorkerConfig("record_metric_async", min_python=(3, 11), is_async=True),
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
        WorkerConfig("hello_world_async", min_python=(3, 11), is_async=True),
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
        WorkerConfig("logging_async", setup=False, min_python=(3, 11), is_async=True),
        "writing to stdout at import time\nsetting up predictor\n",
        "writing to stderr at import time\n",
    ),
]

PREDICT_LOGS_FIXTURES = [
    (
        WorkerConfig("logging"),
        "writing from C\nwriting with print\n",
        "WARNING:root:writing log message\nwriting to stderr\n",
    ),
    (
        WorkerConfig("logging_async", min_python=(3, 11), is_async=True),
        "writing with print\n",
        "WARNING:root:writing log message\nwriting to stderr\n",
    ),
]

SLEEP_FIXTURES = [
    WorkerConfig("sleep"),
    WorkerConfig("sleep_async", min_python=(3, 11), is_async=True),
    WorkerConfig(
        "sleep_async",
        min_python=(3, 11),
        is_async=True,
        max_concurrency=10,
    ),
]

SLEEP_NO_SETUP_FIXTURES = [
    WorkerConfig("sleep", setup=False),
    WorkerConfig("sleep_async", min_python=(3, 11), setup=False, is_async=True),
    WorkerConfig(
        "sleep_async",
        min_python=(3, 11),
        setup=False,
        is_async=True,
        max_concurrency=10,
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
                assert self.output is None, (
                    "Should not get multiple outputs for output type single"
                )
                self.output = event.payload
        elif isinstance(event, PredictionOutputType):
            assert self.output_type is None, (
                "Should not get multiple output type events"
            )
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


@uses_worker_configs(
    [
        WorkerConfig("simple"),
        WorkerConfig("simple_async", min_python=(3, 11), is_async=True),
    ]
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


@uses_worker("sleep_async", max_concurrency=5, min_python=(3, 11), is_async=True)
def test_can_run_predictions_concurrently_on_async_predictor(worker):
    subids = []

    try:
        start = time.perf_counter()
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

        end = time.perf_counter()

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


@pytest.mark.skipif(
    sys.version_info >= (3, 11), reason="Testing error message on python versions <3.11"
)
@uses_worker("simple_async", setup=False, is_async=True)
def test_async_predictor_on_python_3_10_or_older_raises_error(worker):
    fut = worker.setup()
    result = Result()
    worker.subscribe(result.handle_event)

    with pytest.raises(FatalWorkerException):
        fut.result()
    assert result.done
    assert result.done.error
    assert (
        result.done.error_detail
        == "Cog requires Python >=3.11 for `async def predict()` support"
    )


@uses_worker(
    "setup_async", max_concurrency=1, min_python=(3, 11), is_async=True, setup=False
)
def test_setup_async(worker: Worker):
    fut = worker.setup()
    setup_result = Result()
    setup_sid = worker.subscribe(setup_result.handle_event)

    # with pytest.raises(FatalWorkerException):
    fut.result()
    worker.unsubscribe(setup_sid)

    assert setup_result.stdout_lines == [
        "setup starting...\n",
        "download complete!\n",
        "setup complete!\n",
    ]

    predict_result = Result()
    predict_sid = worker.subscribe(predict_result.handle_event, tag="p1")
    worker.predict({}, tag="p1").result()

    assert predict_result.done
    assert predict_result.output == "output"
    assert predict_result.stdout_lines == ["running prediction\n"]

    worker.unsubscribe(predict_sid)


@uses_worker(
    "setup_async_with_sync_predict",
    max_concurrency=1,
    min_python=(3, 11),
    is_async=False,
    setup=False,
)
def test_setup_async_with_sync_predict_raises_error(worker: Worker):
    fut = worker.setup()
    result = Result()
    worker.subscribe(result.handle_event)

    with pytest.raises(FatalWorkerException):
        fut.result()
    assert result.done
    assert result.done.error
    assert (
        result.done.error_detail
        == "Invalid predictor: to use an async setup method you must use an async predict method"
    )


@uses_worker("simple", max_concurrency=5, setup=False)
def test_concurrency_with_sync_predictor_raises_error(worker):
    fut = worker.setup()
    result = Result()
    worker.subscribe(result.handle_event)

    with pytest.raises(FatalWorkerException):
        fut.result()
    assert result.done
    assert result.done.error
    assert (
        result.done.error_detail
        == "max_concurrency > 1 requires an async predict function, e.g. `async def predict()`"
    )


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
def test_setup_logging(worker: Worker, expected_stdout, expected_stderr):
    """
    We should get the logs we expect from predictors that generate logs during
    setup.
    """
    result = _process(worker, worker.setup)
    assert not result.done.error

    assert result.stdout == expected_stdout
    assert result.stderr == expected_stderr


@uses_worker_configs(
    [
        WorkerConfig("import_err", setup=False),
        WorkerConfig("import_err", setup=False, min_python=(3, 11), is_async=True),
    ]
)
def test_predictor_load_error_logging(worker: Worker):
    """
    This test ensures that we capture standard output that occurrs when the predictor
    errors when it is loaded. Before setup or predict are even run.
    """
    result = _process(worker, worker.setup, swallow_exceptions=True)

    assert result.done.error
    assert result.done.error_detail == "No module named 'missing_module'"

    assert result.stdout == "writing to stdout at import time\n"
    stderr_lines = result.stderr.splitlines(keepends=True)
    assert stderr_lines[0] == "writing to stderr at import time\n"

    assert "python/tests/server/fixtures/import_err.py" in stderr_lines[-3]
    assert "line 6" in stderr_lines[-3]
    assert "import missing_module" in stderr_lines[-2]
    assert stderr_lines[-1] == "ModuleNotFoundError: No module named 'missing_module'\n"


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


@uses_worker_configs(SLEEP_NO_SETUP_FIXTURES)
def test_cancel_is_safe(worker: Worker):
    """
    Calls to cancel at any time should not result in unexpected things
    happening or the cancelation of unexpected predictions.
    """

    tag = None
    if worker.uses_concurrency:
        tag = "p1"

    for _ in range(50):
        worker.cancel(tag)

    result = _process(worker, worker.setup)
    assert not result.done.error

    for _ in range(50):
        worker.cancel(tag)

    result1 = _process(
        worker,
        lambda: worker.predict({"sleep": 0.5}, tag),
        swallow_exceptions=True,
        tag=tag,
    )

    for _ in range(50):
        worker.cancel(tag)

    result2 = _process(
        worker,
        lambda: worker.predict({"sleep": 0.1}, tag),
        swallow_exceptions=True,
        tag=tag,
    )

    assert not result1.exception
    assert not result1.done.canceled
    assert not result2.exception
    assert not result2.done.canceled
    assert result2.output == "done in 0.1 seconds"


@uses_worker_configs(SLEEP_NO_SETUP_FIXTURES)
def test_cancel_idempotency(worker: Worker):
    """
    Multiple calls to cancel within the same prediction, while not necessary or
    recommended, should still only result in a single cancelled prediction, and
    should not affect subsequent predictions.
    """

    tag = None
    if worker.uses_concurrency:
        tag = "p1"

    result = _process(worker, worker.setup)
    assert not result.done.error

    fut = worker.predict({"sleep": 0.5}, tag)
    # We call cancel a WHOLE BUNCH to make sure that we don't propagate any
    # of those cancellations to subsequent predictions, regardless of the
    # internal implementation of exceptions raised inside signal handlers.
    for _ in range(5):
        time.sleep(0.05)
        for _ in range(100):
            worker.cancel(tag)
    result1 = fut.result()
    assert result1.canceled

    tag = None
    if worker.uses_concurrency:
        tag = "p2"
    result2 = _process(worker, lambda: worker.predict({"sleep": 0.1}, tag))

    assert not result2.done.canceled
    assert result2.output == "done in 0.1 seconds"


@uses_worker_configs(
    [
        WorkerConfig("sleep"),
        WorkerConfig("sleep_async", min_python=(3, 11), is_async=True),
        WorkerConfig(
            "sleep_async", min_python=(3, 11), is_async=True, max_concurrency=5
        ),
    ]
)
def test_cancel_multiple_predictions(worker: Worker):
    """
    Multiple predictions cancelled in a row shouldn't be a problem. This test
    is mainly ensuring that the _allow_cancel latch in Worker is correctly
    reset every time a prediction starts.
    """
    dones: list[Done] = []
    for i in range(5):
        tag = None
        if worker._max_concurrency > 1:
            tag = f"p{i}"
        fut = worker.predict({"sleep": 0.2}, tag)
        time.sleep(0.1)
        worker.cancel(tag)
        dones.append(fut.result())

    assert dones == [Done(canceled=True)] * 5

    assert not worker.predict({"sleep": 0}, "p6").result().canceled


@uses_worker_configs(
    [
        WorkerConfig(
            "sleep_async", min_python=(3, 11), is_async=True, max_concurrency=5
        ),
    ]
)
def test_cancel_some_predictions_async_with_concurrency(worker: Worker):
    """
    Multiple predictions cancelled in a row shouldn't be a problem. This test
    is mainly ensuring that the _allow_cancel latch in Worker is correctly
    reset every time a prediction starts.
    """
    fut1 = worker.predict({"sleep": 0.2}, "p1")
    fut2 = worker.predict({"sleep": 0.2}, "p2")
    fut3 = worker.predict({"sleep": 0.2}, "p3")

    time.sleep(0.1)

    worker.cancel("p2")

    assert not fut1.result().canceled
    assert fut2.result().canceled
    assert not fut3.result().canceled


@uses_worker_configs(SLEEP_FIXTURES)
def test_graceful_shutdown(worker: Worker):
    """
    On shutdown, the worker should finish running the current prediction, and
    then exit.
    """

    tag = None
    if worker.uses_concurrency:
        tag = "p1"

    saw_first_event = threading.Event()

    # When we see the first event, we'll start the shutdown process.
    worker.subscribe(lambda event: saw_first_event.set(), tag=tag)

    fut = worker.predict({"sleep": 1}, tag)

    saw_first_event.wait(timeout=1)
    worker.shutdown(timeout=2)

    assert fut.result() == Done()


@uses_worker("async_setup_uses_same_loop_as_predict", min_python=(3, 11), is_async=True)
def test_async_setup_uses_same_loop_as_predict(worker: Worker):
    result = _process(worker, lambda: worker.predict({}), tag=None)
    assert result, "Expected worker to return True to assert same event loop"


@uses_worker("with_context")
def test_context(worker: Worker):
    result = _process(
        worker,
        lambda: worker.predict({"name": "context"}, context={"prefix": "hello"}),
        tag=None,
    )
    assert result.done
    assert not result.done.error
    assert result.output == "hello context!"


@uses_worker("with_context_async", min_python=(3, 11), is_async=True)
def test_context_async(worker: Worker):
    result = _process(
        worker,
        lambda: worker.predict(
            {"name": "context"}, tag="t1", context={"prefix": "hello"}
        ),
        tag=None,
    )

    print("\n".join(result.stderr_lines))
    assert result.done
    assert not result.done.error, result.done.error_detail
    assert result.output == "hello context!"


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

    def send_cancel_signal(self):
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
                    PredictionOutput(payload=f"NAME={name},STEP={i + 1}"),
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
                    f"NAME={name},STEP={i + 1}" for i in range(steps)
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
