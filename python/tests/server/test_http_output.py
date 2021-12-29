import base64
import tempfile
import os
from pathlib import Path

import numpy as np
from PIL import Image

import cog
from .test_http import make_client


def test_return_wrong_type():
    class Predictor(cog.Predictor):
        def predict(self) -> int:
            return "foo"

    client = make_client(Predictor())
    resp = client.post("/predict")
    assert resp.status_code == 422


def test_path_output_file():
    class Predictor(cog.Predictor):
        def setup(self):
            pass

        def predict(self) -> cog.Path:
            temp_dir = tempfile.mkdtemp()
            temp_path = os.path.join(temp_dir, "my_file.bmp")
            img = Image.new("RGB", (255, 255), "red")
            img.save(temp_path)
            return cog.Path(temp_path)

    client = make_client(Predictor())
    res = client.post("/predict")
    assert res.status_code == 200
    header, b64data = res.json()["output"].split(",", 1)
    assert header == "data:image/bmp;base64"
    assert len(base64.b64decode(b64data)) == 195894


def test_json_output_numpy():
    class Predictor(cog.Predictor):
        def predict(self) -> np.float:
            return np.float32(1.0)

    client = make_client(Predictor())
    resp = client.post("/predict")
    assert resp.status_code == 200
    assert resp.json() == {"output": 1.0, "status": "success"}
