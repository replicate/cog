import signal
import subprocess
import sys

from cog import BasePredictor


class Predictor(BasePredictor):
    """
    This predictor checks the case where a process is spawned during setup and then each
    prediction causes that process to write to stdout. In the event that stream
    redirection is not working correctly, the forked process will not be able to write to
    stdout/stderr and will likely exit. Any state other than "running" is considered an
    error condition and raises SystemExit to interrupt any more prediction serving.

    This variant runs a simple subprocess to which SIGUSR1 is sent during each call to
    `predict`.
    """

    def setup(self) -> None:
        print("---> starting background process")

        self.bg = subprocess.Popen(["bash", "child.sh"])

        print(f"---> started background process pid={self.bg.pid}")

    def predict(self, s: str) -> str:
        status = self.bg.poll()

        if status is None:
            print(f"---> sending signal to background job pid={self.bg.pid}")

            self.bg.send_signal(signal.SIGUSR1)

            print(f"---> sent signal to background job pid={self.bg.pid}")
        else:
            print(f"---> background job died status={status}")

            raise SystemExit

        return "hello " + s
