import sys

sys.stdout.write("writing to stdout at import time\n")
sys.stderr.write("writing to stderr at import time\n")

import missing_module


class Predictor:
    def setup(self):
        pass

    def predict(self):
        print("did predict")
