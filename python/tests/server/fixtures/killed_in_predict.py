import os
import signal


class Predictor:
    def setup(self):
        print("did setup")

    def predict(self):
        os.kill(os.getpid(), signal.SIGKILL)
