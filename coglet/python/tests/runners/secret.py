import time

from cog import BasePredictor, Secret


class Predictor(BasePredictor):
    test_inputs = {'s': 'foobar'}

    def predict(self, s: Secret) -> Secret:
        time.sleep(0.1)
        print('reading secret')
        s = s.get_secret_value()
        time.sleep(0.5)
        print('writing secret')
        s = Secret(f'*{s}*')
        time.sleep(0.1)
        return s
