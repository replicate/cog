import signal
import subprocess
import sys

import requests

from cog import BasePredictor


class Predictor(BasePredictor):
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
