import tempfile
import io
import os
from pathlib import Path

from PIL import Image
import cog
from .client import make_client



def test_type_signature():
    class Predictor(cog.Predictor):
        def setup(self):
            self.foo = "foo"

        @cog.input("text", type=str, help="Some text")
        @cog.input("num1", type=int, help="First number")
        @cog.input("num2", type=int, default=10, help="Second number")
        @cog.input("path", type=Path, help="A file path")
        def predict(self, text, num1, num2, path):
            with open(path) as f:
                path_contents = f.read()
            return self.foo + " " + text + " " + str(num1 * num2) + " " + path_contents

    client = make_client(Predictor())
    resp = client.get("/type-signature")
    assert resp.status_code == 200
    assert resp.json == {
        "inputs": [
            {
                "name": "text",
                "type": "str",
                "help": "Some text",
            },
            {
                "name": "num1",
                "type": "int",
                "help": "First number",
            },
            {
                "name": "num2",
                "type": "int",
                "help": "Second number",
                "default": "10",
            },
            {
                "name": "path",
                "type": "Path",
                "help": "A file path",
            },
        ]
    }


def test_yielding_strings_from_generator_predictors():
    class Predictor(cog.Predictor):
        def setup(self):
            pass

        def predict(self):
            predictions = ["foo", "bar", "baz"]
            for prediction in predictions:
                yield prediction

    client = make_client(Predictor())
    resp = client.post("/predict")
    assert resp.status_code == 200
    assert resp.content_type == "text/plain; charset=utf-8"
    assert resp.data == b"baz"


def test_yielding_json_from_generator_predictors():
    class Predictor(cog.Predictor):
        def setup(self):
            pass

        def predict(self):
            predictions = [
                {"meaning_of_life": 40},
                {"meaning_of_life": 41},
                {"meaning_of_life": 42},
            ]
            for prediction in predictions:
                yield prediction

    client = make_client(Predictor())
    resp = client.post("/predict")
    assert resp.status_code == 200
    assert resp.content_type == "application/json"
    assert resp.data == b'{"meaning_of_life": 42}'


def test_yielding_files_from_generator_predictors():
    class Predictor(cog.Predictor):
        def setup(self):
            pass

        def predict(self):
            colors = ["red", "blue", "yellow"]
            for i, color in enumerate(colors):
                temp_dir = tempfile.mkdtemp()
                temp_path = os.path.join(temp_dir, f"prediction-{i}.bmp")
                img = Image.new("RGB", (255, 255), color)
                img.save(temp_path)
                yield Path(temp_path)

    client = make_client(Predictor())
    resp = client.post("/predict")

    assert resp.status_code == 200
    # need both image/bmp and image/x-ms-bmp until https://bugs.python.org/issue44211 is fixed
    assert resp.content_type in ["image/bmp", "image/x-ms-bmp"]
    image = Image.open(io.BytesIO(resp.data))
    image_color = Image.Image.getcolors(image)[0][1]
    assert image_color == (255, 255, 0)  # yellow
