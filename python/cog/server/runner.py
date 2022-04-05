import multiprocessing
import os
import sys
import time
import types

from .log_capture import capture_log


class PredictionRunner:
    def __init__(self, predictor):
        """
        The parameter `predictor` must have been set up already.
        """
        self.predictor = predictor
        self.logs_pipe_reader, self.logs_pipe_writer = multiprocessing.Pipe(
            duplex=False
        )

    def run(self, **prediction_input):
        """
        Starts running a prediction using subprocesses and returns two pipes --
        one for prediction output, and one for log output.

        The subprocesses will send prediction output and logs to the pipes as
        soon as they're available. You can check if the pipes have any data
        using `poll()`, and read it using `recv()`.

        Use `is_processing()` to check whether more data is expected in the
        pipe for prediction output.
        """
        # We don't know whether or not we've got a generator (progressive
        # output) until we start getting output from the model
        self._is_output_generator = None

        # We haven't encountered an error yet
        self._error = None

        self.predictor_pipe_reader, predictor_pipe_writer = multiprocessing.Pipe(
            duplex=False
        )
        self.error_pipe_reader, error_pipe_writer = multiprocessing.Pipe(duplex=False)
        self.predictor_process = multiprocessing.Process(
            target=self._run_prediction,
            args=[prediction_input, predictor_pipe_writer, error_pipe_writer],
        )
        self.predictor_process.start()

    def is_processing(self):
        """
        Returns True if the subprocess running the prediction is still
        processing.
        """
        return self.predictor_process is not None and self.predictor_process.is_alive()

    def has_output_waiting(self):
        return self.predictor_pipe_reader.poll()

    def read_output(self):
        if self.is_output_generator() is None:
            return []

        output = []
        while self.has_output_waiting():
            try:
                output.append(self.predictor_pipe_reader.recv())
            except EOFError:
                break
        return output

    def has_logs_waiting(self):
        return self.logs_pipe_reader.poll()

    def read_logs(self):
        logs = []
        while self.has_logs_waiting():
            try:
                logs.append(self.logs_pipe_reader.recv())
            except EOFError:
                break
        return logs

    def is_output_generator(self):
        """
        Returns `True` if the output is a generator, `False` if it's not, and
        `None` if we don't know yet.
        """
        if self._is_output_generator is None:
            if self.has_output_waiting():
                # if there's output waiting use the first one to set whether
                # we've got a generator, with a safety check
                self._is_output_generator = self.predictor_pipe_reader.recv()
                assert isinstance(self._is_output_generator, bool)

        return self._is_output_generator

    def _run_prediction(
        self, prediction_input, predictor_pipe_writer, error_pipe_writer
    ):
        """
        Sends a boolean first, to indicate whether the output is a generator.
        After that it sends the output(s).

        If the predictor raises an exception it'll send it to the error pipe
        writer and then exit.
        """
        with capture_log(self.logs_pipe_writer):
            try:
                output = self.predictor.predict(**prediction_input)

                if isinstance(output, types.GeneratorType):
                    predictor_pipe_writer.send(True)
                    while True:
                        try:
                            predictor_pipe_writer.send(next(output))
                        except StopIteration:
                            break
                else:
                    predictor_pipe_writer.send(False)
                    predictor_pipe_writer.send(output)
            except Exception as e:
                error_pipe_writer.send(e)

    def error(self):
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
