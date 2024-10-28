import signal
import subprocess
import sys

import requests

from cog import BasePredictor


class Predictor(BasePredictor):
    """
    This predictor checks the case where a process is spawned during setup and then each
    prediction depends on being able to communicate with that process. In the event that
    stream redirection is not working correctly, the forked process will not be able to
    write to stdout/stderr and will likely exit. Any state other than "running" is
    considered an error condition and raises SystemExit to interrupt any more prediction
    serving.

    This variant runs a forked python HTTP server via a shell wrapper to which a request
    is made during each call to `predict`.
    """

    def setup(self) -> None:
        print("---> starting background process")

        self.bg = subprocess.Popen(["bash", "run-pong.sh"])

        print(f"---> started background process pid={self.bg.pid}")

    def predict(self, s: str) -> str:
        status = self.bg.poll()

        print(f"---> background job status={status}")

        if status is None:
            print(f"---> sending request to background job pid={self.bg.pid}")

            print(requests.get("http://127.0.0.1:7777/ping"))

            print(f"---> sent request to background job pid={self.bg.pid}")
        else:
            raise SystemExit

        return "hello " + s
