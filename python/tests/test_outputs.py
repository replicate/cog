import tempfile
import os
from pathlib import Path

import numpy as np
from PIL import Image

import cog
from .client import make_client


def test_path_output_str():
    class Predictor(cog.Predictor):
        def setup(self):
            self.foo = "foo"

        @cog.input("text", type=str)
        def predict(self, text):
            temp_dir = tempfile.mkdtemp()
            temp_path = os.path.join(temp_dir, "my_file.txt")
            with open(temp_path, "w") as f:
                f.write(self.foo + text)
            return Path(temp_path)

    client = make_client(Predictor())
    resp = client.post("/predict", data={"text": "baz"})
    assert resp.status_code == 200
    assert resp.content_type == "text/plain; charset=utf-8"
    assert resp.data == b"foobaz"


def test_path_output_image():
    class Predictor(cog.Predictor):
        def setup(self):
            pass

        def predict(self):
            temp_dir = tempfile.mkdtemp()
            temp_path = os.path.join(temp_dir, "my_file.bmp")
            img = Image.new("RGB", (255, 255), "red")
            img.save(temp_path)
            return Path(temp_path)

    client = make_client(Predictor())
    resp = client.post("/predict")
    assert resp.status_code == 200
    # need both image/bmp and image/x-ms-bmp until https://bugs.python.org/issue44211 is fixed
    assert resp.content_type in ["image/bmp", "image/x-ms-bmp"]
    assert resp.content_length == 195894


def test_json_output_numpy():
    class Predictor(cog.Predictor):
        def setup(self):
            pass

        def predict(self):
            return {"foo": np.float32(1.0)}

    client = make_client(Predictor())
    resp = client.post("/predict")
    assert resp.status_code == 200
    assert resp.content_type == "application/json"
    assert resp.data == b'{"foo": 1.0}'
