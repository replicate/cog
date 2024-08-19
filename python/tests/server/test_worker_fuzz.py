import time
from typing import TYPE_CHECKING, Any, Dict, Union

import pytest
from attrs import define, evolve, frozen
from hypothesis import (
    Phase,
    given,
    settings,
)
from hypothesis import (
    strategies as st,
)

from cog.server.eventtypes import Done
from cog.server.exceptions import InvalidStateException
from cog.server.worker import Worker

from .test_worker import (
    ST_NAMES,
    Result,
    _fixture_path,
    _handle_event,
)

if TYPE_CHECKING:
    from concurrent.futures import Future


class Actions:
    @define
    class Setup:
        pass

    @define
    class Predict:
        payload: Dict[str, Any]

    @define
    class AwaitSetup:
        pass

    @define
    class AwaitPredict:
        pass

    @define
    class Cancel:
        pass

    @define
    class Sleep:
        duration: float

    ANY = Union[Setup, Predict, AwaitSetup, AwaitPredict, Sleep, Cancel]


st.register_type_strategy(
    Actions.Predict,
    st.builds(
        Actions.Predict,
        payload=st.fixed_dictionaries(
            {
                "name": ST_NAMES,
                "steps": st.integers(min_value=0, max_value=5),
            }
        ),
    ),
)

st.register_type_strategy(
    Actions.Sleep,
    st.builds(
        Actions.Sleep,
        duration=st.floats(min_value=0, max_value=0.1, allow_nan=False),
    ),
)


@frozen
class SetupState:
    fut: "Future[Done]"


@frozen
class PredictState:
    payload: Dict[str, Any]
    fut: "Future[Done]"
    canceled: bool = False


@pytest.mark.timeout(300)
class TestWorkerFuzz:
    def setup_method(self):
        self.events = []
        self.worker = Worker(_fixture_path("steps"), tee_output=False)
        self.worker.subscribe(self.events.append)

        self.pending_setup = None
        self.pending_predicts = []

    def teardown_method(self):
        self.worker.shutdown()

    @settings(
        phases=(Phase.explicit, Phase.reuse, Phase.generate, Phase.target),
        print_blob=True,
        deadline=None,
    )
    @given(actions=st.lists(st.from_type(Actions.ANY)))
    def test_fuzz(self, actions):
        for action in actions:
            if isinstance(action, Actions.Setup):
                self._handle_setup()
            elif isinstance(action, Actions.Predict):
                self._handle_predict(action.payload)
            elif isinstance(action, Actions.AwaitSetup):
                self._handle_await_setup()
            elif isinstance(action, Actions.AwaitPredict):
                self._handle_await_predict()
            elif isinstance(action, Actions.Cancel):
                self._handle_cancel()
            elif isinstance(action, Actions.Sleep):
                time.sleep(action.duration)
            else:
                raise ValueError(f"Unknown action: {action}")

    def _handle_setup(self):
        try:
            fut = self.worker.setup()
        except InvalidStateException:
            pass
        else:
            self.pending_setup = SetupState(fut=fut)

    def _handle_predict(self, payload):
        try:
            fut = self.worker.predict(payload)
        except InvalidStateException:
            pass
        else:
            self.pending_predicts.append(PredictState(payload=payload, fut=fut))

    def _handle_await_setup(self):
        if not self.pending_setup:
            return
        ev = self.pending_setup.fut.result()
        assert isinstance(ev, Done)
        self._validate_setup()
        self.pending_setup = None

    def _handle_await_predict(self):
        if not self.pending_predicts:
            return

        # We might have run setup but never checked it.
        self._handle_await_setup()

        state = self.pending_predicts.pop(0)
        ev = state.fut.result()
        assert isinstance(ev, Done)
        self._validate_predict(state)

    def _handle_cancel(self):
        self.worker.cancel()
        if not self.pending_predicts:
            return
        self.pending_predicts[-1] = evolve(self.pending_predicts[-1], canceled=True)

    def _validate_setup(self):
        result = self._consume_result()
        assert result.stdout == "did setup\n"
        assert result.stderr == ""
        assert result.done == Done()

    def _validate_predict(self, state: PredictState):
        result = self._consume_result()

        if state.canceled:
            # Requesting cancelation does not guarantee that the prediction is
            # canceled. It may complete before the cancelation is processed.
            assert result.done == Done() or result.done == Done(canceled=True)

            # If it was canceled, we can't make any other assertions.
            return

        expected_stdout = ["START\n"]
        for i in range(state.payload["steps"]):
            expected_stdout.append(f"STEP {i+1}\n")
        expected_stdout.append("END\n")

        assert result.stdout == "".join(expected_stdout)
        assert result.stderr == ""
        assert result.output == f"NAME={state.payload['name']}"
        assert result.done == Done()

    def _consume_result(self):
        r = Result()
        while self.events:
            event = self.events.pop(0)
            _handle_event(r, event)
            if isinstance(event, Done):
                break
        return r
