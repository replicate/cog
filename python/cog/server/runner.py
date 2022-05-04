import multiprocessing
import types
from enum import Enum
from multiprocessing.connection import Connection
from typing import Any, Dict, List, Optional

from pydantic import BaseModel

from ..predictor import load_predictor
from .log_capture import capture_log


class PredictionRunner:
    PREDICTION_DONE = 1

    class OutputType(Enum):
        NOT_STARTED = 0
        SINGLE = 1
        GENERATOR = 2

    def __init__(self) -> None:
        self.logs_pipe_reader, self.logs_pipe_writer = multiprocessing.Pipe(
            duplex=False
        )
        (
            self.prediction_input_pipe_reader,
            self.prediction_input_pipe_writer,
        ) = multiprocessing.Pipe(duplex=False)
        self.predictor_pipe_reader, self.predictor_pipe_writer = multiprocessing.Pipe(
            duplex=False
        )
        self.error_pipe_reader, self.error_pipe_writer = multiprocessing.Pipe(
            duplex=False
        )
        self.done_pipe_reader, self.done_pipe_writer = multiprocessing.Pipe(
            duplex=False
        )

    def setup(self) -> None:
        """
        Sets up the predictor in a subprocess. To start a prediction after
        setup call `run()`.
        """
        # `multiprocessing.get_context("spawn")` returns the same API as
        # `multiprocessing`, but will use the spawn method when creating any
        # subprocess. Using the spawn method for the predictor subprocess is
        # useful for compatibility with CUDA, which cannot run in a process
        # that gets forked. If we can guarantee that all initialization happens
        # within the subprocess, we could probably get away with using fork
        # here instead.
        self.predictor_process = multiprocessing.get_context("spawn").Process(
            target=self._start_predictor_process
        )
        self.predictor_process.start()

    def _start_predictor_process(self) -> None:
        self.predictor = load_predictor()
        self.predictor.setup()

        while True:
            try:
                prediction_input = self.prediction_input_pipe_reader.recv()
                self._run_prediction(prediction_input)
            except EOFError:
                continue

    def run(self, **prediction_input: Dict[str, Any]) -> None:
        """
        Starts running a prediction in the predictor subprocess, using the
        inputs provided in `prediction_input`.

        The subprocess will send prediction output and logs to pipes as soon as
        they're available. You can check if the pipes have any data using
        `has_output_waiting()` and `has_logs_waiting()`. You can read data from
        the pipes using `read_output()` and `read_logs()`.

        Use `is_processing()` to check whether more data is expected in the
        pipe for prediction output.
        """
        # We're starting processing!
        self._is_processing = True

        # We don't know whether or not we've got a generator (progressive
        # output) until we start getting output from the model
        self._is_output_generator = self.OutputType.NOT_STARTED

        # We haven't encountered an error yet
        self._error = None

        # Send prediction input through the pipe to the predictor subprocess
        self.prediction_input_pipe_writer.send(prediction_input)

    def is_processing(self) -> bool:
        """
        Returns True if the subprocess running the prediction is still
        processing.
        """
        if self.done_pipe_reader.poll():
            try:
                if self.done_pipe_reader.recv() == self.PREDICTION_DONE:
                    self._is_processing = False
            except EOFError:
                pass

        return self._is_processing

    def has_output_waiting(self) -> bool:
        return self.predictor_pipe_reader.poll()

    def read_output(self) -> List[Any]:
        if self._is_output_generator is self.OutputType.NOT_STARTED:
            return []

        output = []
        while self.has_output_waiting():
            try:
                output.append(self.predictor_pipe_reader.recv())
            except EOFError:
                break
        return output

    def has_logs_waiting(self) -> bool:
        return self.logs_pipe_reader.poll()

    def read_logs(self) -> List[str]:
        logs = []
        while self.has_logs_waiting():
            try:
                logs.append(self.logs_pipe_reader.recv())
            except EOFError:
                break
        return logs

    def is_output_generator(self) -> Optional[bool]:
        """
        Returns `True` if the output is a generator, `False` if it's not, and
        `None` if we don't know yet.
        """
        if self._is_output_generator is self.OutputType.NOT_STARTED:
            if self.has_output_waiting():
                # if there's output waiting use the first one to set whether
                # we've got a generator, with a safety check
                self._is_output_generator = self.predictor_pipe_reader.recv()
                assert isinstance(self._is_output_generator, self.OutputType)

        if self._is_output_generator is self.OutputType.NOT_STARTED:
            return None
        elif self._is_output_generator is self.OutputType.SINGLE:
            return False
        elif self._is_output_generator is self.OutputType.GENERATOR:
            return True

    def _run_prediction(self, prediction_input: Dict[str, Any]) -> None:
        """
        Sends a boolean first, to indicate whether the output is a generator.
        After that it sends the output(s).

        If the predictor raises an exception it'll send it to the error pipe
        writer and then exit.

        When the prediction is finished it'll send a token to the done pipe.
        """
        # Empty all the pipes before we start sending more messages to them
        drain_pipe(self.logs_pipe_reader)
        drain_pipe(self.predictor_pipe_reader)
        drain_pipe(self.error_pipe_reader)
        drain_pipe(self.done_pipe_reader)

        with capture_log(self.logs_pipe_writer):
            try:
                output = self.predictor.predict(**prediction_input)

                if isinstance(output, types.GeneratorType):
                    self.predictor_pipe_writer.send(self.OutputType.GENERATOR)
                    while True:
                        try:
                            self.predictor_pipe_writer.send(
                                next(make_pickleable(output))
                            )
                        except StopIteration:
                            break
                else:
                    self.predictor_pipe_writer.send(self.OutputType.SINGLE)
                    self.predictor_pipe_writer.send(make_pickleable(output))
            except Exception as e:
                self.error_pipe_writer.send(e)

        self.done_pipe_writer.send(self.PREDICTION_DONE)

    def error(self) -> Optional[str]:
        """
        Returns the error encountered by the predictor, if one exists.
        """
        if self._error is None and self.error_pipe_reader.poll():
            try:
                self._error = self.error_pipe_reader.recv()
            except EOFError:
                # I don't know how this is reachable ¯\_(ツ)_/¯
                pass

        return self._error


def drain_pipe(pipe_reader: Connection) -> None:
    """
    Reads all available messages from a pipe and discards them. This serves to
    clear the pipe for future usage.
    """
    while pipe_reader.poll():
        try:
            pipe_reader.recv()
        except EOFError:
            break


def make_pickleable(obj: Any) -> Any:
    """
    Returns a version of `obj` which can be pickled and therefore sent through
    the pipe to the main process.

    If the predictor uses a custom output like:

        class Output(BaseModel):
            text: str

    then the output can't be sent through the pipe because:

    > Can't pickle <class 'predict.Output'>: it's not the same object as
    > 'predict.Output'

    The way we're getting around this here will only work for singly-nested
    outputs. If there's a complex object inside a complex object, it's likely
    to fall over.

    A better fix for this would be to work out why the pickling process is
    getting a different class when loading `Output`, so the pickling Just
    Works.
    """
    if isinstance(obj, BaseModel):
        return obj.dict(exclude_unset=True)
    else:
        return obj
