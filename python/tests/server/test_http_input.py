import io
import os
from pathlib import Path
from pydantic import BaseModel

import pytest

import cog
from .test_http import make_client


def test_no_input():
    class Predictor(cog.Predictor):
        def predict(self) -> str:
            return "foobar"

    client = make_client(Predictor())
    resp = client.post("/predict")
    assert resp.status_code == 200
    assert resp.json() == {"status": "success", "output": "foobar"}


def test_good_str_input():
    class Input(BaseModel):
        text: str

    class Predictor(cog.Predictor):
        def predict(self, input: Input) -> str:
            return input.text

    client = make_client(Predictor())
    resp = client.post("/predict", json={"text": "baz"})
    assert resp.status_code == 200
    assert resp.json() == {"status": "success", "output": "foobaz"}


def test_good_int_input():
    class Predictor(cog.Predictor):
        @cog.input("num", type=int)
        def predict(self, num):
            return str(num ** 3)

    client = make_client(Predictor())
    resp = client.post("/predict", data={"num": 3})
    assert resp.status_code == 200
    assert resp.data == b"27"
    resp = client.post("/predict", data={"num": -3})
    assert resp.status_code == 200
    assert resp.data == b"-27"


def test_bad_int_input():
    class Predictor(cog.Predictor):
        @cog.input("num", type=int)
        def predict(self, num):
            return str(num ** 2)

    client = make_client(Predictor())
    resp = client.post("/predict", data={"num": "foo"})
    assert resp.status_code == 400


def test_default_int_input():
    class Predictor(cog.Predictor):
        @cog.input("num", type=int, default=5)
        def predict(self, num):
            return str(num ** 2)

    client = make_client(Predictor())
    resp = client.post("/predict", data={"num": 3})
    assert resp.status_code == 200
    assert resp.data == b"9"
    resp = client.post("/predict")
    assert resp.status_code == 200
    assert resp.data == b"25"


def test_good_float_input():
    class Predictor(cog.Predictor):
        @cog.input("num", type=float)
        def predict(self, num):
            return str(num ** 3)

    client = make_client(Predictor())
    resp = client.post("/predict", data={"num": 3})
    assert resp.status_code == 200
    assert resp.data == b"27.0"
    resp = client.post("/predict", data={"num": 3.5})
    assert resp.status_code == 200
    assert resp.data == b"42.875"
    resp = client.post("/predict", data={"num": -3.5})
    assert resp.status_code == 200
    assert resp.data == b"-42.875"


def test_bad_float_input():
    class Predictor(cog.Predictor):
        @cog.input("num", type=float)
        def predict(self, num):
            return str(num ** 2)

    client = make_client(Predictor())
    resp = client.post("/predict", data={"num": "foo"})
    assert resp.status_code == 400


def test_good_bool_input():
    class Predictor(cog.Predictor):
        @cog.input("flag", type=bool)
        def predict(self, flag):
            if flag:
                return "yes"
            else:
                return "no"

    client = make_client(Predictor())
    resp = client.post("/predict", data={"flag": True})
    assert resp.status_code == 200
    assert resp.data == b"yes"
    resp = client.post("/predict", data={"flag": False})
    assert resp.status_code == 200
    assert resp.data == b"no"


def test_good_path_input():
    class Predictor(cog.Predictor):
        @cog.input("path", type=Path)
        def predict(self, path):
            with open(path) as f:
                return f.read() + " " + os.path.basename(path)

    client = make_client(Predictor())
    path_data = (io.BytesIO(b"bar"), "foo.txt")
    resp = client.post(
        "/predict", data={"path": path_data}, content_type="multipart/form-data"
    )
    assert resp.status_code == 200
    assert resp.data == b"bar foo.txt"


def test_bad_path_input():
    class Predictor(cog.Predictor):
        @cog.input("path", type=Path)
        def predict(self, path):
            with open(path) as f:
                return f.read() + " " + os.path.basename(path)

    client = make_client(Predictor())
    resp = client.post("/predict", data={"path": "bar"})
    assert resp.status_code == 400


def test_default_path_input():
    class Predictor(cog.Predictor):
        @cog.input("path", type=Path, default=None)
        def predict(self, path):
            if path is None:
                return "noneee"
            with open(path) as f:
                return f.read() + " " + os.path.basename(path)

    client = make_client(Predictor())
    path_data = (io.BytesIO(b"bar"), "foo.txt")
    resp = client.post(
        "/predict", data={"path": path_data}, content_type="multipart/form-data"
    )
    assert resp.status_code == 200
    assert resp.data == b"bar foo.txt"
    resp = client.post("/predict", data={})
    assert resp.status_code == 200
    assert resp.data == b"noneee"


def test_extranous_input_keys():
    class Input(BaseModel):
        text: str

    class Predictor(cog.Predictor):
        def predict(self, input: Input):
            return input.text

    client = make_client(Predictor())
    resp = client.post("/predict", data={"text": "baz", "text2": "qux"})
    assert resp.status_code == 422


def test_min_max():
    class Predictor(cog.Predictor):
        @cog.input("num1", type=float, min=3, max=10.5)
        @cog.input("num2", type=float, min=-4)
        @cog.input("num3", type=int, max=-4)
        def predict(self, num1, num2, num3):
            return num1 + num2 + num3

    client = make_client(Predictor())
    resp = client.post("/predict", data={"num1": 3, "num2": -4, "num3": -4})
    assert resp.status_code == 200
    assert resp.data == b"-5.0"
    resp = client.post("/predict", data={"num1": 2, "num2": -4, "num3": -4})
    assert resp.status_code == 400
    resp = client.post("/predict", data={"num1": 3, "num2": -4.1, "num3": -4})
    assert resp.status_code == 400
    resp = client.post("/predict", data={"num1": 3, "num2": -4, "num3": -3})
    assert resp.status_code == 400


def test_good_options():
    class Predictor(cog.Predictor):
        @cog.input("text", type=str, options=["foo", "bar"])
        @cog.input("num", type=int, options=[1, 2, 3])
        def predict(self, text, num):
            return text + ("a" * num)

    client = make_client(Predictor())
    resp = client.post("/predict", data={"text": "foo", "num": 2})
    assert resp.status_code == 200
    assert resp.data == b"fooaa"


def test_bad_options():
    class Predictor(cog.Predictor):
        @cog.input("text", type=str, options=["foo", "bar"])
        @cog.input("num", type=int, options=[1, 2, 3])
        def predict(self, text, num):
            return text + ("a" * num)

    client = make_client(Predictor())
    resp = client.post("/predict", data={"text": "baz", "num": 2})
    assert resp.status_code == 400
    resp = client.post("/predict", data={"text": "bar", "num": 4})
    assert resp.status_code == 400


def test_bad_options_type():
    with pytest.raises(ValueError):

        class Predictor(cog.Predictor):
            @cog.input("text", type=str, options=[])
            def predict(self, text):
                return text

    with pytest.raises(ValueError):

        class Predictor(cog.Predictor):
            @cog.input("text", type=str, options=["foo"])
            def predict(self, text):
                return text

    with pytest.raises(ValueError):

        class Predictor(cog.Predictor):
            @cog.input("text", type=Path, options=["foo"])
            def predict(self, text):
                return text


def test_multiple_arguments():
    class Predictor(cog.Predictor):
        @cog.input("text", type=str)
        @cog.input("num1", type=int)
        @cog.input("num2", type=int, default=10)
        @cog.input("path", type=Path)
        def predict(self, text, num1, num2, path):
            with open(path) as f:
                path_contents = f.read()
            return text + " " + str(num1 * num2) + " " + path_contents

    client = make_client(Predictor())
    path_data = (io.BytesIO(b"bar"), "foo.txt")
    resp = client.post(
        "/predict",
        data={"text": "baz", "num1": 5, "path": path_data},
        content_type="multipart/form-data",
    )
    assert resp.status_code == 200
    assert resp.data == b"baz 50 bar"
