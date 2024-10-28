import os.path
import signal
import subprocess
import sys
import time

from cog import BasePredictor


class Predictor(BasePredictor):
    """
    This predictor checks the case where a process is spawned during setup and then each
    prediction depends on being able to communicate with that process. In the event that
    stream redirection is not working correctly, the forked process will not be able to
    write to stdout/stderr and will likely exit. Any state other than "running" is
    considered an error condition and raises SystemExit to interrupt any more prediction
    serving.

    This variant runs a forked python process via a shell wrapper to which a "message" is
    sent via file for each call to `predict`.
    """

    def setup(self) -> None:
        print("---> starting background process")

        self.bg = subprocess.Popen(["bash", "run-forker.sh"])

        print(f"---> started background process pid={self.bg.pid}")

    def predict(self, s: str) -> str:
        status = self.bg.poll()

        print(f"---> background job status={status}")

        if status is not None:
            raise SystemExit

        print(f"---> sending message to background job pid={self.bg.pid}")

        with open(".inbox", "w") as inbox:
            inbox.write(s)

        print(f"---> sent message to background job pid={self.bg.pid}")

        now = time.time()

        print(f"---> waiting for outbox message from background job pid={self.bg.pid}")

        while not os.path.exists(".outbox"):
            if time.time() - now > 5:
                raise TimeoutError

            time.sleep(0.01)

        try:
            with open(".outbox", "r") as outbox:
                print(f"---> relaying message from background job pid={self.bg.pid}")

                return outbox.read()

        finally:
            os.unlink(".outbox")
