import os.path
import signal
import subprocess
import sys
import time

from cog import BasePredictor


class Predictor(BasePredictor):
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
