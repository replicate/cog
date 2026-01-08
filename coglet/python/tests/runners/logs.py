import sys
import time

from cog import BasePredictor


class Predictor(BasePredictor):
    test_inputs = {'s': 'foo'}

    def setup(self) -> None:
        print('STDOUT: starting setup')
        time.sleep(0.1)
        print('STDERR: starting setup', file=sys.stderr)
        time.sleep(0.1)
        print('STDOUT: completed setup')
        time.sleep(0.1)
        print('STDERR: completed setup', file=sys.stderr)

    def predict(self, s: str) -> str:
        print('STDOUT: starting prediction')
        time.sleep(0.1)
        print('STDERR: starting prediction', file=sys.stderr)
        time.sleep(0.1)
        print('[NOT_A_PID] STDOUT not a prediction ID')
        time.sleep(0.1)
        print('[NOT_A_PID] STDERR not a prediction ID', file=sys.stderr)
        time.sleep(0.1)
        print('STDOUT: completed prediction')
        time.sleep(0.1)
        print('STDERR: completed prediction', file=sys.stderr)
        return f'*{s}*'
