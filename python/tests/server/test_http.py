from flask.testing import FlaskClient
import io
import os
from pathlib import Path
import tempfile
from unittest import mock

from PIL import Image
import cog
from cog.server.http import HTTPServer


def make_client(version) -> FlaskClient:
    app = HTTPServer(version).make_app()
    app.config["TESTING"] = True
    with app.test_client() as client:
        return client


def test_setup_is_called():
    class Predictor(cog.Predictor):
        def setup(self):
            self.foo = "bar"

        def predict(self):
            return self.foo

    client = make_client(Predictor())
    resp = client.post("/predict")
    assert resp.status_code == 200
    assert resp.data == b"bar"


def test_type_signature():
    class Predictor(cog.Predictor):
        @cog.input("text", type=str, help="Some text")
        @cog.input("num1", type=int, help="First number")
        @cog.input("num2", type=int, default=10, help="Second number")
        @cog.input("path", type=Path, help="A file path")
        def predict(self, text, num1, num2, path):
            pass

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


@mock.patch("time.time", return_value=0.0)
def test_timing(time_mock):
    class Predictor(cog.Predictor):
        def setup(self):
            time_mock.return_value = 1.0

        def predict(self):
            time_mock.return_value = 3.0
            return ""

    client = make_client(Predictor())
    resp = client.post("/predict")
    assert resp.status_code == 200
    assert float(resp.headers["X-Setup-Time"]) == 1.0
    assert float(resp.headers["X-Run-Time"]) == 2.0
