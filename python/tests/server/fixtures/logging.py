import ctypes
import logging
import sys
import time

libc = ctypes.CDLL(None)

# test that we can still capture type signature even if we write
# a bunch of stuff at import time.
libc.puts(b"writing some stuff from C at import time")
libc.fflush(None)
sys.stdout.write("writing to stdout at import time\n")
sys.stderr.write("writing to stderr at import time\n")


class Predictor:
    def setup(self):
        print("setting up predictor")
        self.foo = "foo"

    def predict(self) -> str:
        time.sleep(0.1)
        logging.warn("writing log message")
        time.sleep(0.1)
        libc.puts(b"writing from C")
        libc.fflush(None)
        time.sleep(0.1)
        sys.stderr.write("writing to stderr\n")
        time.sleep(0.1)
        sys.stderr.flush()
        time.sleep(0.1)
        print("writing with print")
        time.sleep(0.1)
        return "output"
