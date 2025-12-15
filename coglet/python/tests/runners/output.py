import tempfile
import time

from cog import BaseModel, BasePredictor, Path


class Output(BaseModel):
    path: Path
    text: str


class Predictor(BasePredictor):
    test_inputs = {'p': '/etc/hosts'}

    def predict(self, p: Path) -> Output:
        time.sleep(0.1)
        with open(p, 'r') as f:
            print('reading input file')
            s = f.read()
        time.sleep(0.5)
        o = f'*{s}*'
        with tempfile.NamedTemporaryFile(mode='w', suffix='.txt', delete=False) as f:
            print('writing output file')
            f.write(o)
        time.sleep(0.1)
        return Output(path=Path(f.name), text=o)
