import os.path
import urllib.parse
import urllib.request

from cog import BasePredictor, Path


class Predictor(BasePredictor):
    test_inputs = {
        'u': 'https://raw.githubusercontent.com/gabriel-vasile/mimetype/refs/heads/master/testdata/gif.gif'
    }

    def predict(self, u: str) -> Path:
        ext = os.path.splitext(urllib.parse.urlparse(u).path)[1]
        filename = f'out{ext}'
        urllib.request.urlretrieve(u, filename)
        return Path(filename)
