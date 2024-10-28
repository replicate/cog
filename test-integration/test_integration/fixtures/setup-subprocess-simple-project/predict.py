import signal
import subprocess
import sys

from cog import BasePredictor


class Predictor(BasePredictor):
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
