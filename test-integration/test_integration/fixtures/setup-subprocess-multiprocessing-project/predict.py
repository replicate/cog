import atexit
import multiprocessing
import pathlib
import signal
import subprocess
import sys
import time

from cog.types import Path
from cog import BasePredictor

from bg import ponger

def cleanup():
    for tmp in pathlib.Path("./").glob("*.tmp"):
        tmp.unlink(missing_ok=True)


atexit.register(cleanup)


class Predictor(BasePredictor):
    """
    This predictor checks the case where a process is spawned during setup via
    multiprocessing and then each prediction causes that process to write to stdout.
    """

    def setup(self) -> None:
        print("---> starting background process")

        cleanup()

        self.parent_conn, self.child_conn = multiprocessing.Pipe()
        self.lock = multiprocessing.Lock()
        self.bg = multiprocessing.Process(
            target=ponger, args=(self.child_conn, self.lock)
        )
        self.bg.start()

        print(f"---> started background process pid={self.bg.pid}")

    def predict(self, s: str) -> Path:
        if self.bg.is_alive():
            print(f"---> sending ping to background job pid={self.bg.pid}")

            self.child_conn.send("ping")

            print(f"---> sent ping to background job pid={self.bg.pid}")

            pong = self.parent_conn.recv()

            print(f"---> received {pong} from background job pid={self.bg.pid}")
        else:
            print(f"---> background job died status={status}")

            raise SystemExit

        out = Path(f"cog-test-integration-out.{time.time_ns()}.tmp")
        out.write_text("hello " + s)

        print(f"---> wrote output file {out}")

        return out
