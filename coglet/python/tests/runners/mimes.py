import os.path
import urllib.parse
import urllib.request
from typing import List

from cog import BasePredictor, Path


class Predictor(BasePredictor):
    test_inputs = {
        'us': [
            'https://raw.githubusercontent.com/gabriel-vasile/mimetype/refs/heads/master/testdata/gif.gif'
        ]
    }

    def predict(self, us: List[str]) -> List[Path]:
        r = []
        for i, u in enumerate(us):
            ext = os.path.splitext(urllib.parse.urlparse(u).path)[1]
            filename = f'out-{i}{ext}'
            urllib.request.urlretrieve(u, filename)
            r.append(Path(filename))
        return r
