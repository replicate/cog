import sys

argv_on_import = sys.argv[:]


class Predictor:
    def predict(self):
        return argv_on_import
